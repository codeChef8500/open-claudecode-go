package engine

// ────────────────────────────────────────────────────────────────────────────
// [P9.T16] Tail sub-steps — post-tool-execution steps executed at the end
// of each query loop iteration before the next model call.
//
// TS anchors:
//   - Tool refresh: query.ts:L1530-1534
//   - Queued commands attachment: query.ts:L1686-1703
//   - Memory prefetch re-trigger: query.ts:L1545-1548
//   - Skill prefetch re-trigger: query.ts:L1550-1555
// ────────────────────────────────────────────────────────────────────────────

// PostToolRefreshFn is called after tool execution to allow tools to
// update their definitions (e.g. MCP tool refresh).
// TS anchor: query.ts:L1530-1534
type PostToolRefreshFn func()

// QueuedCommand represents a command queued for attachment to the next turn.
// TS anchor: query.ts:L1686-1703
type QueuedCommand struct {
	Prompt     string
	SourceUUID string
}

// BuildQueuedCommandAttachment creates an attachment event for a queued command.
// TS anchor: query.ts:L1696-1703
func BuildQueuedCommandAttachment(cmd QueuedCommand) *StreamEvent {
	return &StreamEvent{
		Type: EventAttachment,
		Attachment: &AttachmentData{
			Type:       "queued_command",
			Prompt:     cmd.Prompt,
			SourceUUID: cmd.SourceUUID,
		},
	}
}

// EmitQueuedCommands yields attachment events for any queued commands,
// then clears the queue.
func EmitQueuedCommands(queue *[]QueuedCommand, out chan<- *StreamEvent) {
	if queue == nil || len(*queue) == 0 {
		return
	}
	for _, cmd := range *queue {
		out <- BuildQueuedCommandAttachment(cmd)
	}
	*queue = nil
}

// SkillPrefetchFn is a no-op placeholder for skill prefetch triggering.
// TS anchor: query.ts:L358-364
type SkillPrefetchFn func()

// MemoryPrefetchFn is a no-op placeholder for memory prefetch re-triggering.
// TS anchor: query.ts:L1545-1548
type MemoryPrefetchFn func()

// DefaultSkillPrefetch is the default no-op skill prefetch.
func DefaultSkillPrefetch() {}

// DefaultMemoryPrefetch is the default no-op memory prefetch.
func DefaultMemoryPrefetch() {}
