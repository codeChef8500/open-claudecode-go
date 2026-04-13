package agent

import (
	"strings"
	"testing"
)

// TestCoordinatorToolWhitelist verifies that CoordinatorModeAllowedTools
// matches the TS COORDINATOR_MODE_ALLOWED_TOOLS exactly (4 tools).
func TestCoordinatorToolWhitelist(t *testing.T) {
	expected := map[string]bool{
		"Task":            true,
		"TaskStop":        true,
		"SendMessage":     true,
		"SyntheticOutput": true,
	}

	if len(CoordinatorModeAllowedTools) != len(expected) {
		t.Fatalf("CoordinatorModeAllowedTools has %d tools, expected %d",
			len(CoordinatorModeAllowedTools), len(expected))
	}

	for name := range expected {
		if !CoordinatorModeAllowedTools[name] {
			t.Errorf("missing tool %q in CoordinatorModeAllowedTools", name)
		}
	}

	// Verify the deprecated alias returns the same set.
	aliasList := CoordinatorAllowedTools
	if len(aliasList) != len(expected) {
		t.Fatalf("CoordinatorAllowedTools (deprecated alias) has %d tools, expected %d",
			len(aliasList), len(expected))
	}
	for _, name := range aliasList {
		if !expected[name] {
			t.Errorf("unexpected tool %q in deprecated CoordinatorAllowedTools", name)
		}
	}
}

// TestCoordinatorSystemPromptSections verifies the system prompt contains
// all 6 sections aligned with the TS coordinatorMode.ts implementation.
func TestCoordinatorSystemPromptSections(t *testing.T) {
	cfg := CoordinatorConfig{
		MaxWorkers:        4,
		MaxTurnsPerWorker: 100,
		WorkDir:           "/tmp/test",
		DefaultModel:      "test-model",
	}
	prompt := BuildCoordinatorSystemPrompt(cfg, nil)

	sections := []string{
		"## 1. Your Role",
		"## 2. Your Tools",
		"## 3. Workers",
		"## 4. Task Workflow",
		"## 5. Writing Worker Prompts",
		"## 6. Example Session",
	}

	for _, section := range sections {
		if !strings.Contains(prompt, section) {
			t.Errorf("system prompt missing section %q", section)
		}
	}

	// Verify key content.
	mustContain := []string{
		"<task-notification>",
		"completed|failed|killed",
		"<usage>",
		"Always synthesize",
		"continue vs. spawn",
		"Good examples",
		"Bad examples",
		// New TS-aligned content:
		`subagent_type`,
		"Add a purpose statement",
		"Continue mechanics",
		"Anti-pattern",
		"How's it going?",
		"task_id",
	}
	for _, keyword := range mustContain {
		if !strings.Contains(prompt, keyword) {
			t.Errorf("system prompt missing keyword %q", keyword)
		}
	}
}

// TestComputeWorkerTools verifies dynamic worker tools list matches
// AsyncAgentAllowedTools minus internalWorkerTools.
func TestComputeWorkerTools(t *testing.T) {
	tools := computeWorkerTools()

	// Must not contain internal tools.
	for _, tool := range tools {
		if internalWorkerTools[tool] {
			t.Errorf("computeWorkerTools() should not include internal tool %q", tool)
		}
	}

	// Every tool must be in AsyncAgentAllowedTools.
	for _, tool := range tools {
		if !AsyncAgentAllowedTools[tool] {
			t.Errorf("computeWorkerTools() returned %q which is not in AsyncAgentAllowedTools", tool)
		}
	}

	// All non-internal async tools must be present.
	expectedCount := 0
	for name := range AsyncAgentAllowedTools {
		if !internalWorkerTools[name] {
			expectedCount++
		}
	}
	if len(tools) != expectedCount {
		t.Errorf("computeWorkerTools() returned %d tools, expected %d", len(tools), expectedCount)
	}

	// Must be sorted.
	for i := 1; i < len(tools); i++ {
		if tools[i] < tools[i-1] {
			t.Errorf("computeWorkerTools() not sorted: %q > %q", tools[i-1], tools[i])
		}
	}
}

// TestComputeSimpleWorkerTools verifies the CLAUDE_CODE_SIMPLE tool set.
func TestComputeSimpleWorkerTools(t *testing.T) {
	tools := computeSimpleWorkerTools()
	expected := []string{"Bash", "FileEdit", "Read"}
	if len(tools) != len(expected) {
		t.Fatalf("computeSimpleWorkerTools() returned %d tools, expected %d", len(tools), len(expected))
	}
	for i, tool := range tools {
		if tool != expected[i] {
			t.Errorf("computeSimpleWorkerTools()[%d] = %q, want %q", i, tool, expected[i])
		}
	}
}

// TestTaskNotificationXML verifies the XML format includes all TS-aligned tags.
func TestTaskNotificationXML(t *testing.T) {
	notifs := []Notification{{
		Type:        NotificationTypeComplete,
		AgentID:     "agent-abc123",
		Description: "Read go.mod",
		Message:     "module github.com/example",
		ToolUseID:   "tool-use-xyz",
		Usage: &NotificationUsage{
			TotalTokens: 1500,
			ToolUses:    3,
			DurationMs:  2000,
		},
		WorktreePath:   "/tmp/worktree/abc",
		WorktreeBranch: "feature/xyz",
	}}

	xml := FormatTaskNotificationXML(notifs)

	mustContain := []string{
		"<task-notification>",
		"<task-id>agent-abc123</task-id>",
		"<tool-use-id>tool-use-xyz</tool-use-id>",
		"<status>completed</status>",
		`<summary>Agent "Read go.mod" completed</summary>`,
		"<result>module github.com/example</result>",
		"<total_tokens>1500</total_tokens>",
		"<tool_uses>3</tool_uses>",
		"<duration_ms>2000</duration_ms>",
		"<worktree-path>/tmp/worktree/abc</worktree-path>",
		"<worktree-branch>feature/xyz</worktree-branch>",
		"</task-notification>",
	}

	for _, s := range mustContain {
		if !strings.Contains(xml, s) {
			t.Errorf("XML missing %q\nGot:\n%s", s, xml)
		}
	}
}

// TestTaskNotificationXMLMinimal verifies XML with only required fields.
func TestTaskNotificationXMLMinimal(t *testing.T) {
	notifs := []Notification{{
		Type:    NotificationTypeComplete,
		AgentID: "agent-min",
		Message: "done",
	}}

	xml := FormatTaskNotificationXML(notifs)

	if !strings.Contains(xml, "<task-id>agent-min</task-id>") {
		t.Error("missing task-id")
	}
	if !strings.Contains(xml, "<status>completed</status>") {
		t.Error("missing status")
	}
	// Optional fields should NOT appear.
	if strings.Contains(xml, "<tool-use-id>") {
		t.Error("tool-use-id should not appear when empty")
	}
	if strings.Contains(xml, "<usage>") {
		t.Error("usage should not appear when nil")
	}
	if strings.Contains(xml, "<worktree>") {
		t.Error("worktree should not appear when empty")
	}
}

// TestTaskNotificationXMLFailedStatus verifies failed agent notification format.
func TestTaskNotificationXMLFailedStatus(t *testing.T) {
	notifs := []Notification{{
		Type:        NotificationTypeError,
		AgentID:     "agent-err",
		Description: "Build project",
		Message:     "compilation failed: missing import",
	}}

	xml := FormatTaskNotificationXML(notifs)

	if !strings.Contains(xml, "<status>failed</status>") {
		t.Errorf("expected failed status, got:\n%s", xml)
	}
	if !strings.Contains(xml, `Agent "Build project" failed: compilation failed`) {
		t.Errorf("expected failed summary with description, got:\n%s", xml)
	}
}

// TestFilterToolsForCoordinator verifies coordinator mode filtering.
func TestFilterToolsForCoordinator(t *testing.T) {
	allTools := []string{
		"Read", "Edit", "Write", "Bash", "Grep", "Glob",
		"Task", "TaskStop", "SendMessage", "SyntheticOutput",
		"WebSearch", "WebFetch", "TodoWrite",
	}

	def := &AgentDefinition{Source: SourceBuiltIn}
	filtered := FilterToolsForAgent(allTools, def, false, false, true)

	expected := map[string]bool{
		"Task":            true,
		"TaskStop":        true,
		"SendMessage":     true,
		"SyntheticOutput": true,
	}

	if len(filtered) != len(expected) {
		t.Fatalf("filtered %d tools for coordinator, expected %d: %v",
			len(filtered), len(expected), filtered)
	}

	for _, name := range filtered {
		if !expected[name] {
			t.Errorf("unexpected tool %q in coordinator filtered set", name)
		}
	}
}
