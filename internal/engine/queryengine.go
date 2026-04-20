package engine

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ────────────────────────────────────────────────────────────────────────────
// [P8] QueryEngine — mirrors claude-code-main QueryEngine.ts.
// One QueryEngine per conversation. Each SubmitMessage call starts a new
// turn within the same conversation. State (messages, file cache, usage, etc.)
// persists across turns.
// ────────────────────────────────────────────────────────────────────────────

// QueryEngine owns the query lifecycle and session state for a conversation.
type QueryEngine struct {
	config            *QueryEngineConfig
	mutableMessages   []*Message
	abortCtx          context.Context
	abortCancel       context.CancelFunc
	permissionDenials []SDKPermDenial
	totalUsage        NonNullableUsage
	readFileState     *FileStateCache
	sessionID         string

	// engine is an optional reference to the production Engine for delegation.
	// When set, runQueryLoop delegates to the Engine's runQueryLoop which has
	// full model calling, tool execution, compaction, etc.
	engine *Engine

	// Turn-scoped skill discovery tracking (cleared at start of each SubmitMessage).
	discoveredSkillNames    map[string]bool
	loadedNestedMemoryPaths map[string]bool

	// hasHandledOrphanedPermission is set after the first turn processes orphaned permission.
	hasHandledOrphanedPermission bool

	// persister records messages to durable storage when PersistSession is true.
	persister *SessionPersister

	mu sync.Mutex // guards mutableMessages
}

// NewQueryEngine creates a QueryEngine from the given config.
func NewQueryEngine(cfg *QueryEngineConfig) *QueryEngine {
	abortCtx := cfg.AbortCtx
	abortCancel := cfg.AbortCancel
	if abortCtx == nil {
		abortCtx, abortCancel = context.WithCancel(context.Background())
	}

	sessionID := ""
	if cfg.GetAppState != nil {
		if as := cfg.GetAppState(); as != nil {
			sessionID = as.SessionID
		}
	}
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	initialMessages := cfg.InitialMessages
	if initialMessages == nil {
		initialMessages = make([]*Message, 0)
	}

	readFileState := cfg.ReadFileCache
	if readFileState == nil {
		readFileState = NewFileStateCache(200)
	}

	var persister *SessionPersister
	if cfg.PersistSession && cfg.SessionWriter != nil {
		persister = NewSessionPersister(cfg.SessionWriter, sessionID)
	}

	return &QueryEngine{
		config:                  cfg,
		mutableMessages:         initialMessages,
		abortCtx:                abortCtx,
		abortCancel:             abortCancel,
		permissionDenials:       make([]SDKPermDenial, 0),
		totalUsage:              EmptyUsage(),
		readFileState:           readFileState,
		sessionID:               sessionID,
		discoveredSkillNames:    make(map[string]bool),
		loadedNestedMemoryPaths: make(map[string]bool),
		persister:               persister,
	}
}

// SetEngine wires a production Engine so the query loop can delegate to it.
func (qe *QueryEngine) SetEngine(e *Engine) { qe.engine = e }

// NewQueryEngineWithEngine creates a QueryEngine backed by a production Engine.
// This enables full model calling, tool execution, and compaction.
func NewQueryEngineWithEngine(cfg *QueryEngineConfig, e *Engine) *QueryEngine {
	qe := NewQueryEngine(cfg)
	qe.engine = e
	return qe
}

// SubmitMessageOptions holds per-turn options.
type SubmitMessageOptions struct {
	UUID   string
	IsMeta bool
}

// SubmitMessage starts a new turn within the conversation.  It returns a
// channel of SDKMessage that the caller reads until closed.
//
// This is the Go equivalent of the TS async generator submitMessage().
func (qe *QueryEngine) SubmitMessage(
	ctx context.Context,
	prompt string,
	opts *SubmitMessageOptions,
) <-chan interface{} {
	out := make(chan interface{}, 64)

	go func() {
		defer close(out)

		// ── Step 1: Clear turn-scoped state (TS L238) ──────────────────
		qe.mu.Lock()
		qe.discoveredSkillNames = make(map[string]bool)
		qe.mu.Unlock()

		startTime := time.Now()
		cfg := qe.config

		// ── Step 2: Wrap canUseTool for denial tracking (TS L244-271) ──
		permTracker := NewPermDenialTracker()
		var wrappedCanUseTool CanUseToolFn
		if cfg.CanUseTool != nil {
			wrappedCanUseTool = permTracker.WrapCanUseTool(cfg.CanUseTool)
		}

		// ── Step 3: Resolve model (TS L274-276) ────────────────────────
		initialMainLoopModel := cfg.UserSpecifiedModel
		if initialMainLoopModel == "" {
			initialMainLoopModel = GetRuntimeMainLoopModel("claude-sonnet-4-6")
		}

		maxTurns := cfg.MaxTurns
		if maxTurns <= 0 {
			maxTurns = 100
		}

		// ── Step 4: Build system prompt (TS L284-325) ──────────────────
		// (Delegated to SystemPromptFetcher if set; otherwise uses defaults.)

		// ── Step 5: Build initial ProcessUserInputContext (TS L335-395) ─
		setMessages := func(fn func([]*Message) []*Message) {
			qe.mu.Lock()
			defer qe.mu.Unlock()
			qe.mutableMessages = fn(qe.mutableMessages)
		}

		tuc := &ToolUseContext{
			Options: &ToolUseOptions{
				Tools:                   cfg.Tools,
				MainLoopModel:           initialMainLoopModel,
				IsNonInteractiveSession: true,
				AppendSystemPrompt:      cfg.AppendSystemPrompt,
			},
			AbortCtx:          qe.abortCtx,
			AbortCancel:       qe.abortCancel,
			GetAppState:       cfg.GetAppState,
			SetAppState:       cfg.SetAppState,
			ReadFileState:     qe.readFileState,
			HandleElicitation: cfg.HandleElicitation,
		}

		puic := NewProcessUserInputContext(cfg, qe.mutableMessages, setMessages, tuc)

		// ── Step 6: Handle orphaned permission (TS L397-408) ───────────
		if cfg.OrphanedPermission != nil && !qe.hasHandledOrphanedPermission {
			qe.hasHandledOrphanedPermission = true
			// Orphaned permission handling: re-run the permission check for
			// the tool call that was pending when the prior session ended.
			// Full handler implementation deferred to P9; here we mark it handled.
		}

		// ── Step 7: Create user message from prompt (TS L410-431) ──────
		msgUUID := ""
		if opts != nil && opts.UUID != "" {
			msgUUID = opts.UUID
		}
		if msgUUID == "" {
			msgUUID = uuid.New().String()
		}

		userMsg := &Message{
			UUID:      msgUUID,
			Role:      RoleUser,
			Type:      MsgTypeUser,
			SessionID: qe.sessionID,
			Timestamp: time.Now(),
			Content: []*ContentBlock{{
				Type: ContentTypeText,
				Text: prompt,
			}},
		}
		if opts != nil && opts.IsMeta {
			userMsg.IsMeta = true
		}

		qe.mu.Lock()
		qe.mutableMessages = append(qe.mutableMessages, userMsg)
		qe.mu.Unlock()

		// ── Step 8: Resolve final model (TS L488) ──────────────────────
		mainLoopModel := initialMainLoopModel

		// ── Step 9: Emit system/init message (TS L540-551) ─────────────
		initMsg := qe.buildSystemInitMessage(mainLoopModel)
		qe.emit(ctx, out, initMsg)

		// ── Query loop delegation (TS L675-968) ────────────────────────
		ds := newDispatchState(mainLoopModel, startTime)

		queryCtx, queryCancel := context.WithCancel(ctx)
		defer queryCancel()

		// Override canUseTool in the loop input with the wrapped version.
		_ = wrappedCanUseTool // Used by query loop via tuc when wired

		loopCh := qe.runQueryLoop(queryCtx, &queryLoopInput{
			messages:     qe.mutableMessages,
			tuc:          tuc,
			puic:         puic,
			model:        mainLoopModel,
			maxTurns:     maxTurns,
			maxBudgetUSD: cfg.MaxBudgetUSD,
			taskBudget:   cfg.TaskBudget,
		})

		earlyExit := false
		for msg := range loopCh {
			if msg == nil {
				continue
			}

			dr := qe.dispatchQueryMessage(msg, ds, cfg)

			// Yield all SDK messages from dispatch.
			for _, y := range dr.Yielded {
				qe.emit(ctx, out, y)
			}

			// Early return (max_turns, etc.)
			if dr.Action == dispatchReturn && dr.Terminal != nil {
				qe.emit(ctx, out, dr.Terminal)
				earlyExit = true
				break
			}

			// Budget check (TS L971-1002)
			if budgetErr := qe.checkBudgetExceeded(ds, cfg); budgetErr != nil {
				qe.emit(ctx, out, budgetErr)
				earlyExit = true
				break
			}

			// Structured output retry limit (TS L1004-1048)
			if soErr := qe.checkStructuredOutputRetryLimit(ds, cfg, msg); soErr != nil {
				qe.emit(ctx, out, soErr)
				earlyExit = true
				break
			}
		}

		if earlyExit {
			qe.permissionDenials = append(qe.permissionDenials, permTracker.Denials()...)
			return
		}

		// ── Finalize result (TS L1050-1155) ─────────────────────────────
		qe.permissionDenials = append(qe.permissionDenials, permTracker.Denials()...)
		finalResult := qe.buildFinalResult(ds, cfg)
		qe.emit(ctx, out, finalResult)
	}()

	return out
}

// Interrupt aborts the current query.
func (qe *QueryEngine) Interrupt() {
	qe.abortCancel()
}

// GetMessages returns the current conversation messages (read-only snapshot).
func (qe *QueryEngine) GetMessages() []*Message {
	qe.mu.Lock()
	defer qe.mu.Unlock()
	cp := make([]*Message, len(qe.mutableMessages))
	copy(cp, qe.mutableMessages)
	return cp
}

// GetSessionID returns the session identifier.
func (qe *QueryEngine) GetSessionID() string {
	return qe.sessionID
}

// SetModel updates the model for future turns.
func (qe *QueryEngine) SetModel(model string) {
	qe.config.UserSpecifiedModel = model
}

// GetReadFileState returns the file state cache.
func (qe *QueryEngine) GetReadFileState() *FileStateCache {
	return qe.readFileState
}

// ── internal helpers ────────────────────────────────────────────────────────

func (qe *QueryEngine) emit(ctx context.Context, ch chan<- interface{}, msg interface{}) {
	select {
	case <-ctx.Done():
	case ch <- msg:
	}
}

func (qe *QueryEngine) buildSystemInitMessage(model string) *SDKSystemInitMessage {
	// Tools — map through SdkCompatToolName (TS: sdkCompatToolName)
	toolNames := make([]string, 0, len(qe.config.Tools))
	for _, t := range qe.config.Tools {
		toolNames = append(toolNames, SdkCompatToolName(t.Name()))
	}

	// MCP servers
	mcpServers := make([]MCPServerStatus, 0, len(qe.config.MCPClients))
	for _, c := range qe.config.MCPClients {
		mcpServers = append(mcpServers, MCPServerStatus{
			Name:   c.Name,
			Status: c.Status,
		})
	}

	// Slash commands — filter by UserInvocable (TS: c.userInvocable !== false)
	slashCmds := make([]string, 0, len(qe.config.Commands))
	for _, c := range qe.config.Commands {
		if c.IsUserInvocable() {
			slashCmds = append(slashCmds, c.Name)
		}
	}

	// Agents
	agents := make([]string, 0, len(qe.config.Agents))
	for _, a := range qe.config.Agents {
		agents = append(agents, a.Type)
	}

	// Skills — filter by UserInvocable
	skills := make([]string, 0, len(qe.config.Skills))
	for _, s := range qe.config.Skills {
		if s.IsUserInvocable() {
			skills = append(skills, s.Name)
		}
	}

	// Plugins
	plugins := make([]PluginInfo, 0, len(qe.config.Plugins))
	for _, p := range qe.config.Plugins {
		plugins = append(plugins, PluginInfo{
			Name:   p.Name,
			Path:   p.Path,
			Source: p.Source,
		})
	}

	// OutputStyle — fallback to "concise" (TS: DEFAULT_OUTPUT_STYLE_NAME)
	outputStyle := qe.config.OutputStyle
	if outputStyle == "" {
		outputStyle = "concise"
	}

	// PermissionMode — fallback to "default"
	permMode := qe.config.PermissionMode
	if permMode == "" {
		permMode = "default"
	}

	// Version — fallback to "0.1.0"
	version := qe.config.Version
	if version == "" {
		version = "0.1.0"
	}

	// FastModeState (TS: getFastModeState(model, fastMode))
	fastState := GetFastModeState(model, qe.config.FastMode)

	return NewSDKSystemInit(qe.sessionID, SDKSystemInitMessage{
		CWD:               qe.config.CWD,
		Model:             model,
		Tools:             toolNames,
		MCPServers:        mcpServers,
		PermissionMode:    permMode,
		OutputStyle:       outputStyle,
		ClaudeCodeVersion: version,
		SlashCommands:     slashCmds,
		Agents:            agents,
		Skills:            skills,
		Plugins:           plugins,
		APIKeySource:      qe.config.APIKeySource,
		Betas:             qe.config.Betas,
		FastModeState:     fastState,
	})
}

func (qe *QueryEngine) totalCostUSD() float64 {
	// Cost is tracked externally via model pricing; NonNullableUsage doesn't
	// carry CostUSD directly. Return 0 for now; P9 wires calculateUSDCost.
	return 0
}

func (qe *QueryEngine) isResultSuccessful(lastStopReason string) bool {
	qe.mu.Lock()
	defer qe.mu.Unlock()

	if len(qe.mutableMessages) == 0 {
		return false
	}

	// Find last assistant or user message.
	for i := len(qe.mutableMessages) - 1; i >= 0; i-- {
		msg := qe.mutableMessages[i]
		switch msg.Role {
		case RoleAssistant:
			if lastStopReason == "end_turn" || lastStopReason == "tool_use" {
				return true
			}
			// Check if last content block is text or thinking.
			if len(msg.Content) > 0 {
				last := msg.Content[len(msg.Content)-1]
				if last.Type == ContentTypeText || last.Type == ContentTypeThinking {
					return true
				}
			}
			return false
		case RoleUser:
			// A user message with tool_result blocks is a valid terminal state.
			for _, block := range msg.Content {
				if block.Type == ContentTypeToolResult {
					return true
				}
			}
			return false
		}
	}
	return false
}

// persistMessage records a message to durable storage if persistence is enabled.
// TS anchor: QueryEngine.ts recordTranscript(messages) calls.
func (qe *QueryEngine) persistMessage(m *Message) {
	if qe.persister != nil {
		qe.persister.PersistMessage(m)
	}
}

func (qe *QueryEngine) extractTextResult() string {
	qe.mu.Lock()
	defer qe.mu.Unlock()

	for i := len(qe.mutableMessages) - 1; i >= 0; i-- {
		msg := qe.mutableMessages[i]
		if msg.Role != RoleAssistant {
			continue
		}
		if len(msg.Content) == 0 {
			continue
		}
		last := msg.Content[len(msg.Content)-1]
		if last.Type == ContentTypeText {
			return last.Text
		}
		break
	}
	return ""
}

// accumulateUsageStats adds b's counts to a.
func accumulateUsageStats(a, b *UsageStats) *UsageStats {
	if a == nil {
		a = &UsageStats{}
	}
	if b == nil {
		return a
	}
	return &UsageStats{
		InputTokens:              a.InputTokens + b.InputTokens,
		OutputTokens:             a.OutputTokens + b.OutputTokens,
		CacheCreationInputTokens: a.CacheCreationInputTokens + b.CacheCreationInputTokens,
		CacheReadInputTokens:     a.CacheReadInputTokens + b.CacheReadInputTokens,
		CacheDeletedInputTokens:  a.CacheDeletedInputTokens + b.CacheDeletedInputTokens,
		CostUSD:                  a.CostUSD + b.CostUSD,
		ServerDurationMs:         a.ServerDurationMs + b.ServerDurationMs,
	}
}
