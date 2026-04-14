package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Async agent lifecycle management aligned with claude-code-main's
// asyncAgentLifecycle.ts.
//
// The async lifecycle wraps the AgentRunner for background agent execution,
// providing:
//   - Goroutine-based concurrent execution with proper cancellation
//   - Progress tracking and notification injection
//   - Graceful shutdown with timeout
//   - Result collection and error handling

// AsyncAgent represents a background agent execution.
type AsyncAgent struct {
	mu sync.RWMutex

	AgentID    string
	Definition AgentDefinition
	Status     AsyncAgentStatus
	StartedAt  time.Time
	FinishedAt time.Time

	// result is populated when the agent finishes.
	result *AgentRunResult
	// cancel cancels the agent's context.
	cancel context.CancelFunc
	// done is closed when the agent goroutine exits.
	done chan struct{}
	// notifications collects notification messages for the parent.
	notifications *NotificationQueue
	// progress tracks incremental progress.
	progress *ProgressTracker
}

// AsyncAgentStatus represents the lifecycle state of an async agent.
type AsyncAgentStatus string

const (
	AsyncStatusPending   AsyncAgentStatus = "pending"
	AsyncStatusRunning   AsyncAgentStatus = "running"
	AsyncStatusDone      AsyncAgentStatus = "done"
	AsyncStatusFailed    AsyncAgentStatus = "failed"
	AsyncStatusCancelled AsyncAgentStatus = "cancelled"
)

// AsyncLifecycleManager manages background agent executions.
type AsyncLifecycleManager struct {
	mu     sync.RWMutex
	agents map[string]*AsyncAgent
	runner *AgentRunner
	// globalSink receives completion notifications for the parent/main loop.
	globalSink *NotificationQueue
}

// NewAsyncLifecycleManager creates a new async lifecycle manager.
func NewAsyncLifecycleManager(runner *AgentRunner) *AsyncLifecycleManager {
	return &AsyncLifecycleManager{
		agents: make(map[string]*AsyncAgent),
		runner: runner,
	}
}

// SetGlobalNotificationSink sets the queue where completion notifications
// are forwarded for the parent/main engine loop to consume.
func (m *AsyncLifecycleManager) SetGlobalNotificationSink(q *NotificationQueue) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.globalSink = q
}

// Launch starts a new background agent execution.
// Returns the agent ID immediately; the agent runs in a goroutine.
func (m *AsyncLifecycleManager) Launch(parentCtx context.Context, params RunAgentParams) (string, error) {
	params.Background = true

	// Create a cancellable context for the agent.
	ctx, cancel := context.WithCancel(parentCtx)

	agentID := params.ExistingAgentID
	if agentID == "" {
		agentID = fmt.Sprintf("async-%d", time.Now().UnixNano())
	}
	params.ExistingAgentID = agentID

	var def AgentDefinition
	if params.AgentDef != nil {
		def = *params.AgentDef
	}

	agent := &AsyncAgent{
		AgentID:       agentID,
		Definition:    def,
		Status:        AsyncStatusPending,
		StartedAt:     time.Now(),
		cancel:        cancel,
		done:          make(chan struct{}),
		notifications: NewNotificationQueue(100),
		progress:      NewProgressTracker(agentID),
	}

	m.mu.Lock()
	m.agents[agentID] = agent
	m.mu.Unlock()

	// Pass the notification queue through to the runner so that
	// notifications from the executeLoop are collected here.
	params.NotificationQueue = agent.notifications

	// Launch the agent in a goroutine.
	go m.runAsync(ctx, agent, params)

	slog.Info("async lifecycle: launched",
		slog.String("agent_id", agentID),
		slog.String("type", agent.Definition.AgentType),
	)

	return agentID, nil
}

// runAsync executes the agent and updates status on completion.
func (m *AsyncLifecycleManager) runAsync(ctx context.Context, agent *AsyncAgent, params RunAgentParams) {
	defer close(agent.done)

	agent.mu.Lock()
	agent.Status = AsyncStatusRunning
	agent.mu.Unlock()

	// Run the agent via the runner.
	result := m.runner.RunAgent(ctx, params)

	agent.mu.Lock()
	agent.result = result
	agent.FinishedAt = time.Now()

	switch {
	case ctx.Err() != nil:
		agent.Status = AsyncStatusCancelled
		if m.runner != nil && m.runner.taskManager != nil {
			_ = m.runner.taskManager.MarkCancelled(agent.AgentID)
		}
	case result.Error != nil:
		agent.Status = AsyncStatusFailed
	default:
		agent.Status = AsyncStatusDone
	}
	agent.mu.Unlock()

	if m.runner != nil && m.runner.taskFramework != nil {
		if !m.runner.taskFramework.MarkNotified(agent.AgentID) {
			slog.Info("async lifecycle: skipping duplicate terminal notification",
				slog.String("agent_id", agent.AgentID))
			return
		}
	}

	// Push completion notification to agent's own queue.
	// Populate extended fields for TS-aligned task-notification XML.
	duration := agent.FinishedAt.Sub(agent.StartedAt)
	notif := Notification{
		Type:        NotificationTypeComplete,
		AgentID:     agent.AgentID,
		Description: params.Description,
		Message:     formatCompletionNotification(agent),
		Usage: &NotificationUsage{
			DurationMs: int(duration.Milliseconds()),
		},
	}
	if result.TurnCount > 0 {
		notif.Usage.ToolUses = result.TurnCount
	}
	agent.notifications.Push(notif)

	// Also push to global sink for parent/main loop injection.
	if m.globalSink != nil {
		m.globalSink.Push(notif)
	}

	slog.Info("async lifecycle: finished",
		slog.String("agent_id", agent.AgentID),
		slog.String("status", string(agent.Status)),
		slog.Duration("duration", result.Duration),
	)
}

// Cancel requests cancellation of a background agent.
func (m *AsyncLifecycleManager) Cancel(agentID string) error {
	m.mu.RLock()
	agent, ok := m.agents[agentID]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("async agent not found: %s", agentID)
	}

	agent.cancel()
	return nil
}

// Wait blocks until the agent finishes or the timeout expires.
func (m *AsyncLifecycleManager) Wait(agentID string, timeout time.Duration) (*AgentRunResult, error) {
	m.mu.RLock()
	agent, ok := m.agents[agentID]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("async agent not found: %s", agentID)
	}

	select {
	case <-agent.done:
		agent.mu.RLock()
		defer agent.mu.RUnlock()
		return agent.result, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for agent %s", agentID)
	}
}

// GetStatus returns the current status of an async agent.
func (m *AsyncLifecycleManager) GetStatus(agentID string) (AsyncAgentStatus, error) {
	m.mu.RLock()
	agent, ok := m.agents[agentID]
	m.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("async agent not found: %s", agentID)
	}

	agent.mu.RLock()
	defer agent.mu.RUnlock()
	return agent.Status, nil
}

// GetResult returns the result of a completed async agent.
func (m *AsyncLifecycleManager) GetResult(agentID string) (*AgentRunResult, error) {
	m.mu.RLock()
	agent, ok := m.agents[agentID]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("async agent not found: %s", agentID)
	}

	agent.mu.RLock()
	defer agent.mu.RUnlock()

	if agent.Status == AsyncStatusRunning || agent.Status == AsyncStatusPending {
		return nil, fmt.Errorf("agent %s is still running", agentID)
	}

	return agent.result, nil
}

// PushNotification pushes a notification into a specific agent's queue.
func (m *AsyncLifecycleManager) PushNotification(agentID string, n Notification) {
	m.mu.RLock()
	agent, ok := m.agents[agentID]
	m.mu.RUnlock()

	if !ok {
		return
	}

	agent.notifications.Push(n)
}

// QueuePendingMessage enqueues a follow-up message for a running/stopped agent.
// Returns false if the agent or task framework is unavailable.
func (m *AsyncLifecycleManager) QueuePendingMessage(agentID, message string) bool {
	if m == nil || m.runner == nil || m.runner.taskFramework == nil {
		return false
	}
	if err := m.runner.taskFramework.QueuePendingMessage(agentID, message); err != nil {
		return false
	}
	return true
}

// DrainNotifications returns and clears pending notifications for an agent.
func (m *AsyncLifecycleManager) DrainNotifications(agentID string) []Notification {
	m.mu.RLock()
	agent, ok := m.agents[agentID]
	m.mu.RUnlock()

	if !ok {
		return nil
	}

	return agent.notifications.DrainAll()
}

// ActiveAgents returns the list of currently running async agents.
func (m *AsyncLifecycleManager) ActiveAgents() []*AsyncAgent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var active []*AsyncAgent
	for _, agent := range m.agents {
		agent.mu.RLock()
		if agent.Status == AsyncStatusRunning || agent.Status == AsyncStatusPending {
			active = append(active, agent)
		}
		agent.mu.RUnlock()
	}
	return active
}

// AllAgents returns all tracked async agents.
func (m *AsyncLifecycleManager) AllAgents() []*AsyncAgent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	agents := make([]*AsyncAgent, 0, len(m.agents))
	for _, a := range m.agents {
		agents = append(agents, a)
	}
	return agents
}

// PeerInfo holds a snapshot of an async agent's status for external consumers.
type PeerInfo struct {
	AgentID   string
	Status    string
	StartedAt time.Time
	Duration  time.Duration
	AgentType string
}

// ListPeerInfos returns status info for all tracked agents.
func (m *AsyncLifecycleManager) ListPeerInfos() []PeerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]PeerInfo, 0, len(m.agents))
	for _, a := range m.agents {
		a.mu.RLock()
		info := PeerInfo{
			AgentID:   a.AgentID,
			Status:    string(a.Status),
			StartedAt: a.StartedAt,
			AgentType: a.Definition.AgentType,
		}
		if !a.FinishedAt.IsZero() {
			info.Duration = a.FinishedAt.Sub(a.StartedAt)
		}
		a.mu.RUnlock()
		infos = append(infos, info)
	}
	return infos
}

// Resume re-launches a stopped/completed agent with a new prompt message.
// Aligned with TS resumeAgentBackground() in SendMessageTool.ts:824.
// Returns the (same) agent ID on success.
func (m *AsyncLifecycleManager) Resume(parentCtx context.Context, agentID, prompt string) (string, error) {
	m.mu.RLock()
	existing, ok := m.agents[agentID]
	m.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("agent %s not found for resume", agentID)
	}

	existing.mu.RLock()
	status := existing.Status
	existing.mu.RUnlock()

	// Only resume stopped agents.
	if status == AsyncStatusRunning || status == AsyncStatusPending {
		return "", fmt.Errorf("agent %s is still running, use PushNotification instead", agentID)
	}

	// Re-use the original agent definition.
	params := RunAgentParams{
		AgentDef:        &existing.Definition,
		Task:            prompt,
		Background:      true,
		ExistingAgentID: agentID,
		Description:     existing.Definition.AgentType + " (resumed)",
	}

	// Create fresh context and agent state.
	ctx, cancel := context.WithCancel(parentCtx)
	resumed := &AsyncAgent{
		AgentID:       agentID,
		Definition:    existing.Definition,
		Status:        AsyncStatusPending,
		StartedAt:     time.Now(),
		cancel:        cancel,
		done:          make(chan struct{}),
		notifications: NewNotificationQueue(100),
		progress:      NewProgressTracker(agentID),
	}

	m.mu.Lock()
	m.agents[agentID] = resumed // replace old entry
	m.mu.Unlock()

	params.NotificationQueue = resumed.notifications
	go m.runAsync(ctx, resumed, params)

	slog.Info("async lifecycle: resumed agent",
		slog.String("agent_id", agentID))

	return agentID, nil
}

// Cleanup removes finished agents from tracking.
func (m *AsyncLifecycleManager) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, agent := range m.agents {
		agent.mu.RLock()
		done := agent.Status == AsyncStatusDone ||
			agent.Status == AsyncStatusFailed ||
			agent.Status == AsyncStatusCancelled
		agent.mu.RUnlock()

		if done {
			delete(m.agents, id)
		}
	}
}

// ShutdownAll cancels all running agents and waits for them to finish.
func (m *AsyncLifecycleManager) ShutdownAll(timeout time.Duration) {
	m.mu.RLock()
	agents := make([]*AsyncAgent, 0, len(m.agents))
	for _, a := range m.agents {
		agents = append(agents, a)
	}
	m.mu.RUnlock()

	// Cancel all.
	for _, a := range agents {
		a.cancel()
	}

	// Wait for all with timeout.
	deadline := time.After(timeout)
	for _, a := range agents {
		select {
		case <-a.done:
		case <-deadline:
			slog.Warn("async lifecycle: shutdown timeout, some agents still running")
			return
		}
	}
}

// GetProgress returns the current progress snapshot for an async agent.
func (m *AsyncLifecycleManager) GetProgress(agentID string) (*ProgressState, error) {
	m.mu.RLock()
	agent, ok := m.agents[agentID]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("async agent not found: %s", agentID)
	}

	if agent.progress == nil {
		return nil, nil
	}
	state := agent.progress.State()
	return &state, nil
}

// DrainAllNotifications returns and clears pending notifications from ALL agents.
// Used by the parent to check all child progress at once.
func (m *AsyncLifecycleManager) DrainAllNotifications() []Notification {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var all []Notification
	for _, a := range m.agents {
		notifs := a.notifications.DrainAll()
		all = append(all, notifs...)
	}
	return all
}

// SummarizeAgent generates a compact summary of the async agent's execution.
// Used when the parent needs to include the agent result in its own context.
func (m *AsyncLifecycleManager) SummarizeAgent(agentID string, maxChars int) (string, error) {
	m.mu.RLock()
	agent, ok := m.agents[agentID]
	m.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("async agent not found: %s", agentID)
	}

	agent.mu.RLock()
	defer agent.mu.RUnlock()

	if agent.result == nil {
		return fmt.Sprintf("Agent %s (%s): still running",
			truncID(agentID), agent.Definition.AgentType), nil
	}

	output := agent.result.Output
	if maxChars > 0 && len(output) > maxChars {
		output = TruncateSubagentOutput(output, maxChars)
	}

	duration := agent.FinishedAt.Sub(agent.StartedAt).Round(time.Second)

	switch agent.Status {
	case AsyncStatusDone:
		return fmt.Sprintf("Agent %s (%s) completed in %s (%d turns):\n%s",
			truncID(agentID), agent.Definition.AgentType, duration,
			agent.result.TurnCount, output), nil
	case AsyncStatusFailed:
		errMsg := "unknown"
		if agent.result.Error != nil {
			errMsg = agent.result.Error.Error()
		}
		return fmt.Sprintf("Agent %s (%s) failed after %s: %s",
			truncID(agentID), agent.Definition.AgentType, duration, errMsg), nil
	case AsyncStatusCancelled:
		return fmt.Sprintf("Agent %s (%s) cancelled after %s",
			truncID(agentID), agent.Definition.AgentType, duration), nil
	default:
		return fmt.Sprintf("Agent %s: %s", truncID(agentID), agent.Status), nil
	}
}

// WaitForAny blocks until at least one of the specified agents finishes
// or the timeout expires. Returns the IDs of finished agents.
func (m *AsyncLifecycleManager) WaitForAny(agentIDs []string, timeout time.Duration) []string {
	deadline := time.After(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return nil
		case <-ticker.C:
			var finished []string
			m.mu.RLock()
			for _, id := range agentIDs {
				if a, ok := m.agents[id]; ok {
					a.mu.RLock()
					if a.Status == AsyncStatusDone || a.Status == AsyncStatusFailed || a.Status == AsyncStatusCancelled {
						finished = append(finished, id)
					}
					a.mu.RUnlock()
				}
			}
			m.mu.RUnlock()
			if len(finished) > 0 {
				return finished
			}
		}
	}
}

// RunAsyncAgentLifecycle is the top-level convenience function that launches
// an async agent lifecycle, monitors it with progress tracking, and returns
// the result when complete. Aligned with claude-code-main's runAsyncAgentLifecycle.
func RunAsyncAgentLifecycle(
	ctx context.Context,
	mgr *AsyncLifecycleManager,
	params RunAgentParams,
	onProgress func(agentID string, notif Notification),
) (*AgentRunResult, error) {
	agentID, err := mgr.Launch(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("launch async agent: %w", err)
	}

	// Poll for completion with progress callbacks.
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = mgr.Cancel(agentID)
			return nil, ctx.Err()
		case <-ticker.C:
			// Drain and forward notifications.
			if onProgress != nil {
				notifs := mgr.DrainNotifications(agentID)
				for _, n := range notifs {
					onProgress(agentID, n)
				}
			}

			// Check completion.
			status, err := mgr.GetStatus(agentID)
			if err != nil {
				return nil, err
			}
			if status == AsyncStatusDone || status == AsyncStatusFailed || status == AsyncStatusCancelled {
				return mgr.GetResult(agentID)
			}
		}
	}
}

// formatCompletionNotification creates a notification message for a completed agent.
func formatCompletionNotification(agent *AsyncAgent) string {
	agent.mu.RLock()
	defer agent.mu.RUnlock()

	duration := agent.FinishedAt.Sub(agent.StartedAt).Round(time.Second)

	switch agent.Status {
	case AsyncStatusDone:
		output := ""
		if agent.result != nil {
			output = agent.result.Output
			if len(output) > 500 {
				output = TruncateSubagentOutput(output, 500)
			}
		}
		return fmt.Sprintf("Agent %s (%s) completed in %s:\n%s",
			truncID(agent.AgentID), agent.Definition.AgentType, duration, output)

	case AsyncStatusFailed:
		errMsg := "unknown error"
		if agent.result != nil && agent.result.Error != nil {
			errMsg = agent.result.Error.Error()
		}
		return fmt.Sprintf("Agent %s (%s) failed after %s: %s",
			truncID(agent.AgentID), agent.Definition.AgentType, duration, errMsg)

	case AsyncStatusCancelled:
		return fmt.Sprintf("Agent %s (%s) was cancelled after %s",
			truncID(agent.AgentID), agent.Definition.AgentType, duration)

	default:
		return fmt.Sprintf("Agent %s (%s) finished with status %s",
			truncID(agent.AgentID), agent.Definition.AgentType, agent.Status)
	}
}
