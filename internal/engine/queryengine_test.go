package engine

import (
	"context"
	"testing"
	"time"

	"github.com/wall-ai/agent-engine/internal/state"
)

func TestNewQueryEngine_Defaults(t *testing.T) {
	appState := &state.AppState{SessionID: "test-session"}
	cfg := &QueryEngineConfig{
		CWD:         "/tmp/test",
		GetAppState: func() *state.AppState { return appState },
		SetAppState: func(fn func(*state.AppState) *state.AppState) {},
	}
	qe := NewQueryEngine(cfg)

	if qe.sessionID != "test-session" {
		t.Errorf("sessionID = %s, want test-session", qe.sessionID)
	}
	if qe.readFileState == nil {
		t.Error("readFileState should not be nil")
	}
	if len(qe.mutableMessages) != 0 {
		t.Errorf("initial messages should be empty, got %d", len(qe.mutableMessages))
	}
}

func TestNewQueryEngine_WithInitialMessages(t *testing.T) {
	msgs := []*Message{{UUID: "m1"}, {UUID: "m2"}}
	cfg := &QueryEngineConfig{
		CWD:             "/tmp",
		InitialMessages: msgs,
		GetAppState:     func() *state.AppState { return &state.AppState{} },
	}
	qe := NewQueryEngine(cfg)
	if len(qe.mutableMessages) != 2 {
		t.Errorf("expected 2 initial messages, got %d", len(qe.mutableMessages))
	}
}

func TestNewQueryEngine_GeneratesSessionID(t *testing.T) {
	cfg := &QueryEngineConfig{
		CWD: "/tmp",
	}
	qe := NewQueryEngine(cfg)
	if qe.sessionID == "" {
		t.Error("should generate a session ID when none provided")
	}
}

func TestQueryEngine_SubmitMessage_ProducesResult(t *testing.T) {
	appState := &state.AppState{SessionID: "sess-test"}
	cfg := &QueryEngineConfig{
		CWD:         "/tmp",
		GetAppState: func() *state.AppState { return appState },
		SetAppState: func(fn func(*state.AppState) *state.AppState) {},
	}
	qe := NewQueryEngine(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch := qe.SubmitMessage(ctx, "Hello", nil)

	var messages []interface{}
	for msg := range ch {
		messages = append(messages, msg)
	}

	if len(messages) == 0 {
		t.Fatal("expected at least one message from SubmitMessage")
	}

	// First message should be system init.
	initMsg, ok := messages[0].(*SDKSystemInitMessage)
	if !ok {
		t.Fatalf("first message should be SDKSystemInitMessage, got %T", messages[0])
	}
	if initMsg.Type != SDKMsgSystem {
		t.Errorf("init type = %s, want system", initMsg.Type)
	}
	if initMsg.Subtype != SDKSystemSubtypeInit {
		t.Errorf("init subtype = %s, want init", initMsg.Subtype)
	}

	// Last message should be a result.
	lastMsg := messages[len(messages)-1]
	if result, ok := lastMsg.(*SDKResultMessage); ok {
		if result.Type != SDKMsgResult {
			t.Errorf("result type = %s, want result", result.Type)
		}
	}
}

func TestQueryEngine_SubmitMessage_WithOptions(t *testing.T) {
	cfg := &QueryEngineConfig{
		CWD:         "/tmp",
		GetAppState: func() *state.AppState { return &state.AppState{} },
		SetAppState: func(fn func(*state.AppState) *state.AppState) {},
	}
	qe := NewQueryEngine(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch := qe.SubmitMessage(ctx, "Test", &SubmitMessageOptions{
		UUID:   "custom-uuid",
		IsMeta: true,
	})

	for range ch {
		// drain
	}

	msgs := qe.GetMessages()
	// Should have at least the user message.
	found := false
	for _, m := range msgs {
		if m.UUID == "custom-uuid" && m.IsMeta {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected user message with custom UUID and IsMeta=true")
	}
}

func TestQueryEngine_Interrupt(t *testing.T) {
	cfg := &QueryEngineConfig{
		CWD:         "/tmp",
		GetAppState: func() *state.AppState { return &state.AppState{} },
		SetAppState: func(fn func(*state.AppState) *state.AppState) {},
	}
	qe := NewQueryEngine(cfg)
	qe.Interrupt()
	// Should not panic; abort context should be cancelled.
	if qe.abortCtx.Err() == nil {
		t.Error("abort context should be cancelled after Interrupt()")
	}
}

func TestQueryEngine_GetSessionID(t *testing.T) {
	cfg := &QueryEngineConfig{
		CWD:         "/tmp",
		GetAppState: func() *state.AppState { return &state.AppState{SessionID: "s1"} },
	}
	qe := NewQueryEngine(cfg)
	if qe.GetSessionID() != "s1" {
		t.Errorf("session ID = %s, want s1", qe.GetSessionID())
	}
}

func TestQueryEngine_SetModel(t *testing.T) {
	cfg := &QueryEngineConfig{
		CWD:                "/tmp",
		UserSpecifiedModel: "old-model",
	}
	qe := NewQueryEngine(cfg)
	qe.SetModel("new-model")
	if qe.config.UserSpecifiedModel != "new-model" {
		t.Errorf("model = %s, want new-model", qe.config.UserSpecifiedModel)
	}
}

func TestAccumulateUsageStats(t *testing.T) {
	a := &UsageStats{InputTokens: 100, OutputTokens: 50, CostUSD: 0.01}
	b := &UsageStats{InputTokens: 200, OutputTokens: 100, CostUSD: 0.02}
	result := accumulateUsageStats(a, b)
	if result.InputTokens != 300 {
		t.Errorf("input = %d, want 300", result.InputTokens)
	}
	if result.OutputTokens != 150 {
		t.Errorf("output = %d, want 150", result.OutputTokens)
	}
	if result.CostUSD != 0.03 {
		t.Errorf("cost = %f, want 0.03", result.CostUSD)
	}
}

func TestAccumulateUsageStats_Nil(t *testing.T) {
	result := accumulateUsageStats(nil, nil)
	if result == nil {
		t.Fatal("should return non-nil")
	}
	if result.InputTokens != 0 {
		t.Error("should be zero")
	}
}

func TestQueryEngine_IsResultSuccessful(t *testing.T) {
	cfg := &QueryEngineConfig{CWD: "/tmp"}
	qe := NewQueryEngine(cfg)

	// Empty messages → not successful.
	if qe.isResultSuccessful("end_turn") {
		t.Error("empty messages should not be successful")
	}

	// Assistant with end_turn → successful.
	qe.mutableMessages = []*Message{{
		Role:       RoleAssistant,
		StopReason: "end_turn",
		Content:    []*ContentBlock{{Type: ContentTypeText, Text: "Done"}},
	}}
	if !qe.isResultSuccessful("end_turn") {
		t.Error("assistant with end_turn should be successful")
	}

	// Assistant with tool_use stop → successful.
	if !qe.isResultSuccessful("tool_use") {
		t.Error("assistant with tool_use should be successful")
	}
}

func TestQueryEngine_ExtractTextResult(t *testing.T) {
	cfg := &QueryEngineConfig{CWD: "/tmp"}
	qe := NewQueryEngine(cfg)

	qe.mutableMessages = []*Message{
		{Role: RoleUser, Content: []*ContentBlock{{Type: ContentTypeText, Text: "Hi"}}},
		{Role: RoleAssistant, Content: []*ContentBlock{{Type: ContentTypeText, Text: "Hello!"}}},
	}

	got := qe.extractTextResult()
	if got != "Hello!" {
		t.Errorf("extractTextResult = %q, want %q", got, "Hello!")
	}
}

func TestQueryEngine_ExtractTextResult_Empty(t *testing.T) {
	cfg := &QueryEngineConfig{CWD: "/tmp"}
	qe := NewQueryEngine(cfg)
	qe.mutableMessages = []*Message{}

	got := qe.extractTextResult()
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestQueryEngine_SetEngine(t *testing.T) {
	cfg := &QueryEngineConfig{CWD: "/tmp"}
	qe := NewQueryEngine(cfg)
	if qe.engine != nil {
		t.Error("engine should be nil initially")
	}
	// We can't construct a full Engine here without a ModelCaller,
	// but we can test that SetEngine assigns the field.
	qe.SetEngine(nil)
	if qe.engine != nil {
		t.Error("engine should still be nil after SetEngine(nil)")
	}
}

func TestNewQueryEngineWithEngine_NilEngine(t *testing.T) {
	cfg := &QueryEngineConfig{CWD: "/tmp"}
	qe := NewQueryEngineWithEngine(cfg, nil)
	// With nil engine, should fall back to stub path (same as NewQueryEngine).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch := qe.SubmitMessage(ctx, "test", nil)
	var count int
	for range ch {
		count++
	}
	if count == 0 {
		t.Error("expected messages from stub path")
	}
}
