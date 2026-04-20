package engine

import (
	"context"
	"errors"
	"testing"
)

func TestParseSlashCommandInput_Valid(t *testing.T) {
	p := parseSlashCommandInput("/search foo bar")
	if p == nil {
		t.Fatal("expected parsed result")
	}
	if p.CommandName != "search" {
		t.Errorf("command = %s, want search", p.CommandName)
	}
	if p.Args != "foo bar" {
		t.Errorf("args = %q, want 'foo bar'", p.Args)
	}
	if p.IsMCP {
		t.Error("expected IsMCP=false")
	}
}

func TestParseSlashCommandInput_MCP(t *testing.T) {
	p := parseSlashCommandInput("/mcp:tool (MCP) arg1")
	if p == nil {
		t.Fatal("expected parsed result")
	}
	if p.CommandName != "mcp:tool (MCP)" {
		t.Errorf("command = %s", p.CommandName)
	}
	if !p.IsMCP {
		t.Error("expected IsMCP=true")
	}
	if p.Args != "arg1" {
		t.Errorf("args = %q", p.Args)
	}
}

func TestParseSlashCommandInput_NoSlash(t *testing.T) {
	if parseSlashCommandInput("hello") != nil {
		t.Error("expected nil for non-slash input")
	}
}

func TestParseSlashCommandInput_Empty(t *testing.T) {
	if parseSlashCommandInput("/ ") != nil {
		t.Error("expected nil for empty command")
	}
}

func TestLooksLikeCommandName(t *testing.T) {
	if !looksLikeCommandName("help") {
		t.Error("'help' should look like command")
	}
	if !looksLikeCommandName("mcp:tool-name_v2") {
		t.Error("'mcp:tool-name_v2' should look like command")
	}
	if looksLikeCommandName("path/to/file") {
		t.Error("'path/to/file' should not look like command")
	}
	if looksLikeCommandName("hello world") {
		t.Error("'hello world' should not look like command")
	}
}

func TestProcessUserInput_RegularPrompt(t *testing.T) {
	result := ProcessUserInput(&ProcessUserInputOpts{
		Input: "Hello, Claude",
		Mode:  "prompt",
	})
	if !result.ShouldQuery {
		t.Error("expected ShouldQuery=true for regular prompt")
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if result.Messages[0].Content[0].Text != "Hello, Claude" {
		t.Error("message text mismatch")
	}
}

func TestProcessUserInput_SlashCommand_Known(t *testing.T) {
	commands := []Command{
		{
			Name: "help",
			Handler: func(ctx context.Context, args string) (string, error) {
				return "Help text here", nil
			},
		},
	}
	ctx := &ProcessUserInputContext{
		AbortCtx: context.Background(),
	}
	result := ProcessUserInput(&ProcessUserInputOpts{
		Input:    "/help",
		Mode:     "prompt",
		Commands: commands,
		Context:  ctx,
	})
	if result.ShouldQuery {
		t.Error("expected ShouldQuery=false for local command")
	}
	if result.ResultText != "Help text here" {
		t.Errorf("resultText = %q", result.ResultText)
	}
}

func TestProcessUserInput_SlashCommand_Error(t *testing.T) {
	commands := []Command{
		{
			Name: "fail",
			Handler: func(ctx context.Context, args string) (string, error) {
				return "", errors.New("boom")
			},
		},
	}
	ctx := &ProcessUserInputContext{
		AbortCtx: context.Background(),
	}
	result := ProcessUserInput(&ProcessUserInputOpts{
		Input:    "/fail",
		Mode:     "prompt",
		Commands: commands,
		Context:  ctx,
	})
	if result.ShouldQuery {
		t.Error("expected ShouldQuery=false on error")
	}
	if result.ResultText != "Command error: boom" {
		t.Errorf("resultText = %q", result.ResultText)
	}
}

func TestProcessUserInput_SlashCommand_Unknown(t *testing.T) {
	result := ProcessUserInput(&ProcessUserInputOpts{
		Input: "/nonexistent",
		Mode:  "prompt",
	})
	if result.ShouldQuery {
		t.Error("expected ShouldQuery=false for unknown command")
	}
	if result.ResultText != "Unknown skill: nonexistent" {
		t.Errorf("resultText = %q", result.ResultText)
	}
}

func TestProcessUserInput_SlashCommand_LooksLikeFilePath(t *testing.T) {
	result := ProcessUserInput(&ProcessUserInputOpts{
		Input: "/var/log/syslog",
		Mode:  "prompt",
	})
	// "/var/log/syslog" doesn't look like a command name (has '/') so
	// it should be treated as a regular prompt
	if !result.ShouldQuery {
		t.Error("expected ShouldQuery=true for file-path-like input")
	}
}

func TestProcessUserInput_SkipSlashCommands(t *testing.T) {
	result := ProcessUserInput(&ProcessUserInputOpts{
		Input:             "/help",
		Mode:              "prompt",
		SkipSlashCommands: true,
	})
	if !result.ShouldQuery {
		t.Error("expected ShouldQuery=true when slash commands are skipped")
	}
}

func TestProcessUserInput_PromptCommand_NoHandler(t *testing.T) {
	commands := []Command{
		{Name: "review", Description: "Code review"},
	}
	ctx := &ProcessUserInputContext{
		AbortCtx: context.Background(),
	}
	result := ProcessUserInput(&ProcessUserInputOpts{
		Input:    "/review my code",
		Mode:     "prompt",
		Commands: commands,
		Context:  ctx,
	})
	if !result.ShouldQuery {
		t.Error("expected ShouldQuery=true for prompt-type command (no handler)")
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if result.Messages[0].Content[0].Text != "/review my code" {
		t.Errorf("text = %q", result.Messages[0].Content[0].Text)
	}
}
