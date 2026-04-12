package swarm

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/gofrs/flock"
	"github.com/google/uuid"
)

// ── File-based mailbox aligned with claude-code-main's teammateMailbox.ts ────
//
// Each agent gets a JSON file at:
//   ~/.claude/teams/{team}/inboxes/{agent_name}.json
//
// The file contains a JSON array of MailboxEnvelope objects.
// Concurrent access is protected by an OS-level file lock (flock).

// FileMailbox provides file-system-backed messaging for a single agent.
type FileMailbox struct {
	mu        sync.Mutex
	teamName  string
	agentName string
	inboxPath string
	lockPath  string
}

// FileMailboxConfig configures a file mailbox.
type FileMailboxConfig struct {
	TeamName  string // team name
	AgentName string // agent name (not full agent ID)
	BaseDir   string // base directory (default: ~/.claude)
}

// NewFileMailbox creates a file mailbox for the given agent.
func NewFileMailbox(cfg FileMailboxConfig) (*FileMailbox, error) {
	baseDir := cfg.BaseDir
	if baseDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("get home dir: %w", err)
		}
		baseDir = filepath.Join(home, ".claude")
	}

	inboxDir := filepath.Join(baseDir, "teams", SanitizeName(cfg.TeamName), "inboxes")
	if err := os.MkdirAll(inboxDir, 0755); err != nil {
		return nil, fmt.Errorf("create inbox dir: %w", err)
	}

	inboxPath := filepath.Join(inboxDir, SanitizeName(cfg.AgentName)+".json")
	lockPath := inboxPath + ".lock"

	return &FileMailbox{
		teamName:  cfg.TeamName,
		agentName: cfg.AgentName,
		inboxPath: inboxPath,
		lockPath:  lockPath,
	}, nil
}

// InboxPath returns the filesystem path to this agent's inbox file.
func (fm *FileMailbox) InboxPath() string {
	return fm.inboxPath
}

// Write appends a message to the inbox file under file lock.
func (fm *FileMailbox) Write(env *MailboxEnvelope) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if env.ID == "" {
		env.ID = uuid.New().String()
	}
	if env.Timestamp.IsZero() {
		env.Timestamp = time.Now()
	}

	return fm.withLock(func() error {
		messages, err := fm.readFileUnlocked()
		if err != nil {
			return err
		}
		messages = append(messages, *env)
		return fm.writeFileUnlocked(messages)
	})
}

// ReadAll returns all messages from the inbox file.
func (fm *FileMailbox) ReadAll() ([]MailboxEnvelope, error) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	var result []MailboxEnvelope
	err := fm.withLock(func() error {
		msgs, err := fm.readFileUnlocked()
		if err != nil {
			return err
		}
		result = msgs
		return nil
	})
	return result, err
}

// ReadUnread returns all unread messages, sorted by priority (leader first).
func (fm *FileMailbox) ReadUnread() ([]MailboxEnvelope, error) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	var result []MailboxEnvelope
	err := fm.withLock(func() error {
		msgs, err := fm.readFileUnlocked()
		if err != nil {
			return err
		}
		for _, m := range msgs {
			if !m.IsRead {
				result = append(result, m)
			}
		}
		return nil
	})

	// Sort: leader messages first, then by timestamp.
	sort.SliceStable(result, func(i, j int) bool {
		li := result[i].IsFromLeader()
		lj := result[j].IsFromLeader()
		if li != lj {
			return li
		}
		return result[i].Timestamp.Before(result[j].Timestamp)
	})

	return result, err
}

// MarkRead marks a message as read by ID.
func (fm *FileMailbox) MarkRead(msgID string) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	return fm.withLock(func() error {
		msgs, err := fm.readFileUnlocked()
		if err != nil {
			return err
		}
		for i := range msgs {
			if msgs[i].ID == msgID {
				msgs[i].IsRead = true
				break
			}
		}
		return fm.writeFileUnlocked(msgs)
	})
}

// MarkAllRead marks all messages as read.
func (fm *FileMailbox) MarkAllRead() error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	return fm.withLock(func() error {
		msgs, err := fm.readFileUnlocked()
		if err != nil {
			return err
		}
		for i := range msgs {
			msgs[i].IsRead = true
		}
		return fm.writeFileUnlocked(msgs)
	})
}

// Clear removes all messages from the inbox.
func (fm *FileMailbox) Clear() error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	return fm.withLock(func() error {
		return fm.writeFileUnlocked(nil)
	})
}

// Delete removes the inbox file and lock file from disk.
func (fm *FileMailbox) Delete() error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	_ = os.Remove(fm.lockPath)
	return os.Remove(fm.inboxPath)
}

// Count returns the total number of messages.
func (fm *FileMailbox) Count() (int, error) {
	msgs, err := fm.ReadAll()
	if err != nil {
		return 0, err
	}
	return len(msgs), nil
}

// UnreadCount returns the number of unread messages.
func (fm *FileMailbox) UnreadCount() (int, error) {
	msgs, err := fm.ReadUnread()
	if err != nil {
		return 0, err
	}
	return len(msgs), nil
}

// ── Internal helpers ─────────────────────────────────────────────────────────

// withLock executes fn while holding the file lock.
func (fm *FileMailbox) withLock(fn func() error) error {
	fl := flock.New(fm.lockPath)

	if err := fl.Lock(); err != nil {
		return fmt.Errorf("acquire file lock %s: %w", fm.lockPath, err)
	}
	defer func() {
		if err := fl.Unlock(); err != nil {
			slog.Warn("filemailbox: unlock failed",
				slog.String("path", fm.lockPath),
				slog.Any("err", err))
		}
	}()

	return fn()
}

// readFileUnlocked reads the inbox JSON file. Must be called under lock.
func (fm *FileMailbox) readFileUnlocked() ([]MailboxEnvelope, error) {
	data, err := os.ReadFile(fm.inboxPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read inbox %s: %w", fm.inboxPath, err)
	}

	if len(data) == 0 {
		return nil, nil
	}

	var messages []MailboxEnvelope
	if err := json.Unmarshal(data, &messages); err != nil {
		slog.Warn("filemailbox: corrupt inbox, resetting",
			slog.String("path", fm.inboxPath),
			slog.Any("err", err))
		return nil, nil
	}
	return messages, nil
}

// writeFileUnlocked writes the messages to the inbox JSON file. Must be called under lock.
func (fm *FileMailbox) writeFileUnlocked(messages []MailboxEnvelope) error {
	if messages == nil {
		messages = []MailboxEnvelope{}
	}

	data, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal messages: %w", err)
	}

	// Write atomically via temp file + rename.
	tmpPath := fm.inboxPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write temp inbox: %w", err)
	}
	if err := os.Rename(tmpPath, fm.inboxPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename inbox: %w", err)
	}
	return nil
}

// ── FileMailboxRegistry ──────────────────────────────────────────────────────

// FileMailboxRegistry manages file mailboxes for all agents in a team.
type FileMailboxRegistry struct {
	mu        sync.RWMutex
	mailboxes map[string]*FileMailbox // agentName → FileMailbox
	baseDir   string
	teamName  string
}

// NewFileMailboxRegistry creates a registry for a team's file mailboxes.
func NewFileMailboxRegistry(teamName, baseDir string) *FileMailboxRegistry {
	return &FileMailboxRegistry{
		mailboxes: make(map[string]*FileMailbox),
		baseDir:   baseDir,
		teamName:  teamName,
	}
}

// GetOrCreate returns the file mailbox for the given agent, creating it if needed.
func (r *FileMailboxRegistry) GetOrCreate(agentName string) (*FileMailbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if mb, ok := r.mailboxes[agentName]; ok {
		return mb, nil
	}

	mb, err := NewFileMailbox(FileMailboxConfig{
		TeamName:  r.teamName,
		AgentName: agentName,
		BaseDir:   r.baseDir,
	})
	if err != nil {
		return nil, err
	}
	r.mailboxes[agentName] = mb
	return mb, nil
}

// Send writes a message to the target agent's file mailbox.
func (r *FileMailboxRegistry) Send(from, to string, env *MailboxEnvelope) error {
	mb, err := r.GetOrCreate(to)
	if err != nil {
		return err
	}
	env.From = from
	env.To = to
	return mb.Write(env)
}

// Remove removes and deletes a file mailbox.
func (r *FileMailboxRegistry) Remove(agentName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if mb, ok := r.mailboxes[agentName]; ok {
		delete(r.mailboxes, agentName)
		return mb.Delete()
	}
	return nil
}

// RemoveAll removes and deletes all file mailboxes in this registry.
func (r *FileMailboxRegistry) RemoveAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for name, mb := range r.mailboxes {
		_ = mb.Delete()
		delete(r.mailboxes, name)
	}
}

// GetInboxPath returns the inbox path helper for static access.
func GetInboxPath(baseDir, teamName, agentName string) string {
	if baseDir == "" {
		home, _ := os.UserHomeDir()
		baseDir = filepath.Join(home, ".claude")
	}
	return filepath.Join(baseDir, "teams", SanitizeName(teamName), "inboxes", SanitizeName(agentName)+".json")
}
