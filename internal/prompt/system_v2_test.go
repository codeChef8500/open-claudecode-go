package prompt

import (
	"strings"
	"testing"

	"github.com/wall-ai/agent-engine/internal/prompt/sections"
)

func TestBuildEffectiveSystemPromptV2_FallbackToV1(t *testing.T) {
	// When UseNewPromptBuilder is false, should delegate to original
	result := BuildEffectiveSystemPromptV2(BuildOptions{
		WorkDir: ".",
	})
	if result == nil {
		t.Fatal("result should not be nil")
	}
	// V1 always produces something (at least env context)
	if result.Text == "" {
		t.Error("V1 fallback should produce non-empty text")
	}
}

func TestBuildEffectiveSystemPromptV2_Basic(t *testing.T) {
	sections.ClearAll()
	defer sections.ClearAll()

	result := BuildEffectiveSystemPromptV2(BuildOptions{
		UseNewPromptBuilder: true,
		WorkDir:             "/home/test/project",
		Model:               "claude-sonnet-4-6",
	})
	if result == nil {
		t.Fatal("result should not be nil")
	}
	if result.Text == "" {
		t.Fatal("V2 should produce non-empty text")
	}
	// Check key sections are present
	if !strings.Contains(result.Text, "# System") {
		t.Error("missing # System section")
	}
	if !strings.Contains(result.Text, "# Executing actions") {
		t.Error("missing actions section")
	}
}

func TestBuildEffectiveSystemPromptV2_CustomAndAppend(t *testing.T) {
	sections.ClearAll()
	defer sections.ClearAll()

	result := BuildEffectiveSystemPromptV2(BuildOptions{
		UseNewPromptBuilder: true,
		WorkDir:             "/tmp",
		CustomSystemPrompt:  "CUSTOM_PROMPT_HERE",
		AppendSystemPrompt:  "APPEND_PROMPT_HERE",
	})
	if !strings.Contains(result.Text, "CUSTOM_PROMPT_HERE") {
		t.Error("missing custom system prompt")
	}
	if !strings.Contains(result.Text, "APPEND_PROMPT_HERE") {
		t.Error("missing append system prompt")
	}
}

func TestBuildEffectiveSystemPromptV2_Memory(t *testing.T) {
	sections.ClearAll()
	defer sections.ClearAll()

	result := BuildEffectiveSystemPromptV2(BuildOptions{
		UseNewPromptBuilder: true,
		WorkDir:             "/tmp",
		MemoryContent:       "MEMORY_CONTENT_XYZ",
	})
	if !strings.Contains(result.Text, "MEMORY_CONTENT_XYZ") {
		t.Error("missing memory content in V2 output")
	}
}

func TestBuildEffectiveSystemPromptV2_AutoMemoryOverrides(t *testing.T) {
	sections.ClearAll()
	defer sections.ClearAll()

	result := BuildEffectiveSystemPromptV2(BuildOptions{
		UseNewPromptBuilder: true,
		WorkDir:             "/tmp",
		MemoryContent:       "OLD_MEMORY",
		AutoMemoryPrompt:    "AUTO_MEMORY_PROMPT",
	})
	if strings.Contains(result.Text, "OLD_MEMORY") {
		t.Error("auto memory should override regular memory")
	}
	if !strings.Contains(result.Text, "AUTO_MEMORY_PROMPT") {
		t.Error("missing auto memory prompt")
	}
}

func TestBuildEffectiveSystemPromptV2_Parts(t *testing.T) {
	sections.ClearAll()
	defer sections.ClearAll()

	result := BuildEffectiveSystemPromptV2(BuildOptions{
		UseNewPromptBuilder: true,
		WorkDir:             "/tmp",
	})
	if len(result.Parts) == 0 {
		t.Error("V2 should produce non-empty Parts for cache-aware usage")
	}
	// First part should have CacheHint = true
	if len(result.Parts) > 0 && !result.Parts[0].CacheHint {
		t.Error("first part should have CacheHint=true")
	}
}
