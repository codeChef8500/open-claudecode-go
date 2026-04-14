package session

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wall-ai/agent-engine/internal/agent"
	agentswarm "github.com/wall-ai/agent-engine/internal/agent/swarm"
	"github.com/wall-ai/agent-engine/internal/analytics"
	"github.com/wall-ai/agent-engine/internal/command"
	"github.com/wall-ai/agent-engine/internal/engine"
	"github.com/wall-ai/agent-engine/internal/memory"
	"github.com/wall-ai/agent-engine/internal/permission"
	"github.com/wall-ai/agent-engine/internal/plugin"
	"github.com/wall-ai/agent-engine/internal/prompt"
	"github.com/wall-ai/agent-engine/internal/provider"
	"github.com/wall-ai/agent-engine/internal/skill"
	"github.com/wall-ai/agent-engine/internal/state"
	"github.com/wall-ai/agent-engine/internal/tool/agentool"
	"github.com/wall-ai/agent-engine/internal/tool/listpeers"
	"github.com/wall-ai/agent-engine/internal/tool/sendmessage"
	"github.com/wall-ai/agent-engine/internal/tool/teamcreate"
	"github.com/wall-ai/agent-engine/internal/tool/teamdelete"
	"github.com/wall-ai/agent-engine/internal/toolset"
	"github.com/wall-ai/agent-engine/internal/util"
)

// BootstrapConfig holds all the inputs needed to set up a new interactive session.
type BootstrapConfig struct {
	AppConfig *util.AppConfig
	WorkDir   string
	SessionID string
	// MainThreadAgentType, if set, indicates the session is running as a
	// specific agent type (e.g. "worker"). Coordinator prompt injection is
	// skipped when this is set, aligned with TS systemPrompt.ts:65
	// (!mainThreadAgentDefinition guard).
	MainThreadAgentType string
}

// BootstrapResult holds the fully initialised components ready for use.
type BootstrapResult struct {
	Engine            *engine.Engine
	Provider          provider.Provider
	Checker           *permission.Checker
	PermStore         *permission.PermissionStore
	CmdExecutor       *command.Executor
	MemoryStore       *memory.Store
	SessionMemory     *memory.SessionMemoryManager
	CostTracker       *provider.CostTracker
	SessionTracker    *analytics.SessionTracker
	SystemPrompt      *prompt.BuiltSystemPrompt
	AgentRunner       *agent.AgentRunner
	AsyncManager      *agent.AsyncLifecycleManager
	ResumeManager     *agent.ResumeManager
	SwarmManager      *agentswarm.SwarmManager
	SendMessageTools  []*sendmessage.SendMessageTool
	PermBridge        *agentswarm.LeaderPermissionBridge
	NotificationQueue *agent.NotificationQueue
}

// Bootstrap wires together all subsystems for an interactive session.
func Bootstrap(ctx context.Context, cfg BootstrapConfig) (*BootstrapResult, error) {
	appCfg := cfg.AppConfig
	if appCfg == nil {
		var err error
		appCfg, err = util.LoadAppConfig(cfg.WorkDir)
		if err != nil {
			return nil, fmt.Errorf("load config: %w", err)
		}
	}

	workDir := cfg.WorkDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	sessionID := cfg.SessionID
	if sessionID == "" {
		sessionID = generateSessionID()
	}

	result := &BootstrapResult{}

	// ── 1. Provider ────────────────────────────────────────────────────────
	prov, err := provider.New(provider.Config{
		Type:    appCfg.Provider,
		APIKey:  appCfg.APIKey,
		Model:   appCfg.Model,
		BaseURL: appCfg.BaseURL,
	})
	if err != nil {
		return nil, fmt.Errorf("create provider: %w", err)
	}
	result.Provider = prov

	// ── 2. Permission checker ──────────────────────────────────────────────
	permStorePath := permission.DefaultPermissionStorePath(workDir)
	permStore := permission.NewPermissionStore(permStorePath)
	_ = permStore.Load()
	allow, deny := permStore.ToRules()
	allowedDirs := append([]string{workDir}, appCfg.AllowedDirs...)
	checker := permission.NewChecker(
		permission.Mode(appCfg.PermissionMode),
		allow, deny,
		allowedDirs,
		appCfg.DeniedCommands,
	)
	result.Checker = checker
	result.PermStore = permStore

	// ── 3. Memory ──────────────────────────────────────────────────────────
	home, _ := os.UserHomeDir()
	memDir := filepath.Join(home, ".claude", "memory", sessionID)
	memStore := memory.NewStore(memDir)
	_ = memStore.Load()
	result.MemoryStore = memStore

	smCfg := memory.DefaultSessionMemoryConfig()
	smCfg.Enabled = appCfg.SessionMemoryEnabled
	sessionMem := memory.NewSessionMemoryManager(smCfg, memStore, prov, sessionID)
	result.SessionMemory = sessionMem

	// ── 4. System prompt ───────────────────────────────────────────────────
	memInjection, _ := memory.ReadClaudeMd(workDir)
	memContent := ""
	if memInjection != nil && memInjection.HasContent() {
		memContent = memInjection.MergedContent()
	}

	// Load auto-memory prompt when enabled.
	autoMemPrompt := ""
	if memory.IsAutoMemoryEnabled() {
		autoMemPrompt = memory.LoadMemoryPrompt(workDir, false, nil)
	}

	sysPrompt := prompt.BuildEffectiveSystemPrompt(prompt.BuildOptions{
		WorkDir:          workDir,
		MemoryContent:    memContent,
		AutoMemoryPrompt: autoMemPrompt,
	})
	result.SystemPrompt = sysPrompt

	// ── 5. Skill & Plugin Discovery ──────────────────────────────────────
	skillReg := skill.NewRegistry()
	for _, s := range skill.DiscoverAll(workDir) {
		skillReg.Add(s)
	}

	// Initialize plugin system and merge plugin-provided skills.
	plugin.InitBuiltinPlugins()
	binDirs, mfDirs := plugin.DefaultPluginDirs()
	plugMgr := plugin.NewManager(binDirs, mfDirs)
	plugMgr.DiscoverAndLoad()
	for _, s := range plugMgr.GetAllSkills() {
		skillReg.Add(s)
	}

	slog.Info("skills discovered", slog.Int("count", len(skillReg.AllSkills())))

	// ── 6. Tools + AgentRunner + AsyncManager ────────────────────────────
	// Build tools initially with nil runner (breaks circular dep).
	allTools := toolset.DefaultTools(nil, skillReg)

	// engineTools is the tool set exposed to the main Engine.
	// In coordinator mode it is filtered; otherwise it equals allTools.
	engineTools := allTools

	// ── 6a. Coordinator mode: inject system prompt + filter tools ────────
	// Aligned with TS systemPrompt.ts:62-75 — coordinator prompt REPLACES the
	// default prompt (not prepended). The coordinator has its own comprehensive
	// instructions; mixing with the generic Claude Code prompt causes conflicts.
	if agent.IsCoordinatorMode() && cfg.MainThreadAgentType == "" {
		coordPrompt := agent.BuildCoordinatorSystemPrompt(agent.CoordinatorConfig{
			MaxWorkers:        4,
			MaxTurnsPerWorker: 100,
			WorkDir:           workDir,
			DefaultModel:      appCfg.Model,
		}, nil)
		// Append coordinator user context (worker tools, MCP, scratchpad).
		// Aligned with TS QueryEngine.ts:302-308 getCoordinatorUserContext().
		coordCtx := agent.GetCoordinatorUserContext(nil, "")
		if workerToolsCtx, ok := coordCtx["workerToolsContext"]; ok && workerToolsCtx != "" {
			coordPrompt += "\n\n" + workerToolsCtx
		}
		sysPrompt.Text = coordPrompt
		slog.Info("coordinator mode: system prompt replaced (not prepended)")

		// Filter tools to coordinator whitelist.
		// When CLAUDE_CODE_SIMPLE is also set, combine simple tools + coordinator
		// tools so the coordinator can do direct operations too.
		// Aligned with TS tools.ts:287-297.
		var filtered []engine.Tool
		simpleCoordAllowed := agent.SimpleCoordinatorAllowedTools()
		for _, t := range allTools {
			if simpleCoordAllowed[t.Name()] || agent.IsPrActivitySubscriptionTool(t.Name()) {
				filtered = append(filtered, t)
			}
		}
		engineTools = filtered
		slog.Info("coordinator mode: tools filtered", slog.Int("count", len(engineTools)))
	}

	// Create AgentRunner for sub-agent execution.
	// NOTE: AgentRunner receives the FULL tool set so that worker agents
	// spawned by the coordinator can access all tools (WebSearch, WebFetch,
	// Bash, FileEdit, etc.). The coordinator's own Engine uses engineTools.
	taskFramework := agent.NewTaskFramework()
	taskManager := agent.NewTaskManager()
	worktreeManager := agent.NewWorktreeManager(filepath.Join(workDir, ".worktrees"))
	agentRunner := agent.NewAgentRunner(agent.AgentRunnerConfig{
		Caller:          prov,
		TaskFramework:   taskFramework,
		TaskManager:     taskManager,
		WorktreeManager: worktreeManager,
		AllTools:        allTools,
	})
	result.AgentRunner = agentRunner

	// Create AsyncLifecycleManager for background agent execution.
	asyncMgr := agent.NewAsyncLifecycleManager(agentRunner)
	result.AsyncManager = asyncMgr
	resumeMgr := agent.NewResumeManager(workDir)
	result.ResumeManager = resumeMgr

	mailboxRegistry := agent.NewMailboxRegistry(256, 0)
	messageBus := agent.NewMessageBus()
	teamManager := agent.NewTeamManager(workDir, mailboxRegistry, messageBus)
	appState := state.New(workDir)
	swarmMgr := agentswarm.NewSwarmManager(agentswarm.SwarmManagerConfig{
		BaseDir:     workDir,
		TeamManager: teamManager,
		AppState:    appState,
		RunAgent: func(runCtx context.Context, prompt string) (string, error) {
			return agentRunner.RunAgentSync(runCtx, agent.RunAgentParams{
				Task:       prompt,
				WorkDir:    workDir,
				Background: false,
			})
		},
	})
	result.SwarmManager = swarmMgr
	result.PermBridge = swarmMgr.PermBridge
	teamCreator := &agentswarm.TeamManagerCreatorAdapter{TM: teamManager}
	teamDeleter := &agentswarm.TeamManagerDeleterAdapter{TM: teamManager}

	// Wire global notification queue for task-notification injection.
	globalNotifQueue := agent.NewNotificationQueue(100)
	asyncMgr.SetGlobalNotificationSink(globalNotifQueue)
	result.NotificationQueue = globalNotifQueue

	// ── Agent name registry (name → agentID) ────────────────────────────
	// Thread-safe map for SendMessage name resolution.
	// Aligned with TS AppState.agentNameRegistry.
	var nameRegistry sync.Map
	registerName := func(name, agentID string) {
		nameRegistry.Store(name, agentID)
		slog.Info("bootstrap: registered agent name",
			slog.String("name", name),
			slog.String("agent_id", agentID))
	}
	resolveName := func(name string) string {
		if v, ok := nameRegistry.Load(name); ok {
			return v.(string)
		}
		return ""
	}

	// Replace the placeholder AgentTool with a fully wired one.
	agentCfg := agentool.AgentToolConfig{
		Runner:            agentRunner,
		AsyncManager:      asyncMgr,
		ResumeManager:     resumeMgr,
		TeamManager:       teamManager,
		SwarmManager:      swarmMgr,
		IsCoordinatorMode: agent.IsCoordinatorMode(),
		RegisterAgentName: registerName,
	}
	// Wire into engineTools (coordinator's own tool set).
	for i, t := range engineTools {
		switch t.Name() {
		case "Task":
			engineTools[i] = agentool.NewWithConfig(agentCfg)
		case "list_peers":
			engineTools[i] = listpeers.NewWithManager(asyncMgr)
		case "SendMessage":
			smTool := sendmessage.NewWithAllDeps(&agentswarm.MailboxSenderAdapter{SM: swarmMgr}, asyncMgr, resolveName)
			engineTools[i] = smTool
			result.SendMessageTools = append(result.SendMessageTools, smTool)
		case "team_create":
			engineTools[i] = teamcreate.NewWithCreator(teamCreator)
		case "team_delete":
			engineTools[i] = teamdelete.NewWithDeleter(teamDeleter)
		}
	}
	// Also wire into allTools so workers get the fully-configured implementations.
	for i, t := range allTools {
		switch t.Name() {
		case "Task":
			allTools[i] = agentool.NewWithConfig(agentCfg)
		case "list_peers":
			allTools[i] = listpeers.NewWithManager(asyncMgr)
		case "SendMessage":
			smTool := sendmessage.NewWithAllDeps(&agentswarm.MailboxSenderAdapter{SM: swarmMgr}, asyncMgr, resolveName)
			allTools[i] = smTool
			result.SendMessageTools = append(result.SendMessageTools, smTool)
		case "team_create":
			allTools[i] = teamcreate.NewWithCreator(teamCreator)
		case "team_delete":
			allTools[i] = teamdelete.NewWithDeleter(teamDeleter)
		}
	}

	// ── 7. Engine ──────────────────────────────────────────────────────────
	eng, err := engine.New(engine.EngineConfig{
		Provider:           appCfg.Provider,
		APIKey:             appCfg.APIKey,
		BaseURL:            appCfg.BaseURL,
		Model:              appCfg.Model,
		MaxTokens:          appCfg.MaxTokens,
		ThinkingBudget:     appCfg.ThinkingBudget,
		WorkDir:            workDir,
		SessionID:          sessionID,
		AutoMode:           appCfg.AutoMode,
		MaxCostUSD:         appCfg.MaxCostUSD,
		PermissionMode:     appCfg.PermissionMode,
		CustomSystemPrompt: sysPrompt.Text,
		Verbose:            appCfg.VerboseMode,
		StopTask:           asyncMgr.Cancel,
	}, prov, engineTools)
	if err != nil {
		return nil, fmt.Errorf("create engine: %w", err)
	}
	result.Engine = eng

	// Wire optional integrations into the engine (same as SDK path).
	eng.SetMemoryLoader(memory.NewAdapter())
	eng.SetSessionWriter(NewAdapter())
	eng.SetPromptBuilder(prompt.NewAdapter())
	eng.SetPermissionChecker(permission.NewAdapterWithChecker(checker))

	// ── 8. Command executor ────────────────────────────────────────────────
	cmdExec := command.NewExecutor(command.Default())
	result.CmdExecutor = cmdExec

	// ── 9. Cost + session tracking ─────────────────────────────────────────
	costTracker := provider.NewCostTracker()
	result.CostTracker = costTracker

	sessionTracker := analytics.NewSessionTracker(sessionID, appCfg.Model, workDir)
	result.SessionTracker = sessionTracker

	// ── 10. Analytics ──────────────────────────────────────────────────────
	analytics.SetSessionID(sessionID)
	analyticsPath := analytics.DefaultAnalyticsPath()
	if analyticsPath != "" {
		sink, err := analytics.NewFileSink(analyticsPath)
		if err != nil {
			slog.Warn("analytics sink disabled", slog.String("error", err.Error()))
		} else {
			analytics.AttachSink(sink)
		}
	}

	analytics.LogEvent("session_start", analytics.EventMetadata{
		"model":      appCfg.Model,
		"provider":   appCfg.Provider,
		"work_dir":   workDir,
		"session_id": sessionID,
	})

	slog.Info("session bootstrapped",
		slog.String("session_id", sessionID),
		slog.String("model", appCfg.Model),
		slog.String("work_dir", workDir),
	)

	return result, nil
}

// Shutdown performs graceful cleanup for a session.
func Shutdown(result *BootstrapResult) {
	if result == nil {
		return
	}
	if result.SwarmManager != nil {
		result.SwarmManager.ShutdownAll("session shutdown")
	}
	if result.AsyncManager != nil {
		result.AsyncManager.ShutdownAll(10 * time.Second)
	}
	if result.SessionTracker != nil {
		result.SessionTracker.EmitSessionEnd()
	}
	_ = analytics.Flush()
	_ = analytics.Close()
}

func generateSessionID() string {
	return fmt.Sprintf("sess_%d_%d", os.Getpid(), time.Now().UnixMilli())
}
