package session

import (
	"sync"
	"time"
)

// Activity tracking — aligned with claude-code-main sessionActivity.ts.
//
// Tracks user activity state (active/idle) for session timeout management,
// resource cleanup, and analytics.

// ActivityState represents the user's activity level.
type ActivityState string

const (
	ActivityActive  ActivityState = "active"
	ActivityIdle    ActivityState = "idle"
	ActivityAway    ActivityState = "away"
)

const (
	// DefaultIdleThreshold is the time after the last input before
	// the user is considered idle.
	DefaultIdleThreshold = 5 * time.Minute

	// DefaultAwayThreshold is the time after the last input before
	// the user is considered away.
	DefaultAwayThreshold = 30 * time.Minute
)

// ActivityTracker monitors user activity within a session.
type ActivityTracker struct {
	mu             sync.RWMutex
	lastInputTime  time.Time
	lastOutputTime time.Time
	state          ActivityState
	idleThreshold  time.Duration
	awayThreshold  time.Duration
	totalInputs    int
	totalOutputs   int
	sessionStart   time.Time
	listeners      []ActivityListener
}

// ActivityListener is called when the activity state changes.
type ActivityListener func(old, new ActivityState)

// ActivitySnapshot captures the current activity state at a point in time.
type ActivitySnapshot struct {
	State          ActivityState `json:"state"`
	LastInputTime  time.Time     `json:"last_input_time"`
	LastOutputTime time.Time     `json:"last_output_time"`
	IdleDuration   time.Duration `json:"idle_duration"`
	TotalInputs    int           `json:"total_inputs"`
	TotalOutputs   int           `json:"total_outputs"`
	SessionUptime  time.Duration `json:"session_uptime"`
}

// NewActivityTracker creates a new activity tracker.
func NewActivityTracker() *ActivityTracker {
	now := time.Now()
	return &ActivityTracker{
		lastInputTime: now,
		state:         ActivityActive,
		idleThreshold: DefaultIdleThreshold,
		awayThreshold: DefaultAwayThreshold,
		sessionStart:  now,
	}
}

// NewActivityTrackerWithThresholds creates a tracker with custom thresholds.
func NewActivityTrackerWithThresholds(idle, away time.Duration) *ActivityTracker {
	t := NewActivityTracker()
	t.idleThreshold = idle
	t.awayThreshold = away
	return t
}

// RecordInput marks that the user provided input (prompt, command, etc.).
func (t *ActivityTracker) RecordInput() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.lastInputTime = time.Now()
	t.totalInputs++
	t.setState(ActivityActive)
}

// RecordOutput marks that the system produced output (response, tool result, etc.).
func (t *ActivityTracker) RecordOutput() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.lastOutputTime = time.Now()
	t.totalOutputs++
}

// State returns the current activity state, updating it based on elapsed time.
func (t *ActivityTracker) State() ActivityState {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.updateState()
	return t.state
}

// Snapshot returns a full activity snapshot.
func (t *ActivityTracker) Snapshot() ActivitySnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.updateState()
	now := time.Now()
	return ActivitySnapshot{
		State:          t.state,
		LastInputTime:  t.lastInputTime,
		LastOutputTime: t.lastOutputTime,
		IdleDuration:  now.Sub(t.lastInputTime),
		TotalInputs:    t.totalInputs,
		TotalOutputs:   t.totalOutputs,
		SessionUptime:  now.Sub(t.sessionStart),
	}
}

// OnStateChange registers a listener for activity state transitions.
func (t *ActivityTracker) OnStateChange(fn ActivityListener) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.listeners = append(t.listeners, fn)
}

// IsIdle returns true if the user has been idle beyond the threshold.
func (t *ActivityTracker) IsIdle() bool {
	state := t.State()
	return state == ActivityIdle || state == ActivityAway
}

// IdleDuration returns how long the user has been idle.
func (t *ActivityTracker) IdleDuration() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return time.Since(t.lastInputTime)
}

// updateState checks elapsed time and transitions state if needed.
// Caller must hold the write lock.
func (t *ActivityTracker) updateState() {
	elapsed := time.Since(t.lastInputTime)

	if elapsed >= t.awayThreshold {
		t.setState(ActivityAway)
	} else if elapsed >= t.idleThreshold {
		t.setState(ActivityIdle)
	}
	// If actively called, state was already set by RecordInput.
}

// setState transitions the activity state and notifies listeners.
// Caller must hold the write lock.
func (t *ActivityTracker) setState(newState ActivityState) {
	if t.state == newState {
		return
	}
	old := t.state
	t.state = newState

	// Notify listeners (non-blocking).
	for _, fn := range t.listeners {
		go fn(old, newState)
	}
}
