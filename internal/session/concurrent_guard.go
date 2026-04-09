package session

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/wall-ai/agent-engine/internal/util"
)

// ── Concurrent Session Guard ─────────────────────────────────────────────────
// Aligned with claude-code-main src/services/session/concurrentSessions.ts
//
// Prevents multiple instances of the agent from writing to the same session
// simultaneously. Uses a PID-based lock file in the session directory.
// The guard is advisory — if a process crashes, the stale lock is detected
// and cleaned up automatically.

const (
	lockFileName       = ".lock"
	staleLockThreshold = 5 * time.Minute
)

// SessionLock represents an acquired lock on a session directory.
type SessionLock struct {
	sessionDir string
	lockPath   string
	pid        int
}

// AcquireSessionLock attempts to acquire an exclusive lock on a session.
// Returns an error if another live process holds the lock.
func AcquireSessionLock(sessionDir string) (*SessionLock, error) {
	lockPath := filepath.Join(sessionDir, lockFileName)
	pid := os.Getpid()

	if err := util.EnsureDir(sessionDir); err != nil {
		return nil, fmt.Errorf("session lock: ensure dir: %w", err)
	}

	// Check for existing lock.
	if info, err := readLockInfo(lockPath); err == nil {
		if isProcessAlive(info.PID) {
			if info.PID == pid {
				// Same process — re-entrant, just update timestamp.
				return writeLock(lockPath, pid)
			}
			return nil, fmt.Errorf(
				"session is locked by another process (PID %d, started %s). "+
					"If this is stale, delete %s",
				info.PID, info.Timestamp.Format(time.RFC3339), lockPath)
		}
		// Stale lock — process is dead, clean it up.
		slog.Debug("session lock: cleaning stale lock",
			slog.Int("stale_pid", info.PID),
			slog.String("path", lockPath))
		_ = os.Remove(lockPath)
	}

	return writeLock(lockPath, pid)
}

// Release removes the lock file if it's still owned by this process.
func (l *SessionLock) Release() {
	if l == nil {
		return
	}
	info, err := readLockInfo(l.lockPath)
	if err != nil {
		return
	}
	if info.PID == l.pid {
		_ = os.Remove(l.lockPath)
	}
}

// Refresh updates the lock timestamp to prevent stale detection.
func (l *SessionLock) Refresh() error {
	_, err := writeLock(l.lockPath, l.pid)
	return err
}

// ── Lock file format ─────────────────────────────────────────────────────────

type lockInfo struct {
	PID       int       `json:"pid"`
	Timestamp time.Time `json:"timestamp"`
	Hostname  string    `json:"hostname,omitempty"`
}

func writeLock(path string, pid int) (*SessionLock, error) {
	hostname, _ := os.Hostname()
	info := lockInfo{
		PID:       pid,
		Timestamp: time.Now(),
		Hostname:  hostname,
	}
	content := fmt.Sprintf("%d\n%s\n%s", info.PID, info.Timestamp.Format(time.RFC3339), info.Hostname)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("write lock: %w", err)
	}
	return &SessionLock{
		sessionDir: filepath.Dir(path),
		lockPath:   path,
		pid:        pid,
	}, nil
}

func readLockInfo(path string) (*lockInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := splitLines(string(data))
	if len(lines) < 2 {
		return nil, fmt.Errorf("malformed lock file")
	}

	pid, err := strconv.Atoi(lines[0])
	if err != nil {
		return nil, fmt.Errorf("invalid PID in lock: %w", err)
	}

	ts, err := time.Parse(time.RFC3339, lines[1])
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp in lock: %w", err)
	}

	info := &lockInfo{PID: pid, Timestamp: ts}
	if len(lines) > 2 {
		info.Hostname = lines[2]
	}
	return info, nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// isProcessAlive checks if a process with the given PID is still running.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds. Signal 0 checks existence.
	// On Windows, FindProcess only succeeds if the process exists.
	err = process.Signal(os.Signal(nil))
	return err == nil
}

// ── Guard integration ────────────────────────────────────────────────────────

// ConcurrentGuard manages a session lock with periodic refresh.
type ConcurrentGuard struct {
	lock   *SessionLock
	done   chan struct{}
}

// NewConcurrentGuard acquires a session lock and starts a background goroutine
// to periodically refresh it.
func NewConcurrentGuard(sessionDir string) (*ConcurrentGuard, error) {
	lock, err := AcquireSessionLock(sessionDir)
	if err != nil {
		return nil, err
	}

	g := &ConcurrentGuard{
		lock: lock,
		done: make(chan struct{}),
	}

	// Refresh the lock every minute.
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-g.done:
				return
			case <-ticker.C:
				if err := lock.Refresh(); err != nil {
					slog.Warn("session: lock refresh failed", slog.Any("err", err))
				}
			}
		}
	}()

	return g, nil
}

// Release stops the refresh goroutine and releases the lock.
func (g *ConcurrentGuard) Release() {
	if g == nil {
		return
	}
	select {
	case <-g.done:
	default:
		close(g.done)
	}
	g.lock.Release()
}
