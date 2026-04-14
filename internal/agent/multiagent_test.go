package agent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// ── Phase 1 Tests ─────────────────────────────────────────────────────────────

// TestForkPromptSection verifies that fork-enabled prompts include
// fork-vs-spawn guidance. Aligned with TS prompt.ts forkEnabled path.
func TestForkPromptSection(t *testing.T) {
	agents := []AgentDefinition{
		{AgentType: "test-worker", WhenToUse: "for testing"},
	}

	// With fork enabled — should include fork guidance.
	forkPrompt := BuildAgentToolPromptFull(agents, false, "coordinator", true)
	if !strings.Contains(forkPrompt, "fork") && !strings.Contains(forkPrompt, "Fork") {
		t.Error("fork-enabled prompt should contain fork guidance")
	}

	// Without fork — should still build without panic.
	noForkPrompt := BuildAgentToolPromptFull(agents, false, "coordinator", false)
	if noForkPrompt == "" {
		t.Error("prompt should not be empty")
	}
	_ = noForkPrompt
}

// TestOneShotTrailer verifies that one-shot agent types omit the
// agentId/SendMessage trailer in the prompt.
func TestOneShotTrailer(t *testing.T) {
	// "explore" and "plan" are one-shot types per TS constants.ts.
	if !IsOneShotBuiltinAgent("explore") {
		t.Error("'explore' should be a one-shot builtin agent")
	}
	if !IsOneShotBuiltinAgent("plan") {
		t.Error("'plan' should be a one-shot builtin agent")
	}
	if IsOneShotBuiltinAgent("general-purpose") {
		t.Error("'general-purpose' should NOT be a one-shot builtin agent")
	}

	// Build prompt for a one-shot agent — must omit trailer.
	agents := []AgentDefinition{{AgentType: "explore"}}
	prompt := BuildAgentToolPromptFull(agents, false, "explore", false)

	// One-shot agents should NOT include the agent ID / SendMessage instructions.
	if strings.Contains(prompt, "agent_id") && strings.Contains(prompt, "SendMessage") {
		t.Error("one-shot agent prompt should omit agentId/SendMessage trailer")
	}
}

// TestAgentListAttachmentMode verifies ShouldInjectAgentListInMessages
// honours the env var.
func TestAgentListAttachmentMode(t *testing.T) {
	// Default: env not set.
	os.Unsetenv("CLAUDE_CODE_AGENT_LIST_IN_MESSAGES")
	if ShouldInjectAgentListInMessages() {
		t.Error("should return false when env not set")
	}

	// Set to "1".
	os.Setenv("CLAUDE_CODE_AGENT_LIST_IN_MESSAGES", "1")
	defer os.Unsetenv("CLAUDE_CODE_AGENT_LIST_IN_MESSAGES")
	if !ShouldInjectAgentListInMessages() {
		t.Error("should return true when CLAUDE_CODE_AGENT_LIST_IN_MESSAGES=1")
	}

	// When attachment mode is on, agent catalog should NOT appear in prompt.
	agents := []AgentDefinition{
		{AgentType: "worker", WhenToUse: "secret use"},
	}
	prompt := BuildAgentToolPromptFull(agents, false, "", false)
	if strings.Contains(prompt, "secret use") {
		t.Error("agent catalog should not appear in prompt when attachment mode is enabled")
	}
}

// TestBuildAgentListAttachment verifies the attachment XML structure.
func TestBuildAgentListAttachment(t *testing.T) {
	agents := []AgentDefinition{
		{AgentType: "researcher", WhenToUse: "research tasks"},
		{AgentType: "coder", WhenToUse: "coding tasks"},
	}
	attachment := BuildAgentListAttachment(agents)

	if attachment == "" {
		t.Fatal("attachment should not be empty")
	}
	if !strings.Contains(attachment, "researcher") {
		t.Error("attachment should contain 'researcher'")
	}
	if !strings.Contains(attachment, "coder") {
		t.Error("attachment should contain 'coder'")
	}
}

// ── Phase 2 Tests ─────────────────────────────────────────────────────────────

// TestStructuredMessageHandler verifies shutdown request routing.
func TestShutdownFlow(t *testing.T) {
	tf := NewTaskFramework()
	handler := NewStructuredMessageHandler(tf, nil, nil)

	// Register a target agent.
	tf.Register(AgentDefinition{AgentID: "agent-b", AgentType: "worker", Task: "work"})

	// Send shutdown request.
	msg := StructuredMessage{
		Type:   MsgTypeShutdownRequest,
		From:   "agent-a",
		To:     "agent-b",
		Reason: "task complete",
	}
	result, err := handler.HandleStructuredMessage(msg)
	if err != nil {
		t.Fatalf("shutdown request failed: %v", err)
	}
	if result == "" {
		t.Error("result should not be empty")
	}

	// Verify pending message was queued.
	pending := tf.DrainPendingMessages("agent-b")
	if len(pending) == 0 {
		t.Error("shutdown request should have queued a pending message for agent-b")
	}
	if !strings.Contains(pending[0], "shutdown-request") {
		t.Errorf("queued message should be a shutdown-request, got: %s", pending[0])
	}
}

func TestStructuredShutdownEmitsEvent(t *testing.T) {
	handler := NewStructuredMessageHandler(NewTaskFramework(), nil, nil)
	var got StructuredMessageEvent
	handler.SetEventCallback(func(ev StructuredMessageEvent) {
		got = ev
	})

	_, err := handler.HandleStructuredMessage(StructuredMessage{
		Type:   MsgTypeShutdownReject,
		From:   "worker-1",
		To:     "team-lead",
		Reason: "still working",
		Color:  "#ff00aa",
	})
	if err != nil {
		t.Fatalf("HandleStructuredMessage: %v", err)
	}
	if got.Kind != MsgTypeShutdownReject {
		t.Fatalf("unexpected event kind: %#v", got)
	}
	if got.From != "worker-1" || got.Reason != "still working" || got.Color != "#ff00aa" {
		t.Fatalf("unexpected event payload: %#v", got)
	}
}

func TestForkAgentParamsMarksForkChild(t *testing.T) {
	parentCtx := &SubagentContext{ParentAgentID: "leader-1", TeamName: "alpha", IsForkChild: false}
	params := ForkAgentParams("do work", nil, "", "repo", parentCtx)

	if !params.IsFork {
		t.Fatal("expected fork params to set IsFork")
	}
	if params.ParentContext == nil {
		t.Fatal("expected fork params to include parent context")
	}
	if !params.ParentContext.IsForkChild {
		t.Fatal("expected fork child context to be marked as IsForkChild")
	}
	if parentCtx.IsForkChild {
		t.Fatal("expected original parent context to remain unchanged")
	}
}

// TestStructuredMessageTypes verifies ParseStructuredMessageType.
func TestStructuredMessageTypes(t *testing.T) {
	tests := []struct {
		input    string
		expected StructuredMessageType
	}{
		{"shutdown_request", MsgTypeShutdownRequest},
		{"shutdown_approved", MsgTypeShutdownApproval},
		{"shutdown_rejected", MsgTypeShutdownReject},
		{"plan_approval", MsgTypePlanApproval},
		{"plan_rejection", MsgTypePlanRejection},
		{"plan_approval_response", MsgTypePlanApproval},
		{"text", MsgTypeText},
		{"unknown", MsgTypeText},
		{"", MsgTypeText},
	}

	for _, tt := range tests {
		got := ParseStructuredMessageType(tt.input)
		if got != tt.expected {
			t.Errorf("ParseStructuredMessageType(%q) = %q, want %q",
				tt.input, got, tt.expected)
		}
	}
}

func TestAsyncLifecycleResumePreservesRunParams(t *testing.T) {
	runner := NewAgentRunner(AgentRunnerConfig{})
	mgr := NewAsyncLifecycleManager(runner)

	mgr.agents["agent-1"] = &AsyncAgent{
		AgentID:    "agent-1",
		Definition: AgentDefinition{AgentID: "agent-1", AgentType: "worker", TeamName: "alpha"},
		Status:     AsyncStatusDone,
		done:       make(chan struct{}),
		runParams: RunAgentParams{
			Task:            "original task",
			WorkDir:         "/tmp/work",
			AllowedTools:    []string{"Read", "Edit"},
			SystemPrompt:    "system",
			TeamName:        "alpha",
			ParentContext:   &SubagentContext{ParentAgentID: "parent-1", TeamName: "alpha", IsForkChild: true},
			ExistingAgentID: "agent-1",
		},
	}

	_, err := mgr.Resume(context.Background(), "agent-1", "resume prompt")
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}

	resumed, ok := mgr.agents["agent-1"]
	if !ok {
		t.Fatal("resumed agent missing")
	}
	if resumed.runParams.WorkDir != "/tmp/work" {
		t.Fatalf("WorkDir not preserved: %q", resumed.runParams.WorkDir)
	}
	if len(resumed.runParams.AllowedTools) != 2 || resumed.runParams.AllowedTools[1] != "Edit" {
		t.Fatalf("AllowedTools not preserved: %#v", resumed.runParams.AllowedTools)
	}
	if resumed.runParams.ParentContext == nil || !resumed.runParams.ParentContext.IsForkChild {
		t.Fatal("ParentContext not preserved on resume")
	}
	if resumed.runParams.Task != "resume prompt" {
		t.Fatalf("Task not updated for resume: %q", resumed.runParams.Task)
	}
	resumed.cancel()
}

func TestBuildResumeParamsPreservesCheckpointFields(t *testing.T) {
	cp := &AgentCheckpoint{
		AgentID:        "agent-2",
		Definition:     AgentDefinition{AgentID: "agent-2", AgentType: "worker", Task: "original", TeamName: "beta"},
		TurnCount:      2,
		MaxTurns:       7,
		WorkDir:        "/repo",
		WorktreeDir:    "/repo/.worktrees/agent-2",
		Background:     true,
		SystemPrompt:   "sys",
		Model:          "sonnet",
		AllowedTools:   []string{"Read", "Edit"},
		PermissionMode: "plan",
		Description:    "resume me",
		IsolationMode:  IsolationRemote,
		IsFork:         true,
		ParentContext:  &SubagentContext{ParentAgentID: "parent-2", IsForkChild: true},
		TeamName:       "beta",
	}

	params := BuildResumeParams(cp)
	if params.WorkDir != cp.WorktreeDir {
		t.Fatalf("expected worktree dir to win, got %q", params.WorkDir)
	}
	if params.IsolationMode != IsolationWorktree {
		t.Fatalf("expected worktree isolation on resume, got %q", params.IsolationMode)
	}
	if params.MaxTurns != 5 {
		t.Fatalf("expected remaining turns 5, got %d", params.MaxTurns)
	}
	if params.PermissionMode != "plan" || params.Description != "resume me" || !params.IsFork {
		t.Fatalf("preserved fields missing: %#v", params)
	}
	if len(params.AllowedTools) != 2 || params.AllowedTools[0] != "Read" {
		t.Fatalf("allowed tools not preserved: %#v", params.AllowedTools)
	}
	if params.ParentContext == nil || params.ParentContext.ParentAgentID != "parent-2" {
		t.Fatalf("parent context not preserved: %#v", params.ParentContext)
	}
}

// ── Phase 4 Tests ─────────────────────────────────────────────────────────────

// TestTaskCRUDLifecycle verifies the full Create/Get/Update/MarkDone lifecycle.
func TestTaskCRUDLifecycle(t *testing.T) {
	tm := NewTaskManager()

	// Create.
	def := AgentDefinition{AgentID: "task-1", AgentType: "worker", Task: "do something"}
	task := tm.Create(def)
	if task.Status != AgentStatusPending {
		t.Errorf("new task status = %q, want %q", task.Status, AgentStatusPending)
	}

	// Get.
	got, ok := tm.Get("task-1")
	if !ok {
		t.Fatal("Get should find task-1")
	}
	if got.Definition.Task != "do something" {
		t.Errorf("task description mismatch: %q", got.Definition.Task)
	}

	// MarkRunning.
	if err := tm.MarkRunning("task-1"); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	got, _ = tm.Get("task-1")
	if got.Status != AgentStatusRunning {
		t.Errorf("after MarkRunning, status = %q", got.Status)
	}
	if got.StartedAt.IsZero() {
		t.Error("StartedAt should be set after MarkRunning")
	}

	// MarkDone.
	if err := tm.MarkDone("task-1", "finished output"); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}
	got, _ = tm.Get("task-1")
	if got.Status != AgentStatusDone {
		t.Errorf("after MarkDone, status = %q", got.Status)
	}
	if got.Output != "finished output" {
		t.Errorf("output mismatch: %q", got.Output)
	}

	// Counts.
	if tm.Count() != 1 {
		t.Errorf("Count() = %d, want 1", tm.Count())
	}
	if tm.ActiveCount() != 0 {
		t.Errorf("ActiveCount() = %d, want 0 (task is done)", tm.ActiveCount())
	}
}

// TestTaskManagerReset verifies Reset clears all tasks.
func TestTaskManagerReset(t *testing.T) {
	tm := NewTaskManager()
	tm.Create(AgentDefinition{AgentID: "t1", Task: "a"})
	tm.Create(AgentDefinition{AgentID: "t2", Task: "b"})

	if tm.Count() != 2 {
		t.Fatalf("expected 2 tasks before reset, got %d", tm.Count())
	}

	tm.Reset()
	if tm.Count() != 0 {
		t.Errorf("expected 0 tasks after reset, got %d", tm.Count())
	}
}

// TestTaskManagerPersistence verifies SaveToFile/LoadFromFile round-trip.
func TestTaskManagerPersistence(t *testing.T) {
	dir := t.TempDir()
	tm := NewTaskManagerWithDir(dir)

	if err := tm.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}

	def := AgentDefinition{AgentID: "persist-1", AgentType: "worker", Task: "persistent task"}
	tm.Create(def)
	_ = tm.MarkRunning("persist-1")
	_ = tm.MarkDone("persist-1", "done!")

	if err := tm.SaveToFile(); err != nil {
		t.Fatalf("SaveToFile: %v", err)
	}

	// Load into a fresh manager.
	tm2 := NewTaskManagerWithDir(dir)
	if err := tm2.LoadFromFile(); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	got, ok := tm2.Get("persist-1")
	if !ok {
		t.Fatal("task should be restored after LoadFromFile")
	}
	if got.Status != AgentStatusDone {
		t.Errorf("restored status = %q, want done", got.Status)
	}
	if got.Output != "done!" {
		t.Errorf("restored output = %q, want 'done!'", got.Output)
	}
}

// ── Phase 5 Tests ─────────────────────────────────────────────────────────────

// TestKillAllAgentTasks verifies KillAllRunningAgentTasks stops all running tasks.
func TestKillAllAgentTasks(t *testing.T) {
	tf := NewTaskFramework()

	// Register several agents.
	for _, id := range []string{"ag-1", "ag-2", "ag-3"} {
		tf.Register(AgentDefinition{AgentID: id, AgentType: "worker", Task: "work"})
	}

	// Manually set them running.
	tf.mu.Lock()
	for _, t2 := range tf.tasks {
		t2.Status = AgentStatusRunning
	}
	tf.mu.Unlock()

	cancelled := tf.KillAllRunningAgentTasks(nil)
	if cancelled != 3 {
		t.Errorf("KillAllRunningAgentTasks returned %d, want 3", cancelled)
	}

	// All should now be cancelled.
	tf.mu.Lock()
	for id, t2 := range tf.tasks {
		if t2.Status != AgentStatusCancelled {
			t.Errorf("agent %s should be cancelled, got %s", id, t2.Status)
		}
	}
	tf.mu.Unlock()
}

// TestMainSessionTaskLifecycle verifies register/complete/foreground.
func TestMainSessionTaskLifecycle(t *testing.T) {
	tf := NewTaskFramework()
	msm := NewMainSessionTaskManager(tf, nil)

	// Register.
	taskID := msm.RegisterMainSessionTask("sess-123", "main query in progress")
	if taskID != "s-sess-123" {
		t.Errorf("expected taskID 's-sess-123', got %q", taskID)
	}

	// IsMainSessionTask.
	if !IsMainSessionTask(taskID) {
		t.Error("IsMainSessionTask should return true for s- prefix")
	}
	if IsMainSessionTask("a-regular-agent") {
		t.Error("IsMainSessionTask should return false for non-session tasks")
	}

	// Task should be in framework.
	task, ok := tf.GetTask(taskID)
	if !ok {
		t.Fatal("task should exist in framework after RegisterMainSessionTask")
	}
	if !task.IsBackgrounded {
		t.Error("session task should be marked as backgrounded")
	}

	// Foreground it.
	if err := msm.ForegroundMainSessionTask(taskID); err != nil {
		t.Fatalf("ForegroundMainSessionTask: %v", err)
	}
	if msm.GetForegroundedTaskID() != taskID {
		t.Errorf("foreground task ID mismatch: %q", msm.GetForegroundedTaskID())
	}

	// Complete it.
	msm.CompleteMainSessionTask(taskID, "task output")
	task, _ = tf.GetTask(taskID)
	if task.Status != AgentStatusDone {
		t.Errorf("after complete, status = %q, want done", task.Status)
	}
}

// ── Phase 12 Tests ────────────────────────────────────────────────────────────

// TestDreamTaskSchedule verifies Schedule/Cancel/ListPending.
func TestDreamTaskSchedule(t *testing.T) {
	dm := NewDreamTaskManager(nil, nil)

	// Schedule a task with a long delay (won't fire during test).
	state, err := dm.Schedule("dream-1", "do background work", "test dream", 10*time.Minute, nil)
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if state.Status != AgentStatusPending {
		t.Errorf("new dream task should be pending, got %s", state.Status)
	}

	// Duplicate should error.
	_, err = dm.Schedule("dream-1", "duplicate", "dup", time.Hour, nil)
	if err == nil {
		t.Error("scheduling duplicate task ID should return error")
	}

	// ListPending.
	pending := dm.ListPending()
	if len(pending) != 1 {
		t.Errorf("ListPending: got %d tasks, want 1", len(pending))
	}

	// Cancel.
	if err := dm.Cancel("dream-1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	got, ok := dm.Get("dream-1")
	if !ok {
		t.Fatal("Get should find dream-1 after cancel")
	}
	if got.Status != AgentStatusCancelled {
		t.Errorf("after cancel, status = %q, want cancelled", got.Status)
	}

	// ListPending should be empty now.
	pending = dm.ListPending()
	if len(pending) != 0 {
		t.Errorf("ListPending after cancel: got %d tasks, want 0", len(pending))
	}

	// Cancel of non-existent task.
	if err := dm.Cancel("no-such-task"); err == nil {
		t.Error("cancelling non-existent task should error")
	}
}

// TestDreamTaskCancelAll verifies CancelAll stops all pending tasks.
func TestDreamTaskCancelAll(t *testing.T) {
	dm := NewDreamTaskManager(nil, nil)

	for i := 0; i < 3; i++ {
		id := string(rune('a'+i)) + "-dream"
		_, err := dm.Schedule(id, "prompt", "desc", time.Hour, nil)
		if err != nil {
			t.Fatalf("Schedule %s: %v", id, err)
		}
	}

	cancelled := dm.CancelAll()
	if cancelled != 3 {
		t.Errorf("CancelAll returned %d, want 3", cancelled)
	}
	if len(dm.ListPending()) != 0 {
		t.Error("ListPending should be empty after CancelAll")
	}
}

// ── Phase 2: Peer Address Tests ───────────────────────────────────────────────

// TestPeerAddressRegistry verifies register/lookup/cleanup.
func TestPeerAddressRegistry(t *testing.T) {
	dir := t.TempDir()
	reg := NewPeerAddressRegistry(dir)

	addr := PeerAddress{
		AgentID:   "agent-xyz",
		AgentName: "my-worker",
		Protocol:  "inprocess",
		Address:   "",
	}

	// Register.
	if err := reg.Register(addr); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Lookup by ID.
	got, err := reg.Lookup("agent-xyz")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got == nil {
		t.Fatal("Lookup should find registered peer")
	}
	if got.AgentName != "my-worker" {
		t.Errorf("AgentName mismatch: %q", got.AgentName)
	}

	// LookupByName.
	byName, err := reg.LookupByName("my-worker")
	if err != nil {
		t.Fatalf("LookupByName: %v", err)
	}
	if byName == nil || byName.AgentID != "agent-xyz" {
		t.Error("LookupByName should find peer by name")
	}

	// ListAll.
	all, err := reg.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("ListAll: got %d, want 1", len(all))
	}

	// Unregister.
	if err := reg.Unregister("agent-xyz"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	got, _ = reg.Lookup("agent-xyz")
	if got != nil {
		t.Error("Lookup after Unregister should return nil")
	}
}

// TestPeerAddressCleanupStale verifies stale peer cleanup.
func TestPeerAddressCleanupStale(t *testing.T) {
	dir := t.TempDir()
	reg := NewPeerAddressRegistry(dir)

	// Register normally first to create the directory structure.
	addr := PeerAddress{
		AgentID:    "old-agent",
		Protocol:   "inprocess",
		LastSeen:   time.Now(),
		RegisterAt: time.Now(),
	}
	if err := reg.Register(addr); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Overwrite with an old LastSeen (Register always sets LastSeen=Now,
	// so we patch the file directly).
	path := reg.peerPath("old-agent")
	oldAddr := PeerAddress{
		AgentID:    "old-agent",
		Protocol:   "inprocess",
		LastSeen:   time.Now().Add(-2 * time.Hour),
		RegisterAt: time.Now().Add(-2 * time.Hour),
	}
	data, _ := json.MarshalIndent(oldAddr, "", "  ")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Cleanup with 1-hour max age.
	removed, err := reg.CleanupStale(time.Hour)
	if err != nil {
		t.Fatalf("CleanupStale: %v", err)
	}
	if removed != 1 {
		t.Errorf("CleanupStale removed %d, want 1", removed)
	}
}

// ── Phase 3 Team Tests ────────────────────────────────────────────────────────

// TestTeamCreateUniqueNames verifies duplicate team creation fails.
func TestTeamCreateUniqueNames(t *testing.T) {
	tm := NewTeamManager("", nil, nil)

	_, err := tm.CreateTeam("alpha", "first team", "lead-1")
	if err != nil {
		t.Fatalf("first CreateTeam: %v", err)
	}

	_, err = tm.CreateTeam("alpha", "duplicate", "lead-2")
	if err == nil {
		t.Error("duplicate team name should fail")
	}
}

// TestTeamDeleteActiveMembers verifies full delete with member cleanup.
func TestTeamDeleteActiveMembers(t *testing.T) {
	reg := NewMailboxRegistry(256, 0)
	bus := NewMessageBus()
	tm := NewTeamManager("", reg, bus)

	_, err := tm.CreateTeam("beta", "test team", "lead-beta")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	_ = tm.AddMember("beta", "worker-1", "worker", "")
	_ = tm.AddMember("beta", "worker-2", "worker", "")

	// Delete team.
	if err := tm.DeleteTeam("beta"); err != nil {
		t.Fatalf("DeleteTeam: %v", err)
	}

	// Team should be gone.
	_, ok := tm.GetTeam("beta")
	if ok {
		t.Error("team should no longer exist after delete")
	}

	// Mailboxes should be removed — Get returns nil for deleted mailboxes.
	reg.mu.RLock()
	_, w1ok := reg.mailboxes["worker-1"]
	_, w2ok := reg.mailboxes["worker-2"]
	reg.mu.RUnlock()
	if w1ok {
		t.Error("worker-1 mailbox should be removed after team delete")
	}
	if w2ok {
		t.Error("worker-2 mailbox should be removed after team delete")
	}
}

// ── TaskFramework State Tests ─────────────────────────────────────────────────

// TestIsBackgroundedAndDiskLoaded verifies the new state fields.
func TestIsBackgroundedAndDiskLoaded(t *testing.T) {
	tf := NewTaskFramework()
	def := AgentDefinition{AgentID: "bg-agent", AgentType: "worker", Task: "work"}
	tf.Register(def)

	// Initially false.
	task, _ := tf.GetTask("bg-agent")
	if task.IsBackgrounded {
		t.Error("IsBackgrounded should be false initially")
	}
	if task.DiskLoaded {
		t.Error("DiskLoaded should be false initially")
	}

	// Mark backgrounded.
	tf.MarkBackgrounded("bg-agent")
	task, _ = tf.GetTask("bg-agent")
	if !task.IsBackgrounded {
		t.Error("IsBackgrounded should be true after MarkBackgrounded")
	}

	// Mark disk loaded.
	tf.MarkDiskLoaded("bg-agent")
	task, _ = tf.GetTask("bg-agent")
	if !task.DiskLoaded {
		t.Error("DiskLoaded should be true after MarkDiskLoaded")
	}
}
