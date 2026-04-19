package engine

import (
	"encoding/json"
	"testing"
)

func TestSDKResultMessage_JSON(t *testing.T) {
	sr := "end_turn"
	msg := &SDKResultMessage{
		Type:         SDKMsgResult,
		Subtype:      SDKResultSuccess,
		DurationMs:   1234,
		DurationAPIMs: 900,
		IsError:      false,
		NumTurns:     3,
		Result:       "Done",
		StopReason:   &sr,
		TotalCostUSD: 0.05,
		UUID:         "test-uuid",
		SessionID:    "sess-1",
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SDKResultMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != SDKMsgResult {
		t.Errorf("type = %s, want result", decoded.Type)
	}
	if decoded.Subtype != SDKResultSuccess {
		t.Errorf("subtype = %s, want success", decoded.Subtype)
	}
	if decoded.NumTurns != 3 {
		t.Errorf("num_turns = %d, want 3", decoded.NumTurns)
	}
}

func TestSDKResultError_JSON(t *testing.T) {
	msg := NewSDKResultError("sess-1", SDKResultErrorMaxTurns, []string{"max turns reached"}, 5000, 4000, 10, 0.20, nil)
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SDKResultMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.IsError {
		t.Error("expected is_error=true")
	}
	if decoded.Subtype != SDKResultErrorMaxTurns {
		t.Errorf("subtype = %s, want error_max_turns", decoded.Subtype)
	}
	if len(decoded.Errors) != 1 {
		t.Errorf("errors len = %d, want 1", len(decoded.Errors))
	}
}

func TestSDKSystemInit_JSON(t *testing.T) {
	msg := NewSDKSystemInit("sess-2", SDKSystemInitMessage{
		ClaudeCodeVersion: "1.0.0",
		CWD:              "/home/test",
		Model:            "claude-sonnet-4-6",
		Tools:            []string{"Bash", "Read"},
		PermissionMode:   "default",
		OutputStyle:      "concise",
	})
	if msg.Type != SDKMsgSystem {
		t.Errorf("type = %s, want system", msg.Type)
	}
	if msg.Subtype != SDKSystemSubtypeInit {
		t.Errorf("subtype = %s, want init", msg.Subtype)
	}
	if msg.SessionID != "sess-2" {
		t.Errorf("session_id = %s, want sess-2", msg.SessionID)
	}
	if msg.UUID == "" {
		t.Error("uuid should not be empty")
	}
}

func TestSDKAPIRetry_JSON(t *testing.T) {
	status := 429
	msg := NewSDKAPIRetry("sess-3", 1, 3, 5000, &status, SDKErrRateLimit)
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SDKAPIRetryMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Attempt != 1 {
		t.Errorf("attempt = %d, want 1", decoded.Attempt)
	}
	if decoded.Error != SDKErrRateLimit {
		t.Errorf("error = %s, want rate_limit", decoded.Error)
	}
	if decoded.ErrorStatus == nil || *decoded.ErrorStatus != 429 {
		t.Error("error_status should be 429")
	}
}

func TestSDKSessionStateChanged(t *testing.T) {
	msg := NewSDKSessionStateChanged("sess-4", SDKSessionIdle)
	if msg.State != SDKSessionIdle {
		t.Errorf("state = %s, want idle", msg.State)
	}
	if msg.UUID == "" {
		t.Error("uuid should not be empty")
	}
}

func TestSDKTaskStarted(t *testing.T) {
	msg := NewSDKTaskStarted("sess-5", "task-1", "Running tests")
	if msg.TaskID != "task-1" {
		t.Errorf("task_id = %s, want task-1", msg.TaskID)
	}
	if msg.Subtype != SDKSystemSubtypeTaskStarted {
		t.Errorf("subtype = %s, want task_started", msg.Subtype)
	}
}

func TestSDKHookStarted(t *testing.T) {
	msg := NewSDKHookStarted("sess-6", "hook-1", "PreToolUse", "PreToolUse")
	if msg.HookID != "hook-1" {
		t.Errorf("hook_id = %s, want hook-1", msg.HookID)
	}
}

func TestSDKToolProgress(t *testing.T) {
	msg := NewSDKToolProgress("sess-7", "tu-1", "Bash", 2.5)
	if msg.ElapsedTimeSecs != 2.5 {
		t.Errorf("elapsed = %f, want 2.5", msg.ElapsedTimeSecs)
	}
}

func TestSDKStatusMessage(t *testing.T) {
	s := "compacting"
	msg := NewSDKStatusMessage("sess-8", &s, "default")
	if msg.Status == nil || *msg.Status != "compacting" {
		t.Error("status should be compacting")
	}
}

func TestNewSDKUUID(t *testing.T) {
	id := NewSDKUUID()
	if len(id) < 32 {
		t.Errorf("uuid too short: %s", id)
	}
}

func TestSDKExtendedTypeConstants(t *testing.T) {
	// Verify const values match TS.
	checks := map[SDKMessageType]string{
		SDKMsgStreamEvent:            "stream_event",
		SDKMsgResult:                 "result",
		SDKMsgRateLimitEvent:         "rate_limit_event",
		SDKMsgStreamlinedText:        "streamlined_text",
		SDKMsgStreamlinedToolSummary: "streamlined_tool_use_summary",
		SDKMsgToolProgress:           "tool_progress",
		SDKMsgAuthStatus:             "auth_status",
		SDKMsgPromptSuggestion:       "prompt_suggestion",
	}
	for got, want := range checks {
		if string(got) != want {
			t.Errorf("%s != %s", got, want)
		}
	}
}

func TestSDKSystemSubtypeConstants(t *testing.T) {
	checks := map[SDKSystemSubtype]string{
		SDKSystemSubtypeInit:                "init",
		SDKSystemSubtypeCompactBoundary:     "compact_boundary",
		SDKSystemSubtypeStatus:              "status",
		SDKSystemSubtypeAPIRetry:            "api_retry",
		SDKSystemSubtypeLocalCommandOutput:  "local_command_output",
		SDKSystemSubtypeHookStarted:         "hook_started",
		SDKSystemSubtypeHookProgress:        "hook_progress",
		SDKSystemSubtypeHookResponse:        "hook_response",
		SDKSystemSubtypeTaskNotification:    "task_notification",
		SDKSystemSubtypeTaskStarted:         "task_started",
		SDKSystemSubtypeTaskProgress:        "task_progress",
		SDKSystemSubtypeSessionStateChanged: "session_state_changed",
		SDKSystemSubtypeFilesPersisted:      "files_persisted",
		SDKSystemSubtypePostTurnSummary:     "post_turn_summary",
		SDKSystemSubtypeElicitationComplete: "elicitation_complete",
	}
	for got, want := range checks {
		if string(got) != want {
			t.Errorf("%s != %s", got, want)
		}
	}
}

func TestSDKAssistantMessageErrorConstants(t *testing.T) {
	checks := map[SDKAssistantMessageErrorType]string{
		SDKErrAuthFailed:     "authentication_failed",
		SDKErrBilling:        "billing_error",
		SDKErrRateLimit:      "rate_limit",
		SDKErrInvalidRequest: "invalid_request",
		SDKErrServer:         "server_error",
		SDKErrUnknown:        "unknown",
		SDKErrMaxOutput:      "max_output_tokens",
	}
	for got, want := range checks {
		if string(got) != want {
			t.Errorf("%s != %s", got, want)
		}
	}
}

func TestSDKResultSubtypeConstants(t *testing.T) {
	checks := map[SDKResultSubtype]string{
		SDKResultSuccess:                   "success",
		SDKResultErrorDuringExecution:      "error_during_execution",
		SDKResultErrorMaxTurns:             "error_max_turns",
		SDKResultErrorMaxBudgetUSD:         "error_max_budget_usd",
		SDKResultErrorMaxStructuredRetries: "error_max_structured_output_retries",
	}
	for got, want := range checks {
		if string(got) != want {
			t.Errorf("%s != %s", got, want)
		}
	}
}
