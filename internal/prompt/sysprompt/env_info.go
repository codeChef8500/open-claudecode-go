package sysprompt

import (
	"runtime"
	"strings"

	"github.com/wall-ai/agent-engine/internal/prompt/constants"
)

// [P4.T1] TS anchor: constants/prompts.ts:L606-710

// EnvInfoOpts holds the runtime data for the environment section.
type EnvInfoOpts struct {
	CWD                        string
	IsGit                      bool // or "Yes"/"No" string
	IsWorktree                 bool
	Platform                   string // "darwin", "linux", "win32"
	Shell                      string // e.g. "zsh", "bash", "powershell"
	OSVersion                  string // uname -sr output
	AdditionalWorkingDirs      []string
	ModelID                    string
	ModelMarketingName         string // e.g. "Claude Sonnet 4.6"
	KnowledgeCutoff            string // e.g. "August 2025"
	IsAnt                      bool
	IsUndercover               bool
}

// ComputeSimpleEnvInfo builds the "# Environment" section.
// TS anchor: constants/prompts.ts:L651-710 (computeSimpleEnvInfo)
func ComputeSimpleEnvInfo(o EnvInfoOpts) string {
	isGitStr := "No"
	if o.IsGit {
		isGitStr = "Yes"
	}

	platform := o.Platform
	if platform == "" {
		platform = runtime.GOOS
	}
	shell := o.Shell
	if shell == "" {
		shell = "unknown"
	}

	var modelDesc string
	if !(o.IsAnt && o.IsUndercover) {
		if o.ModelMarketingName != "" {
			modelDesc = "You are powered by the model named " + o.ModelMarketingName + ". The exact model ID is " + o.ModelID + "."
		} else if o.ModelID != "" {
			modelDesc = "You are powered by the model " + o.ModelID + "."
		}
	}

	var cutoffMsg string
	if o.KnowledgeCutoff != "" {
		cutoffMsg = "Assistant knowledge cutoff is " + o.KnowledgeCutoff + "."
	}

	var envItems []interface{}
	envItems = append(envItems, "Primary working directory: "+o.CWD)

	if o.IsWorktree {
		envItems = append(envItems,
			"This is a git worktree — an isolated copy of the repository. Run all commands from this directory. Do NOT `cd` to the original repository root.",
		)
	}

	envItems = append(envItems, []string{"Is a git repository: " + isGitStr})

	if len(o.AdditionalWorkingDirs) > 0 {
		envItems = append(envItems, "Additional working directories:")
		envItems = append(envItems, o.AdditionalWorkingDirs)
	}

	envItems = append(envItems, "Platform: "+platform)

	shellLine := "Shell: " + shell
	if platform == "win32" || platform == "windows" {
		shellLine = "Shell: " + shell + " (use Unix shell syntax, not Windows — e.g., /dev/null not NUL, forward slashes in paths)"
	}
	envItems = append(envItems, shellLine)

	envItems = append(envItems, "OS Version: "+o.OSVersion)

	if modelDesc != "" {
		envItems = append(envItems, modelDesc)
	}
	if cutoffMsg != "" {
		envItems = append(envItems, cutoffMsg)
	}

	if !(o.IsAnt && o.IsUndercover) {
		envItems = append(envItems,
			"The most recent Claude model family is Claude 4.5/4.6. Model IDs — Opus 4.6: '"+
				constants.Claude45Or46ModelIDs["opus"]+"', Sonnet 4.6: '"+
				constants.Claude45Or46ModelIDs["sonnet"]+"', Haiku 4.5: '"+
				constants.Claude45Or46ModelIDs["haiku"]+"'. When building AI applications, default to the latest and most capable Claude models.",
		)
		envItems = append(envItems,
			"Claude Code is available as a CLI in the terminal, desktop app (Mac/Windows), web app (claude.ai/code), and IDE extensions (VS Code, JetBrains).",
		)
		envItems = append(envItems,
			"Fast mode for Claude Code uses the same "+constants.FrontierModelName+" model with faster output. It does NOT switch to a different model. It can be toggled with /fast.",
		)
	}

	lines := []string{
		"# Environment",
		"You have been invoked in the following environment: ",
	}
	lines = append(lines, PrependBullets(envItems...)...)
	return strings.Join(lines, "\n")
}

// GetKnowledgeCutoff returns the knowledge cutoff string for a model ID, or "".
// TS anchor: constants/prompts.ts:L712-730
func GetKnowledgeCutoff(modelID string) string {
	id := strings.ToLower(modelID)
	switch {
	case strings.Contains(id, "claude-sonnet-4-6"):
		return "August 2025"
	case strings.Contains(id, "claude-opus-4-6"):
		return "May 2025"
	case strings.Contains(id, "claude-opus-4-5"):
		return "May 2025"
	case strings.Contains(id, "claude-haiku-4"):
		return "February 2025"
	case strings.Contains(id, "claude-opus-4") || strings.Contains(id, "claude-sonnet-4"):
		return "January 2025"
	default:
		return ""
	}
}
