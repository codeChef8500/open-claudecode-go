package sections

import "sync"

// ── Session-level section cache ────────────────────────────────────────────
// Mirrors TS getSystemPromptSectionCache / setSystemPromptSectionCacheEntry /
// clearSystemPromptSectionState from bootstrap/state.ts.
//
// The cache stores resolved section values keyed by section name. It is
// cleared on /clear, /compact, or explicit ClearAll() calls.

var (
	cacheMu sync.RWMutex
	cache   = map[string]string{}
)

// cacheGet returns the cached value for a section name.
func cacheGet(name string) (string, bool) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	v, ok := cache[name]
	return v, ok
}

// cacheSet stores a resolved section value.
func cacheSet(name string, value string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cache[name] = value
}

// ClearAll drops every cached section value. Called on /clear and /compact
// (aligned with TS clearSystemPromptSections).
func ClearAll() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cache = map[string]string{}
}
