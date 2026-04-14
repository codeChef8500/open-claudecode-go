package swarm

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/wall-ai/agent-engine/internal/agent"
	"github.com/wall-ai/agent-engine/internal/state"
)

// ── SwarmManager ─────────────────────────────────────────────────────────────
//
// Top-level coordinator that wires all swarm subsystems together.
// This is the main entry point for external code (session runner, TUI).
//
// Owns:
//   - TeamManager (team CRUD, persistence)
//   - BackendRegistry (executor resolution)
//   - TeammateRegistry (active teammate tracking)
//   - MailboxAdapter (hybrid in-memory + file)
//   - LeaderPermissionBridge (in-process permission UI)

// SwarmManager is the top-level swarm coordinator.
type SwarmManager struct {
	TeamManager    *agent.TeamManager
	Backend        *BackendRegistry
	Teammates      *TeammateRegistry
	Mailbox        MailboxAdapter
	PermBridge     *LeaderPermissionBridge
	AppState       *state.AppState
	InProcessBE    *InProcessBackend
	TmuxBE         *TmuxBackendImpl
	UDSBE          *UDSBackendImpl
	BridgeBE       *BridgeBackendImpl
	FileMailboxReg *FileMailboxRegistry
	RunAgent       AgentRunFunc // wired by session runner
}

// SwarmManagerConfig configures the SwarmManager.
type SwarmManagerConfig struct {
	BaseDir     string // for file persistence (~/.claude)
	TeamManager *agent.TeamManager
	AppState    *state.AppState
	BackendMode BackendMode  // auto, in-process, tmux
	RunAgent    AgentRunFunc // agent run callback
}

// NewSwarmManager creates and wires all swarm subsystems.
func NewSwarmManager(cfg SwarmManagerConfig) *SwarmManager {
	teammates := NewTeammateRegistry()
	permBridge := NewLeaderPermissionBridge()
	backendReg := NewBackendRegistry(cfg.BackendMode)

	// Create in-memory mailbox registry from the TeamManager's existing one,
	// or create a standalone one.
	inMemReg := agent.NewMailboxRegistry(256, 0)

	// File mailbox registry for tmux teammates.
	var fileMBReg *FileMailboxRegistry
	if cfg.BaseDir != "" {
		fileMBReg = NewFileMailboxRegistry("", cfg.BaseDir)
	}

	// Members resolver: looks up team members from TeamManager.
	membersResolver := func(teamName string) []string {
		if cfg.TeamManager != nil {
			return cfg.TeamManager.TeamMemberIDs(teamName)
		}
		return nil
	}

	// Backend resolver: checks teammate registry for backend type.
	backendResolver := func(agentID string) BackendType {
		if at, ok := teammates.Get(agentID); ok {
			return at.BackendType
		}
		return BackendInProcess
	}

	// Create hybrid mailbox adapter.
	mailbox := NewHybridMailboxAdapter(HybridMailboxConfig{
		InMemory:        inMemReg,
		FileMB:          fileMBReg,
		BackendResolver: backendResolver,
		MembersResolver: membersResolver,
	})

	// Create in-process backend.
	inProcessBE := NewInProcessBackend(InProcessBackendConfig{
		Registry: teammates,
		Mailbox:  mailbox,
	})
	backendReg.RegisterExecutor(BackendInProcess, inProcessBE)

	// Create tmux backend (if available).
	var tmuxBE *TmuxBackendImpl
	udsBE := NewUDSBackend(UDSBackendConfig{Registry: teammates, FileMB: fileMBReg})
	bridgeBE := NewBridgeBackend(BridgeBackendConfig{Registry: teammates, FileMB: fileMBReg})
	backendReg.RegisterExecutor(BackendUDS, udsBE)
	backendReg.RegisterExecutor(BackendBridge, bridgeBE)
	detection := backendReg.Detect()
	if detection.BackendType == BackendTmux || detection.IsInsideTmux {
		tmuxBE = NewTmuxBackend(TmuxBackendConfig{
			Registry: teammates,
			FileMB:   fileMBReg,
		})
		backendReg.RegisterExecutor(BackendTmux, tmuxBE)
	}

	return &SwarmManager{
		TeamManager:    cfg.TeamManager,
		Backend:        backendReg,
		Teammates:      teammates,
		Mailbox:        mailbox,
		PermBridge:     permBridge,
		AppState:       cfg.AppState,
		InProcessBE:    inProcessBE,
		TmuxBE:         tmuxBE,
		UDSBE:          udsBE,
		BridgeBE:       bridgeBE,
		FileMailboxReg: fileMBReg,
		RunAgent:       cfg.RunAgent,
	}
}

// SpawnTeammate creates and starts a new teammate.
func (sm *SwarmManager) SpawnTeammate(ctx context.Context, cfg TeammateSpawnConfig) (*SpawnOutput, error) {
	if cfg.Identity.Color == "" {
		cfg.Identity.Color = NextTeammateColor()
	}

	output, err := SpawnTeammate(ctx, SpawnMultiAgentConfig{
		BackendRegistry: sm.Backend,
		Mailbox:         sm.Mailbox,
		AppState:        sm.AppState,
		PermBridge:      sm.PermBridge,
		RunAgent:        sm.RunAgent,
	}, cfg)
	if err != nil {
		return nil, err
	}

	// Also register in TeamManager if available.
	if sm.TeamManager != nil {
		_ = sm.TeamManager.AddMember(
			cfg.Identity.TeamName,
			output.AgentID,
			cfg.AgentType,
			"worker",
		)
	}

	return output, nil
}

// SendMessage sends a message from one agent to another.
func (sm *SwarmManager) SendMessage(from, to, text, priority string) (string, error) {
	return sm.Mailbox.Send(from, to, text, priority, "")
}

// BroadcastMessage sends a message to all team members.
func (sm *SwarmManager) BroadcastMessage(from, teamName, text string) error {
	return sm.Mailbox.Broadcast(from, teamName, text)
}

// ShutdownTeammate requests a teammate to shut down.
func (sm *SwarmManager) ShutdownTeammate(agentID, reason string) error {
	exec, _, err := sm.Backend.ResolveExecutor()
	if err != nil {
		// Try direct termination via registry.
		if at, ok := sm.Teammates.Get(agentID); ok && at.Cancel != nil {
			at.Cancel()
			return nil
		}
		return err
	}
	return exec.Terminate(agentID, reason)
}

// KillTeammate forcefully stops a teammate.
func (sm *SwarmManager) KillTeammate(agentID string) error {
	if at, ok := sm.Teammates.Get(agentID); ok && at.Cancel != nil {
		at.Cancel()
	}
	return nil
}

// ShutdownAll gracefully shuts down all teammates.
func (sm *SwarmManager) ShutdownAll(reason string) {
	for _, at := range sm.Teammates.All() {
		if err := sm.ShutdownTeammate(at.Identity.AgentID, reason); err != nil {
			slog.Warn("swarm: shutdown teammate failed",
				slog.String("agent_id", at.Identity.AgentID),
				slog.Any("err", err))
		}
	}

	// Wait briefly for graceful shutdown.
	deadline := time.After(5 * time.Second)
	for sm.Teammates.Count() > 0 {
		select {
		case <-deadline:
			slog.Warn("swarm: force-killing remaining teammates",
				slog.Int("count", sm.Teammates.Count()))
			sm.Teammates.StopAll()
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Cleanup releases all resources.
func (sm *SwarmManager) Cleanup() {
	sm.Teammates.StopAll()
	if sm.FileMailboxReg != nil {
		sm.FileMailboxReg.RemoveAll()
	}
	slog.Info("swarm: cleanup complete")
}

// IsActive returns true if there are active teammates.
func (sm *SwarmManager) IsActive() bool {
	return sm.Teammates.Count() > 0
}

// TeammateCount returns the number of active teammates.
func (sm *SwarmManager) TeammateCount() int {
	return sm.Teammates.Count()
}

// ── TeamCreator/TeamDeleter adapters ─────────────────────────────────────────
// These adapt TeamManager to the interfaces expected by tool packages.

// TeamManagerCreatorAdapter adapts *agent.TeamManager to teamcreate.TeamCreator.
type TeamManagerCreatorAdapter struct {
	TM *agent.TeamManager
}

func (a *TeamManagerCreatorAdapter) CreateTeam(name, description, leadAgentID string) (interface{}, error) {
	return a.TM.CreateTeam(name, description, leadAgentID)
}

func (a *TeamManagerCreatorAdapter) AddMember(teamName, agentID, agentType, role string) error {
	return a.TM.AddMember(teamName, agentID, agentType, role)
}

func (a *TeamManagerCreatorAdapter) TeamMemberIDs(teamName string) []string {
	return a.TM.TeamMemberIDs(teamName)
}

// TeamManagerDeleterAdapter adapts *agent.TeamManager to teamdelete.TeamDeleter.
type TeamManagerDeleterAdapter struct {
	TM *agent.TeamManager
}

func (a *TeamManagerDeleterAdapter) DeleteTeam(teamName string) error {
	return a.TM.DeleteTeam(teamName)
}

func (a *TeamManagerDeleterAdapter) FinishTeam(teamName string) error {
	return a.TM.FinishTeam(teamName)
}

// ── MailboxSender adapter ────────────────────────────────────────────────────

// MailboxSenderAdapter adapts SwarmManager to sendmessage.MailboxSender.
type MailboxSenderAdapter struct {
	SM *SwarmManager
}

func (a *MailboxSenderAdapter) Send(from, to, text, priority, replyTo string) (string, error) {
	return a.SM.Mailbox.Send(from, to, text, priority, replyTo)
}

func (a *MailboxSenderAdapter) Broadcast(from, teamName, text string) error {
	return a.SM.Mailbox.Broadcast(from, teamName, text)
}

func (a *MailboxSenderAdapter) TeamMembers(teamName string) []string {
	return a.SM.Mailbox.TeamMembers(teamName)
}

// FormatSwarmStatus returns a human-readable summary of the swarm state.
func (sm *SwarmManager) FormatSwarmStatus() string {
	teammates := sm.Teammates.All()
	if len(teammates) == 0 {
		return "No active teammates."
	}

	result := fmt.Sprintf("Active teammates: %d\n", len(teammates))
	for _, at := range teammates {
		result += fmt.Sprintf("  - %s (%s) [%s]\n",
			at.Identity.AgentName, at.Identity.AgentID, at.BackendType)
	}
	return result
}
