package engine

import (
	"strings"
)

// ────────────────────────────────────────────────────────────────────────────
// [P8.T3] Local command handling — shouldQuery=false path.
// TS anchor: QueryEngine.ts:L556-639
// ────────────────────────────────────────────────────────────────────────────

const (
	// LocalCommandStdoutTag is the XML tag wrapping local command stdout.
	LocalCommandStdoutTag = "local-command-stdout"
	// LocalCommandStderrTag is the XML tag wrapping local command stderr.
	LocalCommandStderrTag = "local-command-stderr"
)

// ProcessUserInputResult holds the output of processUserInput.
// shouldQuery=false means the input was handled locally (slash command, etc.).
type ProcessUserInputResult struct {
	// Messages are the messages generated from user input.
	Messages []*Message
	// ShouldQuery indicates whether to proceed with the API query loop.
	ShouldQuery bool
	// AllowedTools is the list of tools that got auto-allowed by the input.
	AllowedTools []string
	// Model overrides the model if the user specified one via /model.
	Model string
	// ResultText is the text result for shouldQuery=false paths.
	ResultText string
}

// isLocalCommandOutput checks if a message contains local command output.
func isLocalCommandOutput(content string) bool {
	return strings.Contains(content, "<"+LocalCommandStdoutTag+">") ||
		strings.Contains(content, "<"+LocalCommandStderrTag+">")
}

// handleLocalCommandResult processes the shouldQuery=false path.
// It yields user replay messages and a success result, then returns.
// TS anchor: QueryEngine.ts:L556-638
func (qe *QueryEngine) handleLocalCommandResult(
	out chan<- interface{},
	puResult *ProcessUserInputResult,
	startTimeMs int,
	mainLoopModel string,
) {
	// Yield local command messages as SDK user replays.
	for _, msg := range puResult.Messages {
		if msg.Role == RoleUser && msg.Content != nil && len(msg.Content) > 0 {
			for _, block := range msg.Content {
				if block.Type == ContentTypeText && isLocalCommandOutput(block.Text) {
					// Yield as SDKUserReplayMessage
					out <- &SDKUserReplayMessage{
						Type:      SDKMsgUser,
						SessionID: qe.sessionID,
						UUID:      msg.UUID,
						IsReplay:  true,
					}
					break
				}
			}
		}
	}

	// Yield success result with resultText.
	turnCount := len(qe.mutableMessages)
	out <- NewSDKResultSuccess(
		qe.sessionID,
		puResult.ResultText,
		startTimeMs, 0, turnCount,
		qe.totalCostUSD(),
		qe.totalUsage,
		"", // no stop_reason for local commands
	)
}
