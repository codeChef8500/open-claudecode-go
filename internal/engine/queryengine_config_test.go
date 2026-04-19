package engine

import (
	"context"
	"testing"

	"github.com/wall-ai/agent-engine/internal/state"
)

func TestQueryEngineConfig_Defaults(t *testing.T) {
	cfg := &QueryEngineConfig{
		CWD:   "/tmp/test",
		Tools: nil,
	}
	if cfg.CWD != "/tmp/test" {
		t.Errorf("CWD = %s, want /tmp/test", cfg.CWD)
	}
	if cfg.MaxTurns != 0 {
		t.Errorf("MaxTurns should default to 0, got %d", cfg.MaxTurns)
	}
}

func TestQueryEngineConfig_WithCallbacks(t *testing.T) {
	appState := &state.AppState{}
	cfg := &QueryEngineConfig{
		CWD: "/home/user",
		GetAppState: func() *state.AppState {
			return appState
		},
		SetAppState: func(fn func(*state.AppState) *state.AppState) {
			appState = fn(appState)
		},
	}
	got := cfg.GetAppState()
	if got != appState {
		t.Error("GetAppState should return the state")
	}
}

func TestCommand_Handler(t *testing.T) {
	cmd := Command{
		Name:        "test",
		Description: "A test command",
		Handler: func(ctx context.Context, args string) (string, error) {
			return "handled: " + args, nil
		},
	}
	result, err := cmd.Handler(context.Background(), "foo")
	if err != nil {
		t.Fatal(err)
	}
	if result != "handled: foo" {
		t.Errorf("got %q, want %q", result, "handled: foo")
	}
}

func TestMCPClientConnection_Fields(t *testing.T) {
	c := MCPClientConnection{
		Name:         "test-server",
		Status:       "connected",
		Instructions: "Use carefully",
		Tools: []MCPTool{
			{Name: "search", Description: "Search docs"},
		},
	}
	if c.Name != "test-server" {
		t.Error("Name mismatch")
	}
	if len(c.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(c.Tools))
	}
}

func TestPermissionResult_Allow(t *testing.T) {
	r := &PermissionResult{
		Behavior: "allow",
	}
	if r.Behavior != "allow" {
		t.Errorf("expected allow, got %s", r.Behavior)
	}
}

func TestPermissionResult_Deny(t *testing.T) {
	r := &PermissionResult{
		Behavior:  "deny",
		Message:   "not allowed",
		Interrupt: true,
	}
	if r.Behavior != "deny" {
		t.Error("expected deny")
	}
	if !r.Interrupt {
		t.Error("expected interrupt=true")
	}
}
