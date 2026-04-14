package agent

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// DreamTask represents a deferred/scheduled agent task.
// Aligned with claude-code-main's DreamTask/DreamTask.ts (~5K lines).
//
// Dream tasks allow scheduling agent execution at a future time or
// after a specified delay, useful for background monitoring, periodic
// checks, and deferred work.

// DreamTaskState captures the state of a scheduled dream task.
type DreamTaskState struct {
	TaskID      string           `json:"task_id"`
	Description string           `json:"description"`
	AgentDef    *AgentDefinition `json:"agent_def,omitempty"`
	Prompt      string           `json:"prompt"`
	Status      AgentStatus      `json:"status"`
	ScheduledAt time.Time        `json:"scheduled_at"`
	CreatedAt   time.Time        `json:"created_at"`
	ExecutedAt  time.Time        `json:"executed_at,omitempty"`
	Error       string           `json:"error,omitempty"`
	DelayMs     int64            `json:"delay_ms,omitempty"`
}

// DreamTaskManager manages scheduled/deferred agent tasks.
type DreamTaskManager struct {
	mu        sync.Mutex
	tasks     map[string]*DreamTaskState
	timers    map[string]*time.Timer
	runner    *AgentRunner
	asyncMgr  *AsyncLifecycleManager
	onExecute func(taskID string, result *AgentRunResult)
}

// NewDreamTaskManager creates a new dream task manager.
func NewDreamTaskManager(runner *AgentRunner, asyncMgr *AsyncLifecycleManager) *DreamTaskManager {
	return &DreamTaskManager{
		tasks:    make(map[string]*DreamTaskState),
		timers:   make(map[string]*time.Timer),
		runner:   runner,
		asyncMgr: asyncMgr,
	}
}

// SetOnExecute registers a callback for when a dream task executes.
func (dm *DreamTaskManager) SetOnExecute(fn func(taskID string, result *AgentRunResult)) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	dm.onExecute = fn
}

// Schedule creates a new dream task that will execute after the specified delay.
func (dm *DreamTaskManager) Schedule(taskID, prompt, description string, delay time.Duration, agentDef *AgentDefinition) (*DreamTaskState, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if _, exists := dm.tasks[taskID]; exists {
		return nil, fmt.Errorf("dream task %q already exists", taskID)
	}

	now := time.Now()
	state := &DreamTaskState{
		TaskID:      taskID,
		Description: description,
		AgentDef:    agentDef,
		Prompt:      prompt,
		Status:      AgentStatusPending,
		ScheduledAt: now.Add(delay),
		CreatedAt:   now,
		DelayMs:     delay.Milliseconds(),
	}

	dm.tasks[taskID] = state

	// Schedule the timer.
	timer := time.AfterFunc(delay, func() {
		dm.execute(taskID)
	})
	dm.timers[taskID] = timer

	slog.Info("dream_task: scheduled",
		slog.String("task_id", taskID),
		slog.Duration("delay", delay),
		slog.String("description", description),
	)

	return state, nil
}

// Cancel cancels a pending dream task.
func (dm *DreamTaskManager) Cancel(taskID string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	state, ok := dm.tasks[taskID]
	if !ok {
		return fmt.Errorf("dream task %q not found", taskID)
	}

	if state.Status != AgentStatusPending {
		return fmt.Errorf("dream task %q is not pending (status: %s)", taskID, state.Status)
	}

	// Stop the timer.
	if timer, ok := dm.timers[taskID]; ok {
		timer.Stop()
		delete(dm.timers, taskID)
	}

	state.Status = AgentStatusCancelled
	return nil
}

// Get returns a dream task by ID.
func (dm *DreamTaskManager) Get(taskID string) (*DreamTaskState, bool) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	s, ok := dm.tasks[taskID]
	return s, ok
}

// ListPending returns all pending dream tasks.
func (dm *DreamTaskManager) ListPending() []*DreamTaskState {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	var pending []*DreamTaskState
	for _, s := range dm.tasks {
		if s.Status == AgentStatusPending {
			pending = append(pending, s)
		}
	}
	return pending
}

// CancelAll cancels all pending dream tasks and stops their timers.
func (dm *DreamTaskManager) CancelAll() int {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	cancelled := 0
	for id, state := range dm.tasks {
		if state.Status == AgentStatusPending {
			state.Status = AgentStatusCancelled
			if timer, ok := dm.timers[id]; ok {
				timer.Stop()
				delete(dm.timers, id)
			}
			cancelled++
		}
	}
	return cancelled
}

// execute runs a dream task when its timer fires.
func (dm *DreamTaskManager) execute(taskID string) {
	dm.mu.Lock()
	state, ok := dm.tasks[taskID]
	if !ok || state.Status != AgentStatusPending {
		dm.mu.Unlock()
		return
	}
	state.Status = AgentStatusRunning
	state.ExecutedAt = time.Now()
	delete(dm.timers, taskID)
	onExec := dm.onExecute
	dm.mu.Unlock()

	slog.Info("dream_task: executing",
		slog.String("task_id", taskID),
	)

	// Build run params from the dream task state.
	params := RunAgentParams{
		AgentDef:        state.AgentDef,
		Task:            state.Prompt,
		Background:      true,
		ExistingAgentID: taskID,
		Description:     state.Description,
	}

	// Try async launch first, then sync fallback.
	var result *AgentRunResult
	if dm.asyncMgr != nil {
		_, err := dm.asyncMgr.Launch(nil, params)
		if err != nil {
			result = &AgentRunResult{
				AgentID: taskID,
				Error:   err,
				Status:  AgentStatusFailed,
			}
		} else {
			result = &AgentRunResult{
				AgentID: taskID,
				Status:  AgentStatusRunning,
			}
		}
	} else if dm.runner != nil {
		result = dm.runner.RunAgent(nil, params)
	} else {
		result = &AgentRunResult{
			AgentID: taskID,
			Error:   fmt.Errorf("no runner configured for dream task"),
			Status:  AgentStatusFailed,
		}
	}

	// Update state.
	dm.mu.Lock()
	if result.Error != nil {
		state.Status = AgentStatusFailed
		state.Error = result.Error.Error()
	} else if result.Status == AgentStatusDone {
		state.Status = AgentStatusDone
	}
	dm.mu.Unlock()

	// Notify callback.
	if onExec != nil {
		onExec(taskID, result)
	}
}
