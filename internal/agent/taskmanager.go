package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TaskManager tracks the lifecycle of all sub-agent tasks using a sync.Map.
type TaskManager struct {
	tasks   sync.Map // agentID → *AgentTask
	baseDir string   // directory for task persistence
}

// NewTaskManager creates an empty TaskManager.
func NewTaskManager() *TaskManager { return &TaskManager{} }

// NewTaskManagerWithDir creates a TaskManager with disk persistence support.
func NewTaskManagerWithDir(baseDir string) *TaskManager {
	return &TaskManager{baseDir: baseDir}
}

// EnsureDir creates the task output directory if it doesn't exist.
// Aligned with TS shared task file system pattern.
func (tm *TaskManager) EnsureDir() error {
	if tm.baseDir == "" {
		return nil
	}
	dir := filepath.Join(tm.baseDir, ".claude", "tasks")
	return os.MkdirAll(dir, 0o755)
}

// Reset clears all tracked tasks.
func (tm *TaskManager) Reset() {
	tm.tasks.Range(func(key, _ interface{}) bool {
		tm.tasks.Delete(key)
		return true
	})
}

// Create registers a new task in Pending state.
func (tm *TaskManager) Create(def AgentDefinition) *AgentTask {
	task := &AgentTask{
		Definition: def,
		Status:     AgentStatusPending,
	}
	tm.tasks.Store(def.AgentID, task)
	return task
}

// MarkRunning transitions a task to Running.
func (tm *TaskManager) MarkRunning(agentID string) error {
	return tm.update(agentID, func(t *AgentTask) {
		t.Status = AgentStatusRunning
		t.StartedAt = time.Now()
	})
}

// MarkDone transitions a task to Done with the given output.
func (tm *TaskManager) MarkDone(agentID, output string) error {
	return tm.update(agentID, func(t *AgentTask) {
		t.Status = AgentStatusDone
		t.Output = output
		t.FinishedAt = time.Now()
	})
}

// MarkFailed transitions a task to Failed with an error message.
func (tm *TaskManager) MarkFailed(agentID, errMsg string) error {
	return tm.update(agentID, func(t *AgentTask) {
		t.Status = AgentStatusFailed
		t.Error = errMsg
		t.FinishedAt = time.Now()
	})
}

// MarkCancelled transitions a task to Cancelled.
func (tm *TaskManager) MarkCancelled(agentID string) error {
	return tm.update(agentID, func(t *AgentTask) {
		t.Status = AgentStatusCancelled
		t.FinishedAt = time.Now()
	})
}

// Get returns the task for agentID.
func (tm *TaskManager) Get(agentID string) (*AgentTask, bool) {
	v, ok := tm.tasks.Load(agentID)
	if !ok {
		return nil, false
	}
	return v.(*AgentTask), true
}

// All returns a snapshot of all tasks.
func (tm *TaskManager) All() []*AgentTask {
	var tasks []*AgentTask
	tm.tasks.Range(func(_, v interface{}) bool {
		tasks = append(tasks, v.(*AgentTask))
		return true
	})
	return tasks
}

// Active returns only tasks currently in Running or Pending state.
func (tm *TaskManager) Active() []*AgentTask {
	var active []*AgentTask
	tm.tasks.Range(func(_, v interface{}) bool {
		t := v.(*AgentTask)
		if t.Status == AgentStatusRunning || t.Status == AgentStatusPending {
			active = append(active, t)
		}
		return true
	})
	return active
}

// Count returns the total number of tracked tasks.
func (tm *TaskManager) Count() int {
	count := 0
	tm.tasks.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// ActiveCount returns the number of running or pending tasks.
func (tm *TaskManager) ActiveCount() int {
	count := 0
	tm.tasks.Range(func(_, v interface{}) bool {
		t := v.(*AgentTask)
		if t.Status == AgentStatusRunning || t.Status == AgentStatusPending {
			count++
		}
		return true
	})
	return count
}

// Delete removes a task from the manager.
func (tm *TaskManager) Delete(agentID string) {
	tm.tasks.Delete(agentID)
}

func (tm *TaskManager) update(agentID string, fn func(*AgentTask)) error {
	v, ok := tm.tasks.Load(agentID)
	if !ok {
		return fmt.Errorf("agent task %q not found", agentID)
	}
	fn(v.(*AgentTask))
	return nil
}

// ── Persistence ──────────────────────────────────────────────────────────────

// taskPersistEntry is the serializable form of an AgentTask.
type taskPersistEntry struct {
	AgentID    string      `json:"agent_id"`
	AgentType  string      `json:"agent_type"`
	Status     AgentStatus `json:"status"`
	Task       string      `json:"task"`
	Output     string      `json:"output,omitempty"`
	Error      string      `json:"error,omitempty"`
	StartedAt  time.Time   `json:"started_at,omitempty"`
	FinishedAt time.Time   `json:"finished_at,omitempty"`
}

// SaveToFile persists all tasks to a JSON file for recovery after restart.
func (tm *TaskManager) SaveToFile() error {
	if tm.baseDir == "" {
		return nil
	}
	if err := tm.EnsureDir(); err != nil {
		return err
	}

	var entries []taskPersistEntry
	tm.tasks.Range(func(_, v interface{}) bool {
		t := v.(*AgentTask)
		entries = append(entries, taskPersistEntry{
			AgentID:    t.Definition.AgentID,
			AgentType:  t.Definition.AgentType,
			Status:     t.Status,
			Task:       t.Definition.Task,
			Output:     t.Output,
			Error:      t.Error,
			StartedAt:  t.StartedAt,
			FinishedAt: t.FinishedAt,
		})
		return true
	})

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tasks: %w", err)
	}

	path := filepath.Join(tm.baseDir, ".claude", "tasks", "tasks.json")
	return os.WriteFile(path, data, 0o644)
}

// LoadFromFile restores tasks from a previously saved file.
func (tm *TaskManager) LoadFromFile() error {
	if tm.baseDir == "" {
		return nil
	}

	path := filepath.Join(tm.baseDir, ".claude", "tasks", "tasks.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read tasks: %w", err)
	}

	var entries []taskPersistEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parse tasks: %w", err)
	}

	for _, e := range entries {
		task := &AgentTask{
			Definition: AgentDefinition{
				AgentID:   e.AgentID,
				AgentType: e.AgentType,
				Task:      e.Task,
			},
			Status:     e.Status,
			Output:     e.Output,
			Error:      e.Error,
			StartedAt:  e.StartedAt,
			FinishedAt: e.FinishedAt,
		}
		tm.tasks.Store(e.AgentID, task)
	}

	slog.Info("taskmanager: restored tasks from file",
		slog.Int("count", len(entries)))
	return nil
}

