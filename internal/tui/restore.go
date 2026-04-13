package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wall-ai/agent-engine/internal/engine"
)

// MessagesToChat converts engine messages to TUI ChatMessages for display.
// It walks each message's content blocks and produces the appropriate
// ChatMessage entries (user, assistant, tool_use, tool_result).
func MessagesToChat(msgs []*engine.Message) []ChatMessage {
	var result []ChatMessage
	for _, msg := range msgs {
		switch msg.Role {
		case engine.RoleUser:
			text := extractText(msg)
			// Skip synthetic/meta messages that are not user-visible.
			if msg.IsMeta || text == "" {
				// Still emit tool_result blocks from user messages (API convention).
				for _, b := range msg.Content {
					if b.Type == engine.ContentTypeToolResult {
						output := toolResultText(b)
						result = append(result, ChatMessage{
							Role:     "tool_result",
							ToolName: "",
							Content:  output,
							IsError:  b.IsError,
							ToolID:   b.ToolUseID,
						})
					}
				}
				continue
			}
			result = append(result, ChatMessage{Role: "user", Content: text})
			// Also emit any tool_result blocks in this user message.
			for _, b := range msg.Content {
				if b.Type == engine.ContentTypeToolResult {
					output := toolResultText(b)
					result = append(result, ChatMessage{
						Role:     "tool_result",
						ToolName: "",
						Content:  output,
						IsError:  b.IsError,
						ToolID:   b.ToolUseID,
					})
				}
			}

		case engine.RoleAssistant:
			// Emit text blocks as assistant messages, tool_use blocks separately.
			text := extractText(msg)
			if text != "" {
				result = append(result, ChatMessage{Role: "assistant", Content: text})
			}
			for _, b := range msg.Content {
				if b.Type == engine.ContentTypeToolUse {
					inputStr := ""
					if b.Input != nil {
						if data, err := json.Marshal(b.Input); err == nil {
							inputStr = string(data)
						}
					}
					var toolInput map[string]interface{}
					if inputStr != "" {
						_ = json.Unmarshal([]byte(inputStr), &toolInput)
					}
					result = append(result, ChatMessage{
						Role:      "tool_use",
						ToolName:  b.ToolName,
						Content:   inputStr,
						ToolInput: toolInput,
						ToolID:    b.ToolUseID,
						DotState:  2, // DotSuccess (already completed)
					})
				}
			}

		case engine.RoleSystem:
			text := extractText(msg)
			if text != "" {
				result = append(result, ChatMessage{Role: "system", Content: text})
			}
		}
	}
	return result
}

// extractText concatenates all text content blocks from a message.
func extractText(msg *engine.Message) string {
	var parts []string
	for _, b := range msg.Content {
		if b.Type == engine.ContentTypeText && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// toolResultText extracts readable text from a tool_result content block.
func toolResultText(b *engine.ContentBlock) string {
	if len(b.Content) > 0 {
		var parts []string
		for _, sub := range b.Content {
			if sub.Type == engine.ContentTypeText && sub.Text != "" {
				parts = append(parts, sub.Text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	// Fallback: use the block's own text field.
	if b.Text != "" {
		return b.Text
	}
	return fmt.Sprintf("[tool result for %s]", b.ToolUseID)
}
