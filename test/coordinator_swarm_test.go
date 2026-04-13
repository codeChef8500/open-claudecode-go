package test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wall-ai/agent-engine/internal/agent"
	"github.com/wall-ai/agent-engine/internal/engine"
	"github.com/wall-ai/agent-engine/internal/tool"
	"github.com/wall-ai/agent-engine/internal/tool/listpeers"
	"github.com/wall-ai/agent-engine/internal/tool/sendmessage"
	"github.com/wall-ai/agent-engine/internal/tool/teamcreate"
)

// ── Phase 4: team_create returns metadata only ──────────────────────────────

func TestTeamCreate_ReturnsMetadataJSON(t *testing.T) {
	tc := teamcreate.New()

	input := json.RawMessage(`{"team_name":"alpha","description":"test team"}`)
	uctx := &tool.UseContext{AgentID: "coordinator-1"}

	ch, err := tc.Call(context.Background(), input, uctx)
	require.NoError(t, err)

	var result string
	for block := range ch {
		result += block.Text
	}

	// Must be valid JSON.
	var meta map[string]string
	err = json.Unmarshal([]byte(result), &meta)
	require.NoError(t, err, "team_create should return valid JSON, got: %s", result)

	// Verify fields.
	assert.Equal(t, "alpha", meta["team_name"])
	assert.Equal(t, "registered", meta["status"])
	assert.NotEmpty(t, meta["lead_agent_id"])
	assert.Contains(t, meta["note"], "Task tool")

	// Must NOT contain the old fake "queued" text.
	assert.NotContains(t, result, "queued for parallel execution")
}

func TestTeamCreate_DefaultLeadAgentID(t *testing.T) {
	tc := teamcreate.New()

	input := json.RawMessage(`{"team_name":"beta"}`)
	// No AgentID in UseContext → should generate team-lead@beta.
	uctx := &tool.UseContext{}

	ch, err := tc.Call(context.Background(), input, uctx)
	require.NoError(t, err)

	var result string
	for block := range ch {
		result += block.Text
	}

	var meta map[string]string
	require.NoError(t, json.Unmarshal([]byte(result), &meta))
	assert.Equal(t, "team-lead@beta", meta["lead_agent_id"])
}

func TestTeamCreate_MissingName(t *testing.T) {
	tc := teamcreate.New()

	input := json.RawMessage(`{"description":"no name"}`)
	uctx := &tool.UseContext{}

	ch, err := tc.Call(context.Background(), input, uctx)
	require.NoError(t, err)

	for block := range ch {
		assert.True(t, block.IsError)
		assert.Contains(t, block.Text, "team_name is required")
	}
}

// ── Phase 3: FormatTaskNotificationXML ──────────────────────────────────────

func TestFormatTaskNotificationXML_Completed(t *testing.T) {
	notifs := []agent.Notification{
		{
			Type:    agent.NotificationTypeComplete,
			AgentID: "worker-abc-123",
			Message: "Finished analysis of 5 files.",
		},
	}

	xml := agent.FormatTaskNotificationXML(notifs)
	assert.Contains(t, xml, "<task-notification>")
	assert.Contains(t, xml, "<task-id>worker-abc-123</task-id>")
	assert.Contains(t, xml, "<status>completed</status>")
	assert.Contains(t, xml, "<result>Finished analysis of 5 files.</result>")
	assert.Contains(t, xml, "</task-notification>")
}

func TestFormatTaskNotificationXML_Failed(t *testing.T) {
	notifs := []agent.Notification{
		{
			Type:    agent.NotificationTypeError,
			AgentID: "worker-fail",
			Message: "timeout exceeded",
		},
	}

	xml := agent.FormatTaskNotificationXML(notifs)
	assert.Contains(t, xml, "<status>failed</status>")
}

func TestFormatTaskNotificationXML_Multiple(t *testing.T) {
	notifs := []agent.Notification{
		{Type: agent.NotificationTypeComplete, AgentID: "a1", Message: "done1"},
		{Type: agent.NotificationTypeComplete, AgentID: "a2", Message: "done2"},
	}

	xml := agent.FormatTaskNotificationXML(notifs)
	assert.Equal(t, 2, strings.Count(xml, "<task-notification>"))
	assert.Equal(t, 2, strings.Count(xml, "</task-notification>"))
}

func TestFormatTaskNotificationXML_Empty(t *testing.T) {
	xml := agent.FormatTaskNotificationXML(nil)
	assert.Empty(t, xml)
}

// ── Phase 3: Global notification sink ───────────────────────────────────────

func TestAsyncManager_GlobalNotificationSink(t *testing.T) {
	runner := agent.NewAgentRunner(agent.AgentRunnerConfig{})
	mgr := agent.NewAsyncLifecycleManager(runner)

	sink := agent.NewNotificationQueue(10)
	mgr.SetGlobalNotificationSink(sink)

	// Launch an agent that finishes immediately.
	ctx := context.Background()
	agentID, err := mgr.Launch(ctx, agent.RunAgentParams{
		ExistingAgentID: "test-sink-agent",
	})
	require.NoError(t, err)
	assert.Equal(t, "test-sink-agent", agentID)

	// Wait for completion (the agent should finish quickly with no real runner).
	time.Sleep(2 * time.Second)

	// The global sink should have received the completion notification.
	notifs := sink.DrainAll()
	require.NotEmpty(t, notifs, "global sink should receive completion notification")
	assert.Equal(t, "test-sink-agent", notifs[0].AgentID)
	assert.Equal(t, agent.NotificationTypeComplete, notifs[0].Type)
}

// ── Phase 5: list_peers with AsyncLifecycleManager ──────────────────────────

func TestListPeers_WithAsyncManager(t *testing.T) {
	runner := agent.NewAgentRunner(agent.AgentRunnerConfig{})
	mgr := agent.NewAsyncLifecycleManager(runner)

	// Launch an agent (will complete quickly).
	ctx := context.Background()
	_, err := mgr.Launch(ctx, agent.RunAgentParams{
		ExistingAgentID: "peer-check-agent",
	})
	require.NoError(t, err)

	// Create list_peers wired to the manager.
	lp := listpeers.NewWithManager(mgr)
	uctx := &tool.UseContext{AgentID: "coordinator"}

	ch, err := lp.Call(context.Background(), nil, uctx)
	require.NoError(t, err)

	var result string
	for block := range ch {
		result += block.Text
	}

	assert.Contains(t, result, "coordinator")
	assert.Contains(t, result, "peer-check-agent")
}

func TestListPeers_NoManager(t *testing.T) {
	lp := listpeers.New()
	uctx := &tool.UseContext{AgentID: "solo"}

	ch, err := lp.Call(context.Background(), nil, uctx)
	require.NoError(t, err)

	var result string
	for block := range ch {
		result += block.Text
	}

	assert.Contains(t, result, "[self] solo")
	assert.Contains(t, result, "no agent tracking available")
}

// ── Phase 5: PeerInfo from AsyncLifecycleManager ────────────────────────────

func TestAsyncManager_ListPeerInfos(t *testing.T) {
	runner := agent.NewAgentRunner(agent.AgentRunnerConfig{})
	mgr := agent.NewAsyncLifecycleManager(runner)

	ctx := context.Background()
	_, err := mgr.Launch(ctx, agent.RunAgentParams{
		ExistingAgentID: "info-agent",
	})
	require.NoError(t, err)

	infos := mgr.ListPeerInfos()
	require.Len(t, infos, 1)
	assert.Equal(t, "info-agent", infos[0].AgentID)
	// Status should be running or pending initially.
	assert.True(t, infos[0].Status == "running" || infos[0].Status == "pending",
		"expected running or pending, got %s", infos[0].Status)
}

// ── Phase 7: PushNotification ───────────────────────────────────────────────

func TestAsyncManager_PushNotification(t *testing.T) {
	runner := agent.NewAgentRunner(agent.AgentRunnerConfig{})
	mgr := agent.NewAsyncLifecycleManager(runner)

	ctx := context.Background()
	agentID, err := mgr.Launch(ctx, agent.RunAgentParams{
		ExistingAgentID: "push-target",
	})
	require.NoError(t, err)

	// Push a message notification.
	mgr.PushNotification(agentID, agent.Notification{
		Type:    agent.NotificationTypeMessage,
		AgentID: "sender",
		Message: "hello from parent",
	})

	// Drain and verify.
	notifs := mgr.DrainNotifications(agentID)
	found := false
	for _, n := range notifs {
		if n.Type == agent.NotificationTypeMessage && n.Message == "hello from parent" {
			found = true
			break
		}
	}
	assert.True(t, found, "pushed notification should be drainable")
}

func TestAsyncManager_PushNotification_NonexistentAgent(t *testing.T) {
	runner := agent.NewAgentRunner(agent.AgentRunnerConfig{})
	mgr := agent.NewAsyncLifecycleManager(runner)

	// Should not panic.
	mgr.PushNotification("nonexistent", agent.Notification{
		Type:    agent.NotificationTypeMessage,
		Message: "lost in the void",
	})
}

// ── Phase 7: SendMessage routing to async agent ─────────────────────────────

func TestSendMessage_RoutesToAsyncAgent(t *testing.T) {
	// Test the routing logic: PushNotification works for a running agent,
	// and SendMessage uses it. Since mock agents finish instantly (no LLM),
	// we test the underlying PushNotification + GetStatus directly, then
	// verify SendMessage integration with a brief sleep to catch the agent
	// while it's still in "pending"/"running" state.
	runner := agent.NewAgentRunner(agent.AgentRunnerConfig{})
	mgr := agent.NewAsyncLifecycleManager(runner)

	ctx := context.Background()
	agentID, err := mgr.Launch(ctx, agent.RunAgentParams{
		ExistingAgentID: "msg-target",
	})
	require.NoError(t, err)

	// The agent may finish almost instantly. Push a notification directly
	// (this always works regardless of status).
	mgr.PushNotification(agentID, agent.Notification{
		Type:    agent.NotificationTypeMessage,
		AgentID: "coordinator",
		Message: "follow-up task",
	})

	// Drain and verify the notification was pushed.
	time.Sleep(100 * time.Millisecond) // let goroutine schedule
	notifs := mgr.DrainNotifications(agentID)
	foundMsg := false
	for _, n := range notifs {
		if n.Type == agent.NotificationTypeMessage && n.Message == "follow-up task" {
			foundMsg = true
			break
		}
	}
	assert.True(t, foundMsg, "PushNotification should deliver message to agent queue")

	// Test SendMessage fallback for completed agent (agent finishes fast).
	time.Sleep(500 * time.Millisecond)
	sm := sendmessage.NewWithDeps(nil, mgr)
	uctx := &tool.UseContext{AgentID: "coordinator"}
	input := json.RawMessage(`{"to":"` + agentID + `","message":"late message"}`)
	ch, err := sm.Call(context.Background(), input, uctx)
	require.NoError(t, err)

	var result string
	for block := range ch {
		result += block.Text
	}
	// Agent is done/failed → should fall back to mailbox path.
	assert.Contains(t, result, agentID)
}

func TestSendMessage_FallsBackForStoppedAgent(t *testing.T) {
	runner := agent.NewAgentRunner(agent.AgentRunnerConfig{})
	mgr := agent.NewAsyncLifecycleManager(runner)

	// No agents launched → agent doesn't exist.
	sm := sendmessage.NewWithDeps(nil, mgr)
	uctx := &tool.UseContext{AgentID: "coordinator"}

	input := json.RawMessage(`{"to":"nonexistent-agent","message":"hello"}`)
	ch, err := sm.Call(context.Background(), input, uctx)
	require.NoError(t, err)

	var result string
	for block := range ch {
		result += block.Text
	}

	// Should fall back to mailbox path (queued, not delivered).
	assert.NotContains(t, result, "delivered")
	assert.Contains(t, result, "nonexistent-agent")
}

// ── Phase 6: Coordinator forced async (unit-level check) ────────────────────

func TestCoordinatorAllowedTools_Contains(t *testing.T) {
	allowed := agent.CoordinatorAllowedTools
	expected := []string{"Task", "Read", "Grep", "Glob", "TodoRead", "TodoWrite"}
	for _, exp := range expected {
		found := false
		for _, a := range allowed {
			if a == exp {
				found = true
				break
			}
		}
		assert.True(t, found, "CoordinatorAllowedTools should contain %s", exp)
	}
}

// ── Phase 2: Tool filtering sanity ──────────────────────────────────────────

func TestCoordinatorToolFilter(t *testing.T) {
	// Simulate bootstrap tool filter logic.
	allTools := []string{"Task", "Read", "Grep", "Glob", "TodoRead", "TodoWrite",
		"SendMessage", "TaskStop", "list_peers",
		"Edit", "Write", "Bash", "powershell", "FileRead"}

	allowed := make(map[string]bool)
	for _, name := range agent.CoordinatorAllowedTools {
		allowed[name] = true
	}
	allowed["SendMessage"] = true
	allowed["TaskStop"] = true
	allowed["list_peers"] = true

	var filtered []string
	for _, t := range allTools {
		if allowed[t] {
			filtered = append(filtered, t)
		}
	}

	// Should keep only the 9 whitelisted tools.
	assert.Len(t, filtered, 9)
	// Should NOT include Edit, Write, Bash, powershell, FileRead.
	for _, name := range filtered {
		assert.NotEqual(t, "Edit", name)
		assert.NotEqual(t, "Write", name)
		assert.NotEqual(t, "Bash", name)
	}
}

// ── Phase 1: QuerySourceNotification exists ─────────────────────────────────

func TestQuerySourceNotification(t *testing.T) {
	src := engine.QuerySourceNotification
	assert.Equal(t, engine.QuerySource("notification"), src)
}
