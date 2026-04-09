package provider

import (
	"log/slog"
	"sync"
	"time"
)

// ────────────────────────────────────────────────────────────────────────────
// Prompt cache break detection — 2-phase heuristic aligned with
// claude-code-main services/api/promptCacheBreakDetection.ts.
//
// Phase 1 (pre-call): RecordPromptState — fingerprints system prompt, tools,
//   model, betas, effort etc. and detects what changed vs. prior call.
// Phase 2 (post-call): CheckResponseForCacheBreak — compares cache_read_tokens
//   with previous value to detect actual cache invalidation.
// ────────────────────────────────────────────────────────────────────────────

const (
	// minCacheMissTokens is the minimum absolute token drop to trigger a break warning.
	minCacheMissTokens = 2_000
	// cacheTTL5MinMs is the 5-minute TTL for cache expiry detection.
	cacheTTL5MinMs = 5 * 60 * 1000
	// cacheTTL1HourMs is the 1-hour TTL for cache expiry detection.
	cacheTTL1HourMs = 60 * 60 * 1000
	// maxTrackedSources caps the number of tracked sources to prevent unbounded memory.
	maxTrackedSources = 10
)

// PendingChanges describes what changed between two successive API calls.
// Aligned with TS PendingChanges type.
type PendingChanges struct {
	SystemPromptChanged       bool
	ToolSchemasChanged        bool
	ModelChanged              bool
	FastModeChanged           bool
	CacheControlChanged       bool
	GlobalCacheStrategyChanged bool
	BetasChanged              bool
	AutoModeChanged           bool
	EffortChanged             bool
	ExtraBodyChanged          bool
	AddedToolCount            int
	RemovedToolCount          int
	SystemCharDelta           int
	AddedTools                []string
	RemovedTools              []string
	ChangedToolSchemas        []string
	PreviousModel             string
	NewModel                  string
}

// previousState stores the fingerprint of the previous API call for a given tracking key.
type previousState struct {
	systemHash          uint64
	toolsHash           uint64
	cacheControlHash    uint64
	toolNames           []string
	perToolHashes       map[string]uint64
	systemCharCount     int
	model               string
	fastMode            bool
	globalCacheStrategy string
	betas               []string
	autoModeActive      bool
	effortValue         string
	extraBodyHash       uint64
	callCount           int
	pendingChanges      *PendingChanges
	prevCacheReadTokens *int
	cacheDeletionPending bool
	lastCallTime        time.Time
}

// PromptStateSnapshot captures all observable state that could affect the
// server-side cache key. Aligned with TS PromptStateSnapshot.
type PromptStateSnapshot struct {
	SystemText          string
	ToolSchemaJSON      string // serialized tool schemas
	QuerySource         string
	Model               string
	AgentID             string
	FastMode            bool
	GlobalCacheStrategy string
	Betas               []string
	AutoModeActive      bool
	EffortValue         string
	ExtraBodyHash       uint64
	ToolNames           []string
}

// CacheBreakEvent is the analytics event emitted when a cache break is detected.
type CacheBreakEvent struct {
	Reason                string
	Changes               *PendingChanges
	CallNumber            int
	PrevCacheReadTokens   int
	CacheReadTokens       int
	CacheCreationTokens   int
	TimeSinceLastCallMs   int64
	LastCallOver5Min      bool
	LastCallOver1Hour     bool
}

// PromptCacheBreakTracker implements the full 2-phase cache break detection.
// Thread-safe for concurrent use across multiple query sources.
type PromptCacheBreakTracker struct {
	mu    sync.Mutex
	state map[string]*previousState
}

// NewPromptCacheBreakTracker creates a new tracker.
func NewPromptCacheBreakTracker() *PromptCacheBreakTracker {
	return &PromptCacheBreakTracker{
		state: make(map[string]*previousState),
	}
}

// trackedSourcePrefixes lists the query source prefixes that are tracked.
var trackedSourcePrefixes = []string{
	"repl_main_thread",
	"sdk",
	"agent:custom",
	"agent:default",
	"agent:builtin",
}

// getTrackingKey returns the tracking key for a query source, or "" if untracked.
func getTrackingKey(querySource, agentID string) string {
	if querySource == "compact" {
		return "repl_main_thread"
	}
	for _, prefix := range trackedSourcePrefixes {
		if len(querySource) >= len(prefix) && querySource[:len(prefix)] == prefix {
			if agentID != "" {
				return agentID
			}
			return querySource
		}
	}
	return ""
}

// isExcludedModel returns true for models that should skip cache break detection.
func isExcludedModel(model string) bool {
	for i := 0; i <= len(model)-5; i++ {
		if model[i:i+5] == "haiku" {
			return true
		}
	}
	return false
}

// RecordPromptState is Phase 1 (pre-call): fingerprint the current state and
// detect what changed. Does NOT fire events — stores pending changes for Phase 2.
func (t *PromptCacheBreakTracker) RecordPromptState(snap PromptStateSnapshot) {
	key := getTrackingKey(snap.QuerySource, snap.AgentID)
	if key == "" {
		return
	}

	systemHash := HashString(snap.SystemText)
	toolsHash := HashString(snap.ToolSchemaJSON)

	t.mu.Lock()
	defer t.mu.Unlock()

	prev, exists := t.state[key]
	if !exists {
		// Evict oldest entries if at capacity.
		for len(t.state) >= maxTrackedSources {
			for k := range t.state {
				delete(t.state, k)
				break
			}
		}
		perToolHashes := make(map[string]uint64, len(snap.ToolNames))
		for _, name := range snap.ToolNames {
			perToolHashes[name] = HashString(name + ":" + snap.ToolSchemaJSON)
		}
		t.state[key] = &previousState{
			systemHash:          systemHash,
			toolsHash:           toolsHash,
			toolNames:           snap.ToolNames,
			perToolHashes:       perToolHashes,
			systemCharCount:     len(snap.SystemText),
			model:               snap.Model,
			fastMode:            snap.FastMode,
			globalCacheStrategy: snap.GlobalCacheStrategy,
			betas:               snap.Betas,
			autoModeActive:      snap.AutoModeActive,
			effortValue:         snap.EffortValue,
			extraBodyHash:       snap.ExtraBodyHash,
			callCount:           1,
			lastCallTime:        time.Now(),
		}
		return
	}

	prev.callCount++
	prev.lastCallTime = time.Now()

	systemPromptChanged := systemHash != prev.systemHash
	toolSchemasChanged := toolsHash != prev.toolsHash
	modelChanged := snap.Model != prev.model
	fastModeChanged := snap.FastMode != prev.fastMode
	globalCacheStrategyChanged := snap.GlobalCacheStrategy != prev.globalCacheStrategy
	betasChanged := !stringSliceEqual(snap.Betas, prev.betas)
	autoModeChanged := snap.AutoModeActive != prev.autoModeActive
	effortChanged := snap.EffortValue != prev.effortValue
	extraBodyChanged := snap.ExtraBodyHash != prev.extraBodyHash

	anyChanged := systemPromptChanged || toolSchemasChanged || modelChanged ||
		fastModeChanged || globalCacheStrategyChanged || betasChanged ||
		autoModeChanged || effortChanged || extraBodyChanged

	if anyChanged {
		prevToolSet := make(map[string]bool, len(prev.toolNames))
		for _, n := range prev.toolNames {
			prevToolSet[n] = true
		}
		newToolSet := make(map[string]bool, len(snap.ToolNames))
		for _, n := range snap.ToolNames {
			newToolSet[n] = true
		}
		var addedTools, removedTools []string
		for _, n := range snap.ToolNames {
			if !prevToolSet[n] {
				addedTools = append(addedTools, n)
			}
		}
		for _, n := range prev.toolNames {
			if !newToolSet[n] {
				removedTools = append(removedTools, n)
			}
		}

		prev.pendingChanges = &PendingChanges{
			SystemPromptChanged:        systemPromptChanged,
			ToolSchemasChanged:         toolSchemasChanged,
			ModelChanged:               modelChanged,
			FastModeChanged:            fastModeChanged,
			GlobalCacheStrategyChanged: globalCacheStrategyChanged,
			BetasChanged:               betasChanged,
			AutoModeChanged:            autoModeChanged,
			EffortChanged:              effortChanged,
			ExtraBodyChanged:           extraBodyChanged,
			AddedToolCount:             len(addedTools),
			RemovedToolCount:           len(removedTools),
			AddedTools:                 addedTools,
			RemovedTools:               removedTools,
			SystemCharDelta:            len(snap.SystemText) - prev.systemCharCount,
			PreviousModel:              prev.model,
			NewModel:                   snap.Model,
		}
	} else {
		prev.pendingChanges = nil
	}

	prev.systemHash = systemHash
	prev.toolsHash = toolsHash
	prev.toolNames = snap.ToolNames
	prev.systemCharCount = len(snap.SystemText)
	prev.model = snap.Model
	prev.fastMode = snap.FastMode
	prev.globalCacheStrategy = snap.GlobalCacheStrategy
	prev.betas = snap.Betas
	prev.autoModeActive = snap.AutoModeActive
	prev.effortValue = snap.EffortValue
	prev.extraBodyHash = snap.ExtraBodyHash
}

// CheckResponseForCacheBreak is Phase 2 (post-call): compare cache read tokens
// to detect an actual cache break. Returns a CacheBreakEvent if a break was detected.
func (t *PromptCacheBreakTracker) CheckResponseForCacheBreak(
	querySource string,
	cacheReadTokens int,
	cacheCreationTokens int,
	agentID string,
) *CacheBreakEvent {
	key := getTrackingKey(querySource, agentID)
	if key == "" {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	st, exists := t.state[key]
	if !exists {
		return nil
	}

	if isExcludedModel(st.model) {
		return nil
	}

	prevCacheRead := st.prevCacheReadTokens
	st.prevCacheReadTokens = &cacheReadTokens

	// Skip first call — no previous value.
	if prevCacheRead == nil {
		return nil
	}

	// Cache deletions pending — expected drop.
	if st.cacheDeletionPending {
		st.cacheDeletionPending = false
		st.pendingChanges = nil
		return nil
	}

	// Detect break: cache read dropped >5% AND absolute drop >= threshold.
	tokenDrop := *prevCacheRead - cacheReadTokens
	if cacheReadTokens >= *prevCacheRead*95/100 || tokenDrop < minCacheMissTokens {
		st.pendingChanges = nil
		return nil
	}

	timeSinceLastCall := time.Since(st.lastCallTime).Milliseconds()
	changes := st.pendingChanges

	// Build explanation.
	reason := buildBreakReason(changes, timeSinceLastCall)

	event := &CacheBreakEvent{
		Reason:              reason,
		Changes:             changes,
		CallNumber:          st.callCount,
		PrevCacheReadTokens: *prevCacheRead,
		CacheReadTokens:     cacheReadTokens,
		CacheCreationTokens: cacheCreationTokens,
		TimeSinceLastCallMs: timeSinceLastCall,
		LastCallOver5Min:    timeSinceLastCall > cacheTTL5MinMs,
		LastCallOver1Hour:   timeSinceLastCall > cacheTTL1HourMs,
	}

	slog.Warn("prompt cache break detected",
		slog.String("reason", reason),
		slog.String("source", querySource),
		slog.Int("call", st.callCount),
		slog.Int("prev_cache_read", *prevCacheRead),
		slog.Int("cache_read", cacheReadTokens))

	st.pendingChanges = nil
	return event
}

// NotifyCacheDeletion marks that a cache deletion was sent, so the next
// drop in cache read tokens is expected.
func (t *PromptCacheBreakTracker) NotifyCacheDeletion(querySource, agentID string) {
	key := getTrackingKey(querySource, agentID)
	t.mu.Lock()
	defer t.mu.Unlock()
	if st, ok := t.state[key]; ok {
		st.cacheDeletionPending = true
	}
}

// NotifyCompaction resets the cache read baseline after compaction.
func (t *PromptCacheBreakTracker) NotifyCompaction(querySource, agentID string) {
	key := getTrackingKey(querySource, agentID)
	t.mu.Lock()
	defer t.mu.Unlock()
	if st, ok := t.state[key]; ok {
		st.prevCacheReadTokens = nil
	}
}

// CleanupAgent removes tracking state for a specific agent.
func (t *PromptCacheBreakTracker) CleanupAgent(agentID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.state, agentID)
}

// Reset clears all tracking state.
func (t *PromptCacheBreakTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state = make(map[string]*previousState)
}

// buildBreakReason constructs a human-readable reason string from pending changes.
func buildBreakReason(changes *PendingChanges, timeSinceLastCallMs int64) string {
	if changes != nil {
		var parts []string
		if changes.ModelChanged {
			parts = append(parts, "model changed ("+changes.PreviousModel+" → "+changes.NewModel+")")
		}
		if changes.SystemPromptChanged {
			parts = append(parts, "system prompt changed")
		}
		if changes.ToolSchemasChanged {
			if changes.AddedToolCount > 0 || changes.RemovedToolCount > 0 {
				parts = append(parts, "tools changed")
			} else {
				parts = append(parts, "tools changed (schema changed, same tool set)")
			}
		}
		if changes.FastModeChanged {
			parts = append(parts, "fast mode toggled")
		}
		if changes.GlobalCacheStrategyChanged {
			parts = append(parts, "global cache strategy changed")
		}
		if changes.BetasChanged {
			parts = append(parts, "betas changed")
		}
		if changes.AutoModeChanged {
			parts = append(parts, "auto mode toggled")
		}
		if changes.EffortChanged {
			parts = append(parts, "effort changed")
		}
		if changes.ExtraBodyChanged {
			parts = append(parts, "extra body params changed")
		}
		if len(parts) > 0 {
			result := parts[0]
			for _, p := range parts[1:] {
				result += ", " + p
			}
			return result
		}
	}

	if timeSinceLastCallMs > cacheTTL1HourMs {
		return "possible 1h TTL expiry (prompt unchanged)"
	}
	if timeSinceLastCallMs > cacheTTL5MinMs {
		return "possible 5min TTL expiry (prompt unchanged)"
	}
	if timeSinceLastCallMs > 0 {
		return "likely server-side (prompt unchanged, <5min gap)"
	}
	return "unknown cause"
}

// stringSliceEqual compares two string slices for equality.
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
