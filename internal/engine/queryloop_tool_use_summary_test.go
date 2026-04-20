package engine

import (
	"context"
	"testing"
)

type mockSummaryGenerator struct {
	summary string
	err     error
}

func (m *mockSummaryGenerator) GenerateSummary(_ context.Context, _ *ToolUseSummaryInput) (*ToolUseSummaryOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.summary == "" {
		return nil, nil
	}
	return &ToolUseSummaryOutput{Summary: m.summary}, nil
}

func TestStartToolUseSummaryAsync_NilGenerator(t *testing.T) {
	ch := StartToolUseSummaryAsync(context.Background(), nil, nil, nil, nil, false)
	msg, ok := <-ch
	if ok && msg != nil {
		t.Error("expected nil from nil generator")
	}
}

func TestStartToolUseSummaryAsync_NoToolBlocks(t *testing.T) {
	gen := &mockSummaryGenerator{summary: "test"}
	ch := StartToolUseSummaryAsync(context.Background(), gen, nil, nil, nil, false)
	msg, ok := <-ch
	if ok && msg != nil {
		t.Error("expected nil from empty tool blocks")
	}
}

func TestStartToolUseSummaryAsync_Success(t *testing.T) {
	gen := &mockSummaryGenerator{summary: "Files were modified"}
	tools := []*pendingToolCall{
		{ID: "tu-1", Name: "edit"},
	}
	assistantMsgs := []*Message{{
		Role: RoleAssistant,
		Content: []*ContentBlock{{
			Type: ContentTypeText,
			Text: "I'll edit the file",
		}},
	}}
	toolResult := &Message{
		Role: RoleUser,
		Content: []*ContentBlock{{
			Type:      ContentTypeToolResult,
			ToolUseID: "tu-1",
			Content: []*ContentBlock{{
				Type: ContentTypeText,
				Text: "OK",
			}},
		}},
	}

	ch := StartToolUseSummaryAsync(context.Background(), gen, tools, assistantMsgs, toolResult, false)
	msg := <-ch
	if msg == nil {
		t.Fatal("expected summary message")
	}
	if msg.Summary != "Files were modified" {
		t.Errorf("summary = %q", msg.Summary)
	}
	if len(msg.PrecedingToolUseIDs) != 1 || msg.PrecedingToolUseIDs[0] != "tu-1" {
		t.Error("expected preceding tool use IDs")
	}
}

func TestStartToolUseSummaryAsync_EmptySummary(t *testing.T) {
	gen := &mockSummaryGenerator{summary: ""}
	tools := []*pendingToolCall{{ID: "tu-1", Name: "read"}}
	ch := StartToolUseSummaryAsync(context.Background(), gen, tools, nil, nil, false)
	msg := <-ch
	if msg != nil {
		t.Error("expected nil for empty summary")
	}
}
