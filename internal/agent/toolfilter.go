package agent

import (
	"strings"
)

// Tool permission filtering aligned with claude-code-main's
// constants/tools.ts and agentToolUtils.ts filterToolsForAgent.

// AllAgentDisallowedTools are tools no agent may use, regardless of type.
// Aligned with ALL_AGENT_DISALLOWED_TOOLS in constants/tools.ts.
var AllAgentDisallowedTools = map[string]bool{
	"TaskOutput":      true,
	"ExitPlanMode":    true,
	"EnterPlanMode":   true,
	"Task":            true, // prevent recursive spawning (non-coordinator)
	"AskUserQuestion": true,
	"TaskStop":        true,
}

// CustomAgentDisallowedTools are additional tools denied to custom (non-builtin) agents.
// Aligned with CUSTOM_AGENT_DISALLOWED_TOOLS in constants/tools.ts.
var CustomAgentDisallowedTools = map[string]bool{
	"Task":     true,
	"TaskStop": true,
}

// AsyncAgentAllowedTools are the only tools an async (background) agent may use.
// Aligned with ASYNC_AGENT_ALLOWED_TOOLS in constants/tools.ts.
var AsyncAgentAllowedTools = map[string]bool{
	"Read":            true,
	"WebSearch":       true,
	"TodoWrite":       true,
	"Grep":            true,
	"WebFetch":        true,
	"Glob":            true,
	"Bash":            true,
	"PowerShell":      true,
	"FileEdit":        true,
	"FileWrite":       true,
	"NotebookEdit":    true,
	"Skill":           true,
	"SyntheticOutput": true,
	"ToolSearch":      true,
	"EnterWorktree":   true,
	"ExitWorktree":    true,
}

// InProcessTeammateAllowedTools are additional tools available to in-process teammates.
// These are added ON TOP of the async agent allowed tools.
// Aligned with IN_PROCESS_TEAMMATE_ALLOWED_TOOLS in constants/tools.ts.
var InProcessTeammateAllowedTools = map[string]bool{
	"TaskCreate":  true,
	"TaskGet":     true,
	"TaskList":    true,
	"TaskUpdate":  true,
	"SendMessage": true,
	"CronCreate":  true,
	"CronDelete":  true,
	"CronList":    true,
}

// CoordinatorModeAllowedTools are the tools available to the coordinator agent.
// Aligned with COORDINATOR_MODE_ALLOWED_TOOLS in constants/tools.ts.
var CoordinatorModeAllowedTools = map[string]bool{
	"Task":            true,
	"TaskStop":        true,
	"SendMessage":     true,
	"SyntheticOutput": true,
}

// CoordinatorModeAllowedToolsList returns the coordinator tool whitelist as a
// []string slice. Used by the deprecated CoordinatorAllowedTools variable in
// coordinator_mode.go so that callers referencing the old name still get the
// correct, TS-aligned tool set.
func CoordinatorModeAllowedToolsList() []string {
	out := make([]string, 0, len(CoordinatorModeAllowedTools))
	for name := range CoordinatorModeAllowedTools {
		out = append(out, name)
	}
	return out
}

// SimpleCoordinatorAllowedTools returns the set of tool names the coordinator
// is allowed to use. When CLAUDE_CODE_SIMPLE is active, the set is the union
// of simple tools (Bash, Read, FileEdit) and coordinator tools (Task, TaskStop,
// SendMessage, SyntheticOutput), aligned with TS tools.ts:287-297.
// When simple mode is NOT active, only the coordinator whitelist is returned.
func SimpleCoordinatorAllowedTools() map[string]bool {
	result := make(map[string]bool, len(CoordinatorModeAllowedTools)+3)
	for k, v := range CoordinatorModeAllowedTools {
		result[k] = v
	}
	if isSimpleMode() {
		// Simple + coordinator: coordinator gets direct tool access too.
		result["Bash"] = true
		result["Read"] = true
		result["FileEdit"] = true
	}
	return result
}

// IsPrActivitySubscriptionTool returns true if the tool name matches the PR
// activity subscription MCP tools. Aligned with TS toolPool.ts:
// isPrActivitySubscriptionTool() which checks for these suffixed tool names.
func IsPrActivitySubscriptionTool(name string) bool {
	return strings.HasSuffix(name, "subscribe_pr_activity") ||
		strings.HasSuffix(name, "unsubscribe_pr_activity")
}

// FilterToolsForAgent filters the available tools based on agent type and execution context.
// This implements the layered filtering logic from agentToolUtils.ts filterToolsForAgent.
//
// Filtering layers (applied in order):
//  1. AllAgentDisallowedTools — always removed
//  2. CustomAgentDisallowedTools — removed for non-builtin agents
//  3. AsyncAgentAllowedTools — if async, restrict to this allowlist
//  4. InProcessTeammateAllowedTools — if in-process teammate, add these to async allowlist
//  5. CoordinatorModeAllowedTools — if coordinator, restrict to this set
//  6. Agent definition's AllowedTools / DisallowedTools — final per-agent filter
func FilterToolsForAgent(
	allTools []string,
	agentDef *AgentDefinition,
	isAsync bool,
	isInProcessTeammate bool,
	isCoordinator bool,
) []string {
	if agentDef == nil {
		return allTools
	}

	// Step 0: Coordinator mode — the coordinator whitelist OVERRIDES the
	// global disallow list. In TS, COORDINATOR_MODE_ALLOWED_TOOLS is applied
	// independently, not after ALL_AGENT_DISALLOWED_TOOLS. Task and TaskStop
	// are in AllAgentDisallowedTools (to prevent recursive spawning by workers)
	// but the coordinator MUST have them.
	if isCoordinator {
		var pass []string
		for _, t := range allTools {
			if CoordinatorModeAllowedTools[t] {
				pass = append(pass, t)
			}
		}
		return pass // coordinator tools are final, no further filtering
	}

	// Step 1: Start with all tools, remove globally disallowed.
	var filtered []string
	for _, t := range allTools {
		if AllAgentDisallowedTools[t] {
			continue
		}
		filtered = append(filtered, t)
	}

	// Step 2: For non-builtin agents, also remove custom-agent disallowed tools.
	if agentDef.Source != SourceBuiltIn {
		var pass []string
		for _, t := range filtered {
			if !CustomAgentDisallowedTools[t] {
				pass = append(pass, t)
			}
		}
		filtered = pass
	}

	// Step 4: Async agents — restrict to async allowlist.
	if isAsync {
		allowed := make(map[string]bool)
		for k, v := range AsyncAgentAllowedTools {
			allowed[k] = v
		}
		// Step 4b: In-process teammates get additional tools.
		if isInProcessTeammate {
			for k, v := range InProcessTeammateAllowedTools {
				allowed[k] = v
			}
		}
		var pass []string
		for _, t := range filtered {
			if allowed[t] {
				pass = append(pass, t)
			}
		}
		filtered = pass
	}

	// Step 5: Apply agent definition's own AllowedTools (whitelist).
	if len(agentDef.AllowedTools) > 0 {
		allowed := resolveToolPatterns(agentDef.AllowedTools, filtered)
		var pass []string
		for _, t := range filtered {
			if allowed[t] {
				pass = append(pass, t)
			}
		}
		filtered = pass
	}

	// Step 6: Apply agent definition's DisallowedTools (blacklist).
	if len(agentDef.DisallowedTools) > 0 {
		denied := resolveToolPatterns(agentDef.DisallowedTools, filtered)
		var pass []string
		for _, t := range filtered {
			if !denied[t] {
				pass = append(pass, t)
			}
		}
		filtered = pass
	}

	return filtered
}

// ResolveAgentTools resolves the effective tool set for an agent definition
// against the list of all available tool names. Handles wildcard expansion.
func ResolveAgentTools(agentDef *AgentDefinition, availableTools []string) []string {
	if agentDef == nil {
		return availableTools
	}

	// If no explicit tool restrictions, return all available.
	if len(agentDef.AllowedTools) == 0 && len(agentDef.DisallowedTools) == 0 {
		return availableTools
	}

	result := availableTools

	// Apply whitelist.
	if len(agentDef.AllowedTools) > 0 {
		allowed := resolveToolPatterns(agentDef.AllowedTools, availableTools)
		var filtered []string
		for _, t := range result {
			if allowed[t] {
				filtered = append(filtered, t)
			}
		}
		result = filtered
	}

	// Apply blacklist.
	if len(agentDef.DisallowedTools) > 0 {
		denied := resolveToolPatterns(agentDef.DisallowedTools, availableTools)
		var filtered []string
		for _, t := range result {
			if !denied[t] {
				filtered = append(filtered, t)
			}
		}
		result = filtered
	}

	return result
}

// resolveToolPatterns expands tool name patterns (including wildcards)
// against the set of available tool names.
func resolveToolPatterns(patterns []string, available []string) map[string]bool {
	result := make(map[string]bool)
	for _, pattern := range patterns {
		if pattern == "*" {
			// Wildcard: match all tools.
			for _, t := range available {
				result[t] = true
			}
		} else if strings.Contains(pattern, "*") {
			// Glob pattern: prefix* or *suffix.
			prefix := strings.TrimSuffix(pattern, "*")
			for _, t := range available {
				if strings.HasPrefix(t, prefix) {
					result[t] = true
				}
			}
		} else {
			// Exact match (case-insensitive).
			for _, t := range available {
				if strings.EqualFold(t, pattern) {
					result[t] = true
				}
			}
		}
	}
	return result
}

// ToolsContain checks if a tool name is present in the list.
func ToolsContain(tools []string, name string) bool {
	for _, t := range tools {
		if strings.EqualFold(t, name) {
			return true
		}
	}
	return false
}
