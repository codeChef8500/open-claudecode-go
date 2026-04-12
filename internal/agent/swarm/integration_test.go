package swarm

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wall-ai/agent-engine/internal/agent"
	"github.com/wall-ai/agent-engine/internal/state"
)

// ── Integration Tests ────────────────────────────────────────────────────────
//
// These tests validate the full swarm lifecycle: spawn, message exchange,
// shutdown, and permission bridge — all using in-process backends.

func newTestSwarmManager(t *testing.T) *SwarmManager {
	t.Helper()
	baseDir := t.TempDir()
	appState := state.New(baseDir)

	// Create a TeamManager.
	reg := agent.NewMailboxRegistry(256, 0)
	bus := agent.NewMessageBus()
	tm := agent.NewTeamManager(baseDir, reg, bus)

	sm := NewSwarmManager(SwarmManagerConfig{
		BaseDir:     baseDir,
		TeamManager: tm,
		AppState:    appState,
		BackendMode: BackendModeInProcess,
	})

	t.Cleanup(func() {
		sm.Cleanup()
	})

	return sm
}

func TestIntegration_SpawnAndShutdown(t *testing.T) {
	sm := newTestSwarmManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create team.
	_, err := sm.TeamManager.CreateTeam("test-team", "Integration test", "leader@test-team")
	require.NoError(t, err)

	// Track whether runner was invoked.
	var runCalled atomic.Int32
	sm.RunAgent = func(ctx context.Context, prompt string) (string, error) {
		runCalled.Add(1)
		return "done: " + prompt, nil
	}

	// Spawn teammate.
	output, err := sm.SpawnTeammate(ctx, TeammateSpawnConfig{
		Identity: TeammateIdentity{
			AgentName:       "worker-1",
			TeamName:        "test-team",
			ParentSessionID: "sess-1",
		},
		Prompt:    "Find all Go files",
		AgentType: "code",
		WorkDir:   t.TempDir(),
	})
	require.NoError(t, err)
	assert.Equal(t, BackendInProcess, output.BackendType)
	assert.Equal(t, "worker-1", output.Name)
	assert.Contains(t, output.AgentID, "worker-1@test-team")

	// Wait for runner to be called.
	deadline := time.After(5 * time.Second)
	for runCalled.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for runner to be called")
		case <-time.After(50 * time.Millisecond):
		}
	}

	assert.True(t, sm.IsActive())
	assert.Equal(t, 1, sm.TeammateCount())

	// Shutdown.
	sm.ShutdownAll("test complete")

	// After shutdown, should have 0 teammates.
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 0, sm.TeammateCount())
}

func TestIntegration_MessageExchange(t *testing.T) {
	sm := newTestSwarmManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := sm.TeamManager.CreateTeam("msg-team", "Message test", "leader@msg-team")
	require.NoError(t, err)

	receivedMessages := make(chan string, 10)
	sm.RunAgent = func(ctx context.Context, prompt string) (string, error) {
		receivedMessages <- prompt
		return "ack", nil
	}

	_, err = sm.SpawnTeammate(ctx, TeammateSpawnConfig{
		Identity: TeammateIdentity{
			AgentName: "receiver",
			TeamName:  "msg-team",
		},
		Prompt:  "Wait for messages",
		WorkDir: t.TempDir(),
	})
	require.NoError(t, err)

	// Wait for initial prompt.
	select {
	case msg := <-receivedMessages:
		assert.Equal(t, "Wait for messages", msg)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for initial prompt")
	}

	// Send a message to the teammate.
	agentID := FormatAgentID("receiver", "msg-team")
	_, err = sm.SendMessage("leader@msg-team", agentID, "Hello from leader", "normal")
	require.NoError(t, err)

	// Teammate should receive the message in its poll loop.
	select {
	case msg := <-receivedMessages:
		assert.Equal(t, "Hello from leader", msg)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for leader message")
	}

	sm.ShutdownAll("done")
}

func TestIntegration_PermissionBridge(t *testing.T) {
	sm := newTestSwarmManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := sm.TeamManager.CreateTeam("perm-team", "Permission test", "leader@perm-team")
	require.NoError(t, err)

	// Auto-approve all permission requests.
	sm.PermBridge.SetOnRequest(func(req *PermissionBridgeRequest) {
		go func() {
			time.Sleep(50 * time.Millisecond) // simulate UI delay
			sm.PermBridge.Resolve(req.RequestID, true)
		}()
	})

	// Test permission request from a teammate context.
	granted, err := sm.PermBridge.Request(ctx, PermissionBridgeRequest{
		ToolName:    "Edit",
		Description: "Edit main.go",
		WorkerID:    "worker@perm-team",
		WorkerName:  "worker",
		TeamName:    "perm-team",
	})
	require.NoError(t, err)
	assert.True(t, granted)
}

func TestIntegration_AppStateTracking(t *testing.T) {
	sm := newTestSwarmManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := sm.TeamManager.CreateTeam("state-team", "State test", "leader@state-team")
	require.NoError(t, err)

	sm.RunAgent = func(ctx context.Context, prompt string) (string, error) {
		time.Sleep(100 * time.Millisecond)
		return "done", nil
	}

	_, err = sm.SpawnTeammate(ctx, TeammateSpawnConfig{
		Identity: TeammateIdentity{
			AgentName: "tracked",
			TeamName:  "state-team",
		},
		Prompt:  "Do something",
		WorkDir: t.TempDir(),
	})
	require.NoError(t, err)

	// Check AppState has the teammate.
	time.Sleep(200 * time.Millisecond)
	snap := sm.AppState.Get()

	agentID := FormatAgentID("tracked", "state-team")
	assert.NotNil(t, snap.TeamContext)
	if snap.TeamContext != nil {
		ref, ok := snap.TeamContext.Teammates[agentID]
		assert.True(t, ok, "teammate should be in TeamContext")
		assert.Equal(t, "tracked", ref.Name)
		assert.True(t, ref.IsActive)
	}

	// InProcessTeammates should have the task state.
	ts, ok := snap.InProcessTeammates[agentID]
	assert.True(t, ok, "should be in InProcessTeammates")
	if ok {
		assert.Equal(t, "state-team", ts.TeamName)
	}

	sm.ShutdownAll("done")
}

func TestIntegration_MultipleTeammates(t *testing.T) {
	sm := newTestSwarmManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := sm.TeamManager.CreateTeam("multi-team", "Multi test", "leader@multi-team")
	require.NoError(t, err)

	completions := make(chan string, 10)
	sm.RunAgent = func(ctx context.Context, prompt string) (string, error) {
		completions <- prompt
		return "done", nil
	}

	// Spawn 3 teammates.
	names := []string{"alpha", "beta", "gamma"}
	for _, name := range names {
		_, err := sm.SpawnTeammate(ctx, TeammateSpawnConfig{
			Identity: TeammateIdentity{
				AgentName: name,
				TeamName:  "multi-team",
			},
			Prompt:  "Task for " + name,
			WorkDir: t.TempDir(),
		})
		require.NoError(t, err)
	}

	assert.Equal(t, 3, sm.TeammateCount())

	// All should complete their initial prompts.
	seen := make(map[string]bool)
	for i := 0; i < 3; i++ {
		select {
		case msg := <-completions:
			seen[msg] = true
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout waiting for completion %d, seen: %v", i+1, seen)
		}
	}

	for _, name := range names {
		assert.True(t, seen["Task for "+name], "should have seen task for %s", name)
	}

	sm.ShutdownAll("done")
}

func TestIntegration_Constants(t *testing.T) {
	// Verify constants alignment with TS.
	assert.Equal(t, "team-lead", TeamLeadName)
	assert.Equal(t, "claude-swarm", SwarmSessionPrefix)

	// SanitizeName tests.
	assert.Equal(t, "hello-world", SanitizeName("Hello World!"))
	assert.Equal(t, "test-123", SanitizeName("test---123"))
	assert.Equal(t, "agent", SanitizeName(""))

	// FormatAgentID / ParseAgentID roundtrip.
	id := FormatAgentID("researcher", "my-team")
	assert.Equal(t, "researcher@my-team", id)
	name, team := ParseAgentID(id)
	assert.Equal(t, "researcher", name)
	assert.Equal(t, "my-team", team)
}
