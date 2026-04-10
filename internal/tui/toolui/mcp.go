package toolui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// MCPToolUI renders MCP (Model Context Protocol) tool use.
// Layout matches claude-code-main's MCPTool UI:
//
//	● mcp__server__tool (key: val, key: val)
//	  ⎿  key: value
//	     key: value
type MCPToolUI struct {
	theme ToolUITheme
}

// NewMCPToolUI creates an MCP tool renderer.
func NewMCPToolUI(theme ToolUITheme) *MCPToolUI {
	return &MCPToolUI{theme: theme}
}

// MCP display constants matching claude-code-main.
const (
	mcpMaxInputValueChars     = 80
	mcpMaxFlatJSONKeys        = 12
	mcpMaxFlatJSONChars       = 5000
	mcpOutputWarningThreshold = 10000 // estimated tokens
)

// RenderStart renders an MCP tool header line:
//
//	● mcp__server__tool (key: val, key: val)
func (m *MCPToolUI) RenderStart(dotView, serverName, toolName string, input map[string]interface{}) string {
	displayName := toolName
	if serverName != "" {
		displayName = serverName + "/" + toolName
	}
	params := m.formatInputParams(input)
	return RenderToolHeader(dotView, displayName, params, m.theme)
}

// formatInputParams formats MCP tool input for header display.
func (m *MCPToolUI) formatInputParams(input map[string]interface{}) string {
	if len(input) == 0 {
		return ""
	}
	var parts []string
	for k, v := range input {
		s := fmt.Sprintf("%v", v)
		if len(s) > mcpMaxInputValueChars {
			s = s[:mcpMaxInputValueChars] + "…"
		}
		parts = append(parts, k+": "+s)
	}
	// Sort for stable display.
	sort.Strings(parts)
	result := strings.Join(parts, ", ")
	if len(result) > 200 {
		result = result[:200] + "…"
	}
	return result
}

// RenderProgress renders an MCP tool progress line.
//
//	⎿  Running…
//	⎿  Processing… 45%
func (m *MCPToolUI) RenderProgress(progress, total int, message string) string {
	if message != "" && total > 0 && progress > 0 {
		pct := (progress * 100) / total
		bar := renderProgressBar(pct, 20)
		return RenderResponseLine(m.theme.Dim.Render(fmt.Sprintf("%s %s %d%%", message, bar, pct)), m.theme)
	}
	if message != "" {
		return RenderResponseLine(m.theme.Dim.Render(message), m.theme)
	}
	if total > 0 && progress > 0 {
		pct := (progress * 100) / total
		bar := renderProgressBar(pct, 20)
		return RenderResponseLine(m.theme.Dim.Render(fmt.Sprintf("Processing… %s %d%%", bar, pct)), m.theme)
	}
	return RenderResponseLine(m.theme.Dim.Render("Running…"), m.theme)
}

// renderProgressBar renders a simple text progress bar.
func renderProgressBar(pct, width int) string {
	filled := (pct * width) / 100
	if filled > width {
		filled = width
	}
	empty := width - filled
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", empty) + "]"
}

// RenderResult renders the MCP tool result with smart formatting:
//  1. Try flat key-value display for small JSON objects
//  2. Fall back to truncated text output
//
// Also shows a warning for very large outputs.
func (m *MCPToolUI) RenderResult(content string, elapsed time.Duration, width int, verbose bool) string {
	var sb strings.Builder

	// Estimate token count (rough: 1 token ≈ 4 chars).
	estimatedTokens := len(content) / 4
	if estimatedTokens > mcpOutputWarningThreshold {
		warning := fmt.Sprintf("⚠ Large MCP response (~%dk tokens)", estimatedTokens/1000)
		sb.WriteString(RenderResponseLine(m.theme.Error.Render(warning), m.theme))
		sb.WriteString("\n")
	}

	// Try flat JSON key-value display.
	if flat := m.tryFlattenJSON(content); flat != nil {
		// Show status line.
		status := m.theme.Dim.Render(fmt.Sprintf("Done (%s)", formatDuration(elapsed)))
		sb.WriteString(RenderResponseLine(status, m.theme))
		// Render key-value pairs.
		maxKeyWidth := 0
		for _, kv := range flat {
			if len(kv[0]) > maxKeyWidth {
				maxKeyWidth = len(kv[0])
			}
		}
		for _, kv := range flat {
			key := kv[0]
			val := kv[1]
			if !verbose && len(val) > 120 {
				val = val[:120] + "…"
			}
			padded := key + strings.Repeat(" ", maxKeyWidth-len(key))
			sb.WriteString("\n")
			sb.WriteString(m.theme.TreeConn.Render("  │ "))
			sb.WriteString(m.theme.Dim.Render(padded+": "))
			sb.WriteString(m.theme.Output.Render(val))
		}
		return sb.String()
	}

	// Fallback: show status + truncated text.
	status := m.theme.Dim.Render(fmt.Sprintf("Done (%s)", formatDuration(elapsed)))
	sb.WriteString(RenderResponseLine(status, m.theme))

	if content != "" {
		lines := strings.Split(content, "\n")
		maxShow := 8
		if verbose {
			maxShow = 30
		}
		show := lines
		if len(show) > maxShow {
			show = show[:maxShow]
		}
		for _, line := range show {
			sb.WriteString("\n")
			sb.WriteString(m.theme.TreeConn.Render("  │ "))
			sb.WriteString(m.theme.Output.Render(truncateLine(line, width-6)))
		}
		if len(lines) > maxShow {
			sb.WriteString("\n")
			sb.WriteString(m.theme.Dim.Render(fmt.Sprintf("  │ … (%d more lines)", len(lines)-maxShow)))
		}
	}

	return sb.String()
}

// tryFlattenJSON attempts to parse content as a JSON object with scalar/small values.
// Returns [key, displayValue] pairs or nil if content doesn't qualify.
func (m *MCPToolUI) tryFlattenJSON(content string) [][2]string {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) == 0 || len(trimmed) > mcpMaxFlatJSONChars || trimmed[0] != '{' {
		return nil
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return nil
	}
	if len(parsed) == 0 || len(parsed) > mcpMaxFlatJSONKeys {
		return nil
	}
	// Sort keys for stable output.
	keys := make([]string, 0, len(parsed))
	for k := range parsed {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var result [][2]string
	for _, k := range keys {
		v := parsed[k]
		switch val := v.(type) {
		case string:
			// Multi-line strings get collapsed.
			display := strings.ReplaceAll(val, "\n", "\\n")
			result = append(result, [2]string{k, display})
		case float64:
			if val == float64(int64(val)) {
				result = append(result, [2]string{k, fmt.Sprintf("%d", int64(val))})
			} else {
				result = append(result, [2]string{k, fmt.Sprintf("%g", val)})
			}
		case bool:
			result = append(result, [2]string{k, fmt.Sprintf("%v", val)})
		case nil:
			result = append(result, [2]string{k, "null"})
		default:
			// Nested object/array — try compact JSON.
			b, err := json.Marshal(val)
			if err != nil || len(b) > 120 {
				return nil // too complex for flat display
			}
			result = append(result, [2]string{k, string(b)})
		}
	}
	return result
}

// GetToolUseSummary returns a truncated summary of the MCP tool name + input.
func (m *MCPToolUI) GetToolUseSummary(serverName, toolName string) string {
	display := toolName
	if serverName != "" {
		display = serverName + "/" + toolName
	}
	if len(display) > 80 {
		return display[:80] + "…"
	}
	return display
}
