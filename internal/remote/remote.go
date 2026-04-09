// Package remote provides the framework for remote agent execution.
//
// Aligned with claude-code-main remote/ — enables running agents in
// remote containers (Docker, cloud VMs) with bidirectional streaming.
package remote

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// ────────────────────────────────────────────────────────────────────────────
// Remote execution framework
// ────────────────────────────────────────────────────────────────────────────

// RuntimeType identifies the container runtime.
type RuntimeType string

const (
	RuntimeDocker RuntimeType = "docker"
	RuntimeSSH    RuntimeType = "ssh"
	RuntimeLocal  RuntimeType = "local"
)

// Config configures a remote execution environment.
type Config struct {
	Runtime    RuntimeType       `json:"runtime"`
	Image      string            `json:"image,omitempty"`
	Host       string            `json:"host,omitempty"`
	Port       int               `json:"port,omitempty"`
	WorkDir    string            `json:"work_dir,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Timeout    time.Duration     `json:"timeout,omitempty"`
	MountPaths []string          `json:"mount_paths,omitempty"`
}

// RemoteSession represents an active remote execution session.
type RemoteSession struct {
	mu        sync.Mutex
	id        string
	cfg       Config
	status    SessionStatus
	startedAt time.Time
	output    []byte
}

// SessionStatus tracks the lifecycle of a remote session.
type SessionStatus string

const (
	StatusPending    SessionStatus = "pending"
	StatusStarting   SessionStatus = "starting"
	StatusRunning    SessionStatus = "running"
	StatusCompleted  SessionStatus = "completed"
	StatusFailed     SessionStatus = "failed"
	StatusCancelled  SessionStatus = "cancelled"
)

// Manager orchestrates remote execution sessions.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*RemoteSession
}

// NewManager creates a new remote execution manager.
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*RemoteSession),
	}
}

// StartSession begins a new remote execution session.
func (m *Manager) StartSession(ctx context.Context, id string, cfg Config) (*RemoteSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[id]; exists {
		return nil, fmt.Errorf("remote: session %q already exists", id)
	}

	session := &RemoteSession{
		id:        id,
		cfg:       cfg,
		status:    StatusPending,
		startedAt: time.Now(),
	}
	m.sessions[id] = session

	slog.Info("remote: session created",
		slog.String("id", id),
		slog.String("runtime", string(cfg.Runtime)))

	// Start execution in background.
	go m.runSession(ctx, session)

	return session, nil
}

// GetSession returns a session by ID.
func (m *Manager) GetSession(id string) (*RemoteSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

// CancelSession cancels a running session.
func (m *Manager) CancelSession(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[id]
	if !ok {
		return fmt.Errorf("remote: session %q not found", id)
	}

	s.mu.Lock()
	s.status = StatusCancelled
	s.mu.Unlock()

	slog.Info("remote: session cancelled", slog.String("id", id))
	return nil
}

// ListSessions returns all active sessions.
func (m *Manager) ListSessions() []*RemoteSession {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]*RemoteSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s)
	}
	return result
}

func (m *Manager) runSession(ctx context.Context, s *RemoteSession) {
	s.mu.Lock()
	s.status = StatusStarting
	s.mu.Unlock()

	switch s.cfg.Runtime {
	case RuntimeDocker:
		m.runDocker(ctx, s)
	case RuntimeSSH:
		m.runSSH(ctx, s)
	default:
		s.mu.Lock()
		s.status = StatusFailed
		s.mu.Unlock()
		slog.Error("remote: unsupported runtime", slog.String("runtime", string(s.cfg.Runtime)))
	}
}

func (m *Manager) runDocker(_ context.Context, s *RemoteSession) {
	s.mu.Lock()
	s.status = StatusRunning
	s.mu.Unlock()

	slog.Info("remote: docker execution placeholder",
		slog.String("id", s.id),
		slog.String("image", s.cfg.Image))

	// TODO: Implement Docker container lifecycle:
	// 1. docker run with mounts and env
	// 2. Stream output via attach
	// 3. Detect completion or timeout

	s.mu.Lock()
	s.status = StatusCompleted
	s.mu.Unlock()
}

func (m *Manager) runSSH(_ context.Context, s *RemoteSession) {
	s.mu.Lock()
	s.status = StatusRunning
	s.mu.Unlock()

	slog.Info("remote: SSH execution placeholder",
		slog.String("id", s.id),
		slog.String("host", s.cfg.Host))

	// TODO: Implement SSH session lifecycle:
	// 1. Establish SSH connection
	// 2. Execute commands via session
	// 3. Stream output back

	s.mu.Lock()
	s.status = StatusCompleted
	s.mu.Unlock()
}

// Status returns the current session status.
func (s *RemoteSession) Status() SessionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// ID returns the session identifier.
func (s *RemoteSession) ID() string { return s.id }
