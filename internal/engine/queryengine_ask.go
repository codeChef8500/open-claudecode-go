package engine

import (
	"context"
)

// ────────────────────────────────────────────────────────────────────────────
// [P8.T3] ask() convenience wrapper — mirrors claude-code-main
// QueryEngine.ts ask() function for one-shot SDK usage.
// ────────────────────────────────────────────────────────────────────────────

// AskOptions bundles all parameters for the one-shot Ask function.
type AskOptions struct {
	// Prompt is the user's input text.
	Prompt string
	// PromptUUID is an optional UUID for the user message.
	PromptUUID string
	// IsMeta marks the prompt as a synthetic/meta message.
	IsMeta bool

	// Config is the full QueryEngine configuration.
	Config *QueryEngineConfig
}

// Ask sends a single prompt to the model and returns a channel of SDK messages.
// It is a convenience wrapper around QueryEngine for one-shot usage.
//
// Equivalent to the TS async generator function ask() in QueryEngine.ts.
func Ask(ctx context.Context, opts AskOptions) <-chan interface{} {
	qe := NewQueryEngine(opts.Config)

	submitOpts := &SubmitMessageOptions{
		UUID:   opts.PromptUUID,
		IsMeta: opts.IsMeta,
	}

	return qe.SubmitMessage(ctx, opts.Prompt, submitOpts)
}
