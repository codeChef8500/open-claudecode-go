package engine

import (
	"context"

	"github.com/wall-ai/agent-engine/internal/state"
)

// ────────────────────────────────────────────────────────────────────────────
// [P6.T1] QueryEngineConfig — mirrors claude-code-main QueryEngine.ts
// QueryEngineConfig type.  One instance per conversation session.
// ────────────────────────────────────────────────────────────────────────────

// QueryEngineConfig holds all configuration needed to create a QueryEngine.
// It is the Go equivalent of the TS QueryEngineConfig type.
type QueryEngineConfig struct {
	// CWD is the working directory for this conversation.
	CWD string

	// Tools is the tool registry for this session.
	Tools []Tool

	// Commands holds the registered slash commands.
	Commands []Command

	// MCPClients is the set of connected MCP server connections.
	MCPClients []MCPClientConnection

	// Agents is the list of available agent definitions.
	Agents []AgentDefinition

	// CanUseTool is the permission checker for tool use.
	CanUseTool CanUseToolFn

	// GetAppState returns the current application state.
	GetAppState func() *state.AppState
	// SetAppState atomically updates the application state.
	SetAppState func(fn func(*state.AppState) *state.AppState)

	// InitialMessages, if non-nil, seeds the conversation history.
	InitialMessages []*Message

	// ReadFileCache is the shared file state cache for dedup/unchanged detection.
	ReadFileCache *FileStateCache

	// CustomSystemPrompt replaces the default system prompt entirely.
	CustomSystemPrompt string
	// AppendSystemPrompt is extra text appended after the system prompt.
	AppendSystemPrompt string

	// UserSpecifiedModel overrides the default model for the main loop.
	UserSpecifiedModel string
	// FallbackModel is the model to switch to on FallbackTriggeredError.
	FallbackModel string

	// ThinkingConfig holds extended thinking configuration.
	ThinkingConfig *ThinkingConfig

	// MaxTurns caps the number of agentic turns per submitMessage.
	MaxTurns int
	// MaxBudgetUSD caps the total USD cost for the conversation.
	MaxBudgetUSD *float64
	// TaskBudget caps total tokens for a task-based query.
	TaskBudget *TaskBudget

	// JSONSchema, if non-nil, forces structured (JSON) output.
	JSONSchema map[string]interface{}

	// Verbose enables debug logging.
	Verbose bool

	// ReplayUserMessages enables replaying of user messages from transcript.
	ReplayUserMessages bool

	// IncludePartialMessages includes incomplete streaming messages in output.
	IncludePartialMessages bool

	// SetSDKStatus is called to update the SDK-level session status.
	SetSDKStatus func(status *string)

	// AbortController holds a cancel-able context for the engine.
	AbortCtx    context.Context
	AbortCancel context.CancelFunc

	// HandleElicitation is called when an MCP tool requests user input.
	HandleElicitation func(ctx context.Context, toolUseID, question string) (string, error)
}

// Command is a registered slash command that can be invoked by the user.
type Command struct {
	Name        string
	Description string
	Handler     func(ctx context.Context, args string) (string, error)
}

// MCPClientConnection describes a connected MCP server.
type MCPClientConnection struct {
	Name         string
	Status       string // "connected", "failed", "needs-auth", "pending", "disabled"
	Instructions string // server-provided instructions (empty if none)
	Tools        []MCPTool
}

// MCPTool is a tool provided by an MCP server.
type MCPTool struct {
	Name        string
	Description string
}

// CanUseToolFn checks whether a tool invocation is permitted.
// Returns a PermissionResult indicating allow/deny + optional updated input.
type CanUseToolFn func(
	tool Tool,
	input map[string]interface{},
	tuc *ToolUseContext,
	assistantMessage *Message,
	toolUseID string,
	forceDecision *PermissionDecision,
) (*PermissionResult, error)

// PermissionDecision is a forced decision passed to CanUseToolFn.
type PermissionDecision struct {
	Behavior string // "allow" or "deny"
	Reason   string
}

// PermissionResult is the output of a CanUseToolFn check.
type PermissionResult struct {
	Behavior     string                 `json:"behavior"` // "allow" or "deny"
	Message      string                 `json:"message,omitempty"`
	Interrupt    bool                   `json:"interrupt,omitempty"`
	UpdatedInput map[string]interface{} `json:"updatedInput,omitempty"`
}
