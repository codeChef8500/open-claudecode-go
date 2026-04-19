package engine

// NOTE: engine cannot import prompt (cycle). System prompt assembly is done
// via the SystemPromptBuilder interface injected at construction time.

// ────────────────────────────────────────────────────────────────────────────
// [P6.T3] fetchSystemPromptParts — mirrors claude-code-main
// utils/queryContext.ts fetchSystemPromptParts.
// ────────────────────────────────────────────────────────────────────────────

// SystemPromptParts holds the three context pieces that form the API
// cache-key prefix: default system prompt sections, user context, and
// system context.
type SystemPromptParts struct {
	// DefaultSystemPrompt is the list of prompt sections assembled by
	// getSystemPrompt (empty if customSystemPrompt is set).
	DefaultSystemPrompt []string
	// UserContext carries user-facing key-value pairs injected as a
	// system turn (memory content, slash commands, etc.).
	UserContext map[string]string
	// SystemContext carries system-level key-value pairs (env info, etc.).
	SystemContext map[string]string
}

// FetchSystemPromptPartsOpts contains the inputs for FetchSystemPromptParts.
type FetchSystemPromptPartsOpts struct {
	Tools                        []Tool
	MainLoopModel                string
	AdditionalWorkingDirectories []string
	MCPClients                   []MCPClientConnection
	CustomSystemPrompt           string // empty means use default
}

// SystemPromptFetcher builds SystemPromptParts from FetchSystemPromptPartsOpts.
// Implementations live in the prompt/adapter layer to avoid import cycles.
type SystemPromptFetcher interface {
	FetchParts(opts FetchSystemPromptPartsOpts) *SystemPromptParts
}

// joinDirs joins directory paths with newlines.
func joinDirs(dirs []string) string {
	result := ""
	for i, d := range dirs {
		if i > 0 {
			result += "\n"
		}
		result += d
	}
	return result
}

// AssembleSystemPrompt combines SystemPromptParts into a final system prompt
// string, equivalent to the TS asSystemPrompt([...defaultSystemPrompt, ...extras]).
func AssembleSystemPrompt(parts *SystemPromptParts, custom, append_ string) string {
	var sections []string
	if custom != "" {
		sections = []string{custom}
	} else {
		sections = parts.DefaultSystemPrompt
	}
	if append_ != "" {
		sections = append(sections, append_)
	}

	result := ""
	for i, s := range sections {
		if i > 0 {
			result += "\n\n"
		}
		result += s
	}
	return result
}

// BuildCacheSafeParams creates parameters for cache-safe forked agent queries.
// Aligned with claude-code-main utils/queryContext.ts buildSideQuestionFallbackParams.
type CacheSafeParams struct {
	SystemPrompt        string
	UserContext         map[string]string
	SystemContext       map[string]string
	ToolUseContext      *ToolUseContext
	ForkContextMessages []*Message
}
