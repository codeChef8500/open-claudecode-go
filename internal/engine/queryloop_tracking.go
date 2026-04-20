package engine

// ────────────────────────────────────────────────────────────────────────────
// [P9.T1] Query loop tracking helpers.
// Type definitions (ContinueReason, ContinueTransition, TerminalReason, etc.)
// live in transitions.go.
//
// TS anchor: query.ts:L241-355
// ────────────────────────────────────────────────────────────────────────────

import "github.com/google/uuid"

// IncrementQueryTracking advances the query chain depth, or initializes
// a new chain if no tracking exists yet.
// TS anchor: query.ts:L346-355
func IncrementQueryTracking(qt *QueryTracking) QueryTracking {
	if qt != nil {
		return QueryTracking{
			ChainID: qt.ChainID,
			Depth:   qt.Depth + 1,
		}
	}
	return QueryTracking{
		ChainID: uuid.New().String(),
		Depth:   0,
	}
}
