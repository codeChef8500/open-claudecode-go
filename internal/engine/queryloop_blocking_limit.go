package engine

// ────────────────────────────────────────────────────────────────────────────
// [P9.T7-T8] Blocking limit pre-check + task budget tracking across compacts.
// Mirrors TS query.ts:L508-515 (task budget) and L615-648 (blocking limit).
// ────────────────────────────────────────────────────────────────────────────

// BlockingLimitFraction is the context window fraction above which the
// loop blocks (preemptive prompt-too-long) when auto-compact is OFF.
// TS anchor: autoCompact.ts isAtBlockingLimit = tokenWarningState >= 0.95
const BlockingLimitFraction = 0.95

// IsAtBlockingLimit returns true if the estimated token count exceeds the
// blocking limit for the given model's context window.
// TS anchor: query.ts:L637-648 calculateTokenWarningState().isAtBlockingLimit
func IsAtBlockingLimit(estimatedTokens int, contextWindowSize int) bool {
	if contextWindowSize <= 0 {
		return false
	}
	ratio := float64(estimatedTokens) / float64(contextWindowSize)
	return ratio >= BlockingLimitFraction
}

// ShouldSkipBlockingLimitCheck returns true when the blocking limit pre-check
// should be skipped (compaction just happened, or query is compact/session_memory,
// or reactive compact + auto-compact are both enabled).
// TS anchor: query.ts:L592-636
func ShouldSkipBlockingLimitCheck(
	compactionJustHappened bool,
	querySource QuerySource,
	reactiveCompactEnabled bool,
	autoCompactEnabled bool,
	contextCollapseEnabled bool,
) bool {
	if compactionJustHappened {
		return true
	}
	if querySource.IsCompactOrSessionMemory() {
		return true
	}
	if reactiveCompactEnabled && autoCompactEnabled {
		return true
	}
	if contextCollapseEnabled && autoCompactEnabled {
		return true
	}
	return false
}

// ── Task budget remaining tracking across compaction ─────────────────────

// TaskBudgetRemainingTracker tracks task_budget.remaining across compaction
// boundaries. Separate from the existing TaskBudgetTracker (task_budget.go)
// which tracks USD/token/duration limits.
// TS anchor: query.ts:L282-291
type TaskBudgetRemainingTracker struct {
	// Total is the initial task budget total (from QueryParams).
	Total int
	// Remaining is the current remaining budget after compaction adjustments.
	// nil until the first compact fires.
	Remaining *int
}

// NewTaskBudgetRemainingTracker creates a remaining tracker.
func NewTaskBudgetRemainingTracker(total int) *TaskBudgetRemainingTracker {
	return &TaskBudgetRemainingTracker{Total: total}
}

// RecordCompaction subtracts the pre-compact context token count from the
// remaining budget. Called after each successful compaction.
// TS anchor: query.ts:L508-515
func (t *TaskBudgetRemainingTracker) RecordCompaction(preCompactContextTokens int) {
	base := t.Total
	if t.Remaining != nil {
		base = *t.Remaining
	}
	remaining := base - preCompactContextTokens
	if remaining < 0 {
		remaining = 0
	}
	t.Remaining = &remaining
}

// GetRemaining returns the remaining budget, or nil if no compact has happened yet.
func (t *TaskBudgetRemainingTracker) GetRemaining() *int {
	return t.Remaining
}
