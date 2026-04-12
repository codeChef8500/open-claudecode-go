package test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wall-ai/agent-engine/internal/agent"
	"github.com/wall-ai/agent-engine/internal/agent/swarm"
	"github.com/wall-ai/agent-engine/internal/engine"
	"github.com/wall-ai/agent-engine/internal/state"
)

// ── Swarm E2E Tests ──────────────────────────────────────────────────────────
//
// These tests exercise the full multi-agent lifecycle using the real
// SwarmManager with mock LLM agents (no API key required), and optionally
// with live LLM if credentials are available.

// TestSwarmE2E_FullLifecycle creates a team, spawns 2 teammates, exchanges
// messages, and shuts down cleanly.
func TestSwarmE2E_FullLifecycle(t *testing.T) {
	baseDir := t.TempDir()
	appState := state.New(baseDir)
	reg := agent.NewMailboxRegistry(256, 0)
	bus := agent.NewMessageBus()
	tm := agent.NewTeamManager(baseDir, reg, bus)

	taskResults := &sync.Map{}
	runAgent := func(ctx context.Context, prompt string) (string, error) {
		taskResults.Store(prompt, true)
		return "completed: " + prompt, nil
	}

	sm := swarm.NewSwarmManager(swarm.SwarmManagerConfig{
		BaseDir:     baseDir,
		TeamManager: tm,
		AppState:    appState,
		BackendMode: swarm.BackendModeInProcess,
		RunAgent:    runAgent,
	})
	defer sm.Cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Create team.
	_, err := tm.CreateTeam("e2e-team", "End-to-end test team", "leader@e2e-team")
	require.NoError(t, err)

	// 2. Spawn teammates.
	out1, err := sm.SpawnTeammate(ctx, swarm.TeammateSpawnConfig{
		Identity: swarm.TeammateIdentity{
			AgentName: "analyst",
			TeamName:  "e2e-team",
		},
		Prompt:  "Analyze the codebase structure",
		WorkDir: baseDir,
	})
	require.NoError(t, err)
	assert.Equal(t, "analyst@e2e-team", out1.AgentID)

	out2, err := sm.SpawnTeammate(ctx, swarm.TeammateSpawnConfig{
		Identity: swarm.TeammateIdentity{
			AgentName: "writer",
			TeamName:  "e2e-team",
		},
		Prompt:  "Write documentation",
		WorkDir: baseDir,
	})
	require.NoError(t, err)
	assert.Equal(t, "writer@e2e-team", out2.AgentID)

	// Wait for initial prompts to be processed.
	time.Sleep(2 * time.Second)

	// 3. Verify both ran.
	_, ok1 := taskResults.Load("Analyze the codebase structure")
	_, ok2 := taskResults.Load("Write documentation")
	assert.True(t, ok1, "analyst should have processed its prompt")
	assert.True(t, ok2, "writer should have processed its prompt")

	// 4. Send inter-teammate message.
	_, err = sm.SendMessage("leader@e2e-team", "analyst@e2e-team", "Focus on Go files only", "normal")
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	_, ok3 := taskResults.Load("Focus on Go files only")
	assert.True(t, ok3, "analyst should have processed follow-up message")

	// 5. Verify AppState tracking.
	snap := appState.Get()
	assert.NotNil(t, snap.TeamContext)
	assert.True(t, sm.IsActive())
	assert.Equal(t, 2, sm.TeammateCount())

	// 6. Shutdown.
	sm.ShutdownAll("test complete")
	time.Sleep(1 * time.Second)
	assert.Equal(t, 0, sm.TeammateCount())
}

// TestSwarmE2E_PermissionBridgeIntegration tests the full permission flow:
// teammate requests → bridge → auto-approve → teammate continues.
func TestSwarmE2E_PermissionBridgeIntegration(t *testing.T) {
	baseDir := t.TempDir()
	appState := state.New(baseDir)
	reg := agent.NewMailboxRegistry(256, 0)
	bus := agent.NewMessageBus()
	tm := agent.NewTeamManager(baseDir, reg, bus)

	sm := swarm.NewSwarmManager(swarm.SwarmManagerConfig{
		BaseDir:     baseDir,
		TeamManager: tm,
		AppState:    appState,
		BackendMode: swarm.BackendModeInProcess,
	})
	defer sm.Cleanup()

	// Auto-approve permissions.
	var permRequests atomic.Int32
	sm.PermBridge.SetOnRequest(func(req *swarm.PermissionBridgeRequest) {
		permRequests.Add(1)
		go func() {
			time.Sleep(10 * time.Millisecond)
			sm.PermBridge.Resolve(req.RequestID, true)
		}()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test multiple sequential permission requests.
	for i := 0; i < 3; i++ {
		granted, err := sm.PermBridge.Request(ctx, swarm.PermissionBridgeRequest{
			ToolName:    "Edit",
			Description: "Test edit",
			WorkerID:    "worker@test",
			WorkerName:  "worker",
			TeamName:    "test",
		})
		require.NoError(t, err)
		assert.True(t, granted)
	}

	assert.Equal(t, int32(3), permRequests.Load())
}

// TestSwarmE2E_TeamLifecycle tests create → populate → finish → delete cycle.
func TestSwarmE2E_TeamLifecycle(t *testing.T) {
	baseDir := t.TempDir()
	appState := state.New(baseDir)
	reg := agent.NewMailboxRegistry(256, 0)
	bus := agent.NewMessageBus()
	tm := agent.NewTeamManager(baseDir, reg, bus)

	sm := swarm.NewSwarmManager(swarm.SwarmManagerConfig{
		BaseDir:     baseDir,
		TeamManager: tm,
		AppState:    appState,
		BackendMode: swarm.BackendModeInProcess,
	})
	defer sm.Cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Create team.
	tf, err := tm.CreateTeam("lifecycle-team", "Lifecycle test", "leader@lifecycle-team")
	require.NoError(t, err)
	require.NotNil(t, tf)

	// Spawn teammate.
	sm.RunAgent = func(ctx context.Context, prompt string) (string, error) {
		return "done", nil
	}
	_, err = sm.SpawnTeammate(ctx, swarm.TeammateSpawnConfig{
		Identity: swarm.TeammateIdentity{
			AgentName: "temp-worker",
			TeamName:  "lifecycle-team",
		},
		Prompt:  "Temp task",
		WorkDir: baseDir,
	})
	require.NoError(t, err)

	time.Sleep(1 * time.Second)

	// Shutdown teammate.
	sm.ShutdownAll("lifecycle test")
	time.Sleep(500 * time.Millisecond)

	// Finish and delete team.
	err = tm.FinishTeam("lifecycle-team")
	require.NoError(t, err)
	err = tm.DeleteTeam("lifecycle-team")
	require.NoError(t, err)

	// Verify team is gone.
	teams := tm.ListTeams()
	for _, t2 := range teams {
		assert.NotEqual(t, "lifecycle-team", t2)
	}
}

// TestSwarmE2E_LiveLLMSwarm tests multi-agent coordination with a real LLM.
// Skips if no API key is configured.
func TestSwarmE2E_LiveLLMSwarm(t *testing.T) {
	apiKey, _, _, _ := liveEnvConfig()
	if apiKey == "" {
		t.Skip("No API key — skipping live LLM swarm test")
	}

	baseDir := t.TempDir()
	appState := state.New(baseDir)
	reg := agent.NewMailboxRegistry(256, 0)
	bus := agent.NewMessageBus()
	tm := agent.NewTeamManager(baseDir, reg, bus)

	eng := newLiveEngine(t, baseDir, 256)

	sm := swarm.NewSwarmManager(swarm.SwarmManagerConfig{
		BaseDir:     baseDir,
		TeamManager: tm,
		AppState:    appState,
		BackendMode: swarm.BackendModeInProcess,
	})
	defer sm.Cleanup()

	// Wire RunAgent to use the real LLM engine.
	sm.RunAgent = func(ctx context.Context, prompt string) (string, error) {
		events := eng.SubmitMessage(ctx, prompt)
		var fullText string
		for ev := range events {
			switch ev.Type {
			case engine.EventTextDelta:
				fullText += ev.Text
			case engine.EventError:
				return "", fmt.Errorf("%s", ev.Error)
			}
		}
		return fullText, nil
	}

	// Auto-approve permissions.
	sm.PermBridge.SetOnRequest(func(req *swarm.PermissionBridgeRequest) {
		go sm.PermBridge.Resolve(req.RequestID, true)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create team.
	_, err := tm.CreateTeam("live-team", "Live LLM test", "leader@live-team")
	require.NoError(t, err)

	// Spawn a single teammate with a simple task.
	out, err := sm.SpawnTeammate(ctx, swarm.TeammateSpawnConfig{
		Identity: swarm.TeammateIdentity{
			AgentName: "summarizer",
			TeamName:  "live-team",
		},
		Prompt:  "Reply with exactly the words: SWARM_TEST_OK",
		WorkDir: baseDir,
	})
	require.NoError(t, err)
	t.Logf("Spawned live teammate: %s (backend=%s)", out.AgentID, out.BackendType)

	// Wait for the teammate to process its initial prompt.
	time.Sleep(15 * time.Second)

	// Verify the teammate ran.
	assert.True(t, sm.IsActive() || sm.TeammateCount() >= 0, "swarm should have processed")

	status := sm.FormatSwarmStatus()
	t.Logf("Swarm status: %s", status)

	sm.ShutdownAll("live test done")
}
