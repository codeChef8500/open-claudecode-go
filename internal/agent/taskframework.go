package agent

import (
	"fmt"
	"sync"
	"time"
)

const (
	// panelGraceMs is the delay before an evicted terminal task is removed
	// from the registry (mirrors PANEL_GRACE_MS in the TS source).
	panelGraceMs = 30_000

	// maxUIMessages is the cap for messages kept in a single task's UI buffer.
	maxUIMessages = 50
)

// TaskFramework is the central registry for all agent tasks in a session.
// It handles registration, state transitions, grace-period eviction, and
// per-task UI message buffers.
type TaskFramework struct {
	mu    sync.Mutex
	tasks map[string]*FrameworkTask
}

// FrameworkTask extends AgentTask with UI-level metadata.
type FrameworkTask struct {
	AgentTask

	// Retain prevents grace-period eviction (user is viewing this task).
	Retain bool
	// UIMessages is a capped ring-buffer of recent messages for the TUI.
	UIMessages []string
	// evictAt is non-zero when the task is scheduled for eviction.
	evictAt time.Time
	// LatestInputTokens is updated cumulatively each turn.
	LatestInputTokens int
	// CumulativeOutputTokens accumulates output tokens across all turns.
	CumulativeOutputTokens int
	// RecentActivity holds the latest N tool activity descriptions.
	RecentActivity []string
	// Description stores task details for task management tools.
	Description string
	// Priority stores task priority (high/medium/low).
	Priority string
	// CreatedAt is when the task was created in the task registry.
	CreatedAt time.Time
	// PendingMessages queues follow-up messages for background tasks.
	PendingMessages []string
	// Notified prevents duplicate terminal notifications.
	Notified bool
}

// NewTaskFramework creates an empty TaskFramework.
func NewTaskFramework() *TaskFramework {
	return &TaskFramework{tasks: make(map[string]*FrameworkTask)}
}

// Register adds a new task (or replaces an existing one — resume scenario).
// Fields marked as carry-forward (Retain, UIMessages, startTime) are preserved
// when replacing.
func (f *TaskFramework) Register(def AgentDefinition) *FrameworkTask {
	f.mu.Lock()
	defer f.mu.Unlock()

	task := &FrameworkTask{
		AgentTask: AgentTask{
			Definition: def,
			Status:     AgentStatusPending,
		},
	}

	// Resume: carry forward UI state from the previous task entry.
	if prev, ok := f.tasks[def.AgentID]; ok {
		task.Retain = prev.Retain
		task.UIMessages = prev.UIMessages
		task.AgentTask.StartedAt = prev.AgentTask.StartedAt
		task.Description = prev.Description
		task.Priority = prev.Priority
		task.CreatedAt = prev.CreatedAt
		task.PendingMessages = prev.PendingMessages
		task.Notified = prev.Notified
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now()
	}

	f.tasks[def.AgentID] = task
	return task
}

// GetTask returns the FrameworkTask for agentID.
func (f *TaskFramework) GetTask(agentID string) (*FrameworkTask, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[agentID]
	return t, ok
}

// All returns a snapshot of all registered tasks.
func (f *TaskFramework) All() []*FrameworkTask {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*FrameworkTask, 0, len(f.tasks))
	for _, t := range f.tasks {
		out = append(out, t)
	}
	return out
}

// UpdateTokens updates the token counters for a task.
// inputTokens is the latest cumulative count; outputTokens is incremental.
func (f *TaskFramework) UpdateTokens(agentID string, inputTokens, outputTokensDelta int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[agentID]
	if !ok {
		return fmt.Errorf("taskframework: task %q not found", agentID)
	}
	t.LatestInputTokens = inputTokens
	t.CumulativeOutputTokens += outputTokensDelta
	return nil
}

// AppendActivity prepends a new activity description and keeps at most 5
// entries (sliding window matching the TS implementation).
func (f *TaskFramework) AppendActivity(agentID, activity string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[agentID]
	if !ok {
		return
	}
	t.RecentActivity = append([]string{activity}, t.RecentActivity...)
	if len(t.RecentActivity) > 5 {
		t.RecentActivity = t.RecentActivity[:5]
	}
}

// AppendUIMessage adds a message to the task's capped UI buffer.
func (f *TaskFramework) AppendUIMessage(agentID, msg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[agentID]
	if !ok {
		return
	}
	t.UIMessages = appendCapped(t.UIMessages, msg, maxUIMessages)
}

// SetRetain controls whether the task is protected from grace-period eviction.
func (f *TaskFramework) SetRetain(agentID string, retain bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.tasks[agentID]; ok {
		t.Retain = retain
	}
}

// ScheduleEviction marks a terminal task for removal after the grace period.
// If retain is set or the task is not in a terminal state, eviction is skipped.
func (f *TaskFramework) ScheduleEviction(agentID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[agentID]
	if !ok || t.Retain {
		return
	}
	if t.Status != AgentStatusDone && t.Status != AgentStatusFailed && t.Status != AgentStatusCancelled {
		return
	}
	t.evictAt = time.Now().Add(time.Duration(panelGraceMs) * time.Millisecond)
}

// PruneEvicted removes all tasks whose eviction deadline has passed.
func (f *TaskFramework) PruneEvicted() {
	now := time.Now()
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, t := range f.tasks {
		if !t.evictAt.IsZero() && now.After(t.evictAt) && !t.Retain {
			delete(f.tasks, id)
		}
	}
}

// appendCapped appends item to slice and trims to maxLen from the front
// (oldest messages are dropped).
func appendCapped(s []string, item string, maxLen int) []string {
	s = append(s, item)
	if len(s) > maxLen {
		s = s[len(s)-maxLen:]
	}
	return s
}

func taskStatusString(status AgentStatus) string {
	switch status {
	case AgentStatusPending:
		return "pending"
	case AgentStatusRunning:
		return "in_progress"
	case AgentStatusDone:
		return "completed"
	case AgentStatusCancelled:
		return "cancelled"
	case AgentStatusFailed:
		return "failed"
	default:
		return string(status)
	}
}

// QueuePendingMessage appends a message for later delivery to a background task.
func (f *TaskFramework) QueuePendingMessage(agentID, msg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[agentID]
	if !ok {
		return fmt.Errorf("taskframework: task %q not found", agentID)
	}
	t.PendingMessages = append(t.PendingMessages, msg)
	return nil
}

// DrainPendingMessages drains queued messages for a background task.
func (f *TaskFramework) DrainPendingMessages(agentID string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[agentID]
	if !ok || len(t.PendingMessages) == 0 {
		return nil
	}
	drained := append([]string(nil), t.PendingMessages...)
	t.PendingMessages = nil
	return drained
}

// MarkNotified marks a task as having emitted its terminal notification.
// Returns true only on the first transition to notified.
func (f *TaskFramework) MarkNotified(agentID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[agentID]
	if !ok || t.Notified {
		return false
	}
	t.Notified = true
	return true
}

// ── engine.TaskRegistry adapter ───────────────────────────────────────────────
// TaskFramework implements engine.TaskRegistry so the task tools can use it
// through UseContext.TaskRegistry without an import cycle.

func (f *TaskFramework) Create(id, title, description, priority string) {
	def := AgentDefinition{
		AgentID: id,
		Task:    title,
	}
	t := f.Register(def)
	f.mu.Lock()
	defer f.mu.Unlock()
	t.Description = description
	if priority == "" {
		priority = "medium"
	}
	t.Priority = priority
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
}

func (f *TaskFramework) Update(id string, fields map[string]interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}
	if s, ok := fields["status"].(string); ok {
		switch s {
		case "in_progress":
			t.Status = AgentStatusRunning
			if t.StartedAt.IsZero() {
				t.StartedAt = time.Now()
			}
		case "completed":
			t.Status = AgentStatusDone
			t.FinishedAt = time.Now()
		case "cancelled":
			t.Status = AgentStatusCancelled
			t.FinishedAt = time.Now()
		}
	}
	if title, ok := fields["title"].(string); ok {
		t.Definition.Task = title
	}
	if description, ok := fields["description"].(string); ok {
		t.Description = description
	}
	if priority, ok := fields["priority"].(string); ok && priority != "" {
		t.Priority = priority
	}
	return nil
}

func (f *TaskFramework) Get(id string) (map[string]interface{}, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok {
		return nil, false
	}
	return map[string]interface{}{
		"id":                       t.Definition.AgentID,
		"title":                    t.Definition.Task,
		"description":              t.Description,
		"priority":                 t.Priority,
		"status":                   taskStatusString(t.Status),
		"created_at":               t.CreatedAt.Format(time.RFC3339),
		"latest_input_tokens":      t.LatestInputTokens,
		"cumulative_output_tokens": t.CumulativeOutputTokens,
	}, true
}

func (f *TaskFramework) List() []map[string]interface{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]map[string]interface{}, 0, len(f.tasks))
	for _, t := range f.tasks {
		out = append(out, map[string]interface{}{
			"id":          t.Definition.AgentID,
			"title":       t.Definition.Task,
			"description": t.Description,
			"priority":    t.Priority,
			"status":      taskStatusString(t.Status),
			"created_at":  t.CreatedAt.Format(time.RFC3339),
		})
	}
	return out
}
