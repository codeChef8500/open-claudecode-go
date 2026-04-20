package engine

import "testing"

func TestSnipCompactIfNeeded_NoOp(t *testing.T) {
	msgs := []*Message{{Role: RoleUser}}
	result := SnipCompactIfNeeded(msgs)
	if len(result.Messages) != 1 {
		t.Error("expected messages unchanged")
	}
	if result.TokensFreed != 0 {
		t.Error("expected 0 tokens freed")
	}
	if result.BoundaryMessage != nil {
		t.Error("expected no boundary message")
	}
}

func TestApplyMicrocompact_NoOp(t *testing.T) {
	msgs := []*Message{{Role: RoleAssistant}}
	result := ApplyMicrocompact(msgs)
	if len(result.Messages) != 1 {
		t.Error("expected messages unchanged")
	}
	if result.Info != nil {
		t.Error("expected nil info")
	}
}

func TestApplyCollapsesIfNeeded_NoOp(t *testing.T) {
	msgs := []*Message{{Role: RoleUser}}
	result := ApplyCollapsesIfNeeded(msgs)
	if len(result.Messages) != 1 {
		t.Error("expected messages unchanged")
	}
}

func TestRecoverFromOverflow_NoOp(t *testing.T) {
	msgs := []*Message{{Role: RoleUser}}
	result := RecoverFromOverflow(msgs)
	if result.Committed != 0 {
		t.Error("expected 0 committed")
	}
	if len(result.Messages) != 1 {
		t.Error("expected messages unchanged")
	}
}

func TestBuildPostCompactMessages(t *testing.T) {
	cr := &CompactionResult{
		SummaryMessages: []*Message{{Role: RoleSystem}},
		MessagesToKeep:  []*Message{{Role: RoleUser}, {Role: RoleAssistant}},
	}
	out := BuildPostCompactMessages(cr)
	if len(out) != 3 {
		t.Errorf("expected 3 messages, got %d", len(out))
	}
}

func TestApplyCollapsesIfNeeded_CollapseOversized(t *testing.T) {
	// Build 10 messages, with one oversized tool result in the middle.
	bigText := make([]byte, collapseMaxToolResultChars+1000)
	for i := range bigText {
		bigText[i] = 'x'
	}
	msgs := make([]*Message, 10)
	for i := range msgs {
		msgs[i] = &Message{Role: RoleAssistant}
	}
	// Put an oversized tool result at index 2.
	msgs[2] = &Message{
		Role: RoleUser,
		Content: []*ContentBlock{{
			Type:      ContentTypeToolResult,
			ToolUseID: "tu-1",
			Content: []*ContentBlock{{
				Type: ContentTypeText,
				Text: string(bigText),
			}},
		}},
	}

	result := ApplyCollapsesIfNeeded(msgs)
	if result.CollapsedCount != 1 {
		t.Errorf("expected 1 collapsed, got %d", result.CollapsedCount)
	}
	if result.TokensFreed <= 0 {
		t.Error("expected positive tokens freed")
	}
	// Verify content was replaced with placeholder.
	toolBlock := result.Messages[2].Content[0]
	if len(toolBlock.Content) != 1 || toolBlock.Content[0].Text != collapsePlaceholder {
		t.Error("expected collapse placeholder")
	}
}

func TestApplyCollapsesIfNeeded_ProtectsTail(t *testing.T) {
	bigText := make([]byte, collapseMaxToolResultChars+1000)
	for i := range bigText {
		bigText[i] = 'x'
	}
	// Put oversized result in the protected tail (last 6 messages).
	msgs := make([]*Message, 8)
	for i := range msgs {
		msgs[i] = &Message{Role: RoleAssistant}
	}
	msgs[7] = &Message{
		Role: RoleUser,
		Content: []*ContentBlock{{
			Type: ContentTypeToolResult,
			Content: []*ContentBlock{{
				Type: ContentTypeText,
				Text: string(bigText),
			}},
		}},
	}
	result := ApplyCollapsesIfNeeded(msgs)
	if result.CollapsedCount != 0 {
		t.Error("should not collapse protected tail")
	}
}

func TestRecoverFromOverflow_AggressiveThreshold(t *testing.T) {
	// Content between aggressive limit and normal limit should be collapsed.
	midSize := collapseMaxToolResultChars/2 + 1000
	midText := make([]byte, midSize)
	for i := range midText {
		midText[i] = 'y'
	}
	msgs := make([]*Message, 10)
	for i := range msgs {
		msgs[i] = &Message{Role: RoleAssistant}
	}
	msgs[1] = &Message{
		Role: RoleUser,
		Content: []*ContentBlock{{
			Type: ContentTypeToolResult,
			Content: []*ContentBlock{{
				Type: ContentTypeText,
				Text: string(midText),
			}},
		}},
	}
	result := RecoverFromOverflow(msgs)
	if result.Committed != 1 {
		t.Errorf("expected 1 committed, got %d", result.Committed)
	}
}
