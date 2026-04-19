package prompt

import (
	"strings"
	"testing"

	"github.com/wall-ai/agent-engine/internal/engine"
	"github.com/wall-ai/agent-engine/internal/prompt/sections"
)

func TestSystemPromptFetcherAdapter_CustomPromptSkips(t *testing.T) {
	sections.ClearAll()
	defer sections.ClearAll()

	adapter := NewSystemPromptFetcherAdapter()
	parts := adapter.FetchParts(engine.FetchSystemPromptPartsOpts{
		CustomSystemPrompt: "my custom prompt",
		MainLoopModel:      "claude-sonnet-4-6",
	})

	if len(parts.DefaultSystemPrompt) != 0 {
		t.Error("custom prompt should skip default system prompt build")
	}
}

func TestSystemPromptFetcherAdapter_DefaultBuild(t *testing.T) {
	sections.ClearAll()
	defer sections.ClearAll()

	adapter := NewSystemPromptFetcherAdapter()
	// Pass nil tools — the adapter handles empty tool lists gracefully.
	parts := adapter.FetchParts(engine.FetchSystemPromptPartsOpts{
		MainLoopModel: "claude-sonnet-4-6",
	})

	if len(parts.DefaultSystemPrompt) == 0 {
		t.Error("default build should produce non-empty sections")
	}

	combined := strings.Join(parts.DefaultSystemPrompt, "\n")
	if !strings.Contains(combined, "# System") {
		t.Error("expected system section in output")
	}
}

func TestSystemPromptFetcherAdapter_AdditionalDirs(t *testing.T) {
	sections.ClearAll()
	defer sections.ClearAll()

	adapter := NewSystemPromptFetcherAdapter()
	parts := adapter.FetchParts(engine.FetchSystemPromptPartsOpts{
		MainLoopModel:                "claude-sonnet-4-6",
		AdditionalWorkingDirectories: []string{"/extra/dir1", "/extra/dir2"},
	})

	val, ok := parts.SystemContext["additional_dirs"]
	if !ok {
		t.Error("expected additional_dirs in SystemContext")
	}
	if !strings.Contains(val, "/extra/dir1") || !strings.Contains(val, "/extra/dir2") {
		t.Errorf("additional_dirs = %q, want both dirs", val)
	}
}

func TestSystemPromptFetcherAdapter_MCPClients(t *testing.T) {
	sections.ClearAll()
	defer sections.ClearAll()

	adapter := NewSystemPromptFetcherAdapter()
	parts := adapter.FetchParts(engine.FetchSystemPromptPartsOpts{
		MainLoopModel: "claude-sonnet-4-6",
		MCPClients: []engine.MCPClientConnection{
			{
				Name:         "test-server",
				Status:       "connected",
				Instructions: "Use the test server carefully.",
			},
		},
	})

	combined := strings.Join(parts.DefaultSystemPrompt, "\n")
	if !strings.Contains(combined, "test-server") {
		t.Error("expected MCP server name in output")
	}
}
