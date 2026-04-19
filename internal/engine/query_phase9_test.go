package engine

import (
	"testing"
)

// ── Transitions ─────────────────────────────────────────────────────────────

func TestContinueReasonConstants_P9(t *testing.T) {
	reasons := []ContinueReason{
		ContinueNextTurn,
		ContinueMaxOutputTokensRecovery,
		ContinueMaxOutputTokensEscalate,
		ContinueReactiveCompactRetry,
		ContinueCollapseDrainRetry,
		ContinueStopHookBlocking,
		ContinueTokenBudgetContinuation,
	}
	seen := make(map[ContinueReason]bool)
	for _, r := range reasons {
		if seen[r] {
			t.Errorf("duplicate ContinueReason: %s", r)
		}
		seen[r] = true
	}
}

func TestTerminalReasonConstants_P9(t *testing.T) {
	reasons := []TerminalReason{
		TerminalCompleted,
		TerminalAbortedStreaming,
		TerminalAbortedTools,
		TerminalBlockingLimit,
		TerminalModelError,
		TerminalImageError,
		TerminalPromptTooLong,
		TerminalStopHookPrevented,
		TerminalHookStopped,
		TerminalMaxTurns,
	}
	if len(reasons) != 10 {
		t.Errorf("expected 10 terminal reasons, got %d", len(reasons))
	}
}

// ── Message factories ───────────────────────────────────────────────────────

func TestCreateUserMessage_P9(t *testing.T) {
	msg := CreateUserMessage("hello")
	if msg.Role != RoleUser {
		t.Errorf("role = %s, want user", msg.Role)
	}
	if msg.UUID == "" {
		t.Error("UUID should be set")
	}
	if len(msg.Content) != 1 || msg.Content[0].Text != "hello" {
		t.Error("content mismatch")
	}
}

func TestCreateUserInterruptionMessage_P9(t *testing.T) {
	msg := CreateUserInterruptionMessage(false)
	if !msg.IsMeta {
		t.Error("interruption message should be meta")
	}
	if msg.Role != RoleUser {
		t.Error("should be user role")
	}
}

func TestCreateSystemMessage_P9(t *testing.T) {
	msg := CreateSystemMessage("Server error", SystemLevelInfo)
	if msg.Role != RoleSystem {
		t.Errorf("role = %s, want system", msg.Role)
	}
}

func TestCreateAssistantAPIErrorMessage_P9(t *testing.T) {
	msg := CreateAssistantAPIErrorMessage("rate_limit", "Rate limit exceeded")
	if msg.APIError != "rate_limit" {
		t.Error("APIError not set")
	}
	if msg.Role != RoleAssistant {
		t.Error("should be assistant role")
	}
}

func TestGetMessagesAfterCompactBoundary_NoBoundary(t *testing.T) {
	msgs := []*Message{{UUID: "1"}, {UUID: "2"}}
	result := GetMessagesAfterCompactBoundary(msgs)
	if len(result) != 2 {
		t.Errorf("expected all messages, got %d", len(result))
	}
}

func TestGetMessagesAfterCompactBoundary_WithBoundary(t *testing.T) {
	msgs := []*Message{
		{UUID: "1"},
		{UUID: "2", Type: MsgTypeCompactBoundary},
		{UUID: "3"},
	}
	result := GetMessagesAfterCompactBoundary(msgs)
	// Returns messages AFTER the boundary (exclusive).
	if len(result) != 1 {
		t.Errorf("expected 1 message after boundary, got %d", len(result))
	}
	if result[0].UUID != "3" {
		t.Errorf("first message should be '3', got %s", result[0].UUID)
	}
}

func TestNormalizeMessagesForAPI_Empty(t *testing.T) {
	result := NormalizeMessagesForAPI(nil)
	if len(result) != 0 {
		t.Errorf("expected 0, got %d", len(result))
	}
}

func TestNormalizeMessagesForAPI_SkipsSynthetic(t *testing.T) {
	msgs := []*Message{
		{Role: RoleUser, Content: []*ContentBlock{{Type: ContentTypeText, Text: "hi"}}},
		{Role: RoleAssistant, Type: MsgTypeTombstone, TombstoneFor: "some-id", Content: []*ContentBlock{{Type: ContentTypeText, Text: "x"}}},
		{Role: RoleAssistant, Content: []*ContentBlock{{Type: ContentTypeText, Text: "bye"}}},
	}
	result := NormalizeMessagesForAPI(msgs)
	if len(result) != 2 {
		t.Errorf("expected 2 (skip synthetic tombstone), got %d", len(result))
	}
}

func TestNormalizeMessagesForAPI_SkipsTranscriptOnly(t *testing.T) {
	msgs := []*Message{
		{Role: RoleUser, Content: []*ContentBlock{{Type: ContentTypeText, Text: "hi"}}},
		{Role: RoleAssistant, IsVisibleInTranscriptOnly: true, Content: []*ContentBlock{{Type: ContentTypeText, Text: "x"}}},
		{Role: RoleAssistant, Content: []*ContentBlock{{Type: ContentTypeText, Text: "bye"}}},
	}
	result := NormalizeMessagesForAPI(msgs)
	if len(result) != 2 {
		t.Errorf("expected 2, got %d", len(result))
	}
}

func TestNormalizeMessagesForAPI_MergesConsecutiveUser(t *testing.T) {
	msgs := []*Message{
		{Role: RoleUser, Content: []*ContentBlock{{Type: ContentTypeText, Text: "hi"}}},
		{Role: RoleUser, Content: []*ContentBlock{{Type: ContentTypeText, Text: " world"}}},
		{Role: RoleAssistant, Content: []*ContentBlock{{Type: ContentTypeText, Text: "bye"}}},
	}
	result := NormalizeMessagesForAPI(msgs)
	if len(result) != 2 {
		t.Errorf("expected 2 (merged users), got %d", len(result))
	}
	if len(result) > 0 && len(result[0].Content) != 2 {
		t.Errorf("merged user should have 2 blocks, got %d", len(result[0].Content))
	}
}

func TestNormalizeMessagesForAPI_PreservesOrder(t *testing.T) {
	msgs := []*Message{
		{Role: RoleUser, Content: []*ContentBlock{{Type: ContentTypeText, Text: "hi"}}},
		{Role: RoleAssistant, Content: []*ContentBlock{{Type: ContentTypeText, Text: "hello"}}},
		{Role: RoleUser, Content: []*ContentBlock{{Type: ContentTypeText, Text: "bye"}}},
	}
	result := NormalizeMessagesForAPI(msgs)
	if len(result) != 3 {
		t.Errorf("expected 3, got %d", len(result))
	}
	if result[0].Role != RoleUser {
		t.Error("first should be user")
	}
	if result[1].Role != RoleAssistant {
		t.Error("second should be assistant")
	}
}

// ── QueryParamsV2 / QueryStateV2 ───────────────────────────────────────────

func TestNewQueryStateV2(t *testing.T) {
	msgs := []*Message{{UUID: "m1"}}
	params := &QueryParamsV2{
		Messages:    msgs,
		QuerySource: "sdk",
	}
	st := NewQueryStateV2(params)
	if st.TurnCount != 1 {
		t.Errorf("initial turn count should be 1, got %d", st.TurnCount)
	}
	if len(st.Messages) != 1 {
		t.Errorf("messages = %d, want 1", len(st.Messages))
	}
}

func TestQueryParamsV2_Fields(t *testing.T) {
	maxTokens := 8000
	params := &QueryParamsV2{
		SystemPrompt:            "You are helpful.",
		FallbackModel:           "claude-haiku-3",
		QuerySource:             "sdk",
		MaxTurns:                50,
		MaxOutputTokensOverride: &maxTokens,
	}
	if params.MaxTurns != 50 {
		t.Error("MaxTurns mismatch")
	}
	if *params.MaxOutputTokensOverride != 8000 {
		t.Error("MaxOutputTokensOverride mismatch")
	}
}

func TestQuerySource_IsMainThread(t *testing.T) {
	if !QuerySourceSDK.IsMainThread() {
		t.Error("SDK should be main thread")
	}
	if !QuerySourceREPLMainThread.IsMainThread() {
		t.Error("REPL main should be main thread")
	}
	if QuerySourceREPLSideThread.IsMainThread() {
		t.Error("REPL side should not be main thread")
	}
}
