package permission

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Enhanced interactive permission handler — aligned with claude-code-main
// interactiveHandler.ts.
//
// Provides a structured approval flow with tool details, risk assessment,
// and remember-once/always/session options. Replaces the simple bool
// askFn with a rich InteractiveRequest/InteractiveResponse protocol.

// ApprovalDecision represents the user's decision on a permission request.
type ApprovalDecision string

const (
	DecisionApprove       ApprovalDecision = "approve"
	DecisionDeny          ApprovalDecision = "deny"
	DecisionApproveOnce   ApprovalDecision = "approve_once"
	DecisionApproveAlways ApprovalDecision = "approve_always"
	DecisionApproveSession ApprovalDecision = "approve_session"
	DecisionAbort         ApprovalDecision = "abort"
)

// RiskLevel categorizes the risk of a tool operation.
type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

// InteractiveRequest describes a permission approval request with
// rich metadata for the UI to display.
type InteractiveRequest struct {
	// ToolName is the tool requesting permission.
	ToolName string `json:"tool_name"`
	// ToolID is the unique tool use ID.
	ToolID string `json:"tool_id"`
	// Description is a human-readable summary of the operation.
	Description string `json:"description"`
	// Input is the tool's input parameters (for display).
	Input json.RawMessage `json:"input,omitempty"`
	// Risk is the assessed risk level.
	Risk RiskLevel `json:"risk"`
	// RiskReason explains why this risk level was assigned.
	RiskReason string `json:"risk_reason,omitempty"`
	// Timestamp is when the request was created.
	Timestamp time.Time `json:"timestamp"`
	// AvailableDecisions lists which decisions the user can make.
	AvailableDecisions []ApprovalDecision `json:"available_decisions"`
	// PreviousDecision is the last decision for this tool (if any).
	PreviousDecision ApprovalDecision `json:"previous_decision,omitempty"`
	// SessionApprovedTools are tools already approved for this session.
	SessionApprovedTools []string `json:"session_approved_tools,omitempty"`
}

// InteractiveResponse is the user's response to a permission request.
type InteractiveResponse struct {
	Decision ApprovalDecision `json:"decision"`
	// Reason is an optional user-provided reason for the decision.
	Reason string `json:"reason,omitempty"`
}

// InteractiveHandler is the callback interface for interactive permission prompts.
// The UI layer implements this to display rich approval dialogs.
type InteractiveHandler func(ctx context.Context, req *InteractiveRequest) (*InteractiveResponse, error)

// InteractiveChecker wraps a Checker with enhanced interactive approval flow.
type InteractiveChecker struct {
	checker          *Checker
	handler          InteractiveHandler
	sessionApprovals map[string]ApprovalDecision // toolName → decision
	riskAssessor     RiskAssessor
}

// RiskAssessor evaluates the risk level of a tool operation.
type RiskAssessor func(toolName string, input json.RawMessage) (RiskLevel, string)

// NewInteractiveChecker wraps a Checker with interactive approval.
func NewInteractiveChecker(checker *Checker, handler InteractiveHandler) *InteractiveChecker {
	ic := &InteractiveChecker{
		checker:          checker,
		handler:          handler,
		sessionApprovals: make(map[string]ApprovalDecision),
		riskAssessor:     defaultRiskAssessor,
	}

	// Wire the askFn to use our interactive handler.
	checker.SetAskFunc(ic.askFunc)

	return ic
}

// SetRiskAssessor sets a custom risk assessment function.
func (ic *InteractiveChecker) SetRiskAssessor(fn RiskAssessor) {
	ic.riskAssessor = fn
}

// ClearSessionApprovals clears all session-scoped approvals (call on session end).
func (ic *InteractiveChecker) ClearSessionApprovals() {
	ic.sessionApprovals = make(map[string]ApprovalDecision)
}

// IsSessionApproved checks if a tool has been approved for this session.
func (ic *InteractiveChecker) IsSessionApproved(toolName string) bool {
	d, ok := ic.sessionApprovals[toolName]
	return ok && (d == DecisionApproveSession || d == DecisionApproveAlways)
}

// askFunc is the bridge from Checker.askFn to the interactive handler.
func (ic *InteractiveChecker) askFunc(ctx context.Context, toolName, desc string) (bool, error) {
	if ic.handler == nil {
		// No interactive handler — default allow.
		return true, nil
	}

	// Check session approvals.
	if ic.IsSessionApproved(toolName) {
		return true, nil
	}

	risk, riskReason := ic.riskAssessor(toolName, nil)

	req := &InteractiveRequest{
		ToolName:    toolName,
		Description: desc,
		Risk:        risk,
		RiskReason:  riskReason,
		Timestamp:   time.Now(),
		AvailableDecisions: []ApprovalDecision{
			DecisionApprove,
			DecisionDeny,
			DecisionApproveSession,
			DecisionApproveAlways,
			DecisionAbort,
		},
	}

	// Add session-approved tools for context.
	for tool, d := range ic.sessionApprovals {
		if d == DecisionApproveSession || d == DecisionApproveAlways {
			req.SessionApprovedTools = append(req.SessionApprovedTools, tool)
		}
	}

	resp, err := ic.handler(ctx, req)
	if err != nil {
		return false, fmt.Errorf("interactive permission handler: %w", err)
	}

	switch resp.Decision {
	case DecisionApprove, DecisionApproveOnce:
		return true, nil
	case DecisionApproveSession:
		ic.sessionApprovals[toolName] = DecisionApproveSession
		return true, nil
	case DecisionApproveAlways:
		ic.sessionApprovals[toolName] = DecisionApproveAlways
		return true, nil
	case DecisionDeny:
		return false, nil
	case DecisionAbort:
		return false, fmt.Errorf("user aborted permission request for %s", toolName)
	default:
		return false, nil
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Default risk assessment
// ────────────────────────────────────────────────────────────────────────────

// defaultRiskAssessor assigns risk levels based on tool name heuristics.
func defaultRiskAssessor(toolName string, _ json.RawMessage) (RiskLevel, string) {
	switch toolName {
	case "Bash":
		return RiskHigh, "Shell commands can modify the filesystem and system state"
	case "Write", "MultiEdit":
		return RiskMedium, "File write operations modify the filesystem"
	case "Edit":
		return RiskMedium, "File edit operations modify existing files"
	case "Agent":
		return RiskMedium, "Agent spawning creates sub-processes with autonomous behavior"
	case "Read", "Grep", "Glob", "WebSearch":
		return RiskLow, "Read-only operation"
	default:
		return RiskMedium, "Unknown tool — defaulting to medium risk"
	}
}
