package session

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wall-ai/agent-engine/internal/util"
)

// ────────────────────────────────────────────────────────────────────────────
// Session index — replaces full directory scan with a cached JSON index.
//
// The TS codebase uses a SQLite sessionStorage; since this Go project
// doesn't have a SQLite dependency, we use a lightweight JSON index
// that is atomically updated on each session create/update. This provides
// O(1) session listing instead of O(N) directory scans.
//
// Aligned with claude-code-main services/sessionStorage.
// ────────────────────────────────────────────────────────────────────────────

// IndexEntry is a lightweight record for one session in the index.
type IndexEntry struct {
	ID        string    `json:"id"`
	Title     string    `json:"title,omitempty"`
	Model     string    `json:"model,omitempty"`
	WorkDir   string    `json:"work_dir,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Turns     int       `json:"turns"`
	Tags      []string  `json:"tags,omitempty"`
	Leaf      bool      `json:"leaf,omitempty"` // true if this is a leaf/fork session
	ParentID  string    `json:"parent_id,omitempty"`
}

// SessionIndex provides fast session listing using a cached JSON file.
type SessionIndex struct {
	mu      sync.Mutex
	rootDir string
	entries map[string]*IndexEntry
	dirty   bool
}

// NewSessionIndex creates or loads a session index from the given root.
func NewSessionIndex(rootDir string) *SessionIndex {
	idx := &SessionIndex{
		rootDir: rootDir,
		entries: make(map[string]*IndexEntry),
	}
	idx.load()
	return idx
}

// indexPath returns the path to the index file.
func (idx *SessionIndex) indexPath() string {
	return filepath.Join(idx.rootDir, ".session_index.json")
}

// load reads the index from disk.
func (idx *SessionIndex) load() {
	data, err := os.ReadFile(idx.indexPath())
	if err != nil {
		return // no index yet — will be built on first write
	}

	var entries []*IndexEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		slog.Warn("session_index: corrupt index, will rebuild", slog.Any("err", err))
		return
	}

	for _, e := range entries {
		idx.entries[e.ID] = e
	}
}

// save writes the index to disk atomically.
func (idx *SessionIndex) save() error {
	entries := make([]*IndexEntry, 0, len(idx.entries))
	for _, e := range idx.entries {
		entries = append(entries, e)
	}

	// Sort by UpdatedAt desc for consistent output.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].UpdatedAt.After(entries[j].UpdatedAt)
	})

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}

	if err := util.EnsureDir(idx.rootDir); err != nil {
		return err
	}

	// Atomic write via temp file.
	tmpPath := idx.indexPath() + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, idx.indexPath())
}

// Upsert creates or updates an entry in the index.
func (idx *SessionIndex) Upsert(entry *IndexEntry) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	entry.UpdatedAt = time.Now()
	if existing, ok := idx.entries[entry.ID]; ok {
		// Preserve created_at from existing entry.
		entry.CreatedAt = existing.CreatedAt
	} else if entry.CreatedAt.IsZero() {
		entry.CreatedAt = entry.UpdatedAt
	}

	idx.entries[entry.ID] = entry
	idx.dirty = true
}

// Touch updates the UpdatedAt timestamp and optionally the turn count.
func (idx *SessionIndex) Touch(sessionID string, turns int) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	e, ok := idx.entries[sessionID]
	if !ok {
		e = &IndexEntry{
			ID:        sessionID,
			CreatedAt: time.Now(),
		}
		idx.entries[sessionID] = e
	}
	e.UpdatedAt = time.Now()
	if turns > 0 {
		e.Turns = turns
	}
	idx.dirty = true
}

// Remove deletes a session from the index.
func (idx *SessionIndex) Remove(sessionID string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	delete(idx.entries, sessionID)
	idx.dirty = true
}

// List returns all indexed sessions, sorted by UpdatedAt desc.
func (idx *SessionIndex) List() []*IndexEntry {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	entries := make([]*IndexEntry, 0, len(idx.entries))
	for _, e := range idx.entries {
		entries = append(entries, e)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].UpdatedAt.After(entries[j].UpdatedAt)
	})
	return entries
}

// ListRecent returns up to `limit` most recent sessions.
func (idx *SessionIndex) ListRecent(limit int) []*IndexEntry {
	all := idx.List()
	if limit > 0 && len(all) > limit {
		return all[:limit]
	}
	return all
}

// Get returns a single entry by ID, or nil.
func (idx *SessionIndex) Get(sessionID string) *IndexEntry {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	return idx.entries[sessionID]
}

// Search returns entries matching a query string (searches ID and Title).
func (idx *SessionIndex) Search(query string) []*IndexEntry {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	var results []*IndexEntry
	q := normalizeSearch(query)
	for _, e := range idx.entries {
		if matchesSearch(e, q) {
			results = append(results, e)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].UpdatedAt.After(results[j].UpdatedAt)
	})
	return results
}

// Flush writes pending changes to disk.
func (idx *SessionIndex) Flush() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if !idx.dirty {
		return nil
	}
	idx.dirty = false
	return idx.save()
}

// Rebuild scans the session directory and rebuilds the index from meta files.
func (idx *SessionIndex) Rebuild(storage *Storage) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	metas, err := storage.ListSessions()
	if err != nil {
		return err
	}

	idx.entries = make(map[string]*IndexEntry, len(metas))
	for _, m := range metas {
		idx.entries[m.ID] = &IndexEntry{
			ID:        m.ID,
			Title:     m.Summary,
			Model:     m.Model,
			WorkDir:   m.WorkDir,
			CreatedAt: m.CreatedAt,
			UpdatedAt: m.UpdatedAt,
			Turns:     m.TurnCount,
			Tags:      m.Tags,
			ParentID:  m.ForkOf,
		}
	}

	idx.dirty = true
	return idx.save()
}

// normalizeSearch lowercases the query for case-insensitive matching.
func normalizeSearch(query string) string {
	return strings.ToLower(strings.TrimSpace(query))
}

// matchesSearch checks if an entry matches the search query.
func matchesSearch(e *IndexEntry, query string) bool {
	if query == "" {
		return true
	}
	if strings.Contains(strings.ToLower(e.ID), query) {
		return true
	}
	if strings.Contains(strings.ToLower(e.Title), query) {
		return true
	}
	if strings.Contains(strings.ToLower(e.WorkDir), query) {
		return true
	}
	for _, tag := range e.Tags {
		if strings.Contains(strings.ToLower(tag), query) {
			return true
		}
	}
	return false
}
