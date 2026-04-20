package engine

import (
	"strings"

	"github.com/google/uuid"
)

// ────────────────────────────────────────────────────────────────────────────
// [P8.T8] ProcessUserInput — simplified Go port of TS processUserInput.ts
// TS anchor: processUserInput.ts:L85-270
//
// This handles the core paths:
//   1. Slash commands (starts with "/")
//   2. Regular text prompts
//
// Advanced features deferred:
//   - Image block handling/resizing
//   - Ultraplan keyword routing
//   - UserPromptSubmit hooks
//   - Bridge-safe command resolution
//   - Attachment extraction
// ────────────────────────────────────────────────────────────────────────────

// ProcessUserInputOpts holds the parameters for ProcessUserInput.
type ProcessUserInputOpts struct {
	Input   string
	Mode    string // "prompt", "bash", "task-notification"
	Context *ProcessUserInputContext
	UUID    string
	IsMeta  bool
	// Commands is the registered slash commands for this session.
	Commands []Command
	// SkipSlashCommands suppresses slash command handling (remote/CCR input).
	SkipSlashCommands bool
	// QuerySource indicates the caller ("sdk", "interactive", etc.).
	QuerySource string
}

// ProcessUserInput processes user input and returns the messages to send.
// TS anchor: processUserInput.ts:L85-270
func ProcessUserInput(opts *ProcessUserInputOpts) *ProcessUserInputResult {
	input := opts.Input
	msgUUID := opts.UUID
	if msgUUID == "" {
		msgUUID = uuid.New().String()
	}

	// ── Slash command handling (TS L531-551) ─────────────────────────────
	if !opts.SkipSlashCommands && strings.HasPrefix(strings.TrimSpace(input), "/") {
		return processSlashCommandInput(input, opts.Commands, opts.Context, msgUUID)
	}

	// ── Regular text prompt (TS L576-588) ────────────────────────────────
	return processTextPrompt(input, msgUUID, opts.IsMeta)
}

// processSlashCommandInput handles /command inputs.
// TS anchor: processSlashCommand.tsx:L309-524
func processSlashCommandInput(input string, commands []Command, ctx *ProcessUserInputContext, msgUUID string) *ProcessUserInputResult {
	parsed := parseSlashCommandInput(input)
	if parsed == nil {
		errMsg := "Commands are in the form `/command [args]`"
		return &ProcessUserInputResult{
			Messages: []*Message{
				createUserMessage(errMsg, msgUUID, false),
			},
			ShouldQuery: false,
			ResultText:  errMsg,
		}
	}

	// Check if command exists in registered commands
	for i := range commands {
		cmd := &commands[i]
		if cmd.Name == parsed.CommandName {
			return executeRegisteredCommand(cmd, parsed.Args, ctx, msgUUID)
		}
	}

	// Unknown command — check if it looks like a command name
	if looksLikeCommandName(parsed.CommandName) {
		unknownMsg := "Unknown skill: " + parsed.CommandName
		return &ProcessUserInputResult{
			Messages: []*Message{
				createUserMessage(unknownMsg, msgUUID, false),
			},
			ShouldQuery: false,
			ResultText:  unknownMsg,
		}
	}

	// Doesn't look like a command — treat as regular prompt
	return processTextPrompt(input, msgUUID, false)
}

// processTextPrompt creates a standard user message.
// TS anchor: processTextPrompt.ts
func processTextPrompt(input string, msgUUID string, isMeta bool) *ProcessUserInputResult {
	return &ProcessUserInputResult{
		Messages: []*Message{
			createUserMessage(input, msgUUID, isMeta),
		},
		ShouldQuery: true,
	}
}

// createUserMessage builds a user Message from text.
func createUserMessage(content string, msgUUID string, isMeta bool) *Message {
	msg := &Message{
		UUID: msgUUID,
		Role: RoleUser,
		Type: MsgTypeUser,
		Content: []*ContentBlock{{
			Type: ContentTypeText,
			Text: content,
		}},
	}
	if isMeta {
		msg.IsMeta = true
	}
	return msg
}

// ── Slash command parsing ──────────────────────────────────────────────────

// ParsedSlashCommand holds parsed slash command parts.
type ParsedSlashCommand struct {
	CommandName string
	Args        string
	IsMCP       bool
}

// parseSlashCommandInput parses "/command args" into components.
// TS anchor: slashCommandParsing.ts:L25-60
func parseSlashCommandInput(input string) *ParsedSlashCommand {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/") {
		return nil
	}

	withoutSlash := trimmed[1:]
	words := strings.SplitN(withoutSlash, " ", -1)
	if len(words) == 0 || words[0] == "" {
		return nil
	}

	commandName := words[0]
	isMCP := false
	argsStartIndex := 1

	// MCP commands: second word is "(MCP)"
	if len(words) > 1 && words[1] == "(MCP)" {
		commandName = commandName + " (MCP)"
		isMCP = true
		argsStartIndex = 2
	}

	args := ""
	if argsStartIndex < len(words) {
		args = strings.Join(words[argsStartIndex:], " ")
	}

	return &ParsedSlashCommand{
		CommandName: commandName,
		Args:        args,
		IsMCP:       isMCP,
	}
}

// looksLikeCommandName checks if a string looks like a valid command name.
// TS anchor: processSlashCommand.tsx:L304-308
func looksLikeCommandName(name string) bool {
	for _, ch := range name {
		if !isCommandChar(ch) {
			return false
		}
	}
	return true
}

func isCommandChar(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') || ch == ':' || ch == '-' || ch == '_'
}

// executeRegisteredCommand dispatches a known slash command.
// Returns ProcessUserInputResult with messages from the command.
func executeRegisteredCommand(cmd *Command, args string, ctx *ProcessUserInputContext, msgUUID string) *ProcessUserInputResult {
	// If the command has a Handler, call it
	if cmd.Handler != nil {
		resultText, err := cmd.Handler(ctx.AbortCtx, args)
		if err != nil {
			errMsg := "Command error: " + err.Error()
			return &ProcessUserInputResult{
				Messages: []*Message{
					createUserMessage(errMsg, msgUUID, false),
				},
				ShouldQuery: false,
				ResultText:  errMsg,
			}
		}
		// Handler returned output text — yield as local command result
		if resultText != "" {
			return &ProcessUserInputResult{
				Messages: []*Message{
					{
						UUID:    msgUUID,
						Role:    RoleUser,
						Type:    MsgTypeSystem,
						Subtype: "local_command",
						Content: []*ContentBlock{{
							Type: ContentTypeText,
							Text: "<" + LocalCommandStdoutTag + ">" + resultText + "</" + LocalCommandStdoutTag + ">",
						}},
					},
				},
				ShouldQuery: false,
				ResultText:  resultText,
			}
		}
		// Empty result — command ran but produced no output
		return &ProcessUserInputResult{
			Messages:    []*Message{},
			ShouldQuery: false,
		}
	}

	// No handler — pass through as user message (prompt-type command)
	content := "/" + cmd.Name
	if args != "" {
		content = content + " " + args
	}
	return &ProcessUserInputResult{
		Messages: []*Message{
			createUserMessage(content, msgUUID, false),
		},
		ShouldQuery: true,
	}
}
