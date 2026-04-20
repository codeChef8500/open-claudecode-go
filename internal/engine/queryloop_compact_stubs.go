package engine

// ────────────────────────────────────────────────────────────────────────────
// [P9.T3-T5] Compact stubs — placeholder implementations for HISTORY_SNIP,
// microcompact, and CONTEXT_COLLAPSE. These are feature-gated in TS and
// default to no-op in Go until the full compaction pipeline is ported.
//
// Types CompactionResult, MicrocompactInfo live in config.go.
//
// TS anchors:
//   - snipCompact: query.ts:L400-410, services/compact/snipCompact.ts
//   - microcompact: query.ts:L412-426, services/compact/microCompact.ts
//   - contextCollapse: query.ts:L440-447, services/contextCollapse/
// ────────────────────────────────────────────────────────────────────────────

// SnipResult holds the result of a snip compaction pass.
type SnipResult struct {
	Messages    []*Message
	TokensFreed int
	// BoundaryMessage, if non-nil, is yielded to the stream.
	BoundaryMessage *Message
}

// SnipCompactIfNeeded applies history snip compaction.
// Currently a no-op stub — returns messages unchanged with zero tokens freed.
// TS anchor: services/compact/snipCompact.ts:snipCompactIfNeeded
func SnipCompactIfNeeded(messages []*Message) SnipResult {
	return SnipResult{
		Messages:    messages,
		TokensFreed: 0,
	}
}

// MicrocompactResult holds the result of a microcompact pass.
type MicrocompactResult struct {
	Messages []*Message
	Info     *MicrocompactInfo
}

// PendingCacheEdits tracks deferred cache edit boundary for CACHED_MICROCOMPACT.
type PendingCacheEdits struct {
	BaselineCacheDeletedTokens int
	Trigger                    string
	DeletedToolIDs             []string
}

// ApplyMicrocompact applies microcompact to messages.
// Currently a no-op stub — returns messages unchanged.
// TS anchor: services/compact/microCompact.ts
func ApplyMicrocompact(messages []*Message) MicrocompactResult {
	return MicrocompactResult{
		Messages: messages,
	}
}

// CollapseResult holds the result of a context collapse pass.
type CollapseResult struct {
	Messages       []*Message
	CollapsedCount int
	TokensFreed    int
}

// contextCollapseConfig controls context collapse thresholds.
// TS anchor: services/contextCollapse/constants.ts
const (
	// collapseMaxToolResultChars is the max chars before a tool result is collapsed.
	collapseMaxToolResultChars = 200_000
	// collapseProtectLastN protects the last N messages from collapse.
	collapseProtectLastN = 6
	// collapsePlaceholder replaces oversized tool results.
	collapsePlaceholder = "[tool result collapsed to save context — original output was too large]"
)

// ApplyCollapsesIfNeeded scans older messages for oversized tool results and
// replaces their content with a collapsed placeholder. This is a staged
// operation: the original content is replaced in-place with a shorter summary.
// TS anchor: services/contextCollapse/index.ts:applyCollapsesIfNeeded
func ApplyCollapsesIfNeeded(messages []*Message) CollapseResult {
	if len(messages) <= collapseProtectLastN {
		return CollapseResult{Messages: messages}
	}

	collapsedCount := 0
	tokensFreed := 0
	cutoff := len(messages) - collapseProtectLastN

	for i := 0; i < cutoff; i++ {
		msg := messages[i]
		if msg.Role != RoleUser {
			continue
		}
		for _, block := range msg.Content {
			if block.Type != ContentTypeToolResult {
				continue
			}
			totalChars := 0
			for _, nested := range block.Content {
				totalChars += len(nested.Text)
			}
			if totalChars > collapseMaxToolResultChars {
				// Replace all nested content with collapse placeholder.
				tokensFreed += totalChars / 4
				block.Content = []*ContentBlock{{
					Type: ContentTypeText,
					Text: collapsePlaceholder,
				}}
				collapsedCount++
			}
		}
	}

	return CollapseResult{
		Messages:       messages,
		CollapsedCount: collapsedCount,
		TokensFreed:    tokensFreed,
	}
}

// CollapseRecoverResult holds the result of an overflow recovery drain.
type CollapseRecoverResult struct {
	Messages  []*Message
	Committed int
}

// RecoverFromOverflow is a more aggressive context collapse triggered by a
// prompt-too-long error. It uses a lower threshold than the normal pass.
// TS anchor: services/contextCollapse/index.ts:recoverFromOverflow
func RecoverFromOverflow(messages []*Message) CollapseRecoverResult {
	if len(messages) <= collapseProtectLastN {
		return CollapseRecoverResult{Messages: messages, Committed: 0}
	}

	// More aggressive threshold: 50% of normal.
	aggressiveLimit := collapseMaxToolResultChars / 2
	committed := 0
	cutoff := len(messages) - collapseProtectLastN

	for i := 0; i < cutoff; i++ {
		msg := messages[i]
		if msg.Role != RoleUser {
			continue
		}
		for _, block := range msg.Content {
			if block.Type != ContentTypeToolResult {
				continue
			}
			totalChars := 0
			for _, nested := range block.Content {
				totalChars += len(nested.Text)
			}
			if totalChars > aggressiveLimit {
				block.Content = []*ContentBlock{{
					Type: ContentTypeText,
					Text: collapsePlaceholder,
				}}
				committed++
			}
		}
	}

	return CollapseRecoverResult{
		Messages:  messages,
		Committed: committed,
	}
}

// BuildPostCompactMessages assembles the final message list from a CompactionResult.
// Uses SummaryMessages + MessagesToKeep from the existing CompactionResult type.
// TS anchor: services/compact/compact.ts:buildPostCompactMessages
func BuildPostCompactMessages(result *CompactionResult) []*Message {
	var out []*Message
	out = append(out, result.SummaryMessages...)
	out = append(out, result.MessagesToKeep...)
	return out
}
