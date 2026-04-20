package engine

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ────────────────────────────────────────────────────────────────────────────
// [P8.T4] dispatchQueryMessage — mirrors TS QueryEngine.ts:L757-968
// Handles 10 message types from the query loop.
// ────────────────────────────────────────────────────────────────────────────

// dispatchState holds mutable per-turn tracking state for the dispatch loop.
type dispatchState struct {
	currentMessageUsage          NonNullableUsage
	turnCount                    int
	hasAcknowledgedInitialMsgs   bool
	structuredOutputFromTool     interface{}
	lastStopReason               string
	startTime                    time.Time
	mainLoopModel                string
	initialStructuredOutputCalls int
}

func newDispatchState(model string, startTime time.Time) *dispatchState {
	return &dispatchState{
		currentMessageUsage: EmptyUsage(),
		turnCount:           1,
		startTime:           startTime,
		mainLoopModel:       model,
	}
}

// dispatchAction tells the caller what to do after dispatching a message.
type dispatchAction int

const (
	dispatchContinue dispatchAction = iota // keep looping
	dispatchReturn                         // early return (max_turns, budget, etc.)
	dispatchSkip                           // don't yield, keep looping
)

// dispatchResult holds the output of dispatching a single query message.
type dispatchResult struct {
	Action   dispatchAction
	Yielded  []interface{}     // SDK messages to yield to the caller
	Terminal *SDKResultMessage // non-nil if Action==dispatchReturn
}

// dispatchQueryMessage processes a single message from the query loop.
// TS anchor: QueryEngine.ts:L757-968 (switch statement)
func (qe *QueryEngine) dispatchQueryMessage(
	msg interface{},
	ds *dispatchState,
	cfg *QueryEngineConfig,
) *dispatchResult {
	result := &dispatchResult{Action: dispatchContinue}

	m, isMsg := msg.(*Message)
	se, isStream := msg.(*StreamEvent)

	// Count user turns.
	if isMsg && m.Role == RoleUser {
		ds.turnCount++
	}

	// Type-based dispatch.
	if isMsg {
		switch m.Type {
		case MsgTypeTombstone:
			// Tombstone messages are control signals — skip. (TS L758-760)
			result.Action = dispatchSkip
			return result

		case MsgTypeAssistant, "":
			// Assistant message. (TS L761-770)
			if m.Role == RoleAssistant {
				if m.StopReason != "" {
					ds.lastStopReason = m.StopReason
				}
				qe.mu.Lock()
				qe.mutableMessages = append(qe.mutableMessages, m)
				qe.mu.Unlock()
				qe.persistMessage(m)
				result.Yielded = append(result.Yielded, normalizeToSDK(m, qe.sessionID))
			}

		case MsgTypeProgress:
			// Progress message. (TS L771-783)
			qe.mu.Lock()
			qe.mutableMessages = append(qe.mutableMessages, m)
			qe.mu.Unlock()
			qe.persistMessage(m)
			result.Yielded = append(result.Yielded, normalizeToSDK(m, qe.sessionID))

		case MsgTypeUser:
			// User message (tool results). (TS L784-787)
			qe.mu.Lock()
			qe.mutableMessages = append(qe.mutableMessages, m)
			qe.mu.Unlock()
			qe.persistMessage(m)
			result.Yielded = append(result.Yielded, normalizeToSDK(m, qe.sessionID))

		case MsgTypeAttachment:
			// Attachment message. (TS L829-893)
			qe.mu.Lock()
			qe.mutableMessages = append(qe.mutableMessages, m)
			qe.mu.Unlock()
			qe.persistMessage(m)
			qe.handleAttachment(m, ds, cfg, result)

		case MsgTypeSystem:
			// System message. (TS L897-957)
			qe.handleSystemMessage(m, ds, cfg, result)

		case MsgTypeToolUseSummary:
			// Tool use summary. (TS L959-968)
			result.Yielded = append(result.Yielded, &SDKToolUseSummaryMessage{
				Type:                SDKMsgToolUseSummary,
				Summary:             m.Summary,
				PrecedingToolUseIDs: m.PrecedingToolUseIDs,
				SessionID:           qe.sessionID,
				UUID:                m.UUID,
			})

		default:
			// Unknown message types — skip silently.
			result.Action = dispatchSkip
		}
	}

	// Stream events. (TS L788-828)
	if isStream {
		qe.handleStreamEvent(se, ds, cfg, result)
	}

	return result
}

// handleAttachment processes attachment messages.
// TS anchor: QueryEngine.ts:L829-893
func (qe *QueryEngine) handleAttachment(
	m *Message,
	ds *dispatchState,
	cfg *QueryEngineConfig,
	result *dispatchResult,
) {
	if m.Attachment == nil {
		return
	}

	switch m.Attachment.Type {
	case "structured_output":
		ds.structuredOutputFromTool = m.Attachment.Data

	case "max_turns_reached":
		// TS L842-873
		durationMs := int(time.Since(ds.startTime).Milliseconds())
		maxTurns := cfg.MaxTurns
		if maxTurns <= 0 {
			maxTurns = 100
		}
		result.Action = dispatchReturn
		result.Terminal = NewSDKResultError(
			qe.sessionID,
			SDKResultErrorMaxTurns,
			[]string{fmt.Sprintf("Reached maximum number of turns (%d)", maxTurns)},
			durationMs, 0, ds.turnCount,
			qe.totalCostUSD(),
			qe.totalUsage,
		)

	case "queued_command":
		// TS L875-892 — yield as user replay if replayUserMessages
		if cfg.ReplayUserMessages {
			promptText := ""
			if m.Attachment.Prompt != "" {
				promptText = m.Attachment.Prompt
			}
			sourceUUID := m.UUID
			if m.Attachment.SourceUUID != "" {
				sourceUUID = m.Attachment.SourceUUID
			}
			result.Yielded = append(result.Yielded, &SDKUserReplayMessage{
				Type:      SDKMsgUser,
				SessionID: qe.sessionID,
				UUID:      sourceUUID,
				IsReplay:  true,
				Message: map[string]interface{}{
					"role":    "user",
					"content": promptText,
				},
			})
		}
	}
}

// handleSystemMessage processes system messages.
// TS anchor: QueryEngine.ts:L897-957
func (qe *QueryEngine) handleSystemMessage(
	m *Message,
	ds *dispatchState,
	cfg *QueryEngineConfig,
	result *dispatchResult,
) {
	// Snip replay check (TS L905-915)
	if cfg.SnipReplay != nil {
		qe.mu.Lock()
		snipResult := cfg.SnipReplay(m, qe.mutableMessages)
		if snipResult != nil {
			if snipResult.Executed {
				qe.mutableMessages = snipResult.Messages
			}
			qe.mu.Unlock()
			result.Action = dispatchSkip
			return
		}
		qe.mu.Unlock()
	}

	qe.mu.Lock()
	qe.mutableMessages = append(qe.mutableMessages, m)
	qe.mu.Unlock()

	// Compact boundary (TS L918-942)
	if m.Subtype == "compact_boundary" && m.CompactMetadata != nil {
		// Release pre-compaction messages for GC (TS L926-933)
		qe.mu.Lock()
		if idx := len(qe.mutableMessages) - 1; idx > 0 {
			qe.mutableMessages = qe.mutableMessages[idx:]
		}
		qe.mu.Unlock()

		result.Yielded = append(result.Yielded, &SDKCompactBoundaryMessage{
			Type:            SDKMsgCompactBoundary,
			SessionID:       qe.sessionID,
			UUID:            m.UUID,
			CompactMetadata: m.CompactMetadata,
		})
	}

	// API error / retry (TS L943-955)
	if m.Subtype == "api_error" {
		errStatus := &m.ErrorStatus
		if m.ErrorStatus == 0 {
			errStatus = nil
		}
		result.Yielded = append(result.Yielded, NewSDKAPIRetry(
			qe.sessionID,
			m.RetryAttempt,
			m.MaxRetries,
			m.RetryInMs,
			errStatus,
			SDKAssistantMessageErrorType(m.ErrorMessage),
		))
	}
}

// handleStreamEvent processes stream events for usage tracking.
// TS anchor: QueryEngine.ts:L788-828
func (qe *QueryEngine) handleStreamEvent(
	se *StreamEvent,
	ds *dispatchState,
	cfg *QueryEngineConfig,
	result *dispatchResult,
) {
	switch se.EventType {
	case "message_start":
		// Reset current message usage (TS L789-796)
		ds.currentMessageUsage = EmptyUsage()
		if se.Usage != nil {
			p := usageToPartial(se.Usage)
			ds.currentMessageUsage = UpdateUsage(ds.currentMessageUsage, &p)
		}

	case "message_delta":
		// Update usage + capture stop_reason (TS L797-809)
		if se.Usage != nil {
			p := usageToPartial(se.Usage)
			ds.currentMessageUsage = UpdateUsage(ds.currentMessageUsage, &p)
		}
		if se.StopReason != "" {
			ds.lastStopReason = se.StopReason
		}

	case "message_stop":
		// Accumulate into total (TS L810-816)
		qe.totalUsage = AccumulateUsage(qe.totalUsage, ds.currentMessageUsage)
	}

	// Yield as stream event if includePartialMessages (TS L818-826)
	if cfg.IncludePartialMessages {
		result.Yielded = append(result.Yielded, &SDKStreamEventMessage{
			Type:      SDKMsgStreamEvent,
			Event:     se,
			SessionID: qe.sessionID,
			UUID:      uuid.New().String(),
		})
	}
}

// normalizeToSDK converts an internal Message into the appropriate SDK message.
// Simplified version of TS normalizeMessage.
func normalizeToSDK(m *Message, sessionID string) interface{} {
	switch m.Role {
	case RoleAssistant:
		return &SDKAssistantTurnMessage{
			Type:      SDKMsgAssistant,
			SessionID: sessionID,
			UUID:      m.UUID,
			Message:   m,
		}
	case RoleUser:
		return &SDKUserReplayMessage{
			Type:      SDKMsgUser,
			SessionID: sessionID,
			UUID:      m.UUID,
		}
	default:
		return m
	}
}

// usageToPartial converts a UsageStats pointer to a PartialUsage for UpdateUsage.
func usageToPartial(u *UsageStats) PartialUsage {
	if u == nil {
		return PartialUsage{}
	}
	return PartialUsage{
		InputTokens:              &u.InputTokens,
		OutputTokens:             &u.OutputTokens,
		CacheCreationInputTokens: &u.CacheCreationInputTokens,
		CacheReadInputTokens:     &u.CacheReadInputTokens,
		CacheDeletedInputTokens:  &u.CacheDeletedInputTokens,
	}
}
