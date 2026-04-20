package engine

import (
	"testing"
	"time"
)

func TestCheckBudgetExceeded_NoBudget(t *testing.T) {
	qe := makeQE()
	ds := newDispatchState("claude-sonnet-4-6", time.Now())
	if r := qe.checkBudgetExceeded(ds, qe.config); r != nil {
		t.Error("expected nil when no budget set")
	}
}

func TestCheckBudgetExceeded_UnderBudget(t *testing.T) {
	budget := 10.0
	cfg := &QueryEngineConfig{CWD: "/tmp", MaxBudgetUSD: &budget}
	qe := NewQueryEngine(cfg)
	ds := newDispatchState("claude-sonnet-4-6", time.Now())
	// totalCostUSD returns 0 by default
	if r := qe.checkBudgetExceeded(ds, cfg); r != nil {
		t.Error("expected nil when under budget")
	}
}

func TestCheckStructuredOutputRetryLimit_NoSchema(t *testing.T) {
	qe := makeQE()
	ds := newDispatchState("claude-sonnet-4-6", time.Now())
	msg := &Message{Role: RoleUser, Type: MsgTypeUser}
	if r := qe.checkStructuredOutputRetryLimit(ds, qe.config, msg); r != nil {
		t.Error("expected nil when no JSON schema")
	}
}

func TestCheckStructuredOutputRetryLimit_UnderLimit(t *testing.T) {
	schema := map[string]interface{}{"type": "object"}
	cfg := &QueryEngineConfig{CWD: "/tmp", JSONSchema: schema}
	qe := NewQueryEngine(cfg)
	ds := newDispatchState("claude-sonnet-4-6", time.Now())
	msg := &Message{Role: RoleUser, Type: MsgTypeUser}
	if r := qe.checkStructuredOutputRetryLimit(ds, cfg, msg); r != nil {
		t.Error("expected nil when under limit")
	}
}

func TestBuildFinalResult_Success(t *testing.T) {
	qe := makeQE()
	// Add an assistant message with text
	qe.mu.Lock()
	qe.mutableMessages = append(qe.mutableMessages, &Message{
		Role:       RoleAssistant,
		StopReason: "end_turn",
		Content:    []*ContentBlock{{Type: ContentTypeText, Text: "Done"}},
	})
	qe.mu.Unlock()

	ds := newDispatchState("claude-sonnet-4-6", time.Now())
	ds.lastStopReason = "end_turn"
	result := qe.buildFinalResult(ds, qe.config)
	if result.Subtype != SDKResultSuccess {
		t.Errorf("subtype = %s, want success", result.Subtype)
	}
	if result.Result != "Done" {
		t.Errorf("result = %s, want Done", result.Result)
	}
}

func TestBuildFinalResult_Error(t *testing.T) {
	qe := makeQE()
	// Empty messages => not successful
	ds := newDispatchState("claude-sonnet-4-6", time.Now())
	ds.lastStopReason = ""
	result := qe.buildFinalResult(ds, qe.config)
	if result.Subtype != SDKResultErrorDuringExecution {
		t.Errorf("subtype = %s, want error_during_execution", result.Subtype)
	}
}

func TestBuildFinalResult_StructuredOutput(t *testing.T) {
	qe := makeQE()
	qe.mu.Lock()
	qe.mutableMessages = append(qe.mutableMessages, &Message{
		Role:       RoleAssistant,
		StopReason: "end_turn",
		Content:    []*ContentBlock{{Type: ContentTypeText, Text: "result"}},
	})
	qe.mu.Unlock()

	ds := newDispatchState("claude-sonnet-4-6", time.Now())
	ds.lastStopReason = "end_turn"
	ds.structuredOutputFromTool = map[string]string{"key": "value"}
	result := qe.buildFinalResult(ds, qe.config)
	if result.StructuredOutput == nil {
		t.Error("expected structured output to be set")
	}
}

func TestCountToolCallsByName(t *testing.T) {
	msgs := []*Message{
		{
			Role: RoleAssistant,
			Content: []*ContentBlock{
				{Type: ContentTypeToolUse, ToolName: "SyntheticOutput"},
				{Type: ContentTypeToolUse, ToolName: "Bash"},
			},
		},
		{
			Role: RoleAssistant,
			Content: []*ContentBlock{
				{Type: ContentTypeToolUse, ToolName: "SyntheticOutput"},
			},
		},
		{
			Role: RoleUser,
			Content: []*ContentBlock{
				{Type: ContentTypeToolUse, ToolName: "SyntheticOutput"},
			},
		},
	}
	count := countToolCallsByName(msgs, "SyntheticOutput")
	if count != 2 {
		t.Errorf("count = %d, want 2 (user messages excluded)", count)
	}
}
