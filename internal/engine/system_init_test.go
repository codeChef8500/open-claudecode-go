package engine

import (
	"encoding/json"
	"testing"
)

func TestBuildSystemInitMessage_AllFields(t *testing.T) {
	notInvocable := false
	tr := true
	qe := &QueryEngine{
		sessionID: "sess-init",
		config: &QueryEngineConfig{
			CWD: "/home/test",
			Tools: []Tool{}, // empty for now (no mock needed)
			MCPClients: []MCPClientConnection{
				{Name: "github", Status: "connected"},
			},
			Commands: []Command{
				{Name: "help"},
				{Name: "internal-only", UserInvocable: &notInvocable},
				{Name: "clear"},
			},
			Agents: []AgentDefinition{
				{Type: "code"},
				{Type: "research"},
			},
			Skills: []SkillConfig{
				{Name: "deploy"},
				{Name: "hidden", UserInvocable: &notInvocable},
			},
			Plugins: []PluginConfig{
				{Name: "eslint", Path: "/plugins/eslint", Source: "local"},
			},
			PermissionMode: "plan",
			APIKeySource:   "env",
			Betas:          []string{"beta1"},
			OutputStyle:    "verbose",
			Version:        "1.2.3",
			FastMode:       &tr,
		},
	}

	msg := qe.buildSystemInitMessage("claude-sonnet-4-6")

	// Verify core fields
	if msg.Type != SDKMsgSystem {
		t.Errorf("type = %s, want system", msg.Type)
	}
	if msg.Subtype != SDKSystemSubtypeInit {
		t.Errorf("subtype = %s, want init", msg.Subtype)
	}
	if msg.SessionID != "sess-init" {
		t.Errorf("session_id = %s, want sess-init", msg.SessionID)
	}
	if msg.CWD != "/home/test" {
		t.Errorf("cwd = %s", msg.CWD)
	}
	if msg.Model != "claude-sonnet-4-6" {
		t.Errorf("model = %s", msg.Model)
	}

	// MCP servers
	if len(msg.MCPServers) != 1 || msg.MCPServers[0].Name != "github" {
		t.Errorf("mcp_servers = %v", msg.MCPServers)
	}

	// Slash commands — "internal-only" filtered out
	if len(msg.SlashCommands) != 2 {
		t.Fatalf("slash_commands len = %d, want 2", len(msg.SlashCommands))
	}
	if msg.SlashCommands[0] != "help" || msg.SlashCommands[1] != "clear" {
		t.Errorf("slash_commands = %v", msg.SlashCommands)
	}

	// Agents
	if len(msg.Agents) != 2 || msg.Agents[0] != "code" {
		t.Errorf("agents = %v", msg.Agents)
	}

	// Skills — "hidden" filtered out
	if len(msg.Skills) != 1 || msg.Skills[0] != "deploy" {
		t.Errorf("skills = %v", msg.Skills)
	}

	// Plugins
	if len(msg.Plugins) != 1 || msg.Plugins[0].Name != "eslint" {
		t.Errorf("plugins = %v", msg.Plugins)
	}

	// Config-sourced fields
	if msg.PermissionMode != "plan" {
		t.Errorf("permissionMode = %s", msg.PermissionMode)
	}
	if msg.APIKeySource != "env" {
		t.Errorf("apiKeySource = %s", msg.APIKeySource)
	}
	if len(msg.Betas) != 1 || msg.Betas[0] != "beta1" {
		t.Errorf("betas = %v", msg.Betas)
	}
	if msg.OutputStyle != "verbose" {
		t.Errorf("output_style = %s", msg.OutputStyle)
	}
	if msg.ClaudeCodeVersion != "1.2.3" {
		t.Errorf("claude_code_version = %s", msg.ClaudeCodeVersion)
	}
	if msg.FastModeState != FastModeOn {
		t.Errorf("fast_mode_state = %s, want on", msg.FastModeState)
	}

	// JSON round-trip
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SDKSystemInitMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.PermissionMode != "plan" {
		t.Errorf("decoded permissionMode = %s", decoded.PermissionMode)
	}
}

func TestBuildSystemInitMessage_Defaults(t *testing.T) {
	qe := &QueryEngine{
		sessionID: "sess-def",
		config:    &QueryEngineConfig{CWD: "/tmp"},
	}
	msg := qe.buildSystemInitMessage("claude-sonnet-4-6")

	if msg.PermissionMode != "default" {
		t.Errorf("default permissionMode = %s, want default", msg.PermissionMode)
	}
	if msg.OutputStyle != "concise" {
		t.Errorf("default output_style = %s, want concise", msg.OutputStyle)
	}
	if msg.ClaudeCodeVersion != "0.1.0" {
		t.Errorf("default version = %s, want 0.1.0", msg.ClaudeCodeVersion)
	}
	if msg.FastModeState != FastModeOff {
		t.Errorf("default fast_mode_state = %s, want off", msg.FastModeState)
	}
}
