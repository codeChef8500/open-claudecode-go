package sysprompt

// [P2.T4] TS anchor: constants/prompts.ts:L316-320

// GetAgentToolSection returns guidance for the Agent/fork tool.
// forkSubagentEnabled mirrors isForkSubagentEnabled().
func GetAgentToolSection(agentToolName string, forkSubagentEnabled bool) string {
	if forkSubagentEnabled {
		return `Calling ` + agentToolName + ` without a subagent_type creates a fork, which runs in the background and keeps its tool output out of your context — so you can keep chatting with the user while it works. Reach for it when research or multi-step implementation work would otherwise fill your context with raw output you won't need again. **If you ARE the fork** — execute directly; do not re-delegate.`
	}
	return `Use the ` + agentToolName + ` tool with specialized agents when the task at hand matches the agent's description. Subagents are valuable for parallelizing independent queries or for protecting the main context window from excessive results, but they should not be used excessively when not needed. Importantly, avoid duplicating work that subagents are already doing - if you delegate research to a subagent, do not also perform the same searches yourself.`
}
