package engine

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// ────────────────────────────────────────────────────────────────────────────
// [P8.T2] QueryEngine query loop — bridges the new QueryEngine to the
// underlying model-call + tool-dispatch cycle.
//
// This is a simplified loop aligned with claude-code-main query.ts that
// will be expanded in Phase 9 to full TS parity.
// ────────────────────────────────────────────────────────────────────────────

// queryLoopInput holds the inputs for a single query loop invocation.
type queryLoopInput struct {
	messages     []*Message
	tuc          *ToolUseContext
	puic         *ProcessUserInputContext
	model        string
	maxTurns     int
	maxBudgetUSD *float64
	taskBudget   *TaskBudget
}

// runQueryLoop drives the model-call → tool-dispatch cycle for the new
// QueryEngine.  It returns a channel that yields messages and stream events.
//
// When qe.engine is set (via SetEngine or NewQueryEngineWithEngine), this
// delegates to the production runQueryLoop which has full model calling,
// tool execution, compaction, stop hooks, etc. Otherwise it uses a stub
// that emits a done signal (useful for tests and SDK bootstrap).
func (qe *QueryEngine) runQueryLoop(ctx context.Context, input *queryLoopInput) <-chan interface{} {
	// ── Production delegation path ────────────────────────────────────
	if qe.engine != nil {
		return qe.runQueryLoopWithEngine(ctx, input)
	}

	// ── Stub path (no Engine wired) ───────────────────────────────────
	return qe.runQueryLoopStub(ctx, input)
}

// runQueryLoopWithEngine delegates to the production Engine's runQueryLoop,
// forwarding StreamEvents back as interface{} on the output channel.
func (qe *QueryEngine) runQueryLoopWithEngine(ctx context.Context, input *queryLoopInput) <-chan interface{} {
	out := make(chan interface{}, 64)

	go func() {
		defer close(out)

		// Build QueryParams for the production loop.
		params := QueryParams{
			Text: "", // user message already appended to input.messages
			Config: QueryConfig{
				Model:    input.model,
				MaxTurns: input.maxTurns,
			},
			Source: QuerySourceSDK,
		}

		// Seed the engine's history with our messages so the loop sees them.
		qe.engine.SeedHistory(input.messages)

		// Run the production query loop, forwarding events.
		eventCh := make(chan *StreamEvent, 128)
		errCh := make(chan error, 1)
		go func() {
			errCh <- runQueryLoop(ctx, qe.engine, params, eventCh)
			close(eventCh)
		}()

		for ev := range eventCh {
			qe.emitLoop(ctx, out, ev)
		}

		if err := <-errCh; err != nil && ctx.Err() == nil {
			slog.Warn("queryengine: production loop error", slog.Any("err", err))
		}
	}()

	return out
}

// runQueryLoopStub is the fallback loop when no production Engine is wired.
// It emits a request-start and done signal so SubmitMessage can produce a result.
func (qe *QueryEngine) runQueryLoopStub(ctx context.Context, input *queryLoopInput) <-chan interface{} {
	out := make(chan interface{}, 64)

	go func() {
		defer close(out)

		maxTurns := input.maxTurns
		if maxTurns <= 0 {
			maxTurns = defaultMaxTurns
		}

		turnCount := 0
		for turnCount < maxTurns {
			turnCount++

			if ctx.Err() != nil {
				return
			}

			// Emit turn tracking.
			qe.emitLoop(ctx, out, &StreamEvent{
				Type:      EventRequestStart,
				SessionID: qe.sessionID,
			})

			slog.Debug("queryengine: stub loop (no Engine wired)",
				slog.Int("turn", turnCount))

			// Signal done — no model caller available.
			qe.emitLoop(ctx, out, &StreamEvent{
				Type:      EventDone,
				SessionID: qe.sessionID,
			})
			return
		}

		// Max turns exceeded.
		maxTurnsMsg := &Message{
			UUID:      uuid.New().String(),
			Role:      RoleSystem,
			Type:      MsgTypeAttachment,
			SessionID: qe.sessionID,
			Timestamp: time.Now(),
			Attachment: &AttachmentData{
				Type:    "max_turns_reached",
				Content: fmt.Sprintf("Reached maximum number of turns (%d)", maxTurns),
			},
		}
		qe.emitLoop(ctx, out, maxTurnsMsg)
	}()

	return out
}

// emitLoop sends a message to the loop output channel, respecting context cancellation.
func (qe *QueryEngine) emitLoop(ctx context.Context, ch chan<- interface{}, msg interface{}) {
	select {
	case <-ctx.Done():
	case ch <- msg:
	}
}
