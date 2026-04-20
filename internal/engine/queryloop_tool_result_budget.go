package engine

// ────────────────────────────────────────────────────────────────────────────
// [P9.T2] Tool result budget — enforces per-message size caps on aggregate
// tool_result content. Uses ContentReplacementState from content_replacement.go.
// Mirrors TS query.ts:L369-394 + toolResultStorage.ts.
// ────────────────────────────────────────────────────────────────────────────

const (
	// DefaultMaxToolResultChars is the default per-tool-result character limit.
	// TS anchor: toolResultStorage.ts DEFAULT_MAX_RESULT_SIZE_CHARS (16000).
	DefaultMaxToolResultChars = 16000

	// ToolResultTruncatedMarker is injected when a tool result is truncated.
	ToolResultTruncatedMarker = "[Tool result truncated — exceeded character limit]"
)

// ApplyToolResultBudget enforces per-message tool-result size limits.
// It modifies messages in-place, replacing over-budget tool_result blocks
// with truncation markers. Returns the (potentially modified) slice.
//
// Uses ContentReplacementState (content_replacement.go) for tracking.
//
// TS anchor: query.ts:L379-394 + toolResultStorage.ts:applyToolResultBudget
func ApplyToolResultBudget(
	messages []*Message,
	state *ContentReplacementState,
	unlimitedTools map[string]bool,
) []*Message {
	if state == nil {
		return messages
	}

	for _, msg := range messages {
		if msg.Role != RoleUser {
			continue
		}
		for _, block := range msg.Content {
			if block.Type != ContentTypeToolResult {
				continue
			}
			// Skip if already replaced.
			if _, replaced := state.GetRecord(block.ToolUseID); replaced && state.Records[block.ToolUseID].Replaced {
				continue
			}
			// Skip unlimited tools.
			if unlimitedTools != nil && unlimitedTools[block.ToolName] {
				continue
			}

			// Measure content size.
			totalChars := measureToolResultChars(block)
			if totalChars <= DefaultMaxToolResultChars {
				// Track usage even if under limit.
				estTokens := totalChars / 4 // rough char-to-token estimate
				state.RecordUsage(block.ToolUseID, estTokens)
				continue
			}

			// Record replacement via ContentReplacementState.
			state.RecordReplacement(&ContentReplacementRecord{
				ToolUseID:    block.ToolUseID,
				ToolName:     block.ToolName,
				OriginalSize: totalChars,
				ReplacedSize: len(ToolResultTruncatedMarker),
				Replaced:     true,
			})

			// Replace content with truncation marker.
			block.Content = []*ContentBlock{{
				Type: ContentTypeText,
				Text: ToolResultTruncatedMarker,
			}}
		}
	}

	return messages
}

// measureToolResultChars counts the total text characters in a tool_result block.
func measureToolResultChars(block *ContentBlock) int {
	total := len(block.Text) // direct text
	for _, sub := range block.Content {
		if sub.Type == ContentTypeText {
			total += len(sub.Text)
		}
	}
	return total
}

// extractToolResultText extracts the text content from a tool_result block.
func extractToolResultText(block *ContentBlock) string {
	if block.Text != "" {
		return block.Text
	}
	var text string
	for _, sub := range block.Content {
		if sub.Type == ContentTypeText {
			text += sub.Text
		}
	}
	return text
}
