package engine

import (
	"strings"
	"testing"
)

func TestApplyToolResultBudget_NilState(t *testing.T) {
	msgs := []*Message{{Role: RoleUser}}
	result := ApplyToolResultBudget(msgs, nil, nil)
	if len(result) != 1 {
		t.Fatal("expected messages unchanged")
	}
}

func TestApplyToolResultBudget_UnderLimit(t *testing.T) {
	state := NewContentReplacementState(80000)
	msgs := []*Message{{
		Role: RoleUser,
		Content: []*ContentBlock{{
			Type:      ContentTypeToolResult,
			ToolUseID: "tu-1",
			Content: []*ContentBlock{{
				Type: ContentTypeText,
				Text: "short result",
			}},
		}},
	}}
	result := ApplyToolResultBudget(msgs, state, nil)
	// Should not be replaced.
	if result[0].Content[0].Content[0].Text != "short result" {
		t.Error("expected content unchanged")
	}
}

func TestApplyToolResultBudget_OverLimit(t *testing.T) {
	state := NewContentReplacementState(80000)
	bigText := strings.Repeat("x", DefaultMaxToolResultChars+1)
	msgs := []*Message{{
		Role: RoleUser,
		Content: []*ContentBlock{{
			Type:      ContentTypeToolResult,
			ToolUseID: "tu-2",
			ToolName:  "my_tool",
			Content: []*ContentBlock{{
				Type: ContentTypeText,
				Text: bigText,
			}},
		}},
	}}
	result := ApplyToolResultBudget(msgs, state, nil)
	// Should be replaced with truncation marker.
	replaced := result[0].Content[0].Content[0].Text
	if replaced != ToolResultTruncatedMarker {
		t.Errorf("expected truncation marker, got %q", replaced)
	}
	// Check state recorded the replacement.
	rec, ok := state.GetRecord("tu-2")
	if !ok || !rec.Replaced {
		t.Error("expected replacement recorded in state")
	}
}

func TestApplyToolResultBudget_UnlimitedTool(t *testing.T) {
	state := NewContentReplacementState(80000)
	bigText := strings.Repeat("x", DefaultMaxToolResultChars+1)
	msgs := []*Message{{
		Role: RoleUser,
		Content: []*ContentBlock{{
			Type:      ContentTypeToolResult,
			ToolUseID: "tu-3",
			ToolName:  "unlimited_tool",
			Content: []*ContentBlock{{
				Type: ContentTypeText,
				Text: bigText,
			}},
		}},
	}}
	unlimited := map[string]bool{"unlimited_tool": true}
	result := ApplyToolResultBudget(msgs, state, unlimited)
	// Should NOT be replaced.
	if result[0].Content[0].Content[0].Text != bigText {
		t.Error("expected unlimited tool content unchanged")
	}
}

func TestMeasureToolResultChars(t *testing.T) {
	block := &ContentBlock{
		Type: ContentTypeToolResult,
		Text: "abc",
		Content: []*ContentBlock{
			{Type: ContentTypeText, Text: "defgh"},
			{Type: ContentTypeImage, Data: "ignored"},
		},
	}
	if got := measureToolResultChars(block); got != 8 {
		t.Errorf("measureToolResultChars = %d, want 8", got)
	}
}
