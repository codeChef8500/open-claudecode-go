package engine

import (
	"testing"
)

func TestIsLocalCommandOutput(t *testing.T) {
	if !isLocalCommandOutput("<local-command-stdout>ls output</local-command-stdout>") {
		t.Error("should detect stdout tag")
	}
	if !isLocalCommandOutput("<local-command-stderr>error</local-command-stderr>") {
		t.Error("should detect stderr tag")
	}
	if isLocalCommandOutput("normal text") {
		t.Error("should not match normal text")
	}
}

func TestHandleLocalCommandResult(t *testing.T) {
	qe := NewQueryEngine(&QueryEngineConfig{CWD: "/tmp"})
	out := make(chan interface{}, 16)

	puResult := &ProcessUserInputResult{
		Messages: []*Message{
			{
				UUID: "u1",
				Role: RoleUser,
				Content: []*ContentBlock{{
					Type: ContentTypeText,
					Text: "<local-command-stdout>help output</local-command-stdout>",
				}},
			},
		},
		ShouldQuery: false,
		ResultText:  "Help text",
	}

	qe.handleLocalCommandResult(out, puResult, 100, "claude-sonnet-4-6")
	close(out)

	var msgs []interface{}
	for m := range out {
		msgs = append(msgs, m)
	}

	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (replay + result), got %d", len(msgs))
	}

	// First: user replay
	replay, ok := msgs[0].(*SDKUserReplayMessage)
	if !ok {
		t.Fatalf("expected SDKUserReplayMessage, got %T", msgs[0])
	}
	if !replay.IsReplay {
		t.Error("expected IsReplay=true")
	}

	// Second: result success
	result, ok := msgs[1].(*SDKResultMessage)
	if !ok {
		t.Fatalf("expected SDKResultMessage, got %T", msgs[1])
	}
	if result.Subtype != SDKResultSuccess {
		t.Errorf("subtype = %s, want success", result.Subtype)
	}
	if result.Result != "Help text" {
		t.Errorf("result = %s, want 'Help text'", result.Result)
	}
}

func TestHandleLocalCommandResult_NoMessages(t *testing.T) {
	qe := NewQueryEngine(&QueryEngineConfig{CWD: "/tmp"})
	out := make(chan interface{}, 16)

	puResult := &ProcessUserInputResult{
		Messages:    []*Message{},
		ShouldQuery: false,
		ResultText:  "",
	}

	qe.handleLocalCommandResult(out, puResult, 50, "claude-sonnet-4-6")
	close(out)

	var msgs []interface{}
	for m := range out {
		msgs = append(msgs, m)
	}

	// Only the result message
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (result only), got %d", len(msgs))
	}
}
