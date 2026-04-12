package swarm

import (
	"context"
)

// ── TeammateContext ──────────────────────────────────────────────────────────
//
// Go's context.Context replaces TypeScript's AsyncLocalStorage for
// per-teammate isolation. Each in-process teammate gets its own context
// carrying identity, team, and runtime state.
//
// Aligned with claude-code-main's teammateContext.ts.

type contextKey int

const (
	ctxKeyIdentity contextKey = iota
	ctxKeyTeamName
	ctxKeyAgentID
	ctxKeyAgentName
	ctxKeyIsTeammate
	ctxKeyParentSessionID
	ctxKeyPermissionMode
	ctxKeyColor
	ctxKeyPlanModeRequired
)

// WithTeammateIdentity stores the full TeammateIdentity in context.
func WithTeammateIdentity(ctx context.Context, id TeammateIdentity) context.Context {
	ctx = context.WithValue(ctx, ctxKeyIdentity, id)
	ctx = context.WithValue(ctx, ctxKeyAgentID, id.AgentID)
	ctx = context.WithValue(ctx, ctxKeyAgentName, id.AgentName)
	ctx = context.WithValue(ctx, ctxKeyTeamName, id.TeamName)
	ctx = context.WithValue(ctx, ctxKeyIsTeammate, true)
	ctx = context.WithValue(ctx, ctxKeyParentSessionID, id.ParentSessionID)
	ctx = context.WithValue(ctx, ctxKeyColor, id.Color)
	ctx = context.WithValue(ctx, ctxKeyPlanModeRequired, id.PlanModeRequired)
	return ctx
}

// TeammateIdentityFromContext extracts the TeammateIdentity from context.
func TeammateIdentityFromContext(ctx context.Context) (TeammateIdentity, bool) {
	id, ok := ctx.Value(ctxKeyIdentity).(TeammateIdentity)
	return id, ok
}

// AgentIDFromContext returns the agent ID from context.
func AgentIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyAgentID).(string)
	return v
}

// AgentNameFromContext returns the agent name from context.
func AgentNameFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyAgentName).(string)
	return v
}

// TeamNameFromContext returns the team name from context.
func TeamNameFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyTeamName).(string)
	return v
}

// IsTeammateContext returns true if the context belongs to a teammate.
func IsTeammateContext(ctx context.Context) bool {
	v, _ := ctx.Value(ctxKeyIsTeammate).(bool)
	return v
}

// ColorFromContext returns the teammate's color from context.
func ColorFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyColor).(string)
	return v
}

// WithPermissionMode stores the permission mode in context.
func WithPermissionMode(ctx context.Context, mode string) context.Context {
	return context.WithValue(ctx, ctxKeyPermissionMode, mode)
}

// PermissionModeFromContext returns the permission mode from context.
func PermissionModeFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyPermissionMode).(string)
	return v
}
