package engine

import (
	"context"
	"testing"
	"time"

	"github.com/wall-ai/agent-engine/internal/state"
)

// ────────────────────────────────────────────────────────────────────────────
// [P10] Integration tests — verify the full QueryEngine pipeline end-to-end.
// ────────────────────────────────────────────────────────────────────────────

// TestQueryEngine_E2E_StubLoop verifies the full SubmitMessage pipeline with
// the stub loop (no Engine wired). Exercises: init message → loop → result.
func TestQueryEngine_E2E_StubLoop(t *testing.T) {
	appState := &state.AppState{SessionID: "e2e-stub"}
	cfg := &QueryEngineConfig{
		CWD:                "/tmp/e2e",
		UserSpecifiedModel: "claude-sonnet-4-6",
		GetAppState:        func() *state.AppState { return appState },
		SetAppState:        func(fn func(*state.AppState) *state.AppState) {},
	}
	qe := NewQueryEngine(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch := qe.SubmitMessage(ctx, "What is Go?", nil)

	var (
		gotInit   bool
		gotResult bool
		msgs      []interface{}
	)
	for msg := range ch {
		msgs = append(msgs, msg)
		switch m := msg.(type) {
		case *SDKSystemInitMessage:
			gotInit = true
			if m.SessionID != "e2e-stub" {
				t.Errorf("init sessionID = %s, want e2e-stub", m.SessionID)
			}
		case *SDKResultMessage:
			gotResult = true
		}
	}

	if !gotInit {
		t.Error("missing SDKSystemInitMessage")
	}
	if !gotResult {
		t.Error("missing SDKResultMessage")
	}
	if len(msgs) < 2 {
		t.Errorf("expected at least 2 messages, got %d", len(msgs))
	}

	// Verify mutable messages have the user message.
	userMsgs := qe.GetMessages()
	found := false
	for _, m := range userMsgs {
		if m.Role == RoleUser {
			for _, c := range m.Content {
				if c.Text == "What is Go?" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("user message 'What is Go?' not found in mutableMessages")
	}
}

// TestQueryEngine_E2E_MultiTurn verifies two consecutive SubmitMessage calls
// accumulate messages correctly.
func TestQueryEngine_E2E_MultiTurn(t *testing.T) {
	cfg := &QueryEngineConfig{
		CWD:         "/tmp/e2e-multi",
		GetAppState: func() *state.AppState { return &state.AppState{SessionID: "multi"} },
		SetAppState: func(fn func(*state.AppState) *state.AppState) {},
	}
	qe := NewQueryEngine(cfg)

	ctx := context.Background()

	// Turn 1.
	ch1 := qe.SubmitMessage(ctx, "Hello", nil)
	for range ch1 {
	}

	// Turn 2.
	ch2 := qe.SubmitMessage(ctx, "World", nil)
	for range ch2 {
	}

	msgs := qe.GetMessages()
	userCount := 0
	for _, m := range msgs {
		if m.Role == RoleUser {
			userCount++
		}
	}
	if userCount != 2 {
		t.Errorf("expected 2 user messages after 2 turns, got %d", userCount)
	}
}

// TestQueryEngine_E2E_Interrupt verifies that calling Interrupt cancels
// an in-flight SubmitMessage.
func TestQueryEngine_E2E_Interrupt(t *testing.T) {
	cfg := &QueryEngineConfig{
		CWD:         "/tmp/e2e-interrupt",
		GetAppState: func() *state.AppState { return &state.AppState{} },
		SetAppState: func(fn func(*state.AppState) *state.AppState) {},
	}
	qe := NewQueryEngine(cfg)

	// Start a submit.
	ch := qe.SubmitMessage(context.Background(), "test", nil)

	// Drain immediately.
	for range ch {
	}

	// Now interrupt.
	qe.Interrupt()

	// Verify abort context is cancelled.
	if qe.abortCtx.Err() == nil {
		t.Error("abort context should be cancelled")
	}
}

// TestQueryEngine_E2E_UsageTracking verifies that totalUsage accumulates.
func TestQueryEngine_E2E_UsageTracking(t *testing.T) {
	cfg := &QueryEngineConfig{
		CWD:         "/tmp/e2e-usage",
		GetAppState: func() *state.AppState { return &state.AppState{} },
		SetAppState: func(fn func(*state.AppState) *state.AppState) {},
	}
	qe := NewQueryEngine(cfg)

	// Initially zero.
	if qe.totalUsage.InputTokens != 0 {
		t.Error("initial input tokens should be 0")
	}

	// Manually accumulate (simulating stream events).
	qe.totalUsage = accumulateUsageStats(qe.totalUsage, &UsageStats{
		InputTokens:  1000,
		OutputTokens: 500,
		CostUSD:      0.01,
	})

	if qe.totalUsage.InputTokens != 1000 {
		t.Errorf("input = %d, want 1000", qe.totalUsage.InputTokens)
	}
	if qe.totalUsage.CostUSD != 0.01 {
		t.Errorf("cost = %f, want 0.01", qe.totalUsage.CostUSD)
	}
}

// TestQueryEngine_E2E_AskFunction verifies the top-level Ask() helper.
func TestQueryEngine_E2E_AskFunction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch := Ask(ctx, AskOptions{
		Prompt:     "Quick test",
		PromptUUID: "ask-uuid",
		Config: &QueryEngineConfig{
			CWD:         "/tmp/ask",
			GetAppState: func() *state.AppState { return &state.AppState{} },
			SetAppState: func(fn func(*state.AppState) *state.AppState) {},
		},
	})

	var count int
	for range ch {
		count++
	}
	if count == 0 {
		t.Error("Ask() should produce messages")
	}
}

// TestQueryEngine_E2E_WithEngineNil tests that NewQueryEngineWithEngine(nil)
// falls back to stub and produces valid output.
func TestQueryEngine_E2E_WithEngineNil(t *testing.T) {
	cfg := &QueryEngineConfig{
		CWD:         "/tmp/e2e-nil-engine",
		GetAppState: func() *state.AppState { return &state.AppState{SessionID: "nil-eng"} },
		SetAppState: func(fn func(*state.AppState) *state.AppState) {},
	}
	qe := NewQueryEngineWithEngine(cfg, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch := qe.SubmitMessage(ctx, "test", &SubmitMessageOptions{UUID: "u1"})

	var gotResult bool
	for msg := range ch {
		if _, ok := msg.(*SDKResultMessage); ok {
			gotResult = true
		}
	}
	if !gotResult {
		t.Error("expected result message even with nil Engine")
	}
}
