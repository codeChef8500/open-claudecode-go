package swarm

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// ── InProcessBackend ─────────────────────────────────────────────────────────
//
// TeammateExecutor for same-process teammates running as goroutines.
// Aligned with claude-code-main's InProcessBackend in backends/inProcess.ts.

// InProcessRunnerFunc is the function signature for running an in-process teammate.
// It receives the teammate context and spawn config, and runs until completion or cancellation.
type InProcessRunnerFunc func(ctx context.Context, config TeammateSpawnConfig) error

// InProcessBackend manages in-process teammates.
type InProcessBackend struct {
	mu          sync.RWMutex
	teammates   map[string]*inProcessEntry
	registry    *TeammateRegistry
	mailbox     MailboxAdapter
	runnerFunc  InProcessRunnerFunc
}

type inProcessEntry struct {
	config TeammateSpawnConfig
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

// InProcessBackendConfig configures the in-process backend.
type InProcessBackendConfig struct {
	Registry   *TeammateRegistry
	Mailbox    MailboxAdapter
	RunnerFunc InProcessRunnerFunc
}

// NewInProcessBackend creates a new in-process backend.
func NewInProcessBackend(cfg InProcessBackendConfig) *InProcessBackend {
	return &InProcessBackend{
		teammates:  make(map[string]*inProcessEntry),
		registry:   cfg.Registry,
		mailbox:    cfg.Mailbox,
		runnerFunc: cfg.RunnerFunc,
	}
}

// Spawn creates and starts a new in-process teammate goroutine.
func (b *InProcessBackend) Spawn(ctx context.Context, config TeammateSpawnConfig) (*TeammateSpawnResult, error) {
	agentID := config.Identity.AgentID
	if agentID == "" {
		agentID = FormatAgentID(config.Identity.AgentName, config.Identity.TeamName)
		config.Identity.AgentID = agentID
	}

	b.mu.Lock()
	if _, exists := b.teammates[agentID]; exists {
		b.mu.Unlock()
		return nil, fmt.Errorf("teammate %q already running", agentID)
	}

	childCtx, cancel := context.WithCancel(ctx)
	entry := &inProcessEntry{
		config: config,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	b.teammates[agentID] = entry
	b.mu.Unlock()

	// Register in the global teammate registry.
	if b.registry != nil {
		b.registry.Register(&ActiveTeammate{
			Identity:    config.Identity,
			BackendType: BackendInProcess,
			Cancel:      cancel,
		})
	}

	// Launch the teammate goroutine.
	go func() {
		defer close(entry.done)
		defer func() {
			b.mu.Lock()
			delete(b.teammates, agentID)
			b.mu.Unlock()

			if b.registry != nil {
				b.registry.Unregister(agentID)
			}
			slog.Info("swarm: in-process teammate finished",
				slog.String("agent_id", agentID))
		}()

		if b.runnerFunc != nil {
			entry.err = b.runnerFunc(childCtx, config)
		} else {
			slog.Warn("swarm: no runner func configured, teammate will idle",
				slog.String("agent_id", agentID))
			<-childCtx.Done()
		}
	}()

	return &TeammateSpawnResult{
		Identity:    config.Identity,
		BackendType: BackendInProcess,
	}, nil
}

// SendMessage delivers a message to an in-process teammate via mailbox.
func (b *InProcessBackend) SendMessage(agentID string, message TeammateMessage) error {
	if b.mailbox == nil {
		return fmt.Errorf("mailbox not configured for in-process backend")
	}
	_, err := b.mailbox.Send(message.From, agentID, message.Content, string(message.Priority), message.ReplyTo)
	return err
}

// Terminate requests graceful shutdown of an in-process teammate.
func (b *InProcessBackend) Terminate(agentID string, reason string) error {
	b.mu.RLock()
	entry, ok := b.teammates[agentID]
	b.mu.RUnlock()

	if !ok {
		return fmt.Errorf("teammate %q not found", agentID)
	}

	// Send shutdown request via mailbox.
	if b.mailbox != nil {
		env, err := NewEnvelope(TeamLeadName, agentID, MessageTypeShutdownRequest,
			ShutdownRequestPayload{Reason: reason})
		if err == nil {
			_ = b.mailbox.SendEnvelope(TeamLeadName, agentID, env)
		}
	}

	// Give the teammate time to shut down gracefully; the runner
	// should poll the mailbox and handle shutdown_request.
	// If it doesn't respond, Kill() can be used.
	_ = entry
	return nil
}

// Kill forcefully stops an in-process teammate.
func (b *InProcessBackend) Kill(agentID string) error {
	b.mu.RLock()
	entry, ok := b.teammates[agentID]
	b.mu.RUnlock()

	if !ok {
		return nil // already gone
	}

	entry.cancel()

	// Wait for cleanup.
	<-entry.done
	return nil
}

// IsActive checks if a teammate is still running.
func (b *InProcessBackend) IsActive(agentID string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.teammates[agentID]
	return ok
}

// Type returns the backend type.
func (b *InProcessBackend) Type() BackendType {
	return BackendInProcess
}

// WaitForAll blocks until all in-process teammates have finished.
func (b *InProcessBackend) WaitForAll() {
	b.mu.RLock()
	entries := make([]*inProcessEntry, 0, len(b.teammates))
	for _, e := range b.teammates {
		entries = append(entries, e)
	}
	b.mu.RUnlock()

	for _, e := range entries {
		<-e.done
	}
}

// KillAll forcefully stops all in-process teammates.
func (b *InProcessBackend) KillAll() {
	b.mu.RLock()
	entries := make(map[string]*inProcessEntry, len(b.teammates))
	for k, v := range b.teammates {
		entries[k] = v
	}
	b.mu.RUnlock()

	for _, e := range entries {
		e.cancel()
	}
	for _, e := range entries {
		<-e.done
	}
}
