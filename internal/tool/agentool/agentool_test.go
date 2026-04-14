package agentool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wall-ai/agent-engine/internal/agent"
	"github.com/wall-ai/agent-engine/internal/engine"
)

func TestValidateInput(t *testing.T) {
	tool := New(nil)

	tests := []struct {
		name    string
		input   map[string]interface{}
		wantErr string
	}{
		{
			name:    "empty task",
			input:   map[string]interface{}{"task": ""},
			wantErr: "task must not be empty",
		},
		{
			name:    "negative max_turns",
			input:   map[string]interface{}{"task": "do something", "max_turns": -1},
			wantErr: "max_turns must be non-negative",
		},
		{
			name:    "max_turns too high",
			input:   map[string]interface{}{"task": "do something", "max_turns": 300},
			wantErr: "max_turns exceeds maximum",
		},
		{
			name:    "valid task",
			input:   map[string]interface{}{"task": "search for bugs"},
			wantErr: "",
		},
		{
			name:    "valid with max_turns",
			input:   map[string]interface{}{"task": "refactor code", "max_turns": 50},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, _ := json.Marshal(tt.input)
			err := tool.ValidateInput(context.Background(), raw)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestAgentToolAsyncProgressCallback(t *testing.T) {
	runner := agent.NewAgentRunner(agent.AgentRunnerConfig{})
	mgr := agent.NewAsyncLifecycleManager(runner)
	var got agent.Notification
	var gotID string
	var jsx map[string]string

	tool := NewWithConfig(AgentToolConfig{
		Runner:       runner,
		AsyncManager: mgr,
		ProgressCallback: func(agentID string, notif agent.Notification) {
			gotID = agentID
			got = notif
		},
	})

	input, _ := json.Marshal(Input{Task: "do work", Description: "short desc", RunInBackground: true})
	ch, err := tool.Call(context.Background(), input, &engine.UseContext{
		ToolUseID: "tool-1",
		WorkDir:   t.TempDir(),
		SetToolJSX: func(toolUseID string, value interface{}) {
			if m, ok := value.(map[string]string); ok {
				jsx = m
			}
		},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	for range ch {
	}
	if gotID == "" {
		t.Fatal("expected progress callback to be invoked")
	}
	if got.Type != agent.NotificationTypeProgress {
		t.Fatalf("unexpected notification type: %s", got.Type)
	}
	if got.Message == "" {
		t.Fatal("expected progress message")
	}
	if jsx == nil || jsx["status"] != "background" {
		t.Fatalf("expected jsx background status, got %#v", jsx)
	}
	_ = mgr.Cancel(gotID)
}

func TestAgentToolAutoBackgroundUsesResumeManager(t *testing.T) {
	rm := agent.NewResumeManager(t.TempDir())
	tool := NewWithConfig(AgentToolConfig{
		Runner:           agent.NewAgentRunner(agent.AgentRunnerConfig{}),
		AsyncManager:     agent.NewAsyncLifecycleManager(agent.NewAgentRunner(agent.AgentRunnerConfig{})),
		ResumeManager:    rm,
		AutoBackgroundMs: 1,
	})

	if tool.cfg.ResumeManager == nil {
		t.Fatal("expected resume manager to be wired")
	}
	if tool.cfg.AutoBackgroundMs != 1 {
		t.Fatalf("expected auto background threshold to be retained, got %d", tool.cfg.AutoBackgroundMs)
	}
}
