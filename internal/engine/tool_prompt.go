package engine

import "strings"

// ────────────────────────────────────────────────────────────────────────────
// [P10.T3] Tool Prompt Builder — generates per-tool prompt fragments that are
// appended to the system prompt before each model call.
//
// In the TS codebase, each tool can provide a getPrompt() returning context
// that is injected into the system prompt (e.g. file state, cwd, recent edits).
// TS anchor: services/tools/toolPrompt.ts
// ────────────────────────────────────────────────────────────────────────────

// ToolPromptProvider is implemented by tools that want to inject context into
// the system prompt. When present, GetToolPrompt is called once per query
// loop iteration before the model call.
type ToolPromptProvider interface {
	// GetToolPrompt returns prompt text to inject for this tool.
	// Return "" to skip injection.
	GetToolPrompt(ctx ToolPromptContext) string
}

// ToolPromptContext carries information available to tools for prompt generation.
type ToolPromptContext struct {
	// CWD is the current working directory.
	CWD string
	// TurnCount is the current turn number within this query loop.
	TurnCount int
	// Model is the model name being used.
	Model string
	// IsNonInteractive is true for SDK/CI sessions.
	IsNonInteractive bool
}

// BuildToolPrompts collects prompt fragments from all tools that implement
// ToolPromptProvider and returns the combined prompt text.
// TS anchor: services/tools/toolPrompt.ts:buildToolPrompts
func BuildToolPrompts(tools []Tool, ctx ToolPromptContext) string {
	return buildToolPromptsFromAny(tools, ctx)
}

// buildToolPromptsFromAny extracts prompts from a slice of tools by checking
// for ToolPromptProvider. Also each tool's Prompt() method is checked.
func buildToolPromptsFromAny(tools []Tool, ctx ToolPromptContext) string {
	var parts []string
	for _, t := range tools {
		// Prefer ToolPromptProvider interface for rich context-aware prompts.
		if tp, ok := t.(ToolPromptProvider); ok {
			if prompt := tp.GetToolPrompt(ctx); prompt != "" {
				parts = append(parts, prompt)
				continue
			}
		}
		// Fallback to Tool.Prompt() if it returns non-empty.
		if prompt := t.Prompt(nil); prompt != "" {
			parts = append(parts, prompt)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}

// InjectToolPrompts appends tool prompts to the system prompt parts.
// Returns updated parts with a tool_context part added if any tool prompts exist.
func InjectToolPrompts(
	baseParts []SystemPromptPart,
	tools []Tool,
	ctx ToolPromptContext,
) []SystemPromptPart {
	combined := BuildToolPrompts(tools, ctx)
	if combined == "" {
		return baseParts
	}
	return append(baseParts, SystemPromptPart{
		Content: combined,
	})
}
