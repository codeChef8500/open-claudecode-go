package analytics

import "time"

// ────────────────────────────────────────────────────────────────────────────
// Standard analytics event names — aligned with claude-code-main analytics/.
// ────────────────────────────────────────────────────────────────────────────

const (
	// Session lifecycle
	EventSessionStart       = "session_start"
	EventSessionEnd         = "session_end"
	EventSessionResume      = "session_resume"
	EventSessionCompact     = "session_compact"

	// Model / API
	EventAPICall            = "api_call"
	EventAPIError           = "api_error"
	EventAPIRetry           = "api_retry"
	EventModelFallback      = "model_fallback"
	EventTokenBudgetStop    = "token_budget_stop"
	EventCacheBreak         = "cache_break"

	// Tool usage
	EventToolCall           = "tool_call"
	EventToolError          = "tool_error"
	EventToolPermission     = "tool_permission"

	// Agent
	EventAgentSpawn         = "agent_spawn"
	EventAgentComplete      = "agent_complete"
	EventAgentError         = "agent_error"
	EventCoordinatorStart   = "coordinator_start"
	EventCoordinatorEnd     = "coordinator_end"

	// User interaction
	EventUserMessage        = "user_message"
	EventCommand            = "command"
	EventAutoMode           = "auto_mode"
	EventPermissionGrant    = "permission_grant"
	EventPermissionDeny     = "permission_deny"

	// MCP
	EventMCPConnect         = "mcp_connect"
	EventMCPDisconnect      = "mcp_disconnect"
	EventMCPToolCall        = "mcp_tool_call"
	EventMCPElicitation     = "mcp_elicitation"

	// Error recovery
	EventReactiveCompact    = "reactive_compact"
	EventOTKEscalation      = "otk_escalation"
	EventMaxTokensRecovery  = "max_tokens_recovery"

	// Performance
	EventTTFT               = "ttft" // time to first token
	EventStreamComplete     = "stream_complete"
)

// ────────────────────────────────────────────────────────────────────────────
// Convenience logging helpers
// ────────────────────────────────────────────────────────────────────────────

// LogAPICall logs an API call event with timing and token usage.
func LogAPICall(model string, inputTokens, outputTokens int, latencyMs int64, cached bool) {
	LogEvent(EventAPICall, EventMetadata{
		"model":         model,
		"input_tokens":  inputTokens,
		"output_tokens": outputTokens,
		"latency_ms":    latencyMs,
		"cached":        cached,
	})
}

// LogToolCall logs a tool invocation event.
func LogToolCall(toolName string, durationMs int64, isError bool) {
	LogEvent(EventToolCall, EventMetadata{
		"tool":        toolName,
		"duration_ms": durationMs,
		"is_error":    isError,
	})
}

// LogAgentSpawn logs a sub-agent spawn event.
func LogAgentSpawn(agentID, agentType, task string) {
	LogEvent(EventAgentSpawn, EventMetadata{
		"agent_id":   agentID,
		"agent_type": agentType,
		"task":       truncateForAnalytics(task, 200),
	})
}

// LogAgentComplete logs a sub-agent completion event.
func LogAgentComplete(agentID string, durationMs int64, turns int, err error) {
	meta := EventMetadata{
		"agent_id":    agentID,
		"duration_ms": durationMs,
		"turns":       turns,
	}
	if err != nil {
		meta["error"] = truncateForAnalytics(err.Error(), 200)
		LogEvent(EventAgentError, meta)
	} else {
		LogEvent(EventAgentComplete, meta)
	}
}

// LogTTFT logs time-to-first-token for a streaming response.
func LogTTFT(model string, ttftMs int64) {
	LogEvent(EventTTFT, EventMetadata{
		"model":   model,
		"ttft_ms": ttftMs,
	})
}

// LogCommand logs a slash command execution.
func LogCommand(name string, durationMs int64) {
	LogEvent(EventCommand, EventMetadata{
		"command":     name,
		"duration_ms": durationMs,
	})
}

// LogReactiveCompact logs a reactive compaction event.
func LogReactiveCompact(stage string, tokensFreed int) {
	LogEvent(EventReactiveCompact, EventMetadata{
		"stage":        stage,
		"tokens_freed": tokensFreed,
	})
}

// ────────────────────────────────────────────────────────────────────────────
// Performance tracker — TTFT and streaming latency
// ────────────────────────────────────────────────────────────────────────────

// PerfTracker measures streaming performance for a single API call.
type PerfTracker struct {
	model     string
	startTime time.Time
	firstTok  time.Time
	recorded  bool
}

// NewPerfTracker starts tracking performance for a streaming call.
func NewPerfTracker(model string) *PerfTracker {
	return &PerfTracker{
		model:     model,
		startTime: time.Now(),
	}
}

// RecordFirstToken marks the time of the first streaming token.
func (p *PerfTracker) RecordFirstToken() {
	if p.firstTok.IsZero() {
		p.firstTok = time.Now()
		ttft := p.firstTok.Sub(p.startTime).Milliseconds()
		LogTTFT(p.model, ttft)
	}
}

// RecordComplete marks the stream as complete and logs total duration.
func (p *PerfTracker) RecordComplete(inputTokens, outputTokens int) {
	if p.recorded {
		return
	}
	p.recorded = true

	totalMs := time.Since(p.startTime).Milliseconds()
	ttftMs := int64(0)
	if !p.firstTok.IsZero() {
		ttftMs = p.firstTok.Sub(p.startTime).Milliseconds()
	}

	LogEvent(EventStreamComplete, EventMetadata{
		"model":         p.model,
		"total_ms":      totalMs,
		"ttft_ms":       ttftMs,
		"input_tokens":  inputTokens,
		"output_tokens": outputTokens,
	})
}

// truncateForAnalytics truncates a string for safe analytics logging.
func truncateForAnalytics(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
