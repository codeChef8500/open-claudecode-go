package engine

import "sync"

// ────────────────────────────────────────────────────────────────────────────
// [P7.T2] Permission denial tracking — wrappedCanUseTool pattern.
// TS anchor: QueryEngine.ts:L244-271
// ────────────────────────────────────────────────────────────────────────────

// PermDenialTracker wraps a CanUseToolFn and records every denied invocation
// so the final SDKResultMessage can include permission_denials.
type PermDenialTracker struct {
	mu      sync.Mutex
	denials []SDKPermDenial
}

// NewPermDenialTracker creates a new tracker.
func NewPermDenialTracker() *PermDenialTracker {
	return &PermDenialTracker{
		denials: make([]SDKPermDenial, 0),
	}
}

// WrapCanUseTool wraps an existing CanUseToolFn with denial tracking.
// Returns a new CanUseToolFn that records denials before returning.
// Mirrors TS wrappedCanUseTool in QueryEngine.ts:L244-271.
func (t *PermDenialTracker) WrapCanUseTool(inner CanUseToolFn) CanUseToolFn {
	return func(
		tool Tool,
		input map[string]interface{},
		tuc *ToolUseContext,
		assistantMessage *Message,
		toolUseID string,
		forceDecision *PermissionDecision,
	) (*PermissionResult, error) {
		result, err := inner(tool, input, tuc, assistantMessage, toolUseID, forceDecision)
		if err != nil {
			return result, err
		}

		// Track denials for SDK reporting (TS: result.behavior !== 'allow')
		if result != nil && result.Behavior != "allow" {
			t.RecordDenial(tool.Name(), toolUseID, input)
		}

		return result, nil
	}
}

// RecordDenial manually records a permission denial.
func (t *PermDenialTracker) RecordDenial(toolName, toolUseID string, input map[string]interface{}) {
	t.mu.Lock()
	t.denials = append(t.denials, SDKPermDenial{
		ToolName:  SdkCompatToolName(toolName),
		ToolUseID: toolUseID,
		ToolInput: input,
	})
	t.mu.Unlock()
}

// Denials returns a snapshot of all recorded permission denials.
func (t *PermDenialTracker) Denials() []SDKPermDenial {
	t.mu.Lock()
	defer t.mu.Unlock()
	cp := make([]SDKPermDenial, len(t.denials))
	copy(cp, t.denials)
	return cp
}

// Reset clears all recorded denials (e.g. between turns).
func (t *PermDenialTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.denials = t.denials[:0]
}

// SdkCompatToolName maps internal tool names to their SDK-facing equivalents.
// TS anchor: utils/messages/systemInit.ts:sdkCompatToolName
func SdkCompatToolName(name string) string {
	if mapped, ok := sdkToolNameMap[name]; ok {
		return mapped
	}
	return name
}

// sdkToolNameMap maps internal tool names to SDK-compatible names.
// Mirrors the TS sdkCompatToolName function which maps tool names that differ
// between internal implementation and the SDK-facing interface.
var sdkToolNameMap = map[string]string{
	"BashTool":      "Bash",
	"ComputerTool":  "Computer",
	"EditTool":      "Edit",
	"GlobTool":      "Glob",
	"GrepTool":      "Grep",
	"ReadTool":      "Read",
	"WriteTool":     "Write",
	"MultiEditTool": "MultiEdit",
	"NotebookEdit":  "NotebookEdit",
	"AgentTool":     "Task",
	"WebFetchTool":  "WebFetch",
	"WebSearchTool": "WebSearch",
}
