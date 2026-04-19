package sysprompt

// [P2.T1] TS anchor: constants/prompts.ts:L131-134

// GetSystemRemindersSection returns the system reminders bullet text.
func GetSystemRemindersSection() string {
	return `- Tool results and user messages may include <system-reminder> tags. <system-reminder> tags contain useful information and reminders. They are automatically added by the system, and bear no direct relation to the specific tool results or user messages in which they appear.
- The conversation has unlimited context through automatic summarization.`
}
