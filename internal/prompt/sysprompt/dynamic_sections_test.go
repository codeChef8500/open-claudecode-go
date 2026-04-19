package sysprompt

import (
	"strings"
	"testing"

	"github.com/wall-ai/agent-engine/internal/prompt/sections"
)

// ── P3.T1 session guidance ─────────────────────────────────────────────────

func TestGetSessionSpecificGuidanceSection_Empty(t *testing.T) {
	o := SessionGuidanceOpts{IsNonInteractive: true}
	if s := GetSessionSpecificGuidanceSection(o); s != "" {
		t.Errorf("expected empty, got %q", s)
	}
}

func TestGetSessionSpecificGuidanceSection_WithAskUser(t *testing.T) {
	o := SessionGuidanceOpts{
		HasAskUserQuestionTool: true,
		AskUserQuestionName:    "AskUser",
	}
	s := GetSessionSpecificGuidanceSection(o)
	if !strings.Contains(s, "AskUser") {
		t.Error("should mention AskUser tool")
	}
}

func TestGetSessionSpecificGuidanceSection_Interactive(t *testing.T) {
	o := SessionGuidanceOpts{IsNonInteractive: false}
	s := GetSessionSpecificGuidanceSection(o)
	if !strings.Contains(s, "! <command>") {
		t.Error("interactive session should mention ! prefix")
	}
}

func TestGetSessionSpecificGuidanceSection_NonInteractive(t *testing.T) {
	o := SessionGuidanceOpts{IsNonInteractive: true}
	s := GetSessionSpecificGuidanceSection(o)
	if strings.Contains(s, "! <command>") {
		t.Error("non-interactive should NOT mention ! prefix")
	}
}

// ── P3.T2 MCP instructions ────────────────────────────────────────────────

func TestGetMCPInstructionsSection_Empty(t *testing.T) {
	if s := GetMCPInstructionsSection(nil); s != "" {
		t.Errorf("nil clients should return empty, got %q", s)
	}
}

func TestGetMCPInstructionsSection_NoInstructions(t *testing.T) {
	clients := []MCPClientInfo{{Name: "foo", Instructions: ""}}
	if s := GetMCPInstructionsSection(clients); s != "" {
		t.Errorf("client without instructions should be skipped, got %q", s)
	}
}

func TestGetMCPInstructionsSection_WithInstructions(t *testing.T) {
	clients := []MCPClientInfo{
		{Name: "ServerA", Instructions: "Use tool X."},
		{Name: "ServerB", Instructions: "Use tool Y."},
	}
	s := GetMCPInstructionsSection(clients)
	if !strings.Contains(s, "# MCP Server Instructions") {
		t.Error("missing header")
	}
	if !strings.Contains(s, "## ServerA") || !strings.Contains(s, "## ServerB") {
		t.Error("missing server subheaders")
	}
}

// ── P3.T3 scratchpad ──────────────────────────────────────────────────────

func TestGetScratchpadInstructions_Disabled(t *testing.T) {
	if s := GetScratchpadInstructions(false, "/tmp/sp"); s != "" {
		t.Error("disabled should return empty")
	}
}

func TestGetScratchpadInstructions_Enabled(t *testing.T) {
	s := GetScratchpadInstructions(true, "/tmp/session123")
	if !strings.Contains(s, "# Scratchpad Directory") {
		t.Error("missing header")
	}
	if !strings.Contains(s, "/tmp/session123") {
		t.Error("missing scratchpad dir")
	}
}

// ── P3.T4 FRC ──────────────────────────────────────────────────────────────

func TestGetFunctionResultClearingSection_Disabled(t *testing.T) {
	if s := GetFunctionResultClearingSection(false, true, 5); s != "" {
		t.Error("disabled should return empty")
	}
}

func TestGetFunctionResultClearingSection_Enabled(t *testing.T) {
	s := GetFunctionResultClearingSection(true, true, 3)
	if !strings.Contains(s, "3 most recent") {
		t.Error("should mention keepRecent count")
	}
}

func TestSummarizeToolResultsSection(t *testing.T) {
	if SummarizeToolResultsSection == "" {
		t.Fatal("SummarizeToolResultsSection should not be empty")
	}
}

// ── P3.T5 proactive ───────────────────────────────────────────────────────

func TestGetProactiveSection_Inactive(t *testing.T) {
	if s := GetProactiveSection(false, ProactiveOpts{}); s != "" {
		t.Error("inactive should return empty")
	}
}

func TestGetProactiveSection_Active(t *testing.T) {
	s := GetProactiveSection(true, ProactiveOpts{
		TickTag:       "tick",
		SleepToolName: "Sleep",
	})
	if !strings.Contains(s, "# Autonomous work") {
		t.Error("missing header")
	}
	if !strings.Contains(s, "Sleep") {
		t.Error("should mention sleep tool")
	}
}

// ── P3.T6 getSystemPrompt assembler ────────────────────────────────────────

func TestGetSystemPrompt_Simple(t *testing.T) {
	out := GetSystemPrompt(SystemPromptOpts{
		IsSimple: true,
		CWD:      "/home/user/project",
	})
	if len(out) != 1 {
		t.Fatalf("simple mode should return 1 element, got %d", len(out))
	}
	if !strings.Contains(out[0], "Claude Code") {
		t.Error("simple mode should mention Claude Code")
	}
}

func TestGetSystemPrompt_Normal_HasSections(t *testing.T) {
	sections.ClearAll()
	out := GetSystemPrompt(SystemPromptOpts{
		CWD:   "/home/user/project",
		Model: "claude-sonnet-4-6",
		ToolNames: ToolNames{
			BashTool: "Bash", FileReadTool: "Read", FileEditTool: "Edit",
			FileWriteTool: "Write", GlobTool: "Glob", GrepTool: "Grep",
			AgentTool: "Agent", TaskTool: "Task",
		},
		EnabledToolNames: map[string]bool{"Agent": true, "Task": true},
	})
	if len(out) < 5 {
		t.Errorf("expected at least 5 prompt sections, got %d", len(out))
	}
	// Check key sections present
	found := map[string]bool{}
	for _, s := range out {
		if strings.HasPrefix(s, "# System") {
			found["system"] = true
		}
		if strings.Contains(s, "# Executing actions") {
			found["actions"] = true
		}
		if strings.Contains(s, "# Using your tools") {
			found["tools"] = true
		}
		if strings.Contains(s, "# Output efficiency") || strings.Contains(s, "# Communicating") {
			found["output"] = true
		}
	}
	for _, key := range []string{"system", "actions", "tools", "output"} {
		if !found[key] {
			t.Errorf("missing section: %s", key)
		}
	}
	sections.ClearAll()
}

func TestGetSystemPrompt_Boundary(t *testing.T) {
	sections.ClearAll()
	out := GetSystemPrompt(SystemPromptOpts{
		CWD:                 "/tmp",
		UseGlobalCacheScope: true,
		ToolNames:           ToolNames{BashTool: "Bash", FileReadTool: "R", FileEditTool: "E", FileWriteTool: "W"},
	})
	found := false
	for _, s := range out {
		if s == "__SYSTEM_PROMPT_DYNAMIC_BOUNDARY__" {
			found = true
			break
		}
	}
	if !found {
		t.Error("boundary marker should be present when UseGlobalCacheScope=true")
	}
	sections.ClearAll()
}

func TestDefaultAgentPrompt(t *testing.T) {
	if !strings.Contains(DefaultAgentPrompt, "agent for Claude Code") {
		t.Error("DefaultAgentPrompt should contain expected text")
	}
}
