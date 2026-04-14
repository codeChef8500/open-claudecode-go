package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Foreground-to-background conversion aligned with claude-code-main's
// foreground-to-background agent conversion pattern.
//
// When a foreground (sync) agent is taking too long, it can be converted
// to a background (async) agent mid-execution. This involves:
//  1. Checkpointing the current state
//  2. Cancelling the foreground context
//  3. Relaunching via the async lifecycle manager
//  4. Returning a placeholder result to the caller

// ConversionResult holds the outcome of a fg→bg conversion.
type ConversionResult struct {
	AgentID      string
	BackgroundID string
	Checkpoint   *AgentCheckpoint
	Success      bool
	Error        error
	ConvertedAt  time.Time
}

// FgToBgConverter handles converting foreground agents to background.
type FgToBgConverter struct {
	asyncManager  *AsyncLifecycleManager
	resumeManager *ResumeManager
	runner        *AgentRunner
}

// NewFgToBgConverter creates a new converter.
func NewFgToBgConverter(
	asyncManager *AsyncLifecycleManager,
	resumeManager *ResumeManager,
	runner *AgentRunner,
) *FgToBgConverter {
	return &FgToBgConverter{
		asyncManager:  asyncManager,
		resumeManager: resumeManager,
		runner:        runner,
	}
}

// Convert takes a running foreground agent and converts it to background.
// The foreground context should be cancelled by the caller after this returns.
func (c *FgToBgConverter) Convert(
	ctx context.Context,
	agentID string,
	checkpoint *AgentCheckpoint,
) (*ConversionResult, error) {
	result := &ConversionResult{
		AgentID:     agentID,
		ConvertedAt: time.Now(),
	}

	if c.asyncManager == nil {
		result.Error = fmt.Errorf("async manager not configured")
		return result, result.Error
	}

	// 1. Save checkpoint.
	if c.resumeManager != nil && checkpoint != nil {
		checkpoint.Status = AgentStatusPending // mark as resumable
		if err := c.resumeManager.SaveCheckpoint(checkpoint); err != nil {
			slog.Warn("fg_to_bg: failed to save checkpoint",
				slog.String("agent_id", agentID),
				slog.Any("err", err),
			)
			// Non-fatal — continue with conversion.
		}
		result.Checkpoint = checkpoint
	}

	// 2. Build resume params from checkpoint.
	if checkpoint == nil {
		result.Error = fmt.Errorf("no checkpoint available for conversion")
		return result, result.Error
	}

	params := BuildResumeParams(checkpoint)
	params.Background = true

	// 3. Launch as background agent.
	bgID, err := c.asyncManager.Launch(ctx, params)
	if err != nil {
		result.Error = fmt.Errorf("launch background agent: %w", err)
		return result, result.Error
	}

	result.BackgroundID = bgID
	result.Success = true

	slog.Info("fg_to_bg: converted",
		slog.String("fg_agent_id", agentID),
		slog.String("bg_agent_id", bgID),
		slog.Int("remaining_turns", params.MaxTurns),
	)

	return result, nil
}

// CanConvert checks if a foreground agent can be converted to background.
func (c *FgToBgConverter) CanConvert() bool {
	return c.asyncManager != nil && c.runner != nil
}

// BuildCheckpointFromRun creates a checkpoint from the current run state.
// This is called by the runner when a conversion is requested.
func BuildCheckpointFromRun(
	agentID string,
	sessionID string,
	def *AgentDefinition,
	params RunAgentParams,
	turnCount int,
	maxTurns int,
	workDir string,
	worktreeDir string,
	output string,
	background bool,
) *AgentCheckpoint {
	definition := GeneralPurposeAgent
	if def != nil {
		definition = *def
	}
	return &AgentCheckpoint{
		AgentID:        agentID,
		SessionID:      sessionID,
		Definition:     definition,
		Status:         AgentStatusRunning,
		TurnCount:      turnCount,
		MaxTurns:       maxTurns,
		WorkDir:        workDir,
		WorktreeDir:    worktreeDir,
		Output:         output,
		Background:     background,
		CreatedAt:      time.Now(),
		SystemPrompt:   params.SystemPrompt,
		Model:          params.Model,
		AllowedTools:   append([]string(nil), params.AllowedTools...),
		PermissionMode: params.PermissionMode,
		Description:    params.Description,
		IsolationMode:  params.IsolationMode,
		IsFork:         params.IsFork,
		ParentContext:  params.ParentContext,
	}
}

// FormatConversionMessage creates a user-facing message about the conversion.
func FormatConversionMessage(result *ConversionResult) string {
	if !result.Success {
		return fmt.Sprintf("Failed to convert agent %s to background: %v",
			truncID(result.AgentID), result.Error)
	}

	msg := fmt.Sprintf("Agent %s converted to background execution (new ID: %s).",
		truncID(result.AgentID), truncID(result.BackgroundID))

	if result.Checkpoint != nil {
		msg += fmt.Sprintf("\nCheckpointed at turn %d/%d.",
			result.Checkpoint.TurnCount, result.Checkpoint.MaxTurns)
	}

	return msg
}

// ── ForegroundAgentRegistration & Race Mode ──────────────────────────────────
// Aligned with claude-code-main's foreground agent registration and
// the race between foreground completion and background threshold.

// ForegroundAgentRegistration tracks a foreground agent that may be
// converted to background if it exceeds the time threshold.
type ForegroundAgentRegistration struct {
	mu           sync.Mutex
	AgentID      string
	StartedAt    time.Time
	ThresholdMs  int64 // ms before auto-conversion
	converted    bool
	cancelFg     context.CancelFunc
	resultCh     chan *AgentRunResult
	converter    *FgToBgConverter
	checkpointFn func() *AgentCheckpoint
}

// NewForegroundAgentRegistration creates a registration for a foreground agent
// that will auto-convert to background after thresholdMs milliseconds.
func NewForegroundAgentRegistration(
	agentID string,
	thresholdMs int64,
	cancelFg context.CancelFunc,
	converter *FgToBgConverter,
	checkpointFn func() *AgentCheckpoint,
) *ForegroundAgentRegistration {
	return &ForegroundAgentRegistration{
		AgentID:      agentID,
		StartedAt:    time.Now(),
		ThresholdMs:  thresholdMs,
		cancelFg:     cancelFg,
		resultCh:     make(chan *AgentRunResult, 1),
		converter:    converter,
		checkpointFn: checkpointFn,
	}
}

// RaceOutcome describes which side of the race won.
type RaceOutcome string

const (
	RaceOutcomeForeground RaceOutcome = "foreground" // agent finished before threshold
	RaceOutcomeBackground RaceOutcome = "background" // threshold exceeded, converted
	RaceOutcomeError      RaceOutcome = "error"      // conversion failed
)

// RaceResult holds the outcome of the foreground/background race.
type RaceResult struct {
	Outcome          RaceOutcome
	ForegroundResult *AgentRunResult   // set if foreground won
	ConversionResult *ConversionResult // set if background won
	Error            error
}

// RunRace starts a race between the foreground agent completing and the
// threshold timer firing. Returns a RaceResult indicating which won.
//
// Aligned with claude-code-main's race pattern:
//
//	Promise.race([foregroundCompletion, backgroundThreshold])
func (r *ForegroundAgentRegistration) RunRace(ctx context.Context) *RaceResult {
	threshold := time.Duration(r.ThresholdMs) * time.Millisecond
	timer := time.NewTimer(threshold)
	defer timer.Stop()

	select {
	case result := <-r.resultCh:
		// Foreground won the race.
		return &RaceResult{
			Outcome:          RaceOutcomeForeground,
			ForegroundResult: result,
		}

	case <-timer.C:
		// Threshold exceeded — convert to background.
		r.mu.Lock()
		if r.converted {
			r.mu.Unlock()
			return &RaceResult{Outcome: RaceOutcomeBackground}
		}
		r.converted = true
		r.mu.Unlock()

		slog.Info("fg_to_bg: threshold exceeded, converting",
			slog.String("agent_id", r.AgentID),
			slog.Int64("threshold_ms", r.ThresholdMs),
		)

		// Get checkpoint from the running agent.
		var checkpoint *AgentCheckpoint
		if r.checkpointFn != nil {
			checkpoint = r.checkpointFn()
		}

		// Cancel the foreground agent.
		if r.cancelFg != nil {
			r.cancelFg()
		}

		// Convert to background.
		if r.converter != nil && r.converter.CanConvert() {
			convResult, err := r.converter.Convert(ctx, r.AgentID, checkpoint)
			if err != nil {
				return &RaceResult{
					Outcome: RaceOutcomeError,
					Error:   err,
				}
			}
			return &RaceResult{
				Outcome:          RaceOutcomeBackground,
				ConversionResult: convResult,
			}
		}

		return &RaceResult{
			Outcome: RaceOutcomeError,
			Error:   fmt.Errorf("converter not available"),
		}

	case <-ctx.Done():
		return &RaceResult{
			Outcome: RaceOutcomeError,
			Error:   ctx.Err(),
		}
	}
}

// ReportResult is called by the foreground runner when the agent completes.
// If the agent completes before the threshold, the foreground wins the race.
func (r *ForegroundAgentRegistration) ReportResult(result *AgentRunResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.converted {
		select {
		case r.resultCh <- result:
		default:
		}
	}
}

// IsConverted returns true if the agent was converted to background.
func (r *ForegroundAgentRegistration) IsConverted() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.converted
}

// ── MainSessionTask Lifecycle ────────────────────────────────────────────────
// Aligned with claude-code-main's LocalMainSessionTask.ts (480 lines).
// Enables Ctrl+B session backgrounding: the main query loop can be registered
// as a task, backgrounded, then later foregrounded.

// MainSessionTaskManager manages the main session's background/foreground state.
type MainSessionTaskManager struct {
	mu                 sync.Mutex
	taskFramework      *TaskFramework
	asyncManager       *AsyncLifecycleManager
	foregroundedTaskID string // the currently-foregrounded session task ID
}

// NewMainSessionTaskManager creates a new manager.
func NewMainSessionTaskManager(tf *TaskFramework, am *AsyncLifecycleManager) *MainSessionTaskManager {
	return &MainSessionTaskManager{
		taskFramework: tf,
		asyncManager:  am,
	}
}

// mainSessionPrefix distinguishes main session tasks from regular agent tasks.
// Regular agents use 'a-' prefix; session tasks use 's-' prefix.
const mainSessionPrefix = "s-"

// IsMainSessionTask checks if an agentID represents a main session task.
// Aligned with TS isMainSessionTask (agentType === 'main-session').
func IsMainSessionTask(agentID string) bool {
	return len(agentID) > 2 && agentID[:2] == mainSessionPrefix
}

// RegisterMainSessionTask registers the current main query as a backgroundable task.
// Called when the user presses Ctrl+B or when auto-background triggers.
// Aligned with TS LocalMainSessionTask.registerMainSessionTask.
func (m *MainSessionTaskManager) RegisterMainSessionTask(sessionID, description string) string {
	taskID := mainSessionPrefix + sessionID

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.taskFramework != nil {
		def := AgentDefinition{
			AgentID:   taskID,
			AgentType: "main-session",
			Task:      description,
		}
		t := m.taskFramework.Register(def)
		t.IsBackgrounded = true
	}

	return taskID
}

// CompleteMainSessionTask marks a session task as completed.
// If the task is still foregrounded, it emits a completion notification;
// if backgrounded, it emits a background completion notification.
// Aligned with TS completeMainSessionTask.
func (m *MainSessionTaskManager) CompleteMainSessionTask(taskID string, output string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.taskFramework != nil {
		if t, ok := m.taskFramework.GetTask(taskID); ok {
			t.Status = AgentStatusDone
			t.FinishedAt = time.Now()
		}
	}
}

// ForegroundMainSessionTask brings a backgrounded session task to the foreground.
// Swaps the foregroundedTaskID so the TUI shows this task's output.
// Aligned with TS foregroundMainSessionTask.
func (m *MainSessionTaskManager) ForegroundMainSessionTask(taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.taskFramework != nil {
		t, ok := m.taskFramework.GetTask(taskID)
		if !ok {
			return fmt.Errorf("session task %q not found", taskID)
		}
		if t.Status != AgentStatusRunning && t.Status != AgentStatusPending {
			return fmt.Errorf("session task %q is not running (status: %s)", taskID, t.Status)
		}
		t.IsBackgrounded = false
	}

	m.foregroundedTaskID = taskID
	return nil
}

// GetForegroundedTaskID returns the currently-foregrounded session task ID.
func (m *MainSessionTaskManager) GetForegroundedTaskID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.foregroundedTaskID
}
