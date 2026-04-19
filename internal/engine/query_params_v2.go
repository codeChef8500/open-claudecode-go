package engine

// ────────────────────────────────────────────────────────────────────────────
// [P9.T3] QueryParamsV2 — mirrors claude-code-main query.ts QueryParams.
// This is the new query loop input type that aligns with the TS version.
// The existing QueryParams is preserved for backward compat with the old
// Engine.SubmitMessage path.
// ────────────────────────────────────────────────────────────────────────────

// QueryParamsV2 holds all inputs for a V2 query loop invocation.
// Mirrors claude-code-main query.ts QueryParams.
type QueryParamsV2 struct {
	// Messages is the conversation history.
	Messages []*Message
	// SystemPrompt is the assembled system prompt text.
	SystemPrompt string
	// UserContext carries user-facing key-value pairs.
	UserContext map[string]string
	// SystemContext carries system-level key-value pairs.
	SystemContext map[string]string
	// CanUseTool checks whether a tool invocation is permitted.
	CanUseTool CanUseToolFn
	// ToolUseContext carries all context for tool execution.
	ToolUseContext *ToolUseContext
	// FallbackModel is the model to switch to on FallbackTriggeredError.
	FallbackModel string
	// QuerySource identifies the caller (e.g. "sdk", "repl_main_thread").
	QuerySource string
	// MaxOutputTokensOverride overrides max output tokens per call.
	MaxOutputTokensOverride *int
	// MaxTurns caps the agentic turn count.
	MaxTurns int
	// SkipCacheWrite skips prompt cache writes for this query.
	SkipCacheWrite bool
	// TaskBudget limits total tokens for a task-based query.
	TaskBudget *TaskBudget
	// Deps holds injectable dependencies for testing.
	Deps *QueryDeps
}

// QueryStateV2 is the mutable state carried between loop iterations.
// Mirrors claude-code-main query.ts State type.
type QueryStateV2 struct {
	// Messages is the current conversation history.
	Messages []*Message
	// ToolUseContext for the current iteration.
	ToolUseContext *ToolUseContext
	// AutoCompactTracking tracks compaction state.
	AutoCompactTracking *AutoCompactTrackingState
	// MaxOutputTokensRecoveryCount tracks recovery retries.
	MaxOutputTokensRecoveryCount int
	// HasAttemptedReactiveCompact is true if reactive compact was tried.
	HasAttemptedReactiveCompact bool
	// MaxOutputTokensOverride overrides max output for the next call.
	MaxOutputTokensOverride *int
	// PendingToolUseSummary is an async summary from the previous turn.
	PendingToolUseSummary <-chan *ToolUseSummaryMessage
	// StopHookActive is true when a stop hook forced a retry.
	StopHookActive bool
	// TurnCount is the current turn number (1-indexed).
	TurnCount int
	// Transition records why the previous iteration continued.
	Transition *ContinueTransition
}

// NewQueryStateV2 creates initial query state from params.
func NewQueryStateV2(params *QueryParamsV2) *QueryStateV2 {
	return &QueryStateV2{
		Messages:       params.Messages,
		ToolUseContext: params.ToolUseContext,
		TurnCount:      1,
		MaxOutputTokensOverride: params.MaxOutputTokensOverride,
	}
}
