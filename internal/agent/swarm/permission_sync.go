package swarm

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// ── Permission Sync ──────────────────────────────────────────────────────────
//
// Handles mailbox-based permission request/response flow between
// teammates and the leader. Used by both in-process (via bridge) and
// tmux (via file mailbox) backends.
//
// Aligned with claude-code-main's permissionSync.ts.

// PermissionSyncConfig configures the permission sync module.
type PermissionSyncConfig struct {
	Mailbox    MailboxAdapter
	Bridge     *LeaderPermissionBridge // for in-process
	AgentID    string
	TeamName   string
	IsLeader   bool
}

// RequestPermissionViaMailbox sends a permission request to the team leader
// via mailbox and blocks until a response is received or context is cancelled.
// Used by tmux-backend teammates that can't use the in-process bridge.
func RequestPermissionViaMailbox(ctx context.Context, cfg PermissionSyncConfig, req PermissionRequestPayload) (bool, error) {
	if req.RequestID == "" {
		req.RequestID = uuid.New().String()
	}
	req.WorkerID = cfg.AgentID
	req.TeamName = cfg.TeamName

	leaderID := FormatAgentID(TeamLeadName, cfg.TeamName)

	// Send permission request to leader's mailbox.
	env, err := NewEnvelope(cfg.AgentID, leaderID, MessageTypePermissionRequest, req)
	if err != nil {
		return false, err
	}
	if err := cfg.Mailbox.SendEnvelope(cfg.AgentID, leaderID, env); err != nil {
		return false, err
	}

	slog.Info("permission_sync: request sent to leader",
		slog.String("request_id", req.RequestID),
		slog.String("tool", req.ToolName))

	// Poll for response.
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-ticker.C:
			messages, err := cfg.Mailbox.ReadPending(cfg.AgentID)
			if err != nil {
				continue
			}
			for _, msg := range messages {
				if msg.IsPermissionResponse() {
					var resp PermissionResponsePayload
					if err := msg.DecodePayload(&resp); err != nil {
						continue
					}
					if resp.RequestID == req.RequestID {
						_ = cfg.Mailbox.MarkRead(cfg.AgentID, msg.ID)
						granted := resp.Decision == "allow" || resp.Decision == "allow_always"
						slog.Info("permission_sync: response received",
							slog.String("request_id", req.RequestID),
							slog.Bool("granted", granted))
						return granted, nil
					}
				}
			}
		}
	}
}

// RequestPermissionViaBridge sends a permission request through the
// in-process leader bridge, bypassing the mailbox entirely.
// Used by in-process teammates for lower latency.
func RequestPermissionViaBridge(ctx context.Context, bridge *LeaderPermissionBridge, req PermissionBridgeRequest) (bool, error) {
	return bridge.Request(ctx, req)
}

// HandleLeaderPermissionMessages processes incoming permission requests
// on the leader side. Called from the leader's mailbox poll loop.
// Returns pending requests that need UI interaction.
func HandleLeaderPermissionMessages(mailbox MailboxAdapter, leaderID string) []*PermissionBridgeRequest {
	messages, err := mailbox.ReadPending(leaderID)
	if err != nil {
		return nil
	}

	var requests []*PermissionBridgeRequest
	for _, msg := range messages {
		if !msg.IsPermissionRequest() {
			continue
		}

		var req PermissionRequestPayload
		if err := msg.DecodePayload(&req); err != nil {
			slog.Warn("permission_sync: bad permission request", slog.Any("err", err))
			continue
		}

		_ = mailbox.MarkRead(leaderID, msg.ID)

		inputRaw, _ := json.Marshal(req.Input)
		requests = append(requests, &PermissionBridgeRequest{
			RequestID:   req.RequestID,
			ToolName:    req.ToolName,
			ToolUseID:   req.ToolUseID,
			Input:       inputRaw,
			Description: req.Description,
			WorkerID:    req.WorkerID,
			WorkerName:  req.WorkerName,
			WorkerColor: req.WorkerColor,
			TeamName:    req.TeamName,
		})
	}

	return requests
}

// RespondToPermissionRequest sends a permission response back to the
// requesting worker via mailbox.
func RespondToPermissionRequest(mailbox MailboxAdapter, leaderID string, req *PermissionBridgeRequest, granted bool) error {
	decision := "deny"
	if granted {
		decision = "allow"
	}

	env, err := NewEnvelope(leaderID, req.WorkerID, MessageTypePermissionResponse,
		PermissionResponsePayload{
			RequestID: req.RequestID,
			Decision:  decision,
		})
	if err != nil {
		return err
	}

	return mailbox.SendEnvelope(leaderID, req.WorkerID, env)
}
