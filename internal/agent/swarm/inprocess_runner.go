package swarm

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wall-ai/agent-engine/internal/state"
)

// ── InProcessRunner ──────────────────────────────────────────────────────────
//
// Full lifecycle for an in-process teammate, aligned with
// claude-code-main's inProcessRunner.ts runInProcessTeammate().
//
// Lifecycle:
//   1. Register in AppState.InProcessTeammates
//   2. Run initial prompt through AgentRunner
//   3. Enter mailbox poll loop:
//      a. Check for shutdown_request → respond with shutdown_approved, exit
//      b. Check for permission_response → forward to bridge
//      c. Check for task_assignment → claim task, run through AgentRunner
//      d. Check for plain text → run as new prompt
//      e. If no messages → send idle_notification, wait
//   4. Cleanup: unregister from AppState

// AgentRunFunc is the callback to run agent logic for a single prompt.
// Returns the agent's text response or error.
type AgentRunFunc func(ctx context.Context, prompt string) (string, error)

// InProcessRunnerConfig configures the in-process runner.
type InProcessRunnerConfig struct {
	Config          TeammateSpawnConfig
	Mailbox         MailboxAdapter
	AppState        *state.AppState
	RunAgent        AgentRunFunc
	PermBridge      *LeaderPermissionBridge // nil if not using leader bridge
	PollInterval    time.Duration           // default 1s
	IdleTimeout     time.Duration           // max idle before self-shutdown, 0 = no limit
	CompactInterval time.Duration           // default 30s
}

// RunInProcessTeammate is the main lifecycle function for an in-process teammate.
// It blocks until the teammate is shut down or the context is cancelled.
func RunInProcessTeammate(ctx context.Context, cfg InProcessRunnerConfig) error {
	identity := cfg.Config.Identity
	agentID := identity.AgentID
	agentName := identity.AgentName
	teamName := identity.TeamName

	log := slog.With(
		slog.String("agent_id", agentID),
		slog.String("agent_name", agentName),
		slog.String("team", teamName),
	)

	// Set defaults.
	pollInterval := cfg.PollInterval
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	compactInterval := cfg.CompactInterval
	if compactInterval <= 0 {
		compactInterval = 30 * time.Second
	}

	// Enrich context with teammate identity.
	ctx = WithTeammateIdentity(ctx, identity)
	if cfg.Config.PermissionMode != "" {
		ctx = WithPermissionMode(ctx, cfg.Config.PermissionMode)
	}

	// 1. Register in AppState.
	taskID := uuid.New().String()
	taskState := &state.InProcessTeammateTaskState{
		TaskID:         taskID,
		AgentID:        agentID,
		AgentName:      agentName,
		TeamName:       teamName,
		Status:         "running",
		PermissionMode: cfg.Config.PermissionMode,
	}
	if cfg.AppState != nil {
		cfg.AppState.Update(func(s *state.AppState) {
			if s.InProcessTeammates == nil {
				s.InProcessTeammates = make(map[string]*state.InProcessTeammateTaskState)
			}
			s.InProcessTeammates[agentID] = taskState
		})
	}

	// Cleanup on exit.
	defer func() {
		if cfg.AppState != nil {
			cfg.AppState.Update(func(s *state.AppState) {
				delete(s.InProcessTeammates, agentID)
			})
		}
		log.Info("inprocess: teammate exited")
	}()

	// 2. Run initial prompt.
	log.Info("inprocess: running initial prompt")
	if cfg.Config.Prompt != "" && cfg.RunAgent != nil {
		updateTaskStatus(cfg.AppState, agentID, "running", false)
		_, err := cfg.RunAgent(ctx, cfg.Config.Prompt)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Warn("inprocess: initial prompt failed", slog.Any("err", err))
		}
	}

	// 3. Enter mailbox poll loop.
	log.Info("inprocess: entering poll loop")
	var (
		turnCount     int
		lastCompact   = time.Now()
		idleSent      bool
		totalPausedMs int64
	)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		// Poll mailbox for new messages.
		messages, err := cfg.Mailbox.ReadPending(agentID)
		if err != nil {
			log.Warn("inprocess: mailbox read error", slog.Any("err", err))
			continue
		}

		if len(messages) == 0 {
			// No messages — send idle notification if not already sent.
			if !idleSent {
				idleSent = true
				updateTaskStatus(cfg.AppState, agentID, "idle", true)
				sendIdleNotification(cfg.Mailbox, agentID, teamName, taskID)
				log.Info("inprocess: idle, waiting for messages")
			}
			continue
		}

		// Process messages.
		for _, msg := range messages {
			// Mark as read.
			_ = cfg.Mailbox.MarkRead(agentID, msg.ID)

			switch {
			case msg.IsShutdownRequest():
				log.Info("inprocess: received shutdown request")
				sendShutdownApproved(cfg.Mailbox, agentID, teamName, agentName)
				updateTaskStatus(cfg.AppState, agentID, "completed", false)
				return nil

			case msg.IsPermissionResponse():
				if cfg.PermBridge != nil {
					var resp PermissionResponsePayload
					if err := msg.DecodePayload(&resp); err == nil {
						cfg.PermBridge.Resolve(resp.RequestID, resp.Decision == "allow" || resp.Decision == "allow_always")
					}
				}

			case msg.Type == MessageTypeTaskAssignment:
				var task TaskAssignmentPayload
				if err := msg.DecodePayload(&task); err != nil {
					log.Warn("inprocess: bad task_assignment payload", slog.Any("err", err))
					continue
				}
				idleSent = false
				turnCount++
				updateTaskStatus(cfg.AppState, agentID, "running", false)
				log.Info("inprocess: claimed task", slog.String("task_id", task.TaskID))
				if cfg.RunAgent != nil {
					_, _ = cfg.RunAgent(ctx, task.Description)
				}

			case msg.Type == MessageTypeModeSetRequest:
				var modeReq ModeSetRequestPayload
				if err := msg.DecodePayload(&modeReq); err == nil {
					ctx = WithPermissionMode(ctx, modeReq.Mode)
					log.Info("inprocess: mode changed", slog.String("mode", modeReq.Mode))
				}

			case msg.Type == MessageTypeTeamPermissionUpdate:
				// Forward permission updates — store in state for tool filtering.
				log.Info("inprocess: permission update received")

			default:
				// Plain text or unknown — treat as new prompt.
				var text string
				if msg.Type == MessageTypePlainText {
					var p PlainTextPayload
					if err := msg.DecodePayload(&p); err == nil {
						text = p.Text
					}
				} else {
					text = string(msg.Payload)
				}

				if text != "" {
					idleSent = false
					turnCount++
					updateTaskStatus(cfg.AppState, agentID, "running", false)
					log.Info("inprocess: processing message",
						slog.String("from", msg.From),
						slog.Int("turn", turnCount))
					if cfg.RunAgent != nil {
						pauseStart := time.Now()
						_, _ = cfg.RunAgent(ctx, text)
						totalPausedMs += time.Since(pauseStart).Milliseconds()
					}
				}
			}
		}

		// Compaction: periodically update task stats.
		if time.Since(lastCompact) > compactInterval {
			lastCompact = time.Now()
			updateTaskStats(cfg.AppState, agentID, turnCount, totalPausedMs)
		}
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func updateTaskStatus(appState *state.AppState, agentID, status string, isIdle bool) {
	if appState == nil {
		return
	}
	appState.Update(func(s *state.AppState) {
		if ts, ok := s.InProcessTeammates[agentID]; ok {
			ts.Status = status
			ts.IsIdle = isIdle
		}
	})
}

func updateTaskStats(appState *state.AppState, agentID string, turnCount int, totalPausedMs int64) {
	if appState == nil {
		return
	}
	appState.Update(func(s *state.AppState) {
		if ts, ok := s.InProcessTeammates[agentID]; ok {
			ts.TurnCount = turnCount
			ts.TotalPausedMs = totalPausedMs
		}
	})
}

func sendIdleNotification(mailbox MailboxAdapter, agentID, teamName, taskID string) {
	leaderID := FormatAgentID(TeamLeadName, teamName)
	env, err := NewEnvelope(agentID, leaderID, MessageTypeIdleNotification,
		IdleNotificationPayload{
			AgentName: AgentNameFromAgentID(agentID),
			TaskID:    taskID,
			Reason:    "completed",
		})
	if err != nil {
		return
	}
	_ = mailbox.SendEnvelope(agentID, leaderID, env)
}

func sendShutdownApproved(mailbox MailboxAdapter, agentID, teamName, agentName string) {
	leaderID := FormatAgentID(TeamLeadName, teamName)
	env, err := NewEnvelope(agentID, leaderID, MessageTypeShutdownApproved,
		ShutdownApprovedPayload{
			AgentName: agentName,
			Summary:   "Shutdown approved by teammate",
		})
	if err != nil {
		return
	}
	_ = mailbox.SendEnvelope(agentID, leaderID, env)
}

// AgentNameFromAgentID extracts the name part from an agent ID.
func AgentNameFromAgentID(agentID string) string {
	name, _ := ParseAgentID(agentID)
	if name == "" {
		return agentID
	}
	return name
}

// ── LeaderPermissionBridge ───────────────────────────────────────────────────
//
// Provides a channel-based bridge for in-process teammates to request
// permissions from the leader's UI. Aligned with claude-code-main's
// leaderPermissionBridge.ts.

// PermissionBridgeRequest is a pending permission request.
type PermissionBridgeRequest struct {
	RequestID   string
	ToolName    string
	ToolUseID   string
	Input       json.RawMessage
	Description string
	WorkerID    string
	WorkerName  string
	WorkerColor string
	TeamName    string
	ResponseCh  chan bool // true = allow, false = deny
}

// LeaderPermissionBridge manages permission requests from in-process teammates.
type LeaderPermissionBridge struct {
	mu        sync.Mutex
	pending   map[string]*PermissionBridgeRequest
	onRequest func(req *PermissionBridgeRequest) // callback to TUI
}

// NewLeaderPermissionBridge creates a new permission bridge.
func NewLeaderPermissionBridge() *LeaderPermissionBridge {
	return &LeaderPermissionBridge{
		pending: make(map[string]*PermissionBridgeRequest),
	}
}

// SetOnRequest sets the callback invoked when a new permission request arrives.
func (b *LeaderPermissionBridge) SetOnRequest(fn func(req *PermissionBridgeRequest)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onRequest = fn
}

// Request submits a permission request and blocks until resolved.
// Returns true if permission was granted.
func (b *LeaderPermissionBridge) Request(ctx context.Context, req PermissionBridgeRequest) (bool, error) {
	if req.RequestID == "" {
		req.RequestID = uuid.New().String()
	}
	req.ResponseCh = make(chan bool, 1)

	b.mu.Lock()
	b.pending[req.RequestID] = &req
	onReq := b.onRequest
	b.mu.Unlock()

	// Notify the TUI/leader.
	if onReq != nil {
		onReq(&req)
	}

	select {
	case <-ctx.Done():
		b.mu.Lock()
		delete(b.pending, req.RequestID)
		b.mu.Unlock()
		return false, ctx.Err()
	case granted := <-req.ResponseCh:
		b.mu.Lock()
		delete(b.pending, req.RequestID)
		b.mu.Unlock()
		return granted, nil
	}
}

// Resolve answers a pending permission request.
func (b *LeaderPermissionBridge) Resolve(requestID string, granted bool) {
	b.mu.Lock()
	req, ok := b.pending[requestID]
	b.mu.Unlock()

	if ok && req.ResponseCh != nil {
		select {
		case req.ResponseCh <- granted:
		default:
		}
	}
}

// PendingRequests returns all currently pending permission requests.
func (b *LeaderPermissionBridge) PendingRequests() []*PermissionBridgeRequest {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := make([]*PermissionBridgeRequest, 0, len(b.pending))
	for _, r := range b.pending {
		result = append(result, r)
	}
	return result
}

// HasPending returns true if there are unresolved permission requests.
func (b *LeaderPermissionBridge) HasPending() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending) > 0
}
