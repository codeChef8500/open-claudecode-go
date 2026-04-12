package swarm

import (
	"encoding/json"
	"fmt"
	"time"
)

// ── Message types aligned with claude-code-main's teammateMailbox.ts ─────────
//
// The swarm system uses 14 structured message types for inter-agent communication.
// Each message is JSON-encoded with a "type" discriminator field.

// MessageType enumerates all swarm message types.
type MessageType string

const (
	MessageTypePlainText                 MessageType = "text"
	MessageTypeIdleNotification          MessageType = "idle_notification"
	MessageTypePermissionRequest         MessageType = "permission_request"
	MessageTypePermissionResponse        MessageType = "permission_response"
	MessageTypeShutdownRequest           MessageType = "shutdown_request"
	MessageTypeShutdownApproved          MessageType = "shutdown_approved"
	MessageTypeShutdownRejected          MessageType = "shutdown_rejected"
	MessageTypePlanApprovalRequest       MessageType = "plan_approval_request"
	MessageTypePlanApprovalResponse      MessageType = "plan_approval_response"
	MessageTypeTaskAssignment            MessageType = "task_assignment"
	MessageTypeTeamPermissionUpdate      MessageType = "team_permission_update"
	MessageTypeModeSetRequest            MessageType = "mode_set_request"
	MessageTypeSandboxPermissionRequest  MessageType = "sandbox_permission_request"
	MessageTypeSandboxPermissionResponse MessageType = "sandbox_permission_response"
)

// ── Envelope ─────────────────────────────────────────────────────────────────

// MailboxEnvelope is the on-disk/on-wire JSON format for a single mailbox message.
// Aligned with claude-code-main's TeammateMessage type in teammateMailbox.ts.
type MailboxEnvelope struct {
	ID        string          `json:"id"`
	From      string          `json:"from"`
	To        string          `json:"to"`
	Type      MessageType     `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	IsRead    bool            `json:"is_read"`
	Payload   json.RawMessage `json:"payload"` // type-specific body
}

// ── Payload structs ──────────────────────────────────────────────────────────

// PlainTextPayload is the body for MessageTypePlainText.
type PlainTextPayload struct {
	Text    string `json:"text"`
	Summary string `json:"summary,omitempty"` // short UI preview
}

// IdleNotificationPayload is sent by a teammate when it becomes idle.
type IdleNotificationPayload struct {
	AgentName string `json:"agent_name"`
	TaskID    string `json:"task_id,omitempty"`
	Reason    string `json:"reason,omitempty"` // "completed", "waiting", "no_task"
}

// PermissionRequestPayload is sent by a teammate requesting tool permission.
type PermissionRequestPayload struct {
	RequestID   string `json:"request_id"`
	ToolName    string `json:"tool_name"`
	ToolUseID   string `json:"tool_use_id"`
	Input       string `json:"input"`       // JSON string of tool input
	Description string `json:"description"` // human-readable description
	WorkerID    string `json:"worker_id"`
	WorkerName  string `json:"worker_name"`
	WorkerColor string `json:"worker_color,omitempty"`
	TeamName    string `json:"team_name"`
}

// PermissionResponsePayload is the leader's reply to a permission request.
type PermissionResponsePayload struct {
	RequestID         string            `json:"request_id"`
	Decision          string            `json:"decision"` // "allow", "deny", "allow_always"
	UpdatedInput      string            `json:"updated_input,omitempty"`
	PermissionUpdates []PermissionUpdate `json:"permission_updates,omitempty"`
	Feedback          string            `json:"feedback,omitempty"`
}

// PermissionUpdate describes a permission rule change propagated to teammates.
type PermissionUpdate struct {
	Type     string `json:"type"` // "allow", "deny"
	ToolName string `json:"tool_name"`
	Pattern  string `json:"pattern,omitempty"`
}

// ShutdownRequestPayload asks a teammate to shut down.
type ShutdownRequestPayload struct {
	Reason string `json:"reason,omitempty"`
}

// ShutdownApprovedPayload confirms the teammate will shut down.
type ShutdownApprovedPayload struct {
	AgentName string `json:"agent_name"`
	Summary   string `json:"summary,omitempty"` // what was accomplished
}

// ShutdownRejectedPayload indicates the teammate refuses to shut down.
type ShutdownRejectedPayload struct {
	AgentName string `json:"agent_name"`
	Reason    string `json:"reason"`
}

// PlanApprovalRequestPayload asks the leader to approve a plan.
type PlanApprovalRequestPayload struct {
	RequestID string `json:"request_id"`
	PlanText  string `json:"plan_text"`
	AgentName string `json:"agent_name"`
}

// PlanApprovalResponsePayload is the leader's decision on a plan.
type PlanApprovalResponsePayload struct {
	RequestID string `json:"request_id"`
	Approved  bool   `json:"approved"`
	Feedback  string `json:"feedback,omitempty"`
}

// TaskAssignmentPayload assigns a new task to a teammate.
type TaskAssignmentPayload struct {
	TaskID      string `json:"task_id"`
	Description string `json:"description"`
	Priority    string `json:"priority,omitempty"` // "high", "normal", "low"
}

// TeamPermissionUpdatePayload broadcasts permission changes to all teammates.
type TeamPermissionUpdatePayload struct {
	Updates []PermissionUpdate `json:"updates"`
}

// ModeSetRequestPayload tells a teammate to switch permission mode.
type ModeSetRequestPayload struct {
	Mode string `json:"mode"` // "plan", "auto", "default"
}

// SandboxPermissionRequestPayload requests sandbox/network permissions.
type SandboxPermissionRequestPayload struct {
	RequestID   string `json:"request_id"`
	Resource    string `json:"resource"` // URL or resource identifier
	Description string `json:"description"`
	WorkerID    string `json:"worker_id"`
	WorkerName  string `json:"worker_name"`
}

// SandboxPermissionResponsePayload is the leader's sandbox permission decision.
type SandboxPermissionResponsePayload struct {
	RequestID string `json:"request_id"`
	Allowed   bool   `json:"allowed"`
	Feedback  string `json:"feedback,omitempty"`
}

// ── Constructor helpers ──────────────────────────────────────────────────────

// NewEnvelope creates a MailboxEnvelope with the given type and payload.
func NewEnvelope(from, to string, msgType MessageType, payload interface{}) (*MailboxEnvelope, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	return &MailboxEnvelope{
		From:      from,
		To:        to,
		Type:      msgType,
		Timestamp: time.Now(),
		Payload:   raw,
	}, nil
}

// DecodePayload unmarshals the envelope payload into the target struct.
func (e *MailboxEnvelope) DecodePayload(target interface{}) error {
	return json.Unmarshal(e.Payload, target)
}

// ── Type discriminator helpers ───────────────────────────────────────────────

// IsShutdownRequest returns true if the message is a shutdown request.
func (e *MailboxEnvelope) IsShutdownRequest() bool {
	return e.Type == MessageTypeShutdownRequest
}

// IsPermissionRequest returns true if the message is a permission request.
func (e *MailboxEnvelope) IsPermissionRequest() bool {
	return e.Type == MessageTypePermissionRequest
}

// IsPermissionResponse returns true if the message is a permission response.
func (e *MailboxEnvelope) IsPermissionResponse() bool {
	return e.Type == MessageTypePermissionResponse
}

// IsIdleNotification returns true if the message is an idle notification.
func (e *MailboxEnvelope) IsIdleNotification() bool {
	return e.Type == MessageTypeIdleNotification
}

// IsFromLeader returns true if the message is from the team leader.
func (e *MailboxEnvelope) IsFromLeader() bool {
	name, _ := ParseAgentID(e.From)
	return name == TeamLeadName
}

// IsStructured returns true if the message has a non-text type.
func (e *MailboxEnvelope) IsStructured() bool {
	return e.Type != MessageTypePlainText && e.Type != ""
}

// Priority constants for mailbox messages.
const (
	PriorityHigh   = "high"
	PriorityNormal = "normal"
	PriorityLow    = "low"
)
