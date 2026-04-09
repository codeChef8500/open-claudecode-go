package hooks

import (
	"sync"
)

// Session-scoped hooks — aligned with claude-code-main sessionHooks.ts.
//
// Session hooks are temporary hook configurations that are registered at runtime
// and automatically cleared when a session ends. They allow dynamic hook
// registration (e.g., from skills, plugins, or user commands) without
// persisting to settings.json.

// SessionHookStore manages ephemeral hooks scoped to a single session.
type SessionHookStore struct {
	mu    sync.RWMutex
	hooks map[HookEvent][]HookConfig
}

// NewSessionHookStore creates an empty session hook store.
func NewSessionHookStore() *SessionHookStore {
	return &SessionHookStore{
		hooks: make(map[HookEvent][]HookConfig),
	}
}

// Add registers a session-scoped hook for the given event.
func (s *SessionHookStore) Add(event HookEvent, cfg HookConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg.Event = event
	if cfg.Source == "" {
		cfg.Source = "session"
	}
	s.hooks[event] = append(s.hooks[event], cfg)
}

// Remove removes all session hooks with the given source for the event.
func (s *SessionHookStore) Remove(event HookEvent, source string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hooks := s.hooks[event]
	filtered := hooks[:0]
	for _, h := range hooks {
		if h.Source != source {
			filtered = append(filtered, h)
		}
	}
	s.hooks[event] = filtered
}

// Clear removes all session-scoped hooks.
func (s *SessionHookStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hooks = make(map[HookEvent][]HookConfig)
}

// Get returns a copy of all session hooks for the given event.
func (s *SessionHookStore) Get(event HookEvent) []HookConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	hooks := s.hooks[event]
	if len(hooks) == 0 {
		return nil
	}
	cp := make([]HookConfig, len(hooks))
	copy(cp, hooks)
	return cp
}

// All returns a copy of all session hooks keyed by event.
func (s *SessionHookStore) All() map[HookEvent][]HookConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[HookEvent][]HookConfig, len(s.hooks))
	for event, hooks := range s.hooks {
		cp := make([]HookConfig, len(hooks))
		copy(cp, hooks)
		result[event] = cp
	}
	return result
}

// Count returns the total number of session hooks registered.
func (s *SessionHookStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, hooks := range s.hooks {
		n += len(hooks)
	}
	return n
}

// MergeInto merges session hooks into an existing HooksSettings map.
// Session hooks are appended after persistent hooks for each event.
func (s *SessionHookStore) MergeInto(base HooksSettings) HooksSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.hooks) == 0 {
		return base
	}

	merged := make(HooksSettings, len(base))
	for event, hooks := range base {
		merged[event] = append(merged[event], hooks...)
	}
	for event, hooks := range s.hooks {
		merged[event] = append(merged[event], hooks...)
	}
	return merged
}
