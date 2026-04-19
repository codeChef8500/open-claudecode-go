package sysprompt

import "strings"

// [P3.T2] TS anchor: constants/prompts.ts:L579-604

// MCPClientInfo describes a connected MCP server that has instructions.
type MCPClientInfo struct {
	Name         string
	Instructions string
}

// GetMCPInstructionsSection returns the "# MCP Server Instructions" section,
// or "" when there are no clients with instructions.
func GetMCPInstructionsSection(clients []MCPClientInfo) string {
	if len(clients) == 0 {
		return ""
	}

	var withInstructions []MCPClientInfo
	for _, c := range clients {
		if c.Instructions != "" {
			withInstructions = append(withInstructions, c)
		}
	}
	if len(withInstructions) == 0 {
		return ""
	}

	var blocks []string
	for _, c := range withInstructions {
		blocks = append(blocks, "## "+c.Name+"\n"+c.Instructions)
	}

	return `# MCP Server Instructions

The following MCP servers have provided instructions for how to use their tools and resources:

` + strings.Join(blocks, "\n\n")
}
