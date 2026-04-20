package engine

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// ────────────────────────────────────────────────────────────────────────────
// [P8.T5] finalizeResult — mirrors TS QueryEngine.ts:L972-1155
// Budget checks, structured output retry limits, and final result emission.
// ────────────────────────────────────────────────────────────────────────────

// checkBudgetExceeded returns an error result if the USD budget is exceeded.
// TS anchor: QueryEngine.ts:L971-1002
func (qe *QueryEngine) checkBudgetExceeded(
	ds *dispatchState,
	cfg *QueryEngineConfig,
) *SDKResultMessage {
	if cfg.MaxBudgetUSD == nil {
		return nil
	}
	if qe.totalCostUSD() < *cfg.MaxBudgetUSD {
		return nil
	}
	durationMs := int(time.Since(ds.startTime).Milliseconds())
	return NewSDKResultError(
		qe.sessionID,
		SDKResultErrorMaxBudgetUSD,
		[]string{fmt.Sprintf("Reached maximum budget ($%v)", *cfg.MaxBudgetUSD)},
		durationMs, 0, ds.turnCount,
		qe.totalCostUSD(),
		qe.totalUsage,
	)
}

// checkStructuredOutputRetryLimit returns an error result if the structured
// output retry limit is exceeded.
// TS anchor: QueryEngine.ts:L1004-1048
func (qe *QueryEngine) checkStructuredOutputRetryLimit(
	ds *dispatchState,
	cfg *QueryEngineConfig,
	msg interface{},
) *SDKResultMessage {
	if cfg.JSONSchema == nil {
		return nil
	}
	// Only check on user messages (tool result turns).
	m, ok := msg.(*Message)
	if !ok || m.Role != RoleUser {
		return nil
	}

	currentCalls := countToolCallsByName(qe.mutableMessages, SyntheticOutputToolName)
	callsThisQuery := currentCalls - ds.initialStructuredOutputCalls
	maxRetries := 5
	if env := os.Getenv("MAX_STRUCTURED_OUTPUT_RETRIES"); env != "" {
		if v, err := strconv.Atoi(env); err == nil {
			maxRetries = v
		}
	}

	if callsThisQuery < maxRetries {
		return nil
	}

	durationMs := int(time.Since(ds.startTime).Milliseconds())
	return NewSDKResultError(
		qe.sessionID,
		SDKResultErrorMaxStructuredRetries,
		[]string{fmt.Sprintf("Failed to provide valid structured output after %d attempts", maxRetries)},
		durationMs, 0, ds.turnCount,
		qe.totalCostUSD(),
		qe.totalUsage,
	)
}

// buildFinalResult constructs the terminal result message after the query loop.
// TS anchor: QueryEngine.ts:L1050-1155
func (qe *QueryEngine) buildFinalResult(
	ds *dispatchState,
	cfg *QueryEngineConfig,
) *SDKResultMessage {
	durationMs := int(time.Since(ds.startTime).Milliseconds())

	if !qe.isResultSuccessful(ds.lastStopReason) {
		return NewSDKResultError(
			qe.sessionID,
			SDKResultErrorDuringExecution,
			[]string{fmt.Sprintf(
				"[ede_diagnostic] stop_reason=%s",
				ds.lastStopReason,
			)},
			durationMs, 0, ds.turnCount,
			qe.totalCostUSD(),
			qe.totalUsage,
		)
	}

	textResult := qe.extractTextResult()
	result := NewSDKResultSuccess(
		qe.sessionID,
		textResult,
		durationMs, 0, ds.turnCount,
		qe.totalCostUSD(),
		qe.totalUsage,
		ds.lastStopReason,
	)

	// Attach structured output if captured.
	if ds.structuredOutputFromTool != nil {
		result.StructuredOutput = ds.structuredOutputFromTool
	}

	return result
}

// SyntheticOutputToolName is the tool name used for structured output enforcement.
const SyntheticOutputToolName = "SyntheticOutput"

// countToolCallsByName counts tool_use blocks matching the given name.
func countToolCallsByName(msgs []*Message, toolName string) int {
	count := 0
	for _, m := range msgs {
		if m.Role != RoleAssistant {
			continue
		}
		for _, block := range m.Content {
			if block.Type == ContentTypeToolUse && block.ToolName == toolName {
				count++
			}
		}
	}
	return count
}
