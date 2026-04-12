package swarm

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// ── TmuxBackend ──────────────────────────────────────────────────────────────
//
// PaneBackend and TeammateExecutor for tmux-based teammates.
// Aligned with claude-code-main's TmuxBackend in backends/tmux.ts.
//
// Teammates run in separate tmux panes, communicating via file-based mailbox.

// TmuxBackendImpl implements both PaneBackend and TeammateExecutor for tmux.
type TmuxBackendImpl struct {
	mu          sync.RWMutex
	panes       map[string]*tmuxPane // paneID → tmuxPane
	registry    *TeammateRegistry
	fileMB      *FileMailboxRegistry
	sessionName string
}

type tmuxPane struct {
	paneID  string
	agentID string
	config  TeammateSpawnConfig
}

// TmuxBackendConfig configures the tmux backend.
type TmuxBackendConfig struct {
	Registry    *TeammateRegistry
	FileMB      *FileMailboxRegistry
	SessionName string
}

// NewTmuxBackend creates a new tmux backend.
func NewTmuxBackend(cfg TmuxBackendConfig) *TmuxBackendImpl {
	return &TmuxBackendImpl{
		panes:       make(map[string]*tmuxPane),
		registry:    cfg.Registry,
		fileMB:      cfg.FileMB,
		sessionName: cfg.SessionName,
	}
}

// ── PaneBackend interface ────────────────────────────────────────────────────

// CreatePane creates a new tmux pane.
func (t *TmuxBackendImpl) CreatePane(ctx context.Context, config PaneConfig) (string, error) {
	if runtime.GOOS == "windows" {
		return "", fmt.Errorf("tmux is not available on Windows")
	}

	sessionName := config.SessionName
	if sessionName == "" {
		sessionName = t.sessionName
	}

	// Build env args.
	var envArgs []string
	for k, v := range config.Env {
		envArgs = append(envArgs, fmt.Sprintf("%s=%s", k, v))
	}

	// Create new window/pane in the session.
	args := []string{"new-window", "-t", sessionName, "-n", config.WindowName}
	if config.WorkDir != "" {
		args = append(args, "-c", config.WorkDir)
	}

	// Set environment variables.
	for _, envStr := range envArgs {
		parts := strings.SplitN(envStr, "=", 2)
		if len(parts) == 2 {
			setEnvArgs := []string{"set-environment", "-t", sessionName, parts[0], parts[1]}
			_ = runTmuxCommand(ctx, setEnvArgs...)
		}
	}

	cmd := exec.CommandContext(ctx, "tmux", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux new-window: %w: %s", err, string(output))
	}

	// Get the pane ID of the newly created window.
	paneID, err := getTmuxPaneID(ctx, sessionName, config.WindowName)
	if err != nil {
		return "", fmt.Errorf("get pane ID: %w", err)
	}

	// Send initial command if specified.
	if config.Command != "" {
		if err := t.SendCommand(paneID, config.Command); err != nil {
			return paneID, fmt.Errorf("send initial command: %w", err)
		}
	}

	return paneID, nil
}

// SendCommand sends a command string to a tmux pane.
func (t *TmuxBackendImpl) SendCommand(paneID string, command string) error {
	return runTmuxCommand(context.Background(), "send-keys", "-t", paneID, command, "Enter")
}

// KillPane terminates a tmux pane.
func (t *TmuxBackendImpl) KillPane(paneID string) error {
	return runTmuxCommand(context.Background(), "kill-pane", "-t", paneID)
}

// IsPaneAlive checks if a tmux pane is still running.
func (t *TmuxBackendImpl) IsPaneAlive(paneID string) bool {
	err := runTmuxCommand(context.Background(), "has-session", "-t", paneID)
	return err == nil
}

// ── TeammateExecutor interface ───────────────────────────────────────────────

// Spawn creates a new tmux pane for a teammate.
func (t *TmuxBackendImpl) Spawn(ctx context.Context, config TeammateSpawnConfig) (*TeammateSpawnResult, error) {
	agentID := config.Identity.AgentID
	if agentID == "" {
		agentID = FormatAgentID(config.Identity.AgentName, config.Identity.TeamName)
		config.Identity.AgentID = agentID
	}

	// Build env vars for the teammate pane.
	env := map[string]string{
		TeammateNameEnvVar:  config.Identity.AgentName,
		TeamNameEnvVar:      config.Identity.TeamName,
		TeammateColorEnvVar: config.Identity.Color,
		ParentSessionEnvVar: config.Identity.ParentSessionID,
	}
	if config.Identity.PlanModeRequired {
		env[PlanModeRequiredEnvVar] = "1"
	}

	// Build command to launch the teammate process.
	command := config.Prompt // placeholder; actual command depends on binary path
	if os.Getenv(TeammateCommandEnvVar) != "" {
		command = os.Getenv(TeammateCommandEnvVar)
	}

	paneConfig := PaneConfig{
		SessionName: t.sessionName,
		WindowName:  config.Identity.AgentName,
		WorkDir:     config.WorkDir,
		Env:         env,
		Command:     command,
	}

	paneID, err := t.CreatePane(ctx, paneConfig)
	if err != nil {
		return nil, fmt.Errorf("create tmux pane: %w", err)
	}

	t.mu.Lock()
	t.panes[paneID] = &tmuxPane{
		paneID:  paneID,
		agentID: agentID,
		config:  config,
	}
	t.mu.Unlock()

	// Register in teammate registry.
	if t.registry != nil {
		t.registry.Register(&ActiveTeammate{
			Identity:    config.Identity,
			BackendType: BackendTmux,
			PaneID:      paneID,
			Cancel: func() {
				_ = t.KillPane(paneID)
			},
		})
	}

	// Create file mailbox for this teammate.
	if t.fileMB != nil {
		_, _ = t.fileMB.GetOrCreate(config.Identity.AgentName)
	}

	slog.Info("swarm: tmux teammate spawned",
		slog.String("agent_id", agentID),
		slog.String("pane_id", paneID))

	return &TeammateSpawnResult{
		Identity:    config.Identity,
		PaneID:      paneID,
		BackendType: BackendTmux,
	}, nil
}

// SendMessage delivers a message to a tmux teammate via file mailbox.
func (t *TmuxBackendImpl) SendMessage(agentID string, message TeammateMessage) error {
	if t.fileMB == nil {
		return fmt.Errorf("file mailbox not configured for tmux backend")
	}

	name, _ := ParseAgentID(agentID)
	if name == "" {
		name = agentID
	}

	env, err := NewEnvelope(message.From, agentID, message.MessageType,
		PlainTextPayload{Text: message.Content})
	if err != nil {
		return err
	}

	return t.fileMB.Send(message.From, name, env)
}

// Terminate requests graceful shutdown of a tmux teammate.
func (t *TmuxBackendImpl) Terminate(agentID string, reason string) error {
	if t.fileMB == nil {
		return fmt.Errorf("file mailbox not configured")
	}

	name, _ := ParseAgentID(agentID)
	if name == "" {
		name = agentID
	}

	env, err := NewEnvelope(TeamLeadName, agentID, MessageTypeShutdownRequest,
		ShutdownRequestPayload{Reason: reason})
	if err != nil {
		return err
	}

	return t.fileMB.Send(TeamLeadName, name, env)
}

// Kill forcefully stops a tmux teammate by killing its pane.
func (t *TmuxBackendImpl) Kill(agentID string) error {
	t.mu.Lock()
	var paneID string
	for pid, p := range t.panes {
		if p.agentID == agentID {
			paneID = pid
			delete(t.panes, pid)
			break
		}
	}
	t.mu.Unlock()

	if paneID == "" {
		return nil
	}

	if t.registry != nil {
		t.registry.Unregister(agentID)
	}

	return t.KillPane(paneID)
}

// IsActive checks if a tmux teammate's pane is still alive.
func (t *TmuxBackendImpl) IsActive(agentID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, p := range t.panes {
		if p.agentID == agentID {
			return t.IsPaneAlive(p.paneID)
		}
	}
	return false
}

// Type returns the backend type.
func (t *TmuxBackendImpl) Type() BackendType {
	return BackendTmux
}

// ── Tmux helpers ─────────────────────────────────────────────────────────────

func runTmuxCommand(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "tmux", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux %s: %w: %s", args[0], err, string(output))
	}
	return nil
}

func getTmuxPaneID(ctx context.Context, session, window string) (string, error) {
	target := fmt.Sprintf("%s:%s", session, window)
	cmd := exec.CommandContext(ctx, "tmux", "display-message", "-t", target, "-p", "#{pane_id}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("get pane ID: %w: %s", err, string(output))
	}
	return strings.TrimSpace(string(output)), nil
}
