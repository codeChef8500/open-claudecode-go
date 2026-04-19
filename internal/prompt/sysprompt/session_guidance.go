package sysprompt

import "strings"

// [P3.T1] TS anchor: constants/prompts.ts:L352-400

// SessionGuidanceOpts collects the runtime bits needed to build the
// session-specific guidance section.
type SessionGuidanceOpts struct {
	HasAskUserQuestionTool bool
	AskUserQuestionName    string
	HasAgentTool           bool
	AgentToolName          string
	HasSkills              bool
	SkillToolName          string
	EmbeddedSearchTools    bool
	IsNonInteractive       bool
	ForkSubagentEnabled    bool
	ExplorePlanEnabled     bool
	VerificationAgent      bool
	VerificationAgentType  string
	// Search tool names (when not embedded)
	GlobToolName string
	GrepToolName string
	BashToolName string
	// Explore agent config
	ExploreAgentType     string
	ExploreAgentMinQuery int
	// DiscoverSkills
	DiscoverSkillsToolName string
	DiscoverSkillsEnabled  bool
}

// GetSessionSpecificGuidanceSection returns the "# Session-specific guidance"
// section, or "" when nothing to emit.
func GetSessionSpecificGuidanceSection(o SessionGuidanceOpts) string {
	var items []interface{}

	if o.HasAskUserQuestionTool {
		items = append(items,
			`If you do not understand why the user has denied a tool call, use the `+o.AskUserQuestionName+` to ask them.`,
		)
	}

	if !o.IsNonInteractive {
		items = append(items,
			"If you need the user to run a shell command themselves (e.g., an interactive login like `gcloud auth login`), suggest they type `! <command>` in the prompt — the `!` prefix runs the command in this session so its output lands directly in the conversation.",
		)
	}

	if o.HasAgentTool {
		items = append(items,
			GetAgentToolSection(o.AgentToolName, o.ForkSubagentEnabled),
		)
	}

	if o.HasAgentTool && o.ExplorePlanEnabled && !o.ForkSubagentEnabled {
		searchTools := "`find` or `grep` via the " + o.BashToolName + " tool"
		if !o.EmbeddedSearchTools {
			searchTools = "the " + o.GlobToolName + " or " + o.GrepToolName
		}
		items = append(items,
			`For simple, directed codebase searches (e.g. for a specific file/class/function) use `+searchTools+` directly.`,
		)
		exploreType := o.ExploreAgentType
		if exploreType == "" {
			exploreType = "explore"
		}
		minQ := 5
		if o.ExploreAgentMinQuery > 0 {
			minQ = o.ExploreAgentMinQuery
		}
		items = append(items,
			`For broader codebase exploration and deep research, use the `+o.AgentToolName+` tool with subagent_type=`+exploreType+`. This is slower than using `+searchTools+` directly, so use this only when a simple, directed search proves to be insufficient or when your task will clearly require more than `+itoa(minQ)+` queries.`,
		)
	}

	if o.HasSkills {
		items = append(items,
			`/<skill-name> (e.g., /commit) is shorthand for users to invoke a user-invocable skill. When executed, the skill gets expanded to a full prompt. Use the `+o.SkillToolName+` tool to execute them. IMPORTANT: Only use `+o.SkillToolName+` for skills listed in its user-invocable skills section - do not guess or use built-in CLI commands.`,
		)
	}

	if o.DiscoverSkillsEnabled && o.DiscoverSkillsToolName != "" && o.HasSkills {
		items = append(items,
			`Relevant skills are automatically surfaced each turn as "Skills relevant to your task:" reminders. If you're about to do something those don't cover — a mid-task pivot, an unusual workflow, a multi-step plan — call `+o.DiscoverSkillsToolName+` with a specific description of what you're doing. Skills already visible or loaded are filtered automatically. Skip this if the surfaced skills already cover your next action.`,
		)
	}

	if o.HasAgentTool && o.VerificationAgent {
		items = append(items,
			`The contract: when non-trivial implementation happens on your turn, independent adversarial verification must happen before you report completion — regardless of who did the implementing (you directly, a fork you spawned, or a subagent). You are the one reporting to the user; you own the gate. Non-trivial means: 3+ file edits, backend/API changes, or infrastructure changes. Spawn the `+o.AgentToolName+` tool with subagent_type="`+o.VerificationAgentType+`". Your own checks, caveats, and a fork's self-checks do NOT substitute — only the verifier assigns a verdict; you cannot self-assign PARTIAL. Pass the original user request, all files changed (by anyone), the approach, and the plan file path if applicable. Flag concerns if you have them but do NOT share test results or claim things work. On FAIL: fix, resume the verifier with its findings plus your fix, repeat until PASS. On PASS: spot-check it — re-run 2-3 commands from its report, confirm every PASS has a Command run block with output that matches your re-run. If any PASS lacks a command block or diverges, resume the verifier with the specifics. On PARTIAL (from the verifier): report what passed and what could not be verified.`,
		)
	}

	if len(items) == 0 {
		return ""
	}

	lines := append([]string{"# Session-specific guidance"}, PrependBullets(items...)...)
	return strings.Join(lines, "\n")
}

// itoa is a minimal int-to-string for small positive integers.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
