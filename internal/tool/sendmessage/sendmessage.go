package sendmessage

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/wall-ai/agent-engine/internal/agent"
	agentswarm "github.com/wall-ai/agent-engine/internal/agent/swarm"
	"github.com/wall-ai/agent-engine/internal/engine"
	"github.com/wall-ai/agent-engine/internal/tool"
)

// Input is the JSON input schema for the SendMessage tool.
// Extended to support mailbox, broadcast, and structured messages.
type Input struct {
	Message string `json:"message"`
	// Target agent ID for multi-agent message passing.
	To string `json:"to,omitempty"`
	// MessageType for structured messages: "text", "shutdown_request",
	// "shutdown_response", "shutdown_approved", "shutdown_rejected",
	// "plan_approval_request", "plan_approval_response", "plan_approval",
	// "plan_rejection".
	MessageType string `json:"message_type,omitempty"`
	// Priority: "normal", "high", "low".
	Priority string `json:"priority,omitempty"`
	// Approved is used by structured approval messages.
	Approved *bool `json:"approved,omitempty"`
}

// MailboxSender abstracts mailbox delivery so we don't import agent directly.
type MailboxSender interface {
	// Send delivers a message from one agent to another via mailbox.
	Send(from, to, text string, priority string, replyTo string) (string, error)
	// Broadcast sends a message to all agents in a team.
	Broadcast(from, teamName, text string) error
	// TeamMembers returns agent IDs in a team (for broadcast).
	TeamMembers(teamName string) []string
}

type structuredMailboxSender interface {
	SendEnvelope(from, to string, env *agentswarm.MailboxEnvelope) error
}

// StructuredSendEvent describes a structured inter-agent message that was emitted.
type StructuredSendEvent struct {
	MessageType string
	From        string
	To          string
	Message     string
	Approved    *bool
}

// SendMessageTool sends messages between agents.
type SendMessageTool struct {
	tool.BaseTool
	sender   MailboxSender
	asyncMgr *agent.AsyncLifecycleManager
	// ResolveAgentName resolves a human name to an agent ID.
	// Aligned with TS agentNameRegistry.get(input.to).
	ResolveAgentName func(name string) string
	OnStructuredSend func(StructuredSendEvent)
}

// New creates a SendMessageTool without mailbox integration (legacy).
func New() *SendMessageTool { return &SendMessageTool{} }

// NewWithMailbox creates a SendMessageTool with full mailbox integration.
func NewWithMailbox(sender MailboxSender) *SendMessageTool {
	return &SendMessageTool{sender: sender}
}

// NewWithDeps creates a SendMessageTool with all dependencies wired.
func NewWithDeps(sender MailboxSender, asyncMgr *agent.AsyncLifecycleManager) *SendMessageTool {
	return &SendMessageTool{sender: sender, asyncMgr: asyncMgr}
}

// NewWithAllDeps creates a SendMessageTool with full dependencies including name resolution.
func NewWithAllDeps(sender MailboxSender, asyncMgr *agent.AsyncLifecycleManager, resolver func(string) string) *SendMessageTool {
	return &SendMessageTool{sender: sender, asyncMgr: asyncMgr, ResolveAgentName: resolver}
}

// SetStructuredSendCallback registers an observer for structured sends.
func (t *SendMessageTool) SetStructuredSendCallback(fn func(StructuredSendEvent)) {
	t.OnStructuredSend = fn
}

func (t *SendMessageTool) Name() string           { return "SendMessage" }
func (t *SendMessageTool) UserFacingName() string { return "send_message" }
func (t *SendMessageTool) Description() string {
	return "Send a message to the parent agent or another agent."
}
func (t *SendMessageTool) IsReadOnly(_ json.RawMessage) bool        { return true }
func (t *SendMessageTool) IsConcurrencySafe(_ json.RawMessage) bool { return true }
func (t *SendMessageTool) MaxResultSizeChars() int                  { return 0 }
func (t *SendMessageTool) IsEnabled(uctx *tool.UseContext) bool {
	return uctx.AgentID != ""
}

func (t *SendMessageTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type":"object",
		"properties":{
			"message":{"type":"string","description":"Message content to send."},
			"to":{"type":"string","description":"Target agent ID, 'parent' for parent agent, or '*' to broadcast to team. Omit to send to parent."},
			"message_type":{"type":"string","enum":["text","shutdown_request","shutdown_response","shutdown_approved","shutdown_rejected","plan_approval_request","plan_approval_response","plan_approval","plan_rejection"],"description":"Message type. Default: text."},
			"priority":{"type":"string","enum":["normal","high","low"],"description":"Message priority. Default: normal."}
		},
		"required":["message"]
	}`)
}

func (t *SendMessageTool) Prompt(_ *tool.UseContext) string {
	return `Send a message to the parent agent, another agent, or broadcast to all agents.

Usage:
- Use this tool for inter-agent communication in multi-agent setups
- Specify the "to" field with the target agent name or ID; omit to send to parent
- Use "*" as the "to" value to broadcast to all agents
- Messages can be plain text or structured (shutdown_request, shutdown_response, plan_approval_response)
- Include a short summary for UI preview when sending plain text messages`
}

func (t *SendMessageTool) ValidateInput(_ context.Context, input json.RawMessage) error {
	var in Input
	if err := json.Unmarshal(input, &in); err != nil {
		return fmt.Errorf("invalid input: %w", err)
	}
	if in.Message == "" {
		return fmt.Errorf("message must not be empty")
	}
	return nil
}

func (t *SendMessageTool) CheckPermissions(_ context.Context, input json.RawMessage, _ *tool.UseContext) error {
	var in Input
	if err := json.Unmarshal(input, &in); err != nil {
		return fmt.Errorf("invalid input: %w", err)
	}
	if in.Message == "" {
		return fmt.Errorf("message must not be empty")
	}
	return nil
}

func (t *SendMessageTool) Call(_ context.Context, input json.RawMessage, uctx *tool.UseContext) (<-chan *engine.ContentBlock, error) {
	var in Input
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}

	// Default message type.
	if in.MessageType == "" {
		in.MessageType = "text"
	}
	if in.Priority == "" {
		in.Priority = "normal"
	}

	// Format structured messages.
	msgContent := t.formatMessage(in)

	ch := make(chan *engine.ContentBlock, 2)
	go func() {
		defer close(ch)

		fromAgent := uctx.AgentID

		// ── Route the message ──────────────────────────────────────────
		switch {
		case in.To == "*":
			// Broadcast to team.
			resultText := t.handleBroadcast(fromAgent, uctx.TeammateID, uctx.AgentType, msgContent)
			ch <- &engine.ContentBlock{Type: engine.ContentTypeText, Text: resultText}

		case in.To == "" || in.To == "parent":
			// Send to parent via notification callback.
			if uctx.SendNotification != nil {
				uctx.SendNotification(msgContent)
			}
			ch <- &engine.ContentBlock{Type: engine.ContentTypeText, Text: "Message sent to parent."}

		default:
			// Resolve agent name → ID (aligned with TS agentNameRegistry).
			targetID := in.To
			if t.ResolveAgentName != nil {
				if resolved := t.ResolveAgentName(in.To); resolved != "" {
					targetID = resolved
					slog.Debug("sendmessage: resolved name to agent ID",
						slog.String("name", in.To),
						slog.String("agent_id", resolved))
				}
			}

			// Try routing to a running in-process async agent first.
			if t.asyncMgr != nil {
				if routed := t.tryRouteToAsyncAgent(targetID, msgContent); routed {
					ch <- &engine.ContentBlock{Type: engine.ContentTypeText, Text: fmt.Sprintf("Message delivered to running agent %s.", in.To)}
					return
				}
			}
			if resultText, ok := t.handleStructuredSend(fromAgent, targetID, in); ok {
				ch <- &engine.ContentBlock{Type: engine.ContentTypeText, Text: resultText}
				return
			}
			// Fall back to mailbox.
			resultText := t.handleDirectSend(fromAgent, targetID, msgContent, in.Priority)
			ch <- &engine.ContentBlock{Type: engine.ContentTypeText, Text: resultText}
		}
	}()
	return ch, nil
}

// handleBroadcast sends a message to all team members.
func (t *SendMessageTool) handleBroadcast(from, teammateID, teamName, message string) string {
	if teamName == "" && teammateID != "" {
		if idx := strings.LastIndex(teammateID, "@"); idx >= 0 && idx+1 < len(teammateID) {
			teamName = teammateID[idx+1:]
		}
	}
	if teamName == "" {
		return "Broadcast failed: no team context available."
	}
	if t.sender != nil {
		err := t.sender.Broadcast(from, teamName, message)
		if err != nil {
			slog.Warn("sendmessage: broadcast failed", slog.Any("err", err))
			return fmt.Sprintf("Broadcast failed: %v", err)
		}
		return fmt.Sprintf("Message broadcast to team %s.", teamName)
	}
	return "Broadcast not available (no mailbox configured)."
}

// handleDirectSend sends a message to a specific agent via mailbox.
func (t *SendMessageTool) handleDirectSend(from, to, message, priority string) string {
	if t.sender != nil {
		msgID, err := t.sender.Send(from, to, message, priority, "")
		if err != nil {
			slog.Warn("sendmessage: send failed",
				slog.String("to", to),
				slog.Any("err", err))
			return fmt.Sprintf("Send failed: %v", err)
		}
		return fmt.Sprintf("Message sent to %s (id: %s).", to, msgID)
	}
	return fmt.Sprintf("Message to %s queued (no mailbox configured).", to)
}

func (t *SendMessageTool) handleStructuredSend(from, to string, in Input) (string, bool) {
	if in.MessageType == "" || in.MessageType == "text" {
		return "", false
	}
	structuredSender, ok := t.sender.(structuredMailboxSender)
	if !ok {
		return "", false
	}

	var (
		env *agentswarm.MailboxEnvelope
		err error
	)
	switch in.MessageType {
	case "shutdown_request":
		env, err = agentswarm.NewEnvelope(from, to, agentswarm.MessageTypeShutdownRequest, agentswarm.ShutdownRequestPayload{Reason: in.Message})
	case "shutdown_response", "shutdown_approved", "shutdown_rejected":
		if in.Approved != nil && !*in.Approved {
			env, err = agentswarm.NewEnvelope(from, to, agentswarm.MessageTypeShutdownRejected, agentswarm.ShutdownRejectedPayload{Reason: in.Message})
		} else {
			env, err = agentswarm.NewEnvelope(from, to, agentswarm.MessageTypeShutdownApproved, agentswarm.ShutdownApprovedPayload{Summary: in.Message})
		}
	case "plan_approval_request":
		env, err = agentswarm.NewEnvelope(from, to, agentswarm.MessageTypePlanApprovalRequest, agentswarm.PlanApprovalRequestPayload{PlanText: in.Message, AgentName: from})
	case "plan_approval_response", "plan_approval", "plan_rejection":
		approved := in.Approved == nil || *in.Approved
		if in.MessageType == "plan_rejection" {
			approved = false
		}
		env, err = agentswarm.NewEnvelope(from, to, agentswarm.MessageTypePlanApprovalResponse, agentswarm.PlanApprovalResponsePayload{Approved: approved, Feedback: in.Message})
	default:
		return "", false
	}
	if err != nil {
		return fmt.Sprintf("Structured send failed: %v", err), true
	}
	if err := structuredSender.SendEnvelope(from, to, env); err != nil {
		return fmt.Sprintf("Structured send failed: %v", err), true
	}
	if t.OnStructuredSend != nil {
		t.OnStructuredSend(StructuredSendEvent{
			MessageType: in.MessageType,
			From:        from,
			To:          to,
			Message:     in.Message,
			Approved:    in.Approved,
		})
	}
	return fmt.Sprintf("Structured message sent to %s.", to), true
}

// tryRouteToAsyncAgent attempts to deliver a message to an in-process async agent.
// For running agents: pushes to notification queue.
// For stopped agents: auto-resumes in background (aligned with TS:822-844).
// Returns true if delivery succeeded, and an optional status message.
func (t *SendMessageTool) tryRouteToAsyncAgent(to, message string) bool {
	if t.asyncMgr == nil {
		return false
	}

	// Check if the target agent exists.
	status, err := t.asyncMgr.GetStatus(to)
	if err != nil {
		return false
	}

	// Running/pending → push message to notification queue.
	if status == agent.AsyncStatusRunning || status == agent.AsyncStatusPending {
		if t.asyncMgr.QueuePendingMessage(to, message) {
			slog.Info("sendmessage: queued pending message for async agent",
				slog.String("to", to))
			return true
		}
		t.asyncMgr.PushNotification(to, agent.Notification{
			Type:    agent.NotificationTypeMessage,
			AgentID: to,
			Message: message,
		})
		slog.Info("sendmessage: routed to running async agent",
			slog.String("to", to))
		return true
	}

	// Stopped/done/failed/cancelled → auto-resume with message as new prompt.
	// Aligned with TS resumeAgentBackground() in SendMessageTool.ts:822-844.
	ctx := context.Background()
	_, err = t.asyncMgr.Resume(ctx, to, message)
	if err != nil {
		slog.Warn("sendmessage: failed to resume stopped agent",
			slog.String("to", to),
			slog.Any("err", err))
		return false
	}

	slog.Info("sendmessage: auto-resumed stopped agent",
		slog.String("to", to),
		slog.String("status", string(status)))
	return true
}

// formatMessage formats the message content based on type.
func (t *SendMessageTool) formatMessage(in Input) string {
	switch in.MessageType {
	case "shutdown_request":
		return `{"type":"shutdown_request"}`
	case "shutdown_response":
		decision := "approved"
		if in.Approved != nil && !*in.Approved {
			decision = "rejected"
		}
		return fmt.Sprintf(`{"type":"shutdown_response","decision":%q,"message":%q}`, decision, in.Message)
	case "plan_approval_response":
		approved := in.Approved == nil || *in.Approved
		return fmt.Sprintf(`{"type":"plan_approval_response","approved":%t,"message":%q}`, approved, in.Message)
	default:
		if strings.TrimSpace(in.Message) == "" {
			return `{"type":"text","message":""}`
		}
		return in.Message
	}
}
