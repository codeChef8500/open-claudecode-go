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

// TestSimpleCoordinatorAllowedToolsNormal verifies that without CLAUDE_CODE_SIMPLE,
// SimpleCoordinatorAllowedTools returns only the 4 coordinator tools.
func TestSimpleCoordinatorAllowedToolsNormal(t *testing.T) {
	// Ensure CLAUDE_CODE_SIMPLE is not set.
	t.Setenv("CLAUDE_CODE_SIMPLE", "")

	allowed := SimpleCoordinatorAllowedTools()
	if len(allowed) != 4 {
		t.Fatalf("expected 4 tools (coordinator only), got %d: %v", len(allowed), allowed)
	}
	for name := range CoordinatorModeAllowedTools {
		if !allowed[name] {
			t.Errorf("missing coordinator tool %q", name)
		}
	}
}

// TestSimpleCoordinatorAllowedToolsSimpleMode verifies that with CLAUDE_CODE_SIMPLE,
// the tool set is the union of simple tools + coordinator tools (7 total).
func TestSimpleCoordinatorAllowedToolsSimpleMode(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SIMPLE", "true")

	allowed := SimpleCoordinatorAllowedTools()
	expected := map[string]bool{
		"Task": true, "TaskStop": true, "SendMessage": true, "SyntheticOutput": true,
		"Bash": true, "Read": true, "FileEdit": true,
	}
	if len(allowed) != len(expected) {
		t.Fatalf("expected %d tools, got %d: %v", len(expected), len(allowed), allowed)
	}
	for name := range expected {
		if !allowed[name] {
			t.Errorf("missing tool %q in simple+coordinator set", name)
		}
	}
}

// TestIsPrActivitySubscriptionTool verifies PR activity MCP tool detection.
func TestIsPrActivitySubscriptionTool(t *testing.T) {
	cases := []struct {
		name   string
		expect bool
	}{
		{"mcp__github__subscribe_pr_activity", true},
		{"mcp__github__unsubscribe_pr_activity", true},
		{"subscribe_pr_activity", true},
		{"Bash", false},
		{"Task", false},
		{"mcp__github__pr_review", false},
	}
	for _, tc := range cases {
		if got := IsPrActivitySubscriptionTool(tc.name); got != tc.expect {
			t.Errorf("IsPrActivitySubscriptionTool(%q) = %v, want %v", tc.name, got, tc.expect)
		}
	}
}

// TestMatchSessionModeCoordinator verifies mode switching to coordinator.
func TestMatchSessionModeCoordinator(t *testing.T) {
	t.Setenv("CLAUDE_CODE_COORDINATOR_MODE", "")

	warning := MatchSessionMode(SessionModeCoordinator)
	if warning == "" {
		t.Error("expected warning when switching to coordinator mode")
	}
	if !IsCoordinatorMode() {
		t.Error("IsCoordinatorMode() should be true after MatchSessionMode(coordinator)")
	}

	// Clean up.
	t.Setenv("CLAUDE_CODE_COORDINATOR_MODE", "")
}

// TestMatchSessionModeNormal verifies mode switching to normal.
func TestMatchSessionModeNormal(t *testing.T) {
	t.Setenv("CLAUDE_CODE_COORDINATOR_MODE", "1")

	warning := MatchSessionMode(SessionModeNormal)
	if warning == "" {
		t.Error("expected warning when switching to normal mode")
	}
	if IsCoordinatorMode() {
		t.Error("IsCoordinatorMode() should be false after MatchSessionMode(normal)")
	}
}

// TestMatchSessionModeNoOp verifies no switch when mode matches.
func TestMatchSessionModeNoOp(t *testing.T) {
	t.Setenv("CLAUDE_CODE_COORDINATOR_MODE", "")

	warning := MatchSessionMode(SessionModeNormal)
	if warning != "" {
		t.Errorf("expected no warning for matching mode, got %q", warning)
	}
}

// TestGetCoordinatorUserContextReturnsWorkerTools verifies that
// GetCoordinatorUserContext returns non-empty worker tools context
// when in coordinator mode.
func TestGetCoordinatorUserContextReturnsWorkerTools(t *testing.T) {
	t.Setenv("CLAUDE_CODE_COORDINATOR_MODE", "1")
	defer t.Setenv("CLAUDE_CODE_COORDINATOR_MODE", "")

	ctx := GetCoordinatorUserContext(nil, "")
	if ctx == nil {
		t.Fatal("expected non-nil context in coordinator mode")
	}
	wt, ok := ctx["workerToolsContext"]
	if !ok || wt == "" {
		t.Error("expected non-empty workerToolsContext")
	}
	if !strings.Contains(wt, "Bash") {
		t.Errorf("workerToolsContext should mention Bash, got: %s", wt)
	}
}

// TestGetCoordinatorUserContextNilWhenNotCoordinator verifies that
// GetCoordinatorUserContext returns nil when not in coordinator mode.
func TestGetCoordinatorUserContextNilWhenNotCoordinator(t *testing.T) {
	t.Setenv("CLAUDE_CODE_COORDINATOR_MODE", "")

	ctx := GetCoordinatorUserContext(nil, "")
	if ctx != nil {
		t.Errorf("expected nil context when not in coordinator mode, got %v", ctx)
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
