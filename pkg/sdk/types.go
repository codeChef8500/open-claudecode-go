// Package sdk exports the public types that consumers of the Agent Engine SDK need.
// All core types are aliases of the internal engine types to preserve type identity.
package sdk

import (
	"context"

	"github.com/wall-ai/agent-engine/internal/engine"
)

// Re-export StreamEvent and StreamEventType so callers need only import this package.
type StreamEvent = engine.StreamEvent
type StreamEventType = engine.StreamEventType

// Re-export event type constants.
const (
	EventTextDelta     = engine.EventTextDelta
	EventTextComplete  = engine.EventTextComplete
	EventToolUse       = engine.EventToolUse
	EventToolResult    = engine.EventToolResult
	EventThinking      = engine.EventThinking
	EventUsage         = engine.EventUsage
	EventError         = engine.EventError
	EventDone          = engine.EventDone
	EventSystemMessage = engine.EventSystemMessage
)

// Re-export message and content types.
type Message = engine.Message
type MessageRole = engine.MessageRole
type ContentBlock = engine.ContentBlock
type ContentType = engine.ContentType
type UsageStats = engine.UsageStats

// Re-export config types.
type EngineConfig = engine.EngineConfig
type QueryParams = engine.QueryParams
type QuerySource = engine.QuerySource

// Re-export query sources.
const (
	QuerySourceUser  = engine.QuerySourceUser
	QuerySourceAgent = engine.QuerySourceAgent
)

// Re-export role constants.
const (
	RoleUser      = engine.RoleUser
	RoleAssistant = engine.RoleAssistant
	RoleSystem    = engine.RoleSystem
)

// Re-export content type constants.
const (
	ContentTypeText       = engine.ContentTypeText
	ContentTypeToolUse    = engine.ContentTypeToolUse
	ContentTypeToolResult = engine.ContentTypeToolResult
	ContentTypeImage      = engine.ContentTypeImage
	ContentTypeThinking   = engine.ContentTypeThinking
)

// ── Tool types ───────────────────────────────────────────────────────────────

// ToolDefinition describes a tool's schema for SDK consumers.
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema,omitempty"`
}

// ToolResult is the result of a tool execution.
type ToolResult struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

// ── Conversation types ───────────────────────────────────────────────────────

// ConversationTurn represents a single turn in a conversation (user + assistant).
type ConversationTurn struct {
	UserMessage      *Message `json:"user_message"`
	AssistantMessage *Message `json:"assistant_message,omitempty"`
	TurnIndex        int      `json:"turn_index"`
}

// ── Session types ────────────────────────────────────────────────────────────

// SessionInfo provides public session information.
type SessionInfo struct {
	ID        string `json:"id"`
	Model     string `json:"model"`
	WorkDir   string `json:"work_dir"`
	TurnCount int    `json:"turn_count"`
}

// ── Callback types ───────────────────────────────────────────────────────────

// PermissionRequest describes a tool permission request to the SDK consumer.
type PermissionRequest struct {
	ToolName    string `json:"tool_name"`
	Description string `json:"description"`
}

// PermissionHandler is a callback for tool permission requests.
// Return true to allow, false to deny.
type PermissionHandler func(ctx context.Context, req PermissionRequest) bool
