package engine

import (
	"os"
	"testing"
)

func TestGetRuntimeMainLoopModel_Default(t *testing.T) {
	os.Unsetenv("CLAUDE_CODE_MODEL_OVERRIDE")
	got := GetRuntimeMainLoopModel("claude-sonnet-4-6")
	if got != "claude-sonnet-4-6" {
		t.Errorf("got %s, want claude-sonnet-4-6", got)
	}
}

func TestGetRuntimeMainLoopModel_Override(t *testing.T) {
	os.Setenv("CLAUDE_CODE_MODEL_OVERRIDE", "claude-opus-4")
	defer os.Unsetenv("CLAUDE_CODE_MODEL_OVERRIDE")
	got := GetRuntimeMainLoopModel("claude-sonnet-4-6")
	if got != "claude-opus-4" {
		t.Errorf("got %s, want claude-opus-4", got)
	}
}

func TestGetMemoryMechanicsPrompt_Disabled(t *testing.T) {
	os.Unsetenv("CLAUDE_COWORK_MEMORY_PATH_OVERRIDE")
	got := GetMemoryMechanicsPrompt()
	if got != "" {
		t.Errorf("expected empty, got %s", got)
	}
}

func TestGetMemoryMechanicsPrompt_Enabled(t *testing.T) {
	os.Setenv("CLAUDE_COWORK_MEMORY_PATH_OVERRIDE", "/tmp/memory")
	defer os.Unsetenv("CLAUDE_COWORK_MEMORY_PATH_OVERRIDE")
	got := GetMemoryMechanicsPrompt()
	if got == "" {
		t.Error("expected non-empty")
	}
	if !contains(got, "/tmp/memory") {
		t.Errorf("expected path in prompt, got %s", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsHelper(s, sub))
}

func containsHelper(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
