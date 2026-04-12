package swarm

import (
	"context"
)

// ── Backend types ────────────────────────────────────────────────────────────

// BackendType identifies the teammate execution backend.
type BackendType string

const (
	BackendInProcess BackendType = "in-process"
	BackendTmux      BackendType = "tmux"
)

// ── TeammateIdentity ─────────────────────────────────────────────────────────

// TeammateIdentity captures the immutable identity of a spawned teammate.
// Aligned with claude-code-main's TeammateIdentity in backends/types.ts.
type TeammateIdentity struct {
	AgentID          string `json:"agent_id"`           // "researcher@my-team"
	AgentName        string `json:"agent_name"`         // "researcher"
	TeamName         string `json:"team_name"`          // "my-team"
	Color            string `json:"color,omitempty"`    // ANSI color
	ParentSessionID  string `json:"parent_session_id"`  // leader's session UUID
	PlanModeRequired bool   `json:"plan_mode_required"` // true → teammate enters plan mode
}

// ── Spawn configuration & result ─────────────────────────────────────────────

// TeammateSpawnConfig is the input for spawning a new teammate.
type TeammateSpawnConfig struct {
	Identity          TeammateIdentity `json:"identity"`
	Prompt            string           `json:"prompt"`             // initial task prompt
	Description       string           `json:"description"`        // short human-readable summary
	Model             string           `json:"model,omitempty"`    // LLM model override
	AgentType         string           `json:"agent_type"`         // agent definition type
	WorkDir           string           `json:"work_dir"`           // working directory
	AllowedTools      []string         `json:"allowed_tools,omitempty"`
	SystemPrompt      string           `json:"system_prompt,omitempty"`
	InvokingRequestID string           `json:"invoking_request_id,omitempty"` // parent tool_use ID
	PermissionMode    string           `json:"permission_mode,omitempty"`
}

// TeammateSpawnResult is returned after a teammate is successfully spawned.
type TeammateSpawnResult struct {
	Identity    TeammateIdentity `json:"identity"`
	TaskID      string           `json:"task_id,omitempty"`
	PaneID      string           `json:"pane_id,omitempty"` // tmux pane ID, empty for in-process
	BackendType BackendType      `json:"backend_type"`
}

// SpawnOutput is the high-level output returned to AgentTool after spawning.
// Aligned with claude-code-main's SpawnOutput in spawnMultiAgent.ts.
type SpawnOutput struct {
	TeammateID       string      `json:"teammate_id"`
	AgentID          string      `json:"agent_id"`
	AgentType        string      `json:"agent_type"`
	Model            string      `json:"model"`
	Name             string      `json:"name"`
	Color            string      `json:"color"`
	TeamName         string      `json:"team_name"`
	BackendType      BackendType `json:"backend_type"`
	TmuxSessionName  string      `json:"tmux_session_name,omitempty"`
	TmuxPaneID       string      `json:"tmux_pane_id,omitempty"`
	PlanModeRequired bool        `json:"plan_mode_required"`
}

// ── TeammateExecutor interface ───────────────────────────────────────────────

// TeammateExecutor defines high-level operations for managing teammate lifecycles
// across different backends (in-process, tmux).
// Aligned with claude-code-main's TeammateExecutor in backends/types.ts.
type TeammateExecutor interface {
	// Spawn creates and starts a new teammate.
	Spawn(ctx context.Context, config TeammateSpawnConfig) (*TeammateSpawnResult, error)

	// SendMessage delivers a message to a teammate.
	SendMessage(agentID string, message TeammateMessage) error

	// Terminate requests graceful shutdown of a teammate.
	Terminate(agentID string, reason string) error

	// Kill forcefully stops a teammate.
	Kill(agentID string) error

	// IsActive checks if a teammate is still running.
	IsActive(agentID string) bool

	// Type returns the backend type.
	Type() BackendType
}

// ── PaneBackend interface ────────────────────────────────────────────────────

// PaneBackend defines low-level terminal pane operations (tmux).
// Aligned with claude-code-main's PaneBackend in backends/types.ts.
type PaneBackend interface {
	// CreatePane creates a new terminal pane and returns its ID.
	CreatePane(ctx context.Context, config PaneConfig) (string, error)

	// SendCommand sends a command string to an existing pane.
	SendCommand(paneID string, command string) error

	// KillPane terminates a pane.
	KillPane(paneID string) error

	// IsPaneAlive checks if a pane is still running.
	IsPaneAlive(paneID string) bool

	// Type returns the backend type string.
	Type() BackendType
}

// PaneConfig configures a new terminal pane.
type PaneConfig struct {
	SessionName string            // tmux session name
	WindowName  string            // window/tab name
	WorkDir     string            // initial working directory
	Env         map[string]string // environment variables to set
	Command     string            // initial command to run
	Hidden      bool              // create but don't focus
}

// ── BackendDetectionResult ───────────────────────────────────────────────────

// BackendDetectionResult describes the detected pane backend environment.
type BackendDetectionResult struct {
	Backend      PaneBackend `json:"-"`
	BackendType  BackendType `json:"backend_type"`
	IsInsideTmux bool        `json:"is_inside_tmux"`
	SetupMessage string      `json:"setup_message,omitempty"` // guidance if setup needed
}

// ── TeammateMessage ──────────────────────────────────────────────────────────

// TeammateMessage is the envelope for a message sent to a teammate.
type TeammateMessage struct {
	From        string      `json:"from"`
	Content     string      `json:"content"`
	MessageType MessageType `json:"message_type"`
	Priority    string      `json:"priority,omitempty"` // "normal", "high", "low"
	ReplyTo     string      `json:"reply_to,omitempty"` // ID of message being replied to
}
