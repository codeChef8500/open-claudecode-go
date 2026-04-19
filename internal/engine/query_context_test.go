package engine

import (
	"testing"
)

func TestSystemPromptParts_Empty(t *testing.T) {
	p := &SystemPromptParts{
		UserContext:   make(map[string]string),
		SystemContext: make(map[string]string),
	}
	if len(p.DefaultSystemPrompt) != 0 {
		t.Error("expected empty DefaultSystemPrompt")
	}
}

func TestAssembleSystemPrompt_DefaultOnly(t *testing.T) {
	parts := &SystemPromptParts{
		DefaultSystemPrompt: []string{"section1", "section2"},
	}
	got := AssembleSystemPrompt(parts, "", "")
	want := "section1\n\nsection2"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAssembleSystemPrompt_CustomReplacesDefault(t *testing.T) {
	parts := &SystemPromptParts{
		DefaultSystemPrompt: []string{"default_section"},
	}
	got := AssembleSystemPrompt(parts, "custom_prompt", "")
	if got != "custom_prompt" {
		t.Errorf("custom should replace default, got %q", got)
	}
}

func TestAssembleSystemPrompt_AppendAdded(t *testing.T) {
	parts := &SystemPromptParts{
		DefaultSystemPrompt: []string{"base"},
	}
	got := AssembleSystemPrompt(parts, "", "appended")
	want := "base\n\nappended"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAssembleSystemPrompt_CustomPlusAppend(t *testing.T) {
	parts := &SystemPromptParts{}
	got := AssembleSystemPrompt(parts, "custom", "append")
	want := "custom\n\nappend"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestJoinDirs(t *testing.T) {
	got := joinDirs([]string{"/a", "/b", "/c"})
	want := "/a\n/b\n/c"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestJoinDirs_Empty(t *testing.T) {
	got := joinDirs(nil)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestCacheSafeParams_Fields(t *testing.T) {
	p := CacheSafeParams{
		SystemPrompt:  "prompt",
		UserContext:    map[string]string{"k": "v"},
		SystemContext:  map[string]string{"env": "prod"},
	}
	if p.SystemPrompt != "prompt" {
		t.Error("SystemPrompt mismatch")
	}
	if p.UserContext["k"] != "v" {
		t.Error("UserContext mismatch")
	}
}
