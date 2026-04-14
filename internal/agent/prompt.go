package agent

import (
	"fmt"
	"os"
	"strings"
)

// OneShotBuiltinAgentTypes is the set of agent types that are one-shot
// (do not need agentId/SendMessage trailer). Aligned with TS constants.ts.
var OneShotBuiltinAgentTypes = map[string]bool{
	"explore": true,
	"plan":    true,
}

// IsOneShotBuiltinAgent returns true if the agent type is a one-shot builtin.
func IsOneShotBuiltinAgent(agentType string) bool {
	return OneShotBuiltinAgentTypes[agentType]
}

// ShouldInjectAgentListInMessages returns true if the agent list should be
// injected as an attachment message rather than in the tool description.
// This avoids prompt cache invalidation when MCP/plugin tools change.
// Aligned with TS prompt.ts:shouldInjectAgentListInMessages().
func ShouldInjectAgentListInMessages() bool {
	return os.Getenv("CLAUDE_CODE_AGENT_LIST_IN_MESSAGES") == "1"
}

// Agent prompt building aligned with claude-code-main's prompt.ts.
// Generates the AgentTool's prompt including the dynamic agent catalog
// and guidelines for writing effective agent prompts.

// BuildAgentToolPrompt generates the full prompt for the AgentTool.
// It includes the list of available agents and guidelines for using them.
func BuildAgentToolPrompt(agents []AgentDefinition, isCoordinatorMode bool) string {
	return BuildAgentToolPromptFull(agents, isCoordinatorMode, "", false)
}

// BuildAgentToolPromptFull generates the full prompt for the AgentTool with
// additional options for fork mode and one-shot agent trailer control.
// Aligned with claude-code-main's prompt.ts getAgentToolDescription.
func BuildAgentToolPromptFull(agents []AgentDefinition, isCoordinatorMode bool, forAgentType string, forkEnabled bool) string {
	var sb strings.Builder

	sb.WriteString("Launch a new agent to handle complex, multi-step tasks autonomously.\n\n")

	// Agent catalog — skip if using attachment mode.
	if len(agents) > 0 && !ShouldInjectAgentListInMessages() {
		sb.WriteString("## Available Agent Types\n\n")
		for _, a := range agents {
			sb.WriteString(fmt.Sprintf("- **%s**", a.AgentType))
			if a.WhenToUse != "" {
				sb.WriteString(fmt.Sprintf(": %s", a.WhenToUse))
			}
			sb.WriteString("\n")
			if a.Model != "" {
				sb.WriteString(fmt.Sprintf("  Model: %s\n", a.Model))
			}
			if a.Background {
				sb.WriteString("  Default: runs in background\n")
			}
			if a.Isolation != IsolationNone {
				sb.WriteString(fmt.Sprintf("  Isolation: %s\n", a.Isolation))
			}
		}
		sb.WriteString("\n")
	}

	// When NOT to use.
	sb.WriteString("## When NOT to use the Task tool\n\n")
	sb.WriteString("- If you want to read a specific file path, use the Read tool or Glob tool instead\n")
	sb.WriteString("- If you are searching for a specific class definition like \"class Foo\", use Glob/Grep instead\n")
	sb.WriteString("- If you are searching for code within a specific file or set of 2-3 files, use the Read tool instead\n\n")

	// Fork guidance — enhanced section aligned with TS getWhenToForkSection.
	if forkEnabled || IsForkSubagentEnabled() {
		sb.WriteString(getWhenToForkSection())
	}

	// Usage notes.
	sb.WriteString("## Usage notes\n\n")
	sb.WriteString("- Always include a short description summarizing what the agent will do\n")
	sb.WriteString("- Launch multiple agents concurrently whenever possible to maximize performance\n")
	sb.WriteString("- When the agent is done, it returns a single result message. Summarize it for the user.\n")
	sb.WriteString("- Each agent invocation starts fresh — provide a complete task description\n")
	sb.WriteString("- The agent's outputs should generally be trusted\n")
	sb.WriteString("- Clearly tell the agent whether you expect it to write code or just do research\n")

	// Concurrency note for non-Pro users.
	if os.Getenv("CLAUDE_PRO_TIER") != "1" {
		sb.WriteString("- Note: concurrent agents may be limited by your plan. Launch sequentially if needed.\n")
	}
	sb.WriteString("\n")

	// Writing the prompt.
	sb.WriteString("## Writing the prompt\n\n")
	sb.WriteString("Brief the agent like a smart colleague who just walked into the room:\n")
	sb.WriteString("- Explain what you're trying to accomplish and why\n")
	sb.WriteString("- Describe what you've already learned or ruled out\n")
	sb.WriteString("- Give enough context that the agent can make judgment calls\n")
	sb.WriteString("- Be explicit about whether to write code or just research\n")

	// Agent trailer — omit for one-shot builtin types (Explore, Plan).
	// Aligned with TS: ONE_SHOT_BUILTIN_AGENT_TYPES → omit agentId/SendMessage trailer.
	if !IsOneShotBuiltinAgent(forAgentType) {
		sb.WriteString("\n## Followup\n\n")
		sb.WriteString("After an agent completes, you can send follow-up instructions via SendMessage.\n")
		sb.WriteString("Use the agentId returned in the result to address the same agent instance.\n")
	}

	if isCoordinatorMode {
		sb.WriteString("\n## Coordinator Mode\n\n")
		sb.WriteString("You are operating in coordinator mode. Your role is to:\n")
		sb.WriteString("1. Break complex tasks into independent work items\n")
		sb.WriteString("2. Spawn worker agents for each work item\n")
		sb.WriteString("3. Monitor progress and handle failures\n")
		sb.WriteString("4. Synthesize results when all workers complete\n")
		sb.WriteString("5. Use the shared scratchpad directory for coordination\n")
	}

	return sb.String()
}

// getWhenToForkSection returns the detailed fork guidance section.
// Aligned with TS prompt.ts getWhenToForkSection().
func getWhenToForkSection() string {
	var sb strings.Builder
	sb.WriteString("## When to fork vs. spawn\n\n")
	sb.WriteString("**Fork** when you want to:\n")
	sb.WriteString("- Run multiple independent tasks in parallel from the current conversation context\n")
	sb.WriteString("- Each fork inherits the full conversation history for prompt cache efficiency\n")
	sb.WriteString("- Each fork works in an isolated git worktree to avoid conflicts\n")
	sb.WriteString("- Fork is best for \"do tasks A, B, C in parallel\" patterns\n\n")
	sb.WriteString("**Spawn (regular agent)** when you want to:\n")
	sb.WriteString("- Delegate a self-contained task that doesn't need current conversation context\n")
	sb.WriteString("- The agent starts fresh with only the task description you provide\n")
	sb.WriteString("- Spawn is best for \"go investigate X\" or \"implement Y based on this spec\" patterns\n\n")
	sb.WriteString("**Key difference**: forked agents share your prompt cache (cheaper/faster for context-heavy tasks), ")
	sb.WriteString("while spawned agents start fresh (better for independent tasks).\n\n")
	return sb.String()
}

// BuildAgentListAttachment builds the agent list as an attachment message
// for injection into the conversation. Used when ShouldInjectAgentListInMessages()
// returns true to avoid prompt cache invalidation.
func BuildAgentListAttachment(agents []AgentDefinition) string {
	if len(agents) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<available-agents>\n")
	for _, a := range agents {
		sb.WriteString(fmt.Sprintf("- %s", a.AgentType))
		if a.WhenToUse != "" {
			sb.WriteString(fmt.Sprintf(": %s", a.WhenToUse))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("</available-agents>")
	return sb.String()
}

// BuildAgentSystemPrompt generates the system prompt for a spawned agent.
// It combines the agent definition's system prompt with contextual information.
func BuildAgentSystemPrompt(def *AgentDefinition, parentContext *SubagentContext) string {
	if def.SystemPrompt != "" {
		return def.SystemPrompt
	}

	var sb strings.Builder
	sb.WriteString("You are a specialized agent working on a specific task.\n\n")

	if def.AgentType != "" {
		sb.WriteString(fmt.Sprintf("Agent type: %s\n", def.AgentType))
	}

	if def.WhenToUse != "" {
		sb.WriteString(fmt.Sprintf("Specialization: %s\n", def.WhenToUse))
	}

	if parentContext != nil && parentContext.TeamName != "" {
		sb.WriteString(fmt.Sprintf("Team: %s\n", parentContext.TeamName))
	}

	if def.CriticalSystemReminder != "" {
		sb.WriteString(fmt.Sprintf("\n## CRITICAL REMINDER\n%s\n", def.CriticalSystemReminder))
	}

	return sb.String()
}

// FormatAgentResult formats an agent's output for display to the parent.
func FormatAgentResult(result *AgentRunResult, maxChars int) string {
	if result.Error != nil {
		return fmt.Sprintf("Agent %s failed after %s (%d turns): %v",
			truncID(result.AgentID), result.Duration.Round(1e9),
			result.TurnCount, result.Error)
	}

	output := result.Output
	if maxChars > 0 && len(output) > maxChars {
		output = TruncateSubagentOutput(output, maxChars)
	}

	return fmt.Sprintf("Agent %s completed in %s (%d turns):\n\n%s",
		truncID(result.AgentID), result.Duration.Round(1e9),
		result.TurnCount, output)
}

// truncID returns the first 8 chars of an agent ID for display.
func truncID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
