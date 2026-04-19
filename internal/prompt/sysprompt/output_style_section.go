package sysprompt

// [P2.T3] TS anchor: constants/prompts.ts:L151-158

// GetOutputStyleSection returns the "# Output Style: <name>" section when an
// output style config is active, or "" if none.
func GetOutputStyleSection(name, prompt string) string {
	if name == "" {
		return ""
	}
	return "# Output Style: " + name + "\n" + prompt
}
