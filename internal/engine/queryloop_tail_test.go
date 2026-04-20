package engine

import "testing"

func TestBuildQueuedCommandAttachment(t *testing.T) {
	cmd := QueuedCommand{Prompt: "run tests", SourceUUID: "uuid-1"}
	ev := BuildQueuedCommandAttachment(cmd)
	if ev.Type != EventAttachment {
		t.Error("wrong event type")
	}
	if ev.Attachment.Type != "queued_command" {
		t.Error("wrong attachment type")
	}
	if ev.Attachment.Prompt != "run tests" {
		t.Error("wrong prompt")
	}
	if ev.Attachment.SourceUUID != "uuid-1" {
		t.Error("wrong source UUID")
	}
}

func TestEmitQueuedCommands(t *testing.T) {
	out := make(chan *StreamEvent, 10)
	queue := []QueuedCommand{
		{Prompt: "cmd1", SourceUUID: "u1"},
		{Prompt: "cmd2", SourceUUID: "u2"},
	}
	EmitQueuedCommands(&queue, out)

	if len(queue) != 0 {
		t.Error("queue should be cleared")
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 events, got %d", len(out))
	}
	ev1 := <-out
	if ev1.Attachment.Prompt != "cmd1" {
		t.Error("wrong first command prompt")
	}
}

func TestEmitQueuedCommands_Empty(t *testing.T) {
	out := make(chan *StreamEvent, 10)
	var queue []QueuedCommand
	EmitQueuedCommands(&queue, out)
	if len(out) != 0 {
		t.Error("no events expected for empty queue")
	}
}

func TestEmitQueuedCommands_Nil(t *testing.T) {
	out := make(chan *StreamEvent, 10)
	EmitQueuedCommands(nil, out)
	if len(out) != 0 {
		t.Error("no events expected for nil queue")
	}
}
