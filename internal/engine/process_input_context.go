package engine

import (
	"context"

	"github.com/wall-ai/agent-engine/internal/state"
)

// ────────────────────────────────────────────────────────────────────────────
// [P6.T2] ProcessUserInputContext — mirrors claude-code-main
// processUserInput.ts ProcessUserInputContext.
// ────────────────────────────────────────────────────────────────────────────

// ProcessUserInputContext aggregates all state needed during user input
// processing and tool execution within a single query turn.
type ProcessUserInputContext struct {
	// Messages is the live conversation message store.
	Messages []*Message
	// SetMessages replaces the message store (e.g. after /force-snip).
	SetMessages func(fn func([]*Message) []*Message)

	// Options holds immutable tool execution configuration.
	Options *ToolUseOptions

	// AbortCtx is the cancellable context for interruptions.
	AbortCtx context.Context
	// AbortCancel cancels the abort context.
	AbortCancel context.CancelFunc

	// GetAppState returns the current application state.
	GetAppState func() *state.AppState
	// SetAppState atomically updates the application state.
	SetAppState func(fn func(*state.AppState) *state.AppState)

	// ReadFileState tracks file read cache state.
	ReadFileState *FileStateCache

	// HandleElicitation is called when an MCP tool requests user input.
	HandleElicitation func(ctx context.Context, toolUseID, question string) (string, error)

	// NestedMemoryAttachmentTriggers tracks memory paths that triggered
	// nested attachment loading.
	NestedMemoryAttachmentTriggers map[string]bool

	// LoadedNestedMemoryPaths tracks which nested memory files have been loaded.
	LoadedNestedMemoryPaths map[string]bool

	// DiscoveredSkillNames tracks skill names discovered this turn
	// (feeds was_discovered on skill tool invocation).
	DiscoveredSkillNames map[string]bool

	// DynamicSkillDirTriggers tracks directories that triggered skill loading.
	DynamicSkillDirTriggers map[string]bool

	// SetInProgressToolUseIDs updates the set of currently executing tool use IDs.
	SetInProgressToolUseIDs func(ids []string)

	// SetResponseLength updates the response length tracker.
	SetResponseLength func(length int)

	// UpdateFileHistoryState updates the file history state.
	UpdateFileHistoryState func(fn func(*FileHistoryState) *FileHistoryState)

	// UpdateAttributionState updates the commit attribution state.
	UpdateAttributionState func(fn func(*AttributionState) *AttributionState)

	// SetSDKStatus updates the SDK-level session status.
	SetSDKStatus func(status *string)

	// OnChangeAPIKey is called when the API key changes mid-session.
	OnChangeAPIKey func()
}

// NewProcessUserInputContext creates a ProcessUserInputContext from a
// QueryEngineConfig and mutable message store.
func NewProcessUserInputContext(
	cfg *QueryEngineConfig,
	messages []*Message,
	setMessages func(fn func([]*Message) []*Message),
	tuc *ToolUseContext,
) *ProcessUserInputContext {
	return &ProcessUserInputContext{
		Messages:                       messages,
		SetMessages:                    setMessages,
		Options:                        tuc.Options,
		AbortCtx:                       tuc.AbortCtx,
		AbortCancel:                    tuc.AbortCancel,
		GetAppState:                    tuc.GetAppState,
		SetAppState:                    tuc.SetAppState,
		ReadFileState:                  tuc.ReadFileState,
		HandleElicitation:              cfg.HandleElicitation,
		NestedMemoryAttachmentTriggers: make(map[string]bool),
		LoadedNestedMemoryPaths:        make(map[string]bool),
		DiscoveredSkillNames:           make(map[string]bool),
		DynamicSkillDirTriggers:        make(map[string]bool),
		SetInProgressToolUseIDs:        func([]string) {},
		SetResponseLength:              func(int) {},
		UpdateFileHistoryState: func(fn func(*FileHistoryState) *FileHistoryState) {
			tuc.SetAppState(func(prev *state.AppState) *state.AppState {
				// FileHistory update hook — wired by callers
				return prev
			})
		},
		UpdateAttributionState: func(fn func(*AttributionState) *AttributionState) {
			tuc.SetAppState(func(prev *state.AppState) *state.AppState {
				return prev
			})
		},
		SetSDKStatus:   cfg.SetSDKStatus,
		OnChangeAPIKey: func() {},
	}
}
