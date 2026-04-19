package sysprompt

import "strings"

// [P2.T3] TS anchor: constants/prompts.ts:L430-442

// GetSimpleToneAndStyleSection returns the "# Tone and style" section.
func GetSimpleToneAndStyleSection(isAnt bool) string {
	var items []interface{}
	items = append(items,
		`Only use emojis if the user explicitly requests it. Avoid using emojis in all communication unless asked.`,
	)
	if !isAnt {
		items = append(items,
			`Your responses should be short and concise.`,
		)
	}
	items = append(items,
		`When referencing specific functions or pieces of code include the pattern file_path:line_number to allow the user to easily navigate to the source code location.`,
		`When referencing GitHub issues or pull requests, use the owner/repo#123 format (e.g. anthropics/claude-code#100) so they render as clickable links.`,
		`Do not use a colon before tool calls. Your tool calls may not be shown directly in the output, so text like "Let me read the file:" followed by a read tool call should just be "Let me read the file." with a period.`,
	)

	lines := append([]string{"# Tone and style"}, PrependBullets(items...)...)
	return strings.Join(lines, "\n")
}
