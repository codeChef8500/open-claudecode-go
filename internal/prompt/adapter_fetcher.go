package prompt

import (
	"github.com/wall-ai/agent-engine/internal/engine"
	"github.com/wall-ai/agent-engine/internal/prompt/sysprompt"
)

// ────────────────────────────────────────────────────────────────────────────
// [P6.T4] SystemPromptFetcherAdapter — implements engine.SystemPromptFetcher.
// Lives in the prompt package (which may import engine) to bridge the gap
// between the engine's interface and the sysprompt package.
// ────────────────────────────────────────────────────────────────────────────

// SystemPromptFetcherAdapter implements engine.SystemPromptFetcher.
type SystemPromptFetcherAdapter struct{}

// NewSystemPromptFetcherAdapter creates a new adapter.
func NewSystemPromptFetcherAdapter() *SystemPromptFetcherAdapter {
	return &SystemPromptFetcherAdapter{}
}

// FetchParts builds SystemPromptParts from the given opts using the sysprompt
// package. When opts.CustomSystemPrompt is set, returns empty default prompt
// sections (the custom prompt replaces the default entirely).
func (a *SystemPromptFetcherAdapter) FetchParts(opts engine.FetchSystemPromptPartsOpts) *engine.SystemPromptParts {
	parts := &engine.SystemPromptParts{
		UserContext:   make(map[string]string),
		SystemContext: make(map[string]string),
	}

	if opts.CustomSystemPrompt != "" {
		return parts
	}

	// Build enabled tool names set.
	enabledTools := make(map[string]bool, len(opts.Tools))
	for _, t := range opts.Tools {
		enabledTools[t.Name()] = true
	}

	// Build MCP client info.
	mcpClients := make([]sysprompt.MCPClientInfo, 0, len(opts.MCPClients))
	for _, c := range opts.MCPClients {
		mcpClients = append(mcpClients, sysprompt.MCPClientInfo{
			Name:         c.Name,
			Instructions: c.Instructions,
		})
	}

	spOpts := sysprompt.SystemPromptOpts{
		CWD:              "",
		Model:            opts.MainLoopModel,
		EnabledToolNames: enabledTools,
		ToolNames: sysprompt.ToolNames{
			BashTool:      "Bash",
			FileReadTool:  "Read",
			FileEditTool:  "Edit",
			FileWriteTool: "Write",
			GlobTool:      "Glob",
			GrepTool:      "Grep",
			AgentTool:     "Agent",
			TaskTool:      "Task",
		},
		MCPClients: mcpClients,
	}

	parts.DefaultSystemPrompt = sysprompt.GetSystemPrompt(spOpts)

	// Populate system context with additional directories info.
	if len(opts.AdditionalWorkingDirectories) > 0 {
		combined := ""
		for i, d := range opts.AdditionalWorkingDirectories {
			if i > 0 {
				combined += "\n"
			}
			combined += d
		}
		parts.SystemContext["additional_dirs"] = combined
	}

	return parts
}

// Compile-time interface check.
var _ engine.SystemPromptFetcher = (*SystemPromptFetcherAdapter)(nil)
