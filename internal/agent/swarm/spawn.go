package swarm

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/wall-ai/agent-engine/internal/state"
)

// ── SpawnMultiAgent ──────────────────────────────────────────────────────────
//
// Unified spawn entry point for creating teammates.
// Aligned with claude-code-main's spawnMultiAgent.ts.
//
// Resolves backend via BackendRegistry, spawns the teammate, and
// updates AppState.TeamContext.

// SpawnMultiAgentConfig holds all dependencies needed to spawn a teammate.
type SpawnMultiAgentConfig struct {
	BackendRegistry *BackendRegistry
	Mailbox         MailboxAdapter
	AppState        *state.AppState
	PermBridge      *LeaderPermissionBridge
	RunAgent        AgentRunFunc // agent run callback for in-process
}

// SpawnTeammate creates and starts a new teammate via the appropriate backend.
func SpawnTeammate(ctx context.Context, cfg SpawnMultiAgentConfig, spawnCfg TeammateSpawnConfig) (*SpawnOutput, error) {
	// Resolve agent ID.
	if spawnCfg.Identity.AgentID == "" {
		spawnCfg.Identity.AgentID = FormatAgentID(spawnCfg.Identity.AgentName, spawnCfg.Identity.TeamName)
	}

	agentID := spawnCfg.Identity.AgentID
	agentName := spawnCfg.Identity.AgentName
	teamName := spawnCfg.Identity.TeamName

	log := slog.With(
		slog.String("agent_id", agentID),
		slog.String("team", teamName),
	)

	// Resolve the executor.
	executor, backendType, err := cfg.BackendRegistry.ResolveExecutor()
	if err != nil {
		return nil, fmt.Errorf("resolve backend: %w", err)
	}

	log.Info("spawn: resolved backend", slog.String("backend", string(backendType)))

	// For in-process, wrap the RunAgent with the full lifecycle runner.
	if backendType == BackendInProcess {
		inpBackend, ok := executor.(*InProcessBackend)
		if ok && inpBackend.runnerFunc == nil && cfg.RunAgent != nil {
			// Set the runner func to use RunInProcessTeammate lifecycle.
			inpBackend.runnerFunc = func(innerCtx context.Context, innerCfg TeammateSpawnConfig) error {
				return RunInProcessTeammate(innerCtx, InProcessRunnerConfig{
					Config:     innerCfg,
					Mailbox:    cfg.Mailbox,
					AppState:   cfg.AppState,
					RunAgent:   cfg.RunAgent,
					PermBridge: cfg.PermBridge,
				})
			}
		}
	}

	// Spawn via executor.
	result, err := executor.Spawn(ctx, spawnCfg)
	if err != nil {
		return nil, fmt.Errorf("spawn teammate: %w", err)
	}

	// Update AppState.TeamContext.
	if cfg.AppState != nil {
		cfg.AppState.Update(func(s *state.AppState) {
			if s.TeamContext == nil {
				s.TeamContext = &state.TeamContext{
					TeamName: teamName,
				}
			}
			if s.TeamContext.Teammates == nil {
				s.TeamContext.Teammates = make(map[string]state.TeammateRef)
			}
			s.TeamContext.Teammates[agentID] = state.TeammateRef{
				Name:        agentName,
				AgentID:     agentID,
				AgentType:   spawnCfg.AgentType,
				BackendType: string(backendType),
				Model:       spawnCfg.Model,
				Color:       spawnCfg.Identity.Color,
				CWD:         spawnCfg.WorkDir,
				TmuxPaneID:  result.PaneID,
				IsActive:    true,
				SpawnedAt:   time.Now().Unix(),
			}
		})
	}

	output := &SpawnOutput{
		TeammateID:       agentID,
		AgentID:          agentID,
		AgentType:        spawnCfg.AgentType,
		Model:            spawnCfg.Model,
		Name:             agentName,
		Color:            spawnCfg.Identity.Color,
		TeamName:         teamName,
		BackendType:      backendType,
		TmuxPaneID:       result.PaneID,
		PlanModeRequired: spawnCfg.Identity.PlanModeRequired,
	}

	if backendType == BackendTmux {
		output.TmuxSessionName = GetSwarmSessionName(teamName)
	}

	log.Info("spawn: teammate started",
		slog.String("backend", string(backendType)),
		slog.String("pane_id", result.PaneID))

	return output, nil
}

// ── Color assignment ─────────────────────────────────────────────────────────

// TeammateColors is the set of ANSI colors for teammate badges.
var TeammateColors = []string{
	"#FF6B6B", "#4ECDC4", "#45B7D1", "#96CEB4",
	"#FFEAA7", "#DDA0DD", "#98D8C8", "#F7DC6F",
	"#BB8FCE", "#85C1E9", "#82E0AA", "#F8C471",
}

var colorIndex int

// NextTeammateColor returns the next color in the rotation.
func NextTeammateColor() string {
	c := TeammateColors[colorIndex%len(TeammateColors)]
	colorIndex++
	return c
}
