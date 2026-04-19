package engine

import (
	"context"
	"testing"

	"github.com/wall-ai/agent-engine/internal/state"
)

func TestNewProcessUserInputContext(t *testing.T) {
	appState := &state.AppState{}
	tuc := &ToolUseContext{
		Options: &ToolUseOptions{
			Tools:         nil,
			MainLoopModel: "claude-sonnet-4-6",
		},
		AbortCtx:    context.Background(),
		AbortCancel: func() {},
		GetAppState: func() *state.AppState { return appState },
		SetAppState: func(fn func(*state.AppState) *state.AppState) {
			appState = fn(appState)
		},
		ReadFileState: NewFileStateCache(10),
	}

	cfg := &QueryEngineConfig{
		CWD:               "/tmp",
		SetSDKStatus:      func(*string) {},
		HandleElicitation: nil,
	}

	msgs := []*Message{{UUID: "m1"}}
	setMsgs := func(fn func([]*Message) []*Message) {
		msgs = fn(msgs)
	}

	puic := NewProcessUserInputContext(cfg, msgs, setMsgs, tuc)

	if puic.Options != tuc.Options {
		t.Error("Options should be wired from ToolUseContext")
	}
	if len(puic.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(puic.Messages))
	}
	if puic.DiscoveredSkillNames == nil {
		t.Error("DiscoveredSkillNames should be initialized")
	}
	if puic.NestedMemoryAttachmentTriggers == nil {
		t.Error("NestedMemoryAttachmentTriggers should be initialized")
	}
}

func TestProcessUserInputContext_SetMessages(t *testing.T) {
	appState := &state.AppState{}
	tuc := &ToolUseContext{
		Options:     &ToolUseOptions{},
		AbortCtx:    context.Background(),
		AbortCancel: func() {},
		GetAppState: func() *state.AppState { return appState },
		SetAppState: func(fn func(*state.AppState) *state.AppState) {},
		ReadFileState: NewFileStateCache(10),
	}

	msgs := []*Message{{UUID: "m1"}, {UUID: "m2"}}
	var captured []*Message
	setMsgs := func(fn func([]*Message) []*Message) {
		captured = fn(msgs)
	}

	puic := NewProcessUserInputContext(&QueryEngineConfig{}, msgs, setMsgs, tuc)
	puic.SetMessages(func(prev []*Message) []*Message {
		return prev[:1]
	})
	if len(captured) != 1 {
		t.Errorf("expected 1 message after set, got %d", len(captured))
	}
}
