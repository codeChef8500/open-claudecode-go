package sysprompt

import (
	"github.com/wall-ai/agent-engine/internal/prompt/constants"
	"github.com/wall-ai/agent-engine/internal/prompt/feature"
	"github.com/wall-ai/agent-engine/internal/prompt/sections"
)

// [P3.T6] TS anchor: constants/prompts.ts:L444-577

// SystemPromptOpts collects all the runtime inputs for getSystemPrompt.
type SystemPromptOpts struct {
	// Core
	CWD   string
	Model string
	IsAnt bool

	// Tools
	EnabledToolNames       map[string]bool
	ToolNames              ToolNames
	EmbeddedSearchTools    bool
	ReplMode               bool
	ForkSubagentEnabled    bool
	ExplorePlanEnabled     bool
	ExploreAgentType       string
	ExploreAgentMinQuery   int
	SkillToolCommands      []string // non-empty when skills are available
	DiscoverSkillsToolName string
	DiscoverSkillsEnabled  bool

	// Verification agent
	VerificationAgent     bool
	VerificationAgentType string

	// Session
	IsNonInteractive     bool
	IsSimple             bool // CLAUDE_CODE_SIMPLE mode
	OutputStyleName      string
	OutputStylePrompt    string
	KeepCodingInstructions bool
	LanguagePreference   string

	// Memory
	LoadMemoryPrompt func() string

	// MCP
	MCPClients            []MCPClientInfo
	MCPInstructionsDelta  bool

	// Environment
	ComputeSimpleEnvInfo func() string

	// Scratchpad
	ScratchpadEnabled bool
	ScratchpadDir     string

	// FRC (Function Result Clearing)
	FRCEnabled      bool
	FRCModelSupport bool
	FRCKeepRecent   int

	// Proactive / Kairos
	ProactiveActive bool
	ProactiveOpts   ProactiveOpts

	// Global cache scope
	UseGlobalCacheScope bool

	// Numeric length anchors (ant-only)
	NumericLengthAnchors string

	// Token budget section text (when feature enabled)
	TokenBudgetSection string

	// Brief section text
	BriefSection string
}

// GetSystemPrompt assembles the full system prompt as a []string slice,
// mirroring the TS getSystemPrompt() function.
func GetSystemPrompt(o SystemPromptOpts) []string {
	// Simple mode short-circuit
	if o.IsSimple {
		return []string{
			"You are Claude Code, Anthropic's official CLI for Claude.\n\nCWD: " +
				o.CWD + "\nDate: " + constants.GetSessionStartDate(),
		}
	}

	// Proactive path (PROACTIVE || KAIROS)
	if o.ProactiveActive &&
		(feature.IsEnabled(feature.FlagProactive) || feature.IsEnabled(feature.FlagKairos)) {

		memPrompt := ""
		if o.LoadMemoryPrompt != nil {
			memPrompt = o.LoadMemoryPrompt()
		}
		envInfo := ""
		if o.ComputeSimpleEnvInfo != nil {
			envInfo = o.ComputeSimpleEnvInfo()
		}
		mcpSection := ""
		if !o.MCPInstructionsDelta {
			mcpSection = GetMCPInstructionsSection(o.MCPClients)
		}

		result := filterEmpty(
			"\nYou are an autonomous agent. Use the available tools to do useful work.\n\n"+constants.CyberRiskInstruction,
			GetSystemRemindersSection(),
			memPrompt,
			envInfo,
			GetLanguageSection(o.LanguagePreference),
			mcpSection,
			GetScratchpadInstructions(o.ScratchpadEnabled, o.ScratchpadDir),
			GetFunctionResultClearingSection(o.FRCEnabled, o.FRCModelSupport, o.FRCKeepRecent),
			SummarizeToolResultsSection,
			GetProactiveSection(true, o.ProactiveOpts),
		)
		return result
	}

	// ── Build dynamic sections via registry ────────────────────────────

	hasSkills := len(o.SkillToolCommands) > 0 && o.EnabledToolNames[o.ToolNames.AgentTool]
	askToolName := "AskUserQuestion"
	if tn, ok := o.EnabledToolNames["AskUserQuestion"]; ok && tn {
		askToolName = "AskUserQuestion"
	}

	guidanceOpts := SessionGuidanceOpts{
		HasAskUserQuestionTool: o.EnabledToolNames[askToolName],
		AskUserQuestionName:    askToolName,
		HasAgentTool:           o.EnabledToolNames[o.ToolNames.AgentTool],
		AgentToolName:          o.ToolNames.AgentTool,
		HasSkills:              hasSkills,
		SkillToolName:          "Skill",
		EmbeddedSearchTools:    o.EmbeddedSearchTools,
		IsNonInteractive:       o.IsNonInteractive,
		ForkSubagentEnabled:    o.ForkSubagentEnabled,
		ExplorePlanEnabled:     o.ExplorePlanEnabled,
		VerificationAgent:      o.VerificationAgent,
		VerificationAgentType:  o.VerificationAgentType,
		GlobToolName:           o.ToolNames.GlobTool,
		GrepToolName:           o.ToolNames.GrepTool,
		BashToolName:           o.ToolNames.BashTool,
		ExploreAgentType:       o.ExploreAgentType,
		ExploreAgentMinQuery:   o.ExploreAgentMinQuery,
		DiscoverSkillsToolName: o.DiscoverSkillsToolName,
		DiscoverSkillsEnabled:  o.DiscoverSkillsEnabled,
	}

	dynamicSecs := []sections.Section{
		sections.SystemPromptSection("session_guidance", func() string {
			return GetSessionSpecificGuidanceSection(guidanceOpts)
		}),
		sections.SystemPromptSection("memory", func() string {
			if o.LoadMemoryPrompt != nil {
				return o.LoadMemoryPrompt()
			}
			return ""
		}),
		sections.SystemPromptSection("env_info_simple", func() string {
			if o.ComputeSimpleEnvInfo != nil {
				return o.ComputeSimpleEnvInfo()
			}
			return ""
		}),
		sections.SystemPromptSection("language", func() string {
			return GetLanguageSection(o.LanguagePreference)
		}),
		sections.SystemPromptSection("output_style", func() string {
			return GetOutputStyleSection(o.OutputStyleName, o.OutputStylePrompt)
		}),
		sections.DANGEROUSUncachedSystemPromptSection("mcp_instructions", func() string {
			if o.MCPInstructionsDelta {
				return ""
			}
			return GetMCPInstructionsSection(o.MCPClients)
		}, "MCP servers connect/disconnect between turns"),
		sections.SystemPromptSection("scratchpad", func() string {
			return GetScratchpadInstructions(o.ScratchpadEnabled, o.ScratchpadDir)
		}),
		sections.SystemPromptSection("frc", func() string {
			return GetFunctionResultClearingSection(o.FRCEnabled, o.FRCModelSupport, o.FRCKeepRecent)
		}),
		sections.SystemPromptSection("summarize_tool_results", func() string {
			return SummarizeToolResultsSection
		}),
	}

	// Ant-only numeric length anchors
	if o.IsAnt && o.NumericLengthAnchors != "" {
		dynamicSecs = append(dynamicSecs,
			sections.SystemPromptSection("numeric_length_anchors", func() string {
				return o.NumericLengthAnchors
			}),
		)
	}

	// Token budget (feature-gated)
	if feature.IsEnabled(feature.FlagTokenBudget) && o.TokenBudgetSection != "" {
		dynamicSecs = append(dynamicSecs,
			sections.SystemPromptSection("token_budget", func() string {
				return o.TokenBudgetSection
			}),
		)
	}

	// Brief (kairos/kairos_brief)
	if (feature.IsEnabled(feature.FlagKairos) || feature.IsEnabled(feature.FlagKairosBrief)) &&
		o.BriefSection != "" {
		dynamicSecs = append(dynamicSecs,
			sections.SystemPromptSection("brief", func() string {
				return o.BriefSection
			}),
		)
	}

	resolved := sections.ResolveSections(dynamicSecs)

	// ── Assemble final prompt ──────────────────────────────────────────

	var out []string

	// Static content (cacheable)
	out = appendNonEmpty(out, GetSimpleIntroSection(o.OutputStyleName))
	out = appendNonEmpty(out, GetSimpleSystemSection())

	if o.OutputStyleName == "" || o.KeepCodingInstructions {
		out = appendNonEmpty(out, GetSimpleDoingTasksSection(o.IsAnt, o.ToolNames.AgentTool, ""))
	}

	out = appendNonEmpty(out, GetActionsSection())
	out = appendNonEmpty(out, GetUsingYourToolsSection(o.ToolNames, o.EmbeddedSearchTools, o.ReplMode))
	out = appendNonEmpty(out, GetSimpleToneAndStyleSection(o.IsAnt))
	out = appendNonEmpty(out, GetOutputEfficiencySection(o.IsAnt))

	// Boundary marker
	if o.UseGlobalCacheScope {
		out = append(out, constants.SystemPromptDynamicBoundary)
	}

	// Dynamic content (registry-managed)
	for _, s := range resolved {
		out = appendNonEmpty(out, s)
	}

	return out
}

// DefaultAgentPrompt is the system prompt for subagents.
// TS anchor: constants/prompts.ts:L758
const DefaultAgentPrompt = `You are an agent for Claude Code, Anthropic's official CLI for Claude. Given the user's message, you should use the tools available to complete the task. Complete the task fully—don't gold-plate, but don't leave it half-done. When you complete the task, respond with a concise report covering what was done and any key findings — the caller will relay this to the user, so it only needs the essentials.`

// filterEmpty removes empty strings from a slice.
func filterEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// appendNonEmpty appends s to out if s is non-empty.
func appendNonEmpty(out []string, s string) []string {
	if s != "" {
		return append(out, s)
	}
	return out
}
