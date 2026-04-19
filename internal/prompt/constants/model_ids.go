package constants

// Model-related constants ported from constants/prompts.ts:L117-125.
// [P1.T2] TS anchor: constants/prompts.ts:L117-125

// FrontierModelName is the human-readable name of the latest frontier model.
// @[MODEL LAUNCH]: Update when Anthropic ships a new top model.
const FrontierModelName = "Claude Opus 4.6"

// Claude45Or46ModelIDs maps tier → model ID for the Claude 4.5/4.6 family.
// @[MODEL LAUNCH]: Update the IDs below to the latest in each tier.
var Claude45Or46ModelIDs = map[string]string{
	"opus":   "claude-opus-4-6",
	"sonnet": "claude-sonnet-4-6",
	"haiku":  "claude-haiku-4-5-20251001",
}
