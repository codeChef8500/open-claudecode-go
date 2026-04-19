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
	totalUsage        *UsageStats
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

	return &QueryEngine{
		config:                  cfg,
		mutableMessages:         initialMessages,
		abortCtx:                abortCtx,
		abortCancel:             abortCancel,
		permissionDenials:       make([]SDKPermDenial, 0),
		totalUsage:              &UsageStats{},
		readFileState:           readFileState,
		sessionID:               sessionID,
		discoveredSkillNames:    make(map[string]bool),
		loadedNestedMemoryPaths: make(map[string]bool),
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

		// Clear turn-scoped state.
		qe.mu.Lock()
		qe.discoveredSkillNames = make(map[string]bool)
		qe.mu.Unlock()

		startTime := time.Now()

		cfg := qe.config
		maxTurns := cfg.MaxTurns
		if maxTurns <= 0 {
			maxTurns = 100
		}

		// Determine the model.
		mainLoopModel := cfg.UserSpecifiedModel
		if mainLoopModel == "" {
			mainLoopModel = "claude-sonnet-4-6"
		}

		// Emit system init message.
		initMsg := qe.buildSystemInitMessage(mainLoopModel)
		qe.emit(ctx, out, initMsg)

		// Create the user message from the prompt.
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

		// Build ToolUseContext for the query loop.
		tuc := &ToolUseContext{
			Options: &ToolUseOptions{
				Tools:                   cfg.Tools,
				MainLoopModel:           mainLoopModel,
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

		// Build ProcessUserInputContext.
		puic := NewProcessUserInputContext(cfg, qe.mutableMessages, func(fn func([]*Message) []*Message) {
			qe.mu.Lock()
			defer qe.mu.Unlock()
			qe.mutableMessages = fn(qe.mutableMessages)
		}, tuc)

		// ── Query loop delegation ───────────────────────────────────────────
		// The actual query loop is in queryloop.go. Here we invoke it and
		// forward yielded messages to the output channel.
		turnCount := 1
		var lastStopReason string

		queryCtx, queryCancel := context.WithCancel(ctx)
		defer queryCancel()

		loopCh := qe.runQueryLoop(queryCtx, &queryLoopInput{
			messages:     qe.mutableMessages,
			tuc:          tuc,
			puic:         puic,
			model:        mainLoopModel,
			maxTurns:     maxTurns,
			maxBudgetUSD: cfg.MaxBudgetUSD,
			taskBudget:   cfg.TaskBudget,
		})

		for msg := range loopCh {
			if msg == nil {
				continue
			}

			// Track usage from stream events.
			if se, ok := msg.(*StreamEvent); ok {
				if se.Usage != nil {
					qe.totalUsage = accumulateUsageStats(qe.totalUsage, se.Usage)
				}
			}

			// Track assistant message stop reasons.
			if m, ok := msg.(*Message); ok {
				if m.Role == RoleAssistant && m.StopReason != "" {
					lastStopReason = m.StopReason
				}
				if m.Role == RoleUser {
					turnCount++
				}
				qe.mu.Lock()
				qe.mutableMessages = append(qe.mutableMessages, m)
				qe.mu.Unlock()
			}

			qe.emit(ctx, out, msg)

			// Budget check.
			if cfg.MaxBudgetUSD != nil && qe.totalCostUSD() >= *cfg.MaxBudgetUSD {
				errResult := NewSDKResultError(
					qe.sessionID,
					SDKResultErrorMaxBudgetUSD,
					[]string{"Reached maximum budget"},
					int(time.Since(startTime).Milliseconds()),
					0, turnCount,
					qe.totalCostUSD(),
					qe.totalUsage,
				)
				qe.emit(ctx, out, errResult)
				return
			}
		}

		// ── Determine final result ──────────────────────────────────────
		durationMs := int(time.Since(startTime).Milliseconds())

		if !qe.isResultSuccessful(lastStopReason) {
			errResult := NewSDKResultError(
				qe.sessionID,
				SDKResultErrorDuringExecution,
				[]string{"Execution completed without successful result"},
				durationMs, 0, turnCount,
				qe.totalCostUSD(),
				qe.totalUsage,
			)
			qe.emit(ctx, out, errResult)
			return
		}

		textResult := qe.extractTextResult()
		successResult := NewSDKResultSuccess(
			qe.sessionID,
			textResult,
			durationMs, 0, turnCount,
			qe.totalCostUSD(),
			qe.totalUsage,
			lastStopReason,
		)
		qe.emit(ctx, out, successResult)
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
	toolNames := make([]string, 0, len(qe.config.Tools))
	for _, t := range qe.config.Tools {
		toolNames = append(toolNames, t.Name())
	}

	mcpServers := make([]MCPServerStatus, 0, len(qe.config.MCPClients))
	for _, c := range qe.config.MCPClients {
		mcpServers = append(mcpServers, MCPServerStatus{
			Name:   c.Name,
			Status: c.Status,
		})
	}

	return NewSDKSystemInit(qe.sessionID, SDKSystemInitMessage{
		CWD:               qe.config.CWD,
		Model:             model,
		Tools:             toolNames,
		MCPServers:        mcpServers,
		PermissionMode:    "default",
		OutputStyle:       "concise",
		ClaudeCodeVersion: "0.1.0",
	})
}

func (qe *QueryEngine) totalCostUSD() float64 {
	if qe.totalUsage == nil {
		return 0
	}
	return qe.totalUsage.CostUSD
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
