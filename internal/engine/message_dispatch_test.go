package engine

import (
	"testing"
	"time"
)

func makeQE() *QueryEngine {
	return NewQueryEngine(&QueryEngineConfig{CWD: "/tmp"})
}

func TestDispatch_Tombstone_Skipped(t *testing.T) {
	qe := makeQE()
	ds := newDispatchState("claude-sonnet-4-6", time.Now())
	msg := &Message{Type: MsgTypeTombstone, Role: RoleAssistant, UUID: "t1"}
	r := qe.dispatchQueryMessage(msg, ds, qe.config)
	if r.Action != dispatchSkip {
		t.Errorf("expected skip, got %d", r.Action)
	}
}

func TestDispatch_Assistant_Yielded(t *testing.T) {
	qe := makeQE()
	ds := newDispatchState("claude-sonnet-4-6", time.Now())
	msg := &Message{
		Role:       RoleAssistant,
		UUID:       "a1",
		StopReason: "end_turn",
		Content:    []*ContentBlock{{Type: ContentTypeText, Text: "hello"}},
	}
	r := qe.dispatchQueryMessage(msg, ds, qe.config)
	if r.Action != dispatchContinue {
		t.Errorf("expected continue, got %d", r.Action)
	}
	if len(r.Yielded) != 1 {
		t.Fatalf("expected 1 yielded, got %d", len(r.Yielded))
	}
	if ds.lastStopReason != "end_turn" {
		t.Errorf("lastStopReason = %s, want end_turn", ds.lastStopReason)
	}
	// Verify it was pushed to mutableMessages
	msgs := qe.GetMessages()
	if len(msgs) != 1 || msgs[0].UUID != "a1" {
		t.Errorf("mutableMessages not updated correctly")
	}
}

func TestDispatch_User_IncrementsTurnCount(t *testing.T) {
	qe := makeQE()
	ds := newDispatchState("claude-sonnet-4-6", time.Now())
	msg := &Message{
		Role: RoleUser,
		Type: MsgTypeUser,
		UUID: "u1",
		Content: []*ContentBlock{{Type: ContentTypeText, Text: "hi"}},
	}
	r := qe.dispatchQueryMessage(msg, ds, qe.config)
	if r.Action != dispatchContinue {
		t.Errorf("expected continue")
	}
	if ds.turnCount != 2 {
		t.Errorf("turnCount = %d, want 2", ds.turnCount)
	}
}

func TestDispatch_Progress_Yielded(t *testing.T) {
	qe := makeQE()
	ds := newDispatchState("claude-sonnet-4-6", time.Now())
	msg := &Message{
		Type: MsgTypeProgress,
		UUID: "p1",
		ProgressData: &ProgressData{ToolUseID: "tool1"},
	}
	r := qe.dispatchQueryMessage(msg, ds, qe.config)
	if len(r.Yielded) != 1 {
		t.Fatalf("expected 1 yielded, got %d", len(r.Yielded))
	}
}

func TestDispatch_StreamEvent_MessageStop_AccumulatesUsage(t *testing.T) {
	qe := makeQE()
	ds := newDispatchState("claude-sonnet-4-6", time.Now())
	ds.currentMessageUsage = EmptyUsage()
	ds.currentMessageUsage.InputTokens = 500
	ds.currentMessageUsage.OutputTokens = 200

	se := &StreamEvent{EventType: "message_stop"}
	r := qe.dispatchQueryMessage(se, ds, qe.config)
	if r.Action != dispatchContinue {
		t.Errorf("expected continue")
	}
	if qe.totalUsage.InputTokens != 500 {
		t.Errorf("total input = %d, want 500", qe.totalUsage.InputTokens)
	}
	if qe.totalUsage.OutputTokens != 200 {
		t.Errorf("total output = %d, want 200", qe.totalUsage.OutputTokens)
	}
}

func TestDispatch_StreamEvent_MessageDelta_CapturesStopReason(t *testing.T) {
	qe := makeQE()
	ds := newDispatchState("claude-sonnet-4-6", time.Now())
	se := &StreamEvent{
		EventType:  "message_delta",
		StopReason: "end_turn",
	}
	qe.dispatchQueryMessage(se, ds, qe.config)
	if ds.lastStopReason != "end_turn" {
		t.Errorf("lastStopReason = %s, want end_turn", ds.lastStopReason)
	}
}

func TestDispatch_StreamEvent_IncludePartial(t *testing.T) {
	cfg := &QueryEngineConfig{CWD: "/tmp", IncludePartialMessages: true}
	qe := NewQueryEngine(cfg)
	ds := newDispatchState("claude-sonnet-4-6", time.Now())
	se := &StreamEvent{EventType: "content_block_delta", Text: "hi"}
	r := qe.dispatchQueryMessage(se, ds, cfg)
	if len(r.Yielded) != 1 {
		t.Fatalf("expected 1 yielded stream event, got %d", len(r.Yielded))
	}
}

func TestDispatch_Attachment_MaxTurnsReached(t *testing.T) {
	qe := makeQE()
	ds := newDispatchState("claude-sonnet-4-6", time.Now())
	msg := &Message{
		Type: MsgTypeAttachment,
		UUID: "att1",
		Attachment: &AttachmentData{
			Type:     "max_turns_reached",
			MaxTurns: 50,
		},
	}
	r := qe.dispatchQueryMessage(msg, ds, qe.config)
	if r.Action != dispatchReturn {
		t.Fatalf("expected return, got %d", r.Action)
	}
	if r.Terminal == nil {
		t.Fatal("expected terminal result")
	}
	if r.Terminal.Subtype != SDKResultErrorMaxTurns {
		t.Errorf("subtype = %s, want error_max_turns", r.Terminal.Subtype)
	}
}

func TestDispatch_Attachment_StructuredOutput(t *testing.T) {
	qe := makeQE()
	ds := newDispatchState("claude-sonnet-4-6", time.Now())
	msg := &Message{
		Type: MsgTypeAttachment,
		UUID: "att2",
		Attachment: &AttachmentData{
			Type: "structured_output",
			Data: map[string]string{"key": "value"},
		},
	}
	qe.dispatchQueryMessage(msg, ds, qe.config)
	if ds.structuredOutputFromTool == nil {
		t.Error("expected structuredOutputFromTool to be set")
	}
}

func TestDispatch_System_CompactBoundary(t *testing.T) {
	qe := makeQE()
	// Pre-populate some messages to verify GC trim
	qe.mu.Lock()
	qe.mutableMessages = append(qe.mutableMessages,
		&Message{UUID: "old1"},
		&Message{UUID: "old2"},
	)
	qe.mu.Unlock()

	ds := newDispatchState("claude-sonnet-4-6", time.Now())
	msg := &Message{
		Type:            MsgTypeSystem,
		UUID:            "cb1",
		Subtype:         "compact_boundary",
		CompactMetadata: &CompactMetadata{Trigger: "auto", PreTokens: 10000},
	}
	r := qe.dispatchQueryMessage(msg, ds, qe.config)
	if len(r.Yielded) != 1 {
		t.Fatalf("expected 1 yielded (compact_boundary), got %d", len(r.Yielded))
	}
	// Verify GC trim: only the boundary msg should remain
	msgs := qe.GetMessages()
	if len(msgs) != 1 {
		t.Errorf("expected 1 msg after GC trim, got %d", len(msgs))
	}
}

func TestDispatch_System_APIRetry(t *testing.T) {
	qe := makeQE()
	ds := newDispatchState("claude-sonnet-4-6", time.Now())
	msg := &Message{
		Type:         MsgTypeSystem,
		UUID:         "api1",
		Subtype:      "api_error",
		RetryAttempt: 1,
		MaxRetries:   3,
		RetryInMs:    1000,
		ErrorStatus:  429,
		ErrorMessage: "overloaded",
	}
	r := qe.dispatchQueryMessage(msg, ds, qe.config)
	// api_error yields a retry message
	found := false
	for _, y := range r.Yielded {
		if retry, ok := y.(*SDKAPIRetryMessage); ok {
			found = true
			if retry.Attempt != 1 {
				t.Errorf("attempt = %d, want 1", retry.Attempt)
			}
		}
	}
	if !found {
		t.Error("expected SDKAPIRetryMessage in yielded")
	}
}

func TestDispatch_ToolUseSummary(t *testing.T) {
	qe := makeQE()
	ds := newDispatchState("claude-sonnet-4-6", time.Now())
	msg := &Message{
		Type:                MsgTypeToolUseSummary,
		UUID:                "tus1",
		Summary:             "Edited file.go",
		PrecedingToolUseIDs: []string{"tu1", "tu2"},
	}
	r := qe.dispatchQueryMessage(msg, ds, qe.config)
	if len(r.Yielded) != 1 {
		t.Fatalf("expected 1 yielded, got %d", len(r.Yielded))
	}
	tus, ok := r.Yielded[0].(*SDKToolUseSummaryMessage)
	if !ok {
		t.Fatalf("expected SDKToolUseSummaryMessage, got %T", r.Yielded[0])
	}
	if tus.Summary != "Edited file.go" {
		t.Errorf("summary = %s", tus.Summary)
	}
	if len(tus.PrecedingToolUseIDs) != 2 {
		t.Errorf("preceding IDs len = %d", len(tus.PrecedingToolUseIDs))
	}
}
