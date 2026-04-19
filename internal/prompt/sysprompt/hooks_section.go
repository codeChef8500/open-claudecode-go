package sysprompt

// [P2.T1] TS anchor: constants/prompts.ts:L127-129

// GetHooksSection returns the hooks guidance paragraph.
func GetHooksSection() string {
	return `Users may configure 'hooks', shell commands that execute in response to events like tool calls, in settings. Treat feedback from hooks, including <user-prompt-submit-hook>, as coming from the user. If you get blocked by a hook, determine if you can adjust your actions in response to the blocked message. If not, ask the user to check their hooks configuration.`
}
