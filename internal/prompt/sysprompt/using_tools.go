package sysprompt

import "strings"

// [P2.T4] TS anchor: constants/prompts.ts:L269-314

// ToolNames holds the tool name constants injected by the caller, avoiding
// hard-coded dependency on the tools package.
type ToolNames struct {
	BashTool      string
	FileReadTool  string
	FileEditTool  string
	FileWriteTool string
	GlobTool      string
	GrepTool      string
	AgentTool     string
	TaskTool      string // empty if no task tool
}

// GetUsingYourToolsSection returns the "# Using your tools" section.
//
// embeddedSearch → true when ant-native builds alias find/grep to embedded
// bfs/ugrep and the dedicated Glob/Grep tools are removed.
// replMode → true when REPL mode is enabled (simplified guidance).
func GetUsingYourToolsSection(tn ToolNames, embeddedSearch, replMode bool) string {
	if replMode {
		var items []interface{}
		if tn.TaskTool != "" {
			items = append(items,
				`Break down and manage your work with the `+tn.TaskTool+` tool. These tools are helpful for planning your work and helping the user track your progress. Mark each task as completed as soon as you are done with the task. Do not batch up multiple tasks before marking them as completed.`,
			)
		}
		if len(items) == 0 {
			return ""
		}
		lines := append([]string{"# Using your tools"}, PrependBullets(items...)...)
		return strings.Join(lines, "\n")
	}

	providedSubitems := []string{
		`To read files use ` + tn.FileReadTool + ` instead of cat, head, tail, or sed`,
		`To edit files use ` + tn.FileEditTool + ` instead of sed or awk`,
		`To create files use ` + tn.FileWriteTool + ` instead of cat with heredoc or echo redirection`,
	}
	if !embeddedSearch {
		providedSubitems = append(providedSubitems,
			`To search for files use `+tn.GlobTool+` instead of find or ls`,
			`To search the content of files, use `+tn.GrepTool+` instead of grep or rg`,
		)
	}
	providedSubitems = append(providedSubitems,
		`Reserve using the `+tn.BashTool+` exclusively for system commands and terminal operations that require shell execution. If you are unsure and there is a relevant dedicated tool, default to using the dedicated tool and only fallback on using the `+tn.BashTool+` tool for these if it is absolutely necessary.`,
	)

	var items []interface{}
	items = append(items,
		`Do NOT use the `+tn.BashTool+` to run commands when a relevant dedicated tool is provided. Using dedicated tools allows the user to better understand and review your work. This is CRITICAL to assisting the user:`,
		providedSubitems,
	)
	if tn.TaskTool != "" {
		items = append(items,
			`Break down and manage your work with the `+tn.TaskTool+` tool. These tools are helpful for planning your work and helping the user track your progress. Mark each task as completed as soon as you are done with the task. Do not batch up multiple tasks before marking them as completed.`,
		)
	}
	items = append(items,
		`You can call multiple tools in a single response. If you intend to call multiple tools and there are no dependencies between them, make all independent tool calls in parallel. Maximize use of parallel tool calls where possible to increase efficiency. However, if some tool calls depend on previous calls to inform dependent values, do NOT call these tools in parallel and instead call them sequentially. For instance, if one operation must complete before another starts, run these operations sequentially instead.`,
	)

	lines := append([]string{"# Using your tools"}, PrependBullets(items...)...)
	return strings.Join(lines, "\n")
}
