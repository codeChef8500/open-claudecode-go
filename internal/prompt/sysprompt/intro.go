package sysprompt

import "github.com/wall-ai/agent-engine/internal/prompt/constants"

// [P2.T1] TS anchor: constants/prompts.ts:L175-184

// GetSimpleIntroSection builds the intro paragraph for getSystemPrompt.
// outputStyleName is non-empty when an OutputStyle is active.
func GetSimpleIntroSection(outputStyleName string) string {
	taskDesc := "with software engineering tasks."
	if outputStyleName != "" {
		taskDesc = `according to your "Output Style" below, which describes how you should respond to user queries.`
	}

	return "\nYou are an interactive agent that helps users " + taskDesc + ` Use the instructions below and the tools available to you to assist the user.

` + constants.CyberRiskInstruction + `
IMPORTANT: You must NEVER generate or guess URLs for the user unless you are confident that the URLs are for helping the user with programming. You may use URLs provided by the user in their messages or local files.`
}
