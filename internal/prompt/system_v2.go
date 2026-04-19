package prompt

import (
	"runtime"
	"strings"

	"github.com/wall-ai/agent-engine/internal/prompt/sysprompt"
)

// [P5.T1] BuildEffectiveSystemPromptV2 uses the new sysprompt.GetSystemPrompt
// chain internally while preserving the same BuiltSystemPrompt output type.
//
// Callers opt-in by setting BuildOptions.UseNewPromptBuilder = true. When
// false, the original BuildEffectiveSystemPrompt is used unchanged.
func BuildEffectiveSystemPromptV2(opts BuildOptions) *BuiltSystemPrompt {
	if !opts.UseNewPromptBuilder {
		return BuildEffectiveSystemPrompt(opts)
	}

	// Map BuildOptions → SystemPromptOpts
	isGit := false
	if opts.WorkDir != "" {
		git := DetectGitContext(opts.WorkDir)
		isGit = git.IsRepo
	}

	platform := runtime.GOOS
	shell := "unknown"
	if s := getShell(); s != "" {
		shell = s
	}

	osVersion := getOSVersion()

	modelID := opts.Model
	cutoff := sysprompt.GetKnowledgeCutoff(modelID)

	memoryFn := func() string {
		if opts.AutoMemoryPrompt != "" {
			return opts.AutoMemoryPrompt
		}
		return opts.MemoryContent
	}

	envInfoFn := func() string {
		return sysprompt.ComputeSimpleEnvInfo(sysprompt.EnvInfoOpts{
			CWD:             opts.WorkDir,
			IsGit:           isGit,
			Platform:        platform,
			Shell:           shell,
			OSVersion:       osVersion,
			ModelID:         modelID,
			KnowledgeCutoff: cutoff,
		})
	}

	// Build tool names mapping from current tool set
	tn := sysprompt.ToolNames{
		BashTool:      "Bash",
		FileReadTool:  "Read",
		FileEditTool:  "Edit",
		FileWriteTool: "Write",
		GlobTool:      "Glob",
		GrepTool:      "Grep",
		AgentTool:     "Agent",
		TaskTool:      "Task",
	}

	enabledTools := map[string]bool{}
	for _, t := range opts.Tools {
		enabledTools[t.Name()] = true
	}

	spOpts := sysprompt.SystemPromptOpts{
		CWD:              opts.WorkDir,
		Model:            modelID,
		EnabledToolNames: enabledTools,
		ToolNames:        tn,

		// Session
		LanguagePreference:     opts.LanguagePreference,
		OutputStyleName:        opts.OutputStyleName,
		OutputStylePrompt:      opts.OutputStylePrompt,
		KeepCodingInstructions: opts.KeepCodingInstructions,

		// Memory
		LoadMemoryPrompt: memoryFn,

		// Environment
		ComputeSimpleEnvInfo: envInfoFn,

		// MCP
		MCPClients:           opts.MCPClients,
		MCPInstructionsDelta: opts.MCPInstructionsDelta,

		// Scratchpad
		ScratchpadEnabled: opts.ScratchpadEnabled,
		ScratchpadDir:     opts.ScratchpadDir,

		// FRC
		FRCEnabled:      opts.FRCEnabled,
		FRCModelSupport: opts.FRCModelSupport,
		FRCKeepRecent:   opts.FRCKeepRecent,

		// Proactive / Kairos
		ProactiveActive: opts.KairosActive && opts.ProactiveActive,
		ProactiveOpts:   opts.ProactiveOpts,

		// Cache
		UseGlobalCacheScope: opts.UseGlobalCacheScope,
	}

	sections := sysprompt.GetSystemPrompt(spOpts)

	// Build combined text
	combined := strings.Join(sections, "\n\n")

	// Custom system prompt (layer 5)
	if opts.CustomSystemPrompt != "" {
		combined += "\n\n" + opts.CustomSystemPrompt
	}

	// Append system prompt (layer 6)
	if opts.AppendSystemPrompt != "" {
		combined += "\n\n" + opts.AppendSystemPrompt
	}

	// For cache-aware API calls, split into parts
	var parts []PromptPart
	for i, s := range sections {
		if s == "" {
			continue
		}
		parts = append(parts, PromptPart{
			Content:   s,
			Order:     CacheOrder(i),
			CacheHint: i == 0 && !opts.SkipCacheWrite,
		})
	}

	return &BuiltSystemPrompt{
		Text:  combined,
		Parts: parts,
	}
}
