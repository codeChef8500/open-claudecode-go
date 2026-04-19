package sysprompt

import (
	"strings"
	"testing"
)

// ── P2.T1 intro / system / hooks / reminders ──────────────────────────────

func TestGetHooksSection(t *testing.T) {
	s := GetHooksSection()
	if !strings.Contains(s, "hooks") {
		t.Error("hooks section missing keyword 'hooks'")
	}
}

func TestGetSimpleIntroSection_NoStyle(t *testing.T) {
	s := GetSimpleIntroSection("")
	if !strings.Contains(s, "software engineering tasks") {
		t.Error("expected software engineering text in intro without style")
	}
	if !strings.Contains(s, "IMPORTANT:") {
		t.Error("missing CYBER_RISK_INSTRUCTION")
	}
}

func TestGetSimpleIntroSection_WithStyle(t *testing.T) {
	s := GetSimpleIntroSection("Concise")
	if !strings.Contains(s, "Output Style") {
		t.Error("expected Output Style reference when style is active")
	}
}

func TestGetSimpleSystemSection(t *testing.T) {
	s := GetSimpleSystemSection()
	if !strings.HasPrefix(s, "# System") {
		t.Error("should start with # System")
	}
	if !strings.Contains(s, " - ") {
		t.Error("should contain bullet lines")
	}
}

func TestGetSystemRemindersSection(t *testing.T) {
	s := GetSystemRemindersSection()
	if !strings.Contains(s, "system-reminder") {
		t.Error("missing system-reminder tag reference")
	}
}

// ── P2.T2 doing_tasks ─────────────────────────────────────────────────────

func TestGetSimpleDoingTasksSection_NonAnt(t *testing.T) {
	s := GetSimpleDoingTasksSection(false, "", "")
	if !strings.HasPrefix(s, "# Doing tasks") {
		t.Error("should start with # Doing tasks")
	}
	if strings.Contains(s, "collaborator, not just an executor") {
		t.Error("ant-only paragraph should not appear for non-ant")
	}
}

func TestGetSimpleDoingTasksSection_Ant(t *testing.T) {
	s := GetSimpleDoingTasksSection(true, "AskUser", "run /issue")
	if !strings.Contains(s, "collaborator, not just an executor") {
		t.Error("ant-only paragraph should appear for ant")
	}
	if !strings.Contains(s, "AskUser") {
		t.Error("should reference injected tool name")
	}
}

// ── P2.T3 actions + tone_style ─────────────────────────────────────────────

func TestGetActionsSection(t *testing.T) {
	s := GetActionsSection()
	if !strings.HasPrefix(s, "# Executing actions with care") {
		t.Error("wrong header")
	}
	if !strings.Contains(s, "measure twice, cut once") {
		t.Error("missing closing guidance")
	}
}

func TestGetSimpleToneAndStyleSection_NonAnt(t *testing.T) {
	s := GetSimpleToneAndStyleSection(false)
	if !strings.Contains(s, "short and concise") {
		t.Error("non-ant should include 'short and concise'")
	}
}

func TestGetSimpleToneAndStyleSection_Ant(t *testing.T) {
	s := GetSimpleToneAndStyleSection(true)
	if strings.Contains(s, "short and concise") {
		t.Error("ant should NOT include 'short and concise'")
	}
}

// ── P2.T4 using_tools ─────────────────────────────────────────────────────

func TestGetUsingYourToolsSection_Normal(t *testing.T) {
	tn := ToolNames{
		BashTool: "Bash", FileReadTool: "Read", FileEditTool: "Edit",
		FileWriteTool: "Write", GlobTool: "Glob", GrepTool: "Grep",
		AgentTool: "Agent", TaskTool: "Task",
	}
	s := GetUsingYourToolsSection(tn, false, false)
	if !strings.HasPrefix(s, "# Using your tools") {
		t.Error("wrong header")
	}
	if !strings.Contains(s, "Glob") {
		t.Error("should mention Glob when not embedded")
	}
}

func TestGetUsingYourToolsSection_EmbeddedSearch(t *testing.T) {
	tn := ToolNames{
		BashTool: "Bash", FileReadTool: "Read", FileEditTool: "Edit",
		FileWriteTool: "Write", GlobTool: "Glob", GrepTool: "Grep",
		AgentTool: "Agent", TaskTool: "",
	}
	s := GetUsingYourToolsSection(tn, true, false)
	if strings.Contains(s, "Glob") {
		t.Error("should NOT mention Glob when embedded")
	}
}

func TestGetUsingYourToolsSection_REPL_NoTask(t *testing.T) {
	tn := ToolNames{BashTool: "Bash"}
	s := GetUsingYourToolsSection(tn, false, true)
	if s != "" {
		t.Errorf("REPL mode without task tool should return empty, got %q", s)
	}
}

func TestGetUsingYourToolsSection_REPL_WithTask(t *testing.T) {
	tn := ToolNames{BashTool: "Bash", TaskTool: "Task"}
	s := GetUsingYourToolsSection(tn, false, true)
	if !strings.Contains(s, "Task") {
		t.Error("REPL mode with task tool should mention task tool")
	}
}

// ── P2.T5 output_efficiency ────────────────────────────────────────────────

func TestGetOutputEfficiencySection_NonAnt(t *testing.T) {
	s := GetOutputEfficiencySection(false)
	if !strings.HasPrefix(s, "# Output efficiency") {
		t.Error("wrong header for non-ant")
	}
}

func TestGetOutputEfficiencySection_Ant(t *testing.T) {
	s := GetOutputEfficiencySection(true)
	if !strings.HasPrefix(s, "# Communicating with the user") {
		t.Error("wrong header for ant")
	}
}

// ── P2.T3 language / output style ──────────────────────────────────────────

func TestGetLanguageSection_Empty(t *testing.T) {
	if s := GetLanguageSection(""); s != "" {
		t.Errorf("should be empty, got %q", s)
	}
}

func TestGetLanguageSection_Set(t *testing.T) {
	s := GetLanguageSection("Japanese")
	if !strings.HasPrefix(s, "# Language") {
		t.Error("wrong header")
	}
	if !strings.Contains(s, "Japanese") {
		t.Error("should reference the language")
	}
}

func TestGetOutputStyleSection_Empty(t *testing.T) {
	if s := GetOutputStyleSection("", ""); s != "" {
		t.Errorf("should be empty, got %q", s)
	}
}

func TestGetOutputStyleSection_Set(t *testing.T) {
	s := GetOutputStyleSection("Concise", "Be brief.")
	if !strings.HasPrefix(s, "# Output Style: Concise") {
		t.Error("wrong header")
	}
	if !strings.Contains(s, "Be brief.") {
		t.Error("should contain prompt body")
	}
}

// ── P2.T4 agent tool section ───────────────────────────────────────────────

func TestGetAgentToolSection_Fork(t *testing.T) {
	s := GetAgentToolSection("Agent", true)
	if !strings.Contains(s, "fork") {
		t.Error("fork mode should mention fork")
	}
}

func TestGetAgentToolSection_NoFork(t *testing.T) {
	s := GetAgentToolSection("Agent", false)
	if !strings.Contains(s, "specialized agents") {
		t.Error("non-fork mode should mention specialized agents")
	}
}
