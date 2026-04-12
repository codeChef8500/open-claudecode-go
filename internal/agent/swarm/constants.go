package swarm

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// ── Constants aligned with claude-code-main's swarm/constants.ts ─────────────

// TeamLeadName is the canonical name for the team leader agent.
const TeamLeadName = "team-lead"

// SwarmSessionPrefix is used to generate tmux session names for swarm panes.
const SwarmSessionPrefix = "claude-swarm"

// Environment variable keys set on spawned teammate processes.
const (
	TeammateCommandEnvVar  = "CLAUDE_TEAMMATE_COMMAND"
	TeammateColorEnvVar    = "CLAUDE_TEAMMATE_COLOR"
	PlanModeRequiredEnvVar = "CLAUDE_PLAN_MODE_REQUIRED"
	TeammateNameEnvVar     = "CLAUDE_TEAMMATE_NAME"
	TeamNameEnvVar         = "CLAUDE_TEAM_NAME"
	ParentSessionEnvVar    = "CLAUDE_PARENT_SESSION_ID"
)

// Feature flag environment variable.
const AgentSwarmsEnvVar = "AGENT_SWARMS_ENABLED"

// AgentID format: "name@teamName"
const agentIDSeparator = "@"

// ── Name helpers ─────────────────────────────────────────────────────────────

var nonAlphaNumDash = regexp.MustCompile(`[^a-zA-Z0-9-]`)

// SanitizeName normalises a string into a safe identifier (lowercase, alphanumeric + dash).
func SanitizeName(name string) string {
	s := strings.TrimSpace(name)
	s = nonAlphaNumDash.ReplaceAllString(s, "-")
	s = strings.ToLower(s)
	// Collapse consecutive dashes.
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if s == "" {
		s = "agent"
	}
	return s
}

// FormatAgentID produces a deterministic agent ID from name and team.
// Format: "name@teamName" (aligned with TS formatAgentId).
func FormatAgentID(name, teamName string) string {
	return SanitizeName(name) + agentIDSeparator + SanitizeName(teamName)
}

// ParseAgentID splits an agent ID into (name, teamName).
// Returns ("", "") if the format is invalid.
func ParseAgentID(agentID string) (name, teamName string) {
	idx := strings.LastIndex(agentID, agentIDSeparator)
	if idx < 0 {
		return "", ""
	}
	return agentID[:idx], agentID[idx+1:]
}

// GetSwarmSessionName returns a unique tmux session name for a team.
func GetSwarmSessionName(teamName string) string {
	return fmt.Sprintf("%s-%s", SwarmSessionPrefix, SanitizeName(teamName))
}

// IsAgentSwarmsEnabled checks if the swarm feature is active.
func IsAgentSwarmsEnabled() bool {
	v := os.Getenv(AgentSwarmsEnvVar)
	return v == "1" || strings.EqualFold(v, "true")
}
