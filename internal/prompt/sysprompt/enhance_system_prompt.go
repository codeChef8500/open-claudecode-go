package sysprompt

// [P4.T2] TS anchor: constants/prompts.ts:L760-791

// EnhanceOpts holds the config for enhanceSystemPromptWithEnvDetails.
type EnhanceOpts struct {
	Model                   string
	AdditionalWorkingDirs   []string
	EnabledToolNames        map[string]bool
	DiscoverSkillsToolName  string
	DiscoverSkillsEnabled   bool
	// EnvInfo is the pre-computed environment info string (computeEnvInfo result).
	EnvInfo                 string
}

// EnhanceSystemPromptWithEnvDetails appends agent-specific notes and env info
// to an existing system prompt slice.
// TS anchor: constants/prompts.ts:L760-791
func EnhanceSystemPromptWithEnvDetails(existing []string, o EnhanceOpts) []string {
	notes := `Notes:
- Agent threads always have their cwd reset between bash calls, as a result please only use absolute file paths.
- In your final response, share file paths (always absolute, never relative) that are relevant to the task. Include code snippets only when the exact text is load-bearing (e.g., a bug you found, a function signature the caller asked for) — do not recap code you merely read.
- For clear communication with the user the assistant MUST avoid using emojis.
- Do not use a colon before tool calls. Text like "Let me read the file:" followed by a read tool call should just be "Let me read the file." with a period.`

	out := make([]string, len(existing))
	copy(out, existing)
	out = append(out, notes)

	if o.DiscoverSkillsEnabled && o.DiscoverSkillsToolName != "" &&
		(o.EnabledToolNames == nil || o.EnabledToolNames[o.DiscoverSkillsToolName]) {
		guidance := `Relevant skills are automatically surfaced each turn as "Skills relevant to your task:" reminders. If you're about to do something those don't cover — a mid-task pivot, an unusual workflow, a multi-step plan — call ` +
			o.DiscoverSkillsToolName + ` with a specific description of what you're doing. Skills already visible or loaded are filtered automatically. Skip this if the surfaced skills already cover your next action.`
		out = append(out, guidance)
	}

	if o.EnvInfo != "" {
		out = append(out, o.EnvInfo)
	}

	return out
}
