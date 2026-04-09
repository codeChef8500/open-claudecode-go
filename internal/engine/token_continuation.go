package engine

import (
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ────────────────────────────────────────────────────────────────────────────
// Token budget continuation — decides whether the agent should keep running
// after the model stops, based on token usage patterns and diminishing returns.
// Aligned with claude-code-main query/tokenBudget.ts checkTokenBudget.
// ────────────────────────────────────────────────────────────────────────────

const (
	// completionThreshold is the fraction of the budget that triggers a stop.
	// Aligned with TS COMPLETION_THRESHOLD = 0.9.
	completionThreshold = 0.9
	// diminishingThreshold is the minimum token delta to avoid diminishing returns.
	// Aligned with TS DIMINISHING_THRESHOLD = 500.
	diminishingThreshold = 500
)

// TokenBudgetDecision is the output of CheckTokenBudgetContinuation.
type TokenBudgetDecision struct {
	// ShouldContinue is true if the loop should auto-continue.
	ShouldContinue bool
	// Reason describes why continuation was approved or rejected.
	Reason string
	// NudgeMessage is the message to inject when continuing (empty on stop).
	NudgeMessage string
	// ContinuationCount is the updated continuation counter.
	ContinuationCount int
	// CompletionEvent is populated on stop if any continuations occurred.
	CompletionEvent *BudgetCompletionEvent
}

// BudgetCompletionEvent captures analytics for the token budget session.
// Aligned with TS StopDecision.completionEvent.
type BudgetCompletionEvent struct {
	ContinuationCount  int
	Pct                int
	TurnTokens         int
	Budget             int
	DiminishingReturns bool
	DurationMs         int64
}

// BudgetContinuationTracker tracks state for the diminishing-returns heuristic.
// Aligned with claude-code-main BudgetTracker.
type BudgetContinuationTracker struct {
	// ContinuationCount tracks how many times we auto-continued.
	ContinuationCount int
	// LastDeltaTokens is the output token delta from the last iteration.
	LastDeltaTokens int
	// LastGlobalTurnTokens is the total turn tokens from the last iteration.
	LastGlobalTurnTokens int
	// StartedAt is when the query started.
	StartedAt time.Time
	// InitialOutputTokens is the output tokens at the start of the chain.
	InitialOutputTokens int
	// Budget is the user-specified token budget (0 = no budget).
	Budget int
	// AgentID is the agent identifier (non-empty disables budget continuation).
	AgentID string
}

// NewBudgetContinuationTracker creates a tracker.
func NewBudgetContinuationTracker() *BudgetContinuationTracker {
	return &BudgetContinuationTracker{
		StartedAt: time.Now(),
	}
}

// CheckTokenBudgetContinuation decides whether the query loop should
// auto-continue after the model produces a stop_reason of "end_turn"
// (no tool calls).
//
// The logic mirrors claude-code-main's checkTokenBudget exactly:
//   - Sub-agents (AgentID set) never auto-continue.
//   - No budget or budget <= 0 means no continuation.
//   - Diminishing returns: if continuationCount >= 3 and both current + last
//     delta are below DIMINISHING_THRESHOLD (500), stop.
//   - If not diminishing and turnTokens < budget * COMPLETION_THRESHOLD (0.9), continue.
//   - Otherwise stop.
func CheckTokenBudgetContinuation(
	tracker *BudgetContinuationTracker,
	currentOutputTokens int,
	maxOutputTokens int,
	stopReason string,
) *TokenBudgetDecision {
	budget := tracker.Budget

	// Sub-agents and unbudgeted queries never auto-continue.
	// Aligned with TS: if (agentId || budget === null || budget <= 0)
	if tracker.AgentID != "" || budget <= 0 {
		return &TokenBudgetDecision{
			ShouldContinue:    false,
			Reason:            "no budget or sub-agent",
			ContinuationCount: tracker.ContinuationCount,
		}
	}

	turnTokens := currentOutputTokens
	pct := int(math.Round(float64(turnTokens) / float64(budget) * 100))
	deltaSinceLastCheck := currentOutputTokens - tracker.LastGlobalTurnTokens

	// Diminishing returns detection — aligned with TS:
	// tracker.continuationCount >= 3 &&
	// deltaSinceLastCheck < DIMINISHING_THRESHOLD &&
	// tracker.lastDeltaTokens < DIMINISHING_THRESHOLD
	isDiminishing := tracker.ContinuationCount >= 3 &&
		deltaSinceLastCheck < diminishingThreshold &&
		tracker.LastDeltaTokens < diminishingThreshold

	// Continue case: not diminishing AND under the completion threshold.
	if !isDiminishing && turnTokens < int(float64(budget)*completionThreshold) {
		tracker.ContinuationCount++
		tracker.LastDeltaTokens = deltaSinceLastCheck
		tracker.LastGlobalTurnTokens = currentOutputTokens

		nudge := getBudgetContinuationMessage(pct, turnTokens, budget)

		slog.Info("token_continuation: auto-continuing",
			slog.Int("continuation", tracker.ContinuationCount),
			slog.Int("pct", pct),
			slog.Int("turn_tokens", turnTokens),
			slog.Int("budget", budget))

		return &TokenBudgetDecision{
			ShouldContinue:    true,
			Reason:            "budget headroom available",
			NudgeMessage:      nudge,
			ContinuationCount: tracker.ContinuationCount,
		}
	}

	// Stop with completion event if any continuations occurred.
	if isDiminishing || tracker.ContinuationCount > 0 {
		durationMs := time.Since(tracker.StartedAt).Milliseconds()
		slog.Info("token_continuation: stopping",
			slog.Bool("diminishing", isDiminishing),
			slog.Int("continuations", tracker.ContinuationCount),
			slog.Int("pct", pct))
		return &TokenBudgetDecision{
			ShouldContinue:    false,
			Reason:            "completion threshold or diminishing returns",
			ContinuationCount: tracker.ContinuationCount,
			CompletionEvent: &BudgetCompletionEvent{
				ContinuationCount:  tracker.ContinuationCount,
				Pct:                pct,
				TurnTokens:         turnTokens,
				Budget:             budget,
				DiminishingReturns: isDiminishing,
				DurationMs:         durationMs,
			},
		}
	}

	// No budget continuation was ever started.
	return &TokenBudgetDecision{
		ShouldContinue:    false,
		Reason:            "no continuations started",
		ContinuationCount: tracker.ContinuationCount,
	}
}

// getBudgetContinuationMessage generates the nudge message injected when
// auto-continuing. Aligned with TS getBudgetContinuationMessage.
func getBudgetContinuationMessage(pct, turnTokens, budget int) string {
	return fmt.Sprintf(
		"Stopped at %d%% of token target (%s / %s). Keep working — do not summarize.",
		pct, formatNumber(turnTokens), formatNumber(budget))
}

// formatNumber formats an integer with comma separators (en-US style).
func formatNumber(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return "-" + formatNumber(-n)
	}
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	mod := len(s) % 3
	if mod > 0 {
		b.WriteString(s[:mod])
		if len(s) > mod {
			b.WriteByte(',')
		}
	}
	for i := mod; i < len(s); i += 3 {
		if i > mod {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// ────────────────────────────────────────────────────────────────────────────
// Token budget parsing — aligned with claude-code-main utils/tokenBudget.ts
// ────────────────────────────────────────────────────────────────────────────

var (
	shorthandStartRE = regexp.MustCompile(`(?i)^\s*\+(\d+(?:\.\d+)?)\s*(k|m|b)\b`)
	shorthandEndRE   = regexp.MustCompile(`(?i)\s\+(\d+(?:\.\d+)?)\s*(k|m|b)\s*[.!?]?\s*$`)
	verboseRE        = regexp.MustCompile(`(?i)\b(?:use|spend)\s+(\d+(?:\.\d+)?)\s*(k|m|b)\s*tokens?\b`)
)

var multipliers = map[string]float64{
	"k": 1_000,
	"m": 1_000_000,
	"b": 1_000_000_000,
}

// ParseTokenBudget extracts a token budget from user text.
// Returns 0 if no budget was found.
// Aligned with TS parseTokenBudget.
func ParseTokenBudget(text string) int {
	if m := shorthandStartRE.FindStringSubmatch(text); m != nil {
		return parseBudgetMatch(m[1], m[2])
	}
	if m := shorthandEndRE.FindStringSubmatch(text); m != nil {
		return parseBudgetMatch(m[1], m[2])
	}
	if m := verboseRE.FindStringSubmatch(text); m != nil {
		return parseBudgetMatch(m[1], m[2])
	}
	return 0
}

func parseBudgetMatch(value, suffix string) int {
	v, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	mul, ok := multipliers[strings.ToLower(suffix)]
	if !ok {
		return 0
	}
	return int(v * mul)
}
