package agent

import (
	"fmt"
	"log/slog"
	"strings"
)

// Structured message handling for SendMessage tool.
// Aligned with claude-code-main's SendMessageTool.ts:887-912.
//
// Supports discriminated union message types:
//   - shutdown_request: leader asks teammate to shut down
//   - shutdown_response: teammate approves or rejects shutdown
//   - plan_approval_response: leader approves or rejects teammate's plan

// StructuredMessageType is the discriminated union tag.
type StructuredMessageType string

const (
	MsgTypeText             StructuredMessageType = "text"
	MsgTypeShutdownRequest  StructuredMessageType = "shutdown_request"
	MsgTypeShutdownApproval StructuredMessageType = "shutdown_approved"
	MsgTypeShutdownReject   StructuredMessageType = "shutdown_rejected"
	MsgTypePlanApproval     StructuredMessageType = "plan_approval"
	MsgTypePlanRejection    StructuredMessageType = "plan_rejection"
)

// StructuredMessage is a typed message between agents.
type StructuredMessage struct {
	Type      StructuredMessageType `json:"type"`
	From      string                `json:"from"`
	To        string                `json:"to"`
	Content   string                `json:"content,omitempty"`
	Summary   string                `json:"summary,omitempty"`
	TeamName  string                `json:"team_name,omitempty"`
	Reason    string                `json:"reason,omitempty"`
	Color     string                `json:"color,omitempty"`
	Timestamp int64                 `json:"timestamp,omitempty"`
}

// StructuredMessageEvent is an observable lifecycle event emitted while handling a structured message.
type StructuredMessageEvent struct {
	Kind    StructuredMessageType
	From    string
	To      string
	Reason  string
	Summary string
	Content string
	Color   string
}

// StructuredMessageHandler processes typed inter-agent messages.
type StructuredMessageHandler struct {
	taskFramework *TaskFramework
	asyncManager  *AsyncLifecycleManager
	teamManager   *TeamManager
	onEvent       func(StructuredMessageEvent)
}

// NewStructuredMessageHandler creates a handler.
func NewStructuredMessageHandler(
	tf *TaskFramework,
	am *AsyncLifecycleManager,
	tm *TeamManager,
) *StructuredMessageHandler {
	return &StructuredMessageHandler{
		taskFramework: tf,
		asyncManager:  am,
		teamManager:   tm,
	}
}

// SetEventCallback registers an observer for structured message lifecycle events.
func (h *StructuredMessageHandler) SetEventCallback(fn func(StructuredMessageEvent)) {
	h.onEvent = fn
}

func (h *StructuredMessageHandler) emitEvent(ev StructuredMessageEvent) {
	if h != nil && h.onEvent != nil {
		h.onEvent(ev)
	}
}

// HandleStructuredMessage routes a structured message to the appropriate handler.
func (h *StructuredMessageHandler) HandleStructuredMessage(msg StructuredMessage) (string, error) {
	switch msg.Type {
	case MsgTypeShutdownRequest:
		return h.handleShutdownRequest(msg)
	case MsgTypeShutdownApproval:
		return h.handleShutdownApproval(msg)
	case MsgTypeShutdownReject:
		return h.handleShutdownRejection(msg)
	case MsgTypePlanApproval:
		return h.handlePlanApproval(msg)
	case MsgTypePlanRejection:
		return h.handlePlanRejection(msg)
	case MsgTypeText:
		return h.handleTextMessage(msg)
	default:
		return "", fmt.Errorf("unknown message type %q", msg.Type)
	}
}

// handleShutdownRequest sends a shutdown request to a teammate.
// Aligned with TS handleShutdownRequest.
func (h *StructuredMessageHandler) handleShutdownRequest(msg StructuredMessage) (string, error) {
	slog.Info("structured_msg: shutdown request",
		slog.String("from", msg.From),
		slog.String("to", msg.To),
		slog.String("reason", msg.Reason),
	)

	// Queue the shutdown request as a pending message for the target agent.
	if h.taskFramework != nil {
		shutdownMsg := fmt.Sprintf("<shutdown-request from=%q reason=%q/>", msg.From, msg.Reason)
		h.taskFramework.QueuePendingMessage(msg.To, shutdownMsg)
	}
	h.emitEvent(StructuredMessageEvent{Kind: MsgTypeShutdownRequest, From: msg.From, To: msg.To, Reason: msg.Reason, Color: msg.Color})

	return fmt.Sprintf("Shutdown request sent to %s.", msg.To), nil
}

// handleShutdownApproval processes a shutdown approval from a teammate.
// Aligned with TS handleShutdownApproval — aborts the teammate's execution.
func (h *StructuredMessageHandler) handleShutdownApproval(msg StructuredMessage) (string, error) {
	slog.Info("structured_msg: shutdown approved",
		slog.String("from", msg.From),
		slog.String("to", msg.To),
	)

	// Cancel the teammate's agent via async manager.
	if h.asyncManager != nil {
		if err := h.asyncManager.Cancel(msg.From); err != nil {
			slog.Warn("structured_msg: failed to stop approved agent",
				slog.String("agent_id", msg.From),
				slog.Any("err", err),
			)
		}
	}
	h.emitEvent(StructuredMessageEvent{Kind: MsgTypeShutdownApproval, From: msg.From, To: msg.To, Summary: msg.Summary, Color: msg.Color})

	return fmt.Sprintf("Teammate %s approved shutdown and is stopping.", msg.From), nil
}

// handleShutdownRejection processes a shutdown rejection — teammate continues working.
func (h *StructuredMessageHandler) handleShutdownRejection(msg StructuredMessage) (string, error) {
	slog.Info("structured_msg: shutdown rejected",
		slog.String("from", msg.From),
		slog.String("reason", msg.Reason),
	)
	h.emitEvent(StructuredMessageEvent{Kind: MsgTypeShutdownReject, From: msg.From, To: msg.To, Reason: msg.Reason, Color: msg.Color})
	return fmt.Sprintf("Teammate %s rejected shutdown: %s", msg.From, msg.Reason), nil
}

// handlePlanApproval processes a plan approval from the team leader.
// Only the team leader can approve plans.
func (h *StructuredMessageHandler) handlePlanApproval(msg StructuredMessage) (string, error) {
	slog.Info("structured_msg: plan approved",
		slog.String("from", msg.From),
		slog.String("to", msg.To),
	)

	if h.taskFramework != nil {
		approvalMsg := fmt.Sprintf("<plan-approved by=%q>%s</plan-approved>", msg.From, msg.Content)
		h.taskFramework.QueuePendingMessage(msg.To, approvalMsg)
	}

	return fmt.Sprintf("Plan approved for %s.", msg.To), nil
}

// handlePlanRejection processes a plan rejection from the team leader.
func (h *StructuredMessageHandler) handlePlanRejection(msg StructuredMessage) (string, error) {
	slog.Info("structured_msg: plan rejected",
		slog.String("from", msg.From),
		slog.String("to", msg.To),
		slog.String("reason", msg.Reason),
	)

	if h.taskFramework != nil {
		rejectionMsg := fmt.Sprintf("<plan-rejected by=%q reason=%q>%s</plan-rejected>",
			msg.From, msg.Reason, msg.Content)
		h.taskFramework.QueuePendingMessage(msg.To, rejectionMsg)
	}

	return fmt.Sprintf("Plan rejected for %s: %s", msg.To, msg.Reason), nil
}

// handleTextMessage handles a plain text message between agents.
func (h *StructuredMessageHandler) handleTextMessage(msg StructuredMessage) (string, error) {
	if h.taskFramework != nil {
		var formattedMsg string
		if msg.From != "" {
			formattedMsg = fmt.Sprintf("[From %s]: %s", msg.From, msg.Content)
		} else {
			formattedMsg = msg.Content
		}
		h.taskFramework.QueuePendingMessage(msg.To, formattedMsg)
	}
	return fmt.Sprintf("Message delivered to %s.", msg.To), nil
}

// ParseStructuredMessageType extracts the message type from an input string.
// Returns MsgTypeText if no structured type is detected.
func ParseStructuredMessageType(input string) StructuredMessageType {
	input = strings.TrimSpace(input)
	typeMap := map[string]StructuredMessageType{
		"shutdown_request":       MsgTypeShutdownRequest,
		"shutdown_approved":      MsgTypeShutdownApproval,
		"shutdown_rejected":      MsgTypeShutdownReject,
		"plan_approval":          MsgTypePlanApproval,
		"plan_rejection":         MsgTypePlanRejection,
		"plan_approval_response": MsgTypePlanApproval,
	}
	if t, ok := typeMap[input]; ok {
		return t
	}
	return MsgTypeText
}
