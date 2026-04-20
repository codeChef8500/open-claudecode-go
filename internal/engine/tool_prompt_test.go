package engine

import "testing"

func TestToolPromptProvider_Interface(t *testing.T) {
	// Verify ToolPromptProvider is a standalone interface.
	var _ ToolPromptProvider = toolPromptProviderImpl{prompt: "test"}
}

type toolPromptProviderImpl struct {
	prompt string
}

func (tp toolPromptProviderImpl) GetToolPrompt(_ ToolPromptContext) string {
	return tp.prompt
}

func TestToolPromptContext_Fields(t *testing.T) {
	ctx := ToolPromptContext{
		CWD:              "/tmp",
		TurnCount:        5,
		Model:            "claude-3",
		IsNonInteractive: true,
	}
	if ctx.CWD != "/tmp" {
		t.Error("wrong CWD")
	}
	if ctx.TurnCount != 5 {
		t.Error("wrong TurnCount")
	}
	if ctx.Model != "claude-3" {
		t.Error("wrong Model")
	}
	if !ctx.IsNonInteractive {
		t.Error("expected IsNonInteractive")
	}
}

func TestInjectToolPrompts_NoTools(t *testing.T) {
	base := []SystemPromptPart{{Content: "base"}}
	result := InjectToolPrompts(base, nil, ToolPromptContext{})
	if len(result) != 1 {
		t.Error("should return base unchanged when no tools")
	}
}

func TestBuildToolPrompts_NilTools(t *testing.T) {
	result := BuildToolPrompts(nil, ToolPromptContext{})
	if result != "" {
		t.Error("expected empty for nil tools")
	}
}

func TestSystemPromptPart_ToolContext(t *testing.T) {
	// Verify SystemPromptPart can carry tool context content.
	part := SystemPromptPart{Content: "tool info here"}
	if part.Content != "tool info here" {
		t.Error("wrong content")
	}
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
