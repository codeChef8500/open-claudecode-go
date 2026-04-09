package mcp

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// ── MCP Utility Functions ────────────────────────────────────────────────────
// Aligned with claude-code-main src/services/mcp/utils.ts

// FilterToolsByServer returns tools belonging to the named MCP server.
func FilterToolsByServer(tools []MCPTool, serverName string) []MCPTool {
	prefix := GetMcpPrefix(serverName)
	var out []MCPTool
	for _, t := range tools {
		if strings.HasPrefix(t.Name, prefix) {
			out = append(out, t)
		}
	}
	return out
}

// ExcludeToolsByServer returns tools NOT belonging to the named MCP server.
func ExcludeToolsByServer(tools []MCPTool, serverName string) []MCPTool {
	prefix := GetMcpPrefix(serverName)
	var out []MCPTool
	for _, t := range tools {
		if !strings.HasPrefix(t.Name, prefix) {
			out = append(out, t)
		}
	}
	return out
}

// IsToolFromMcpServer checks if a tool name belongs to a specific server.
func IsToolFromMcpServer(toolName, serverName string) bool {
	info := McpInfoFromString(toolName)
	return info != nil && info.ServerName == serverName
}

// IsMcpTool checks if a tool name belongs to any MCP server.
func IsMcpTool(toolName string) bool {
	return strings.HasPrefix(toolName, "mcp__")
}

// ── Config hashing ──────────────────────────────────────────────────────────

// HashMcpConfig produces a stable hash of a ServerConfig for change detection.
// Excludes scope-like metadata; sorts keys for determinism.
func HashMcpConfig(cfg *ServerConfig) string {
	m := map[string]interface{}{
		"transport": cfg.Transport,
		"command":   cfg.Command,
		"args":      cfg.Args,
		"url":       cfg.URL,
		"headers":   sortedMapKeys(cfg.Headers),
		"env":       sortedSlice(cfg.Env),
	}
	data, _ := json.Marshal(m)
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:8])
}

// ── Config scope helpers ─────────────────────────────────────────────────────
// ConfigScope type and base constants are defined in types.go.

// ScopeLabel returns a human-readable label for a config scope.
func ScopeLabel(scope ConfigScope) string {
	switch scope {
	case ScopeUser:
		return "User config (available in all your projects)"
	case ScopeProject:
		return "Project config (shared via .mcp.json)"
	case ScopeLocal:
		return "Local config (private to you in this project)"
	case ScopeDynamic:
		return "Dynamic config (from command line)"
	case ScopeEnterprise:
		return "Enterprise config (managed by your organization)"
	case ScopeClaudeAI:
		return "claude.ai config"
	default:
		return string(scope)
	}
}

// EnsureConfigScope validates and defaults a scope string.
func EnsureConfigScope(scope string) (ConfigScope, error) {
	if scope == "" {
		return ScopeLocal, nil
	}
	switch ConfigScope(scope) {
	case ScopeUser, ScopeProject, ScopeLocal, ScopeDynamic, ScopeEnterprise, ScopeClaudeAI:
		return ConfigScope(scope), nil
	default:
		return "", fmt.Errorf("invalid scope: %s. Must be one of: user, project, local, dynamic, enterprise, claudeai", scope)
	}
}

// EnsureTransport validates and defaults a transport string.
func EnsureTransport(transport string) (string, error) {
	if transport == "" {
		return TransportStdio, nil
	}
	switch transport {
	case TransportStdio, TransportSSE, TransportHTTP:
		return transport, nil
	default:
		return "", fmt.Errorf("invalid transport type: %s. Must be one of: stdio, sse, http", transport)
	}
}

// ── Header parsing ──────────────────────────────────────────────────────────

// ParseHeaders converts a slice of "Key: Value" strings to a map.
func ParseHeaders(headerStrs []string) (map[string]string, error) {
	headers := make(map[string]string, len(headerStrs))
	for _, h := range headerStrs {
		idx := strings.IndexByte(h, ':')
		if idx == -1 {
			return nil, fmt.Errorf("invalid header format: %q. Expected \"Header-Name: value\"", h)
		}
		key := strings.TrimSpace(h[:idx])
		value := strings.TrimSpace(h[idx+1:])
		if key == "" {
			return nil, fmt.Errorf("invalid header: %q. Header name cannot be empty", h)
		}
		headers[key] = value
	}
	return headers, nil
}

// ── URL helpers ─────────────────────────────────────────────────────────────

// LoggingSafeMcpBaseURL strips query parameters (which may contain tokens)
// from an MCP server URL for safe logging. Returns empty string for non-URL configs.
func LoggingSafeMcpBaseURL(cfg *ServerConfig) string {
	if cfg.URL == "" {
		return ""
	}
	parsed, err := url.Parse(cfg.URL)
	if err != nil {
		return ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

// ── Stale client detection ──────────────────────────────────────────────────

// StaleClientResult holds the results of stale client detection.
type StaleClientResult struct {
	// ActiveClients are clients that should be kept.
	ActiveClients []*Client
	// StaleClients are clients that should be disconnected.
	StaleClients []*Client
}

// DetectStaleClients identifies MCP clients that should be disconnected because
// their config changed or they were removed from the active config set.
func DetectStaleClients(clients []*Client, configs map[string]*ServerConfig) *StaleClientResult {
	result := &StaleClientResult{}
	for _, c := range clients {
		freshCfg, exists := configs[c.Name()]
		if !exists {
			// Client no longer in config — stale.
			result.StaleClients = append(result.StaleClients, c)
			continue
		}
		// Config changed — need reconnect.
		if HashMcpConfig(&c.cfg) != HashMcpConfig(freshCfg) {
			result.StaleClients = append(result.StaleClients, c)
			continue
		}
		result.ActiveClients = append(result.ActiveClients, c)
	}
	return result
}

// ── internal helpers ─────────────────────────────────────────────────────────

func sortedMapKeys(m map[string]string) [][2]string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([][2]string, len(keys))
	for i, k := range keys {
		pairs[i] = [2]string{k, m[k]}
	}
	return pairs
}

func sortedSlice(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	out := make([]string, len(s))
	copy(out, s)
	sort.Strings(out)
	return out
}
