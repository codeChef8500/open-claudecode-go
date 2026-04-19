package sysprompt

import "strconv"

// [P3.T4] TS anchor: constants/prompts.ts:L821-839

// GetFunctionResultClearingSection returns the "# Function Result Clearing"
// section, or "" when not applicable.
//
// enabled: feature('CACHED_MICROCOMPACT') && config.enabled && config.systemPromptSuggestSummaries
// modelSupported: whether current model matches config.supportedModels
// keepRecent: number of recent results to keep
func GetFunctionResultClearingSection(enabled, modelSupported bool, keepRecent int) string {
	if !enabled || !modelSupported {
		return ""
	}
	return `# Function Result Clearing

Old tool results will be automatically cleared from context to free up space. The ` + strconv.Itoa(keepRecent) + ` most recent results are always kept.`
}

// SummarizeToolResultsSection is the constant instruction about saving
// important info from tool results.
// TS anchor: constants/prompts.ts:L841
const SummarizeToolResultsSection = `When working with tool results, write down any important information you might need later in your response, as the original tool result may be cleared later.`
