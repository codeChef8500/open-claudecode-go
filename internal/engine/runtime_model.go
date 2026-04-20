package engine

import (
	"os"
	"strings"
)

// ────────────────────────────────────────────────────────────────────────────
// [P7.T7] Runtime model selection helpers.
// TS anchor: query.ts:L570 (getRuntimeMainLoopModel)
// ────────────────────────────────────────────────────────────────────────────

// GetRuntimeMainLoopModel returns the model to use for the main loop.
// In TS this resolves plan-200k → opus and checks for overrides.
// For now: if CLAUDE_CODE_MODEL_OVERRIDE is set, use that; otherwise
// return the configured model.
func GetRuntimeMainLoopModel(configuredModel string) string {
	if override := os.Getenv("CLAUDE_CODE_MODEL_OVERRIDE"); override != "" {
		return override
	}
	return configuredModel
}

// ────────────────────────────────────────────────────────────────────────────
// [P7.T7] Memory mechanics prompt.
// TS anchor: QueryEngine.ts:L316 (memoryMechanicsPrompt)
// ────────────────────────────────────────────────────────────────────────────

// GetMemoryMechanicsPrompt returns the memory mechanics injection text.
// In the TS codebase, this is gate-controlled by CLAUDE_COWORK_MEMORY_PATH_OVERRIDE.
// Returns empty string if not enabled.
func GetMemoryMechanicsPrompt() string {
	memoryPath := os.Getenv("CLAUDE_COWORK_MEMORY_PATH_OVERRIDE")
	if memoryPath == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Memory Mechanics\n")
	sb.WriteString("You have access to a persistent memory store at: ")
	sb.WriteString(memoryPath)
	sb.WriteString("\n")
	sb.WriteString("Use this to store and retrieve information across sessions.\n")
	return sb.String()
}
