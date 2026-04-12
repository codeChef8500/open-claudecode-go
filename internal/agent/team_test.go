package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTeamManager_BasicWorkflow tests create, add member, broadcast.
func TestTeamManager_BasicWorkflow(t *testing.T) {
	tmpDir := t.TempDir()
	bus := NewMessageBus()
	registry := NewMailboxRegistry(256, 0)
	tm := NewTeamManager(tmpDir, registry, bus)

	// Create team.
	team, err := tm.CreateTeam("test-team", "A test team", "agent-lead")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if team.Name != "test-team" {
		t.Errorf("expected team name 'test-team', got %q", team.Name)
	}
	if team.LeadAgent != "agent-lead" {
		t.Errorf("expected lead 'agent-lead', got %q", team.LeadAgent)
	}

	// Add members.
	err = tm.AddMember("test-team", "agent-worker-1", "worker", "worker")
	if err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	err = tm.AddMember("test-team", "agent-worker-2", "worker", "worker")
	if err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	// List teams.
	teams := tm.ListTeams()
	if len(teams) != 1 {
		t.Errorf("expected 1 team, got %d", len(teams))
	}

	// Check member count.
	members := tm.TeamMemberIDs("test-team")
	if len(members) != 3 {
		t.Errorf("expected 3 members, got %d", len(members))
	}

	// Get team.
	got, ok := tm.GetTeam("test-team")
	if !ok {
		t.Fatal("expected to get team")
	}
	if len(got.Members) != 3 {
		t.Errorf("expected 3 members, got %d", len(got.Members))
	}
}

// TestTeamManager_Persistence tests team file persistence.
func TestTeamManager_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	bus := NewMessageBus()
	registry := NewMailboxRegistry(256, 0)
	tm := NewTeamManager(tmpDir, registry, bus)

	// Create and save team.
	_, err := tm.CreateTeam("persist-team", "Persistent test", "lead-1")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	_ = tm.AddMember("persist-team", "worker-1", "worker", "worker")

	// Verify file exists.
	teamFile := filepath.Join(tmpDir, ".claude", "teams", "persist-team.json")
	if _, err := os.Stat(teamFile); os.IsNotExist(err) {
		t.Errorf("team file not created: %s", teamFile)
	}

	// Create new manager and load.
	tm2 := NewTeamManager(tmpDir, registry, bus)
	if err := tm2.LoadAllTeams(); err != nil {
		t.Fatalf("LoadAllTeams: %v", err)
	}

	loaded, ok := tm2.GetTeam("persist-team")
	if !ok {
		t.Fatal("failed to load team from disk")
	}
	if len(loaded.Members) != 2 {
		t.Errorf("expected 2 members, got %d", len(loaded.Members))
	}
}

// TestTeamManager_FindByMember tests finding team by agent ID.
func TestTeamManager_FindByMember(t *testing.T) {
	tmpDir := t.TempDir()
	bus := NewMessageBus()
	registry := NewMailboxRegistry(256, 0)
	tm := NewTeamManager(tmpDir, registry, bus)

	_, _ = tm.CreateTeam("team-A", "Team A", "lead-A")
	_ = tm.AddMember("team-A", "worker-A", "worker", "worker")
	_, _ = tm.CreateTeam("team-B", "Team B", "lead-B")

	// Find team for worker-A.
	teamName, ok := tm.FindTeamByMember("worker-A")
	if !ok {
		t.Fatal("expected to find team for worker-A")
	}
	if teamName != "team-A" {
		t.Errorf("expected 'team-A', got %q", teamName)
	}

	// Non-existent agent.
	_, ok = tm.FindTeamByMember("non-existent")
	if ok {
		t.Error("should not find team for non-existent agent")
	}
}

// TestMailbox_Basic tests mailbox deliver, read, ack.
func TestMailbox_Basic(t *testing.T) {
	mb := NewMailbox("agent-1", 10, 0)

	// Deliver message.
	msgID, err := mb.Deliver("sender", "hello world", MailboxPriorityNormal, "")
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if msgID == "" {
		t.Error("expected message ID")
	}

	// Read messages.
	messages := mb.Read()
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].Text != "hello world" {
		t.Errorf("expected 'hello world', got %q", messages[0].Text)
	}
	if messages[0].From != "sender" {
		t.Errorf("expected from 'sender', got %q", messages[0].From)
	}

	// Ack message.
	if !mb.Ack(msgID) {
		t.Error("failed to ack message")
	}
}

// TestMailbox_Priority tests priority is stored.
func TestMailbox_Priority(t *testing.T) {
	mb := NewMailbox("agent-1", 10, 0)

	// Deliver in mixed order.
	_, _ = mb.Deliver("A", "msg-A", MailboxPriorityLow, "")
	_, _ = mb.Deliver("B", "msg-B", MailboxPriorityHigh, "")
	_, _ = mb.Deliver("C", "msg-C", MailboxPriorityNormal, "")

	messages := mb.Peek()
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}

	// Verify priorities are stored.
	priorities := make([]MailboxPriority, 3)
	for i, m := range messages {
		priorities[i] = m.Priority
	}

	// Should have all three priorities.
	hasHigh := false
	hasLow := false
	hasNormal := false
	for _, p := range priorities {
		if p == MailboxPriorityHigh {
			hasHigh = true
		}
		if p == MailboxPriorityLow {
			hasLow = true
		}
		if p == MailboxPriorityNormal {
			hasNormal = true
		}
	}
	if !hasHigh || !hasLow || !hasNormal {
		t.Errorf("expected all priorities present, got %v", priorities)
	}
}

// TestMailboxRegistry_Send tests sending between mailboxes.
func TestMailboxRegistry_Send(t *testing.T) {
	registry := NewMailboxRegistry(256, 0)

	// Send from A to B.
	msgID, err := registry.Send("agent-A", "agent-B", "test message", MailboxPriorityNormal, "")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if msgID == "" {
		t.Error("expected message ID")
	}

	// Read from B's mailbox.
	mb := registry.GetOrCreate("agent-B")
	messages := mb.Read()
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].Text != "test message" {
		t.Errorf("expected 'test message', got %q", messages[0].Text)
	}
	if messages[0].From != "agent-A" {
		t.Errorf("expected from 'agent-A', got %q", messages[0].From)
	}
}

// TestTeamManager_UpdateMemberStatus tests status updates.
func TestTeamManager_UpdateMemberStatus(t *testing.T) {
	tmpDir := t.TempDir()
	bus := NewMessageBus()
	registry := NewMailboxRegistry(256, 0)
	tm := NewTeamManager(tmpDir, registry, bus)

	_, _ = tm.CreateTeam("status-test", "Status test", "lead")
	_ = tm.AddMember("status-test", "worker-1", "worker", "worker")

	// Update to idle.
	err := tm.UpdateMemberStatus("status-test", "worker-1", "idle")
	if err != nil {
		t.Fatalf("UpdateMemberStatus: %v", err)
	}

	team, _ := tm.GetTeam("status-test")
	for _, m := range team.Members {
		if m.AgentID == "worker-1" && m.Status != "idle" {
			t.Errorf("expected status 'idle', got %q", m.Status)
		}
	}
}

// TestTeamManager_BroadcastToTeam tests team broadcast.
func TestTeamManager_BroadcastToTeam(t *testing.T) {
	tmpDir := t.TempDir()
	bus := NewMessageBus()
	registry := NewMailboxRegistry(256, 0)
	tm := NewTeamManager(tmpDir, registry, bus)

	_, _ = tm.CreateTeam("broadcast-test", "Broadcast test", "lead")
	_ = tm.AddMember("broadcast-test", "worker-1", "worker", "worker")
	_ = tm.AddMember("broadcast-test", "worker-2", "worker", "worker")

	// Lead broadcasts to team.
	err := tm.BroadcastToTeam("broadcast-test", "lead", "hello team")
	if err != nil {
		t.Fatalf("BroadcastToTeam: %v", err)
	}

	// Workers should receive the message.
	mb1 := registry.GetOrCreate("worker-1")
	mb2 := registry.GetOrCreate("worker-2")

	msgs1 := mb1.Read()
	msgs2 := mb2.Read()

	if len(msgs1) != 1 {
		t.Errorf("worker-1: expected 1 message, got %d", len(msgs1))
	}
	if len(msgs2) != 1 {
		t.Errorf("worker-2: expected 1 message, got %d", len(msgs2))
	}
}

// TestTeamManager_ActiveMemberCount tests counting active members.
func TestTeamManager_ActiveMemberCount(t *testing.T) {
	tmpDir := t.TempDir()
	bus := NewMessageBus()
	registry := NewMailboxRegistry(256, 0)
	tm := NewTeamManager(tmpDir, registry, bus)

	_, _ = tm.CreateTeam("count-test", "Count test", "lead")
	_ = tm.AddMember("count-test", "worker-1", "worker", "worker")

	count := tm.ActiveMemberCount("count-test")
	if count != 2 {
		t.Errorf("expected 2 active, got %d", count)
	}

	// Set one to idle.
	_ = tm.UpdateMemberStatus("count-test", "worker-1", "idle")

	count = tm.ActiveMemberCount("count-test")
	if count != 1 {
		t.Errorf("expected 1 active after setting worker to idle, got %d", count)
	}
}

// TestTeamManager_DeleteTeam tests team deletion.
func TestTeamManager_DeleteTeam(t *testing.T) {
	tmpDir := t.TempDir()
	bus := NewMessageBus()
	registry := NewMailboxRegistry(256, 0)
	tm := NewTeamManager(tmpDir, registry, bus)

	_, _ = tm.CreateTeam("delete-test", "Delete test", "lead")
	_ = tm.AddMember("delete-test", "worker-1", "worker", "worker")

	// Delete team.
	err := tm.DeleteTeam("delete-test")
	if err != nil {
		t.Fatalf("DeleteTeam: %v", err)
	}

	// Verify deleted.
	_, ok := tm.GetTeam("delete-test")
	if ok {
		t.Error("team should be deleted")
	}

	// Verify file removed.
	teamFile := filepath.Join(tmpDir, ".claude", "teams", "delete-test.json")
	if _, err := os.Stat(teamFile); !os.IsNotExist(err) {
		t.Error("team file should be removed")
	}
}

// TestTeammateRegistry_Basic tests teammate registry.
func TestTeammateRegistry_Basic(t *testing.T) {
	registry := NewTeammateRegistry()

	// Get non-existent.
	_, ok := registry.Get("non-existent")
	if ok {
		t.Error("should not find non-existent teammate")
	}

	// All should be empty.
	all := registry.All()
	if len(all) != 0 {
		t.Errorf("expected empty, got %d", len(all))
	}
}

// TestInProcessTeammate_Lifecycle tests teammate start/stop.
// Note: Requires a Runner which needs full engine setup - skip in unit tests.
func TestInProcessTeammate_Lifecycle(t *testing.T) {
	t.Skip("requires Runner which needs full engine setup")
}