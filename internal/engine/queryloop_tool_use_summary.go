package engine

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
)

// ────────────────────────────────────────────────────────────────────────────
// [P9.T14] Tool use summary — async generation of human-readable summaries
// for tool batches, yielded at the start of the next iteration.
// Mirrors TS query.ts:L1411-1482 + services/toolUseSummary/
// ────────────────────────────────────────────────────────────────────────────

// ToolUseSummaryGenerator generates human-readable summaries of tool batches.
// Implementations may use a lightweight model (e.g. Haiku) for generation.
type ToolUseSummaryGenerator interface {
	// GenerateSummary produces a summary for a batch of tool calls.
	// Returns nil if no summary should be emitted.
	GenerateSummary(ctx context.Context, input *ToolUseSummaryInput) (*ToolUseSummaryOutput, error)
}

// ToolUseSummaryInput holds the data needed to generate a tool use summary.
type ToolUseSummaryInput struct {
	// Tools is the list of tool calls and their results.
	Tools []ToolSummaryInfo
	// LastAssistantText is the last assistant text block (for context).
	LastAssistantText string
	// IsNonInteractive is true for SDK/headless sessions.
	IsNonInteractive bool
}

// ToolSummaryInfo holds info about a single tool call for summary generation.
type ToolSummaryInfo struct {
	Name   string
	Input  interface{}
	Output interface{}
}

// ToolUseSummaryOutput holds the generated summary text.
type ToolUseSummaryOutput struct {
	Summary string
}

// StartToolUseSummaryAsync fires off summary generation without blocking
// the next API call. Returns a channel that will receive the result.
// TS anchor: query.ts:L1468-1482
func StartToolUseSummaryAsync(
	ctx context.Context,
	generator ToolUseSummaryGenerator,
	toolBlocks []*pendingToolCall,
	assistantMsgs []*Message,
	toolResultMsg *Message,
	isNonInteractive bool,
) <-chan *Message {
	ch := make(chan *Message, 1)
	if generator == nil || len(toolBlocks) == 0 {
		close(ch)
		return ch
	}

	go func() {
		defer close(ch)

		// Extract last assistant text for context.
		var lastAssistantText string
		if len(assistantMsgs) > 0 {
			last := assistantMsgs[len(assistantMsgs)-1]
			for i := len(last.Content) - 1; i >= 0; i-- {
				if last.Content[i].Type == ContentTypeText {
					lastAssistantText = last.Content[i].Text
					break
				}
			}
		}

		// Build tool info.
		var tools []ToolSummaryInfo
		for _, tc := range toolBlocks {
			info := ToolSummaryInfo{
				Name:  tc.Name,
				Input: tc.Input,
			}
			// Find matching tool result.
			if toolResultMsg != nil {
				for _, block := range toolResultMsg.Content {
					if block.Type == ContentTypeToolResult && block.ToolUseID == tc.ID {
						info.Output = extractToolResultText(block)
						break
					}
				}
			}
			tools = append(tools, info)
		}

		output, err := generator.GenerateSummary(ctx, &ToolUseSummaryInput{
			Tools:             tools,
			LastAssistantText: lastAssistantText,
			IsNonInteractive:  isNonInteractive,
		})
		if err != nil {
			slog.Debug("tool_use_summary: generation failed", slog.Any("err", err))
			return
		}
		if output == nil || output.Summary == "" {
			return
		}

		// Build tool use IDs list.
		toolUseIDs := make([]string, len(toolBlocks))
		for i, tc := range toolBlocks {
			toolUseIDs[i] = tc.ID
		}

		ch <- &Message{
			UUID:    uuid.New().String(),
			Role:    RoleSystem,
			Type:    MsgTypeToolUseSummary,
			Content: []*ContentBlock{{Type: ContentTypeText, Text: output.Summary}},
			Summary: output.Summary,
			PrecedingToolUseIDs: toolUseIDs,
		}
	}()

	return ch
}
