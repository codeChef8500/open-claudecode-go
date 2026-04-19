package sysprompt

// [P2.T3] TS anchor: constants/prompts.ts:L142-149

// GetLanguageSection returns the "# Language" section when a language
// preference is configured, or "" if none.
func GetLanguageSection(languagePreference string) string {
	if languagePreference == "" {
		return ""
	}
	return `# Language
Always respond in ` + languagePreference + `. Use ` + languagePreference + ` for all explanations, comments, and communications with the user. Technical terms and code identifiers should remain in their original form.`
}
