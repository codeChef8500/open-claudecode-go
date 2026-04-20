package engine

import (
	"context"
	"fmt"
	"strings"
)

// compactSystemPrompt is aligned with TS services/compact/compact.ts:getSystemPrompt.
const compactSystemPrompt = `You are a conversation compactor. Your job is to create a detailed summary of a conversation that can be used as a replacement for the full conversation history.

The summary must:
1. Preserve ALL important technical details: file paths, function names, variable names, class names, error messages, stack traces, URLs, command names
2. Preserve ALL decisions made and their reasoning
3. Preserve ALL code that was written, modified, or discussed (include actual code snippets)
4. Preserve ALL open tasks, todos, and next steps
5. Preserve the current state of any work in progress
6. Preserve any constraints or requirements that were established
7. Note any tools that were used and their results
8. Note any errors encountered and how they were resolved (or not)

Format your output as:

## Summary
[High-level overview of the conversation in 1-2 sentences]

## Key Decisions
- [Decision 1 and reasoning]
- [Decision 2 and reasoning]

## What Was Done
- [Action 1 with specific details]
- [Action 2 with specific details]

## Current State
[Description of where things currently stand]

## Open Tasks / Next Steps
- [Task 1]
- [Task 2]

## Important Context
- [Any constraints, requirements, file paths, or technical details that must be preserved]

Be thorough. It is much better to include too much detail than too little. The person reading this summary will not have access to the original conversation.`

// CompactMessages summarises the given messages into a single synthetic user
// message, then returns a fresh message slice suitable for resuming the session.
// This is the Go equivalent of /compact in the TypeScript implementation.
func CompactMessages(
	ctx context.Context,
	prov ModelCaller,
	messages []*Message,
	model string,
) ([]*Message, string, error) {
	return CompactMessagesWithOpts(ctx, prov, messages, model, "")
}

// CompactMessagesWithOpts is like CompactMessages but accepts optional custom
// instructions to inject into the compact prompt.
// TS anchor: services/compact/compact.ts:compactConversation
func CompactMessagesWithOpts(
	ctx context.Context,
	prov ModelCaller,
	messages []*Message,
	model string,
	customInstructions string,
) ([]*Message, string, error) {

	if len(messages) == 0 {
		return messages, "", nil
	}

	// Build plain-text transcript for the summariser.
	var sb strings.Builder
	for _, m := range messages {
		role := string(m.Role)
		for _, b := range m.Content {
			switch b.Type {
			case ContentTypeText:
				if b.Text != "" {
					fmt.Fprintf(&sb, "[%s]: %s\n\n", role, b.Text)
				}
			case ContentTypeToolUse:
				fmt.Fprintf(&sb, "[%s uses tool %s]\n\n", role, b.ToolName)
			case ContentTypeToolResult:
				var parts []string
				for _, inner := range b.Content {
					if inner.Type == ContentTypeText {
						// Truncate very large tool results in transcript.
						text := inner.Text
						if len(text) > 2000 {
							text = text[:2000] + "\n... [truncated]"
						}
						parts = append(parts, text)
					}
				}
				fmt.Fprintf(&sb, "[tool result]: %s\n\n", strings.Join(parts, " "))
			}
		}
	}

	// Build effective system prompt with optional custom instructions.
	effPrompt := compactSystemPrompt
	if customInstructions != "" {
		effPrompt += "\n\nAdditional instructions from the user:\n" + customInstructions
	}

	summariserMsg := []*Message{
		{
			Role: RoleUser,
			Content: []*ContentBlock{
				{Type: ContentTypeText, Text: "Summarise this conversation:\n\n" + sb.String()},
			},
		},
	}

	params := CallParams{
		Model:        model,
		MaxTokens:    8192,
		SystemPrompt: effPrompt,
		Messages:     summariserMsg,
	}

	eventCh, err := prov.CallModel(ctx, params)
	if err != nil {
		return nil, "", fmt.Errorf("compact: %w", err)
	}

	var summary strings.Builder
	for ev := range eventCh {
		if ev.Type == EventTextDelta {
			summary.WriteString(ev.Text)
		}
		if ev.Type == EventError {
			return nil, "", fmt.Errorf("compact provider error: %s", ev.Error)
		}
	}

	summaryStr := summary.String()

	// Build the compacted message history: one synthetic context message.
	compactedMessages := []*Message{
		{
			Role: RoleUser,
			Content: []*ContentBlock{
				{Type: ContentTypeText, Text: "[Previous conversation summary]\n\n" + summaryStr},
			},
		},
		{
			Role: RoleAssistant,
			Content: []*ContentBlock{
				{Type: ContentTypeText, Text: "I have reviewed the conversation summary and am ready to continue. I will use the context above to inform my responses."},
			},
		},
	}

	return compactedMessages, summaryStr, nil
}

// EstimateTokens provides a rough token estimate (4 chars ≈ 1 token).
func EstimateTokens(messages []*Message) int {
	total := 0
	for _, m := range messages {
		for _, b := range m.Content {
			total += len(b.Text)/4 + len(b.Thinking)/4
		}
	}
	return total
}
