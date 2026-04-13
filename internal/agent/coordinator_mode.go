package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Coordinator mode aligned with claude-code-main's coordinatorMode.ts.
//
// The coordinator orchestrates multiple worker agents to complete a complex task:
//  1. Breaks tasks into independent work items
//  2. Spawns worker agents (each in its own worktree)
//  3. Monitors progress and handles failures
//  4. Synthesizes results when all workers complete
//  5. Uses a shared scratchpad directory for coordination

// IsCoordinatorMode checks if coordinator mode is active.
// Aligned with TS isCoordinatorMode().
func IsCoordinatorMode() bool {
	v := os.Getenv("CLAUDE_CODE_COORDINATOR_MODE")
	return v == "1" || strings.EqualFold(v, "true")
}

// CoordinatorSessionMode is "coordinator" or "normal".
type CoordinatorSessionMode string

const (
	SessionModeCoordinator CoordinatorSessionMode = "coordinator"
	SessionModeNormal      CoordinatorSessionMode = "normal"
)

// MatchSessionMode checks if the current coordinator mode matches the session's
// stored mode. If mismatched, flips the environment variable. Returns a warning
// message if the mode was switched, or "" if no switch was needed.
// Aligned with TS matchSessionMode().
func MatchSessionMode(sessionMode CoordinatorSessionMode) string {
	if sessionMode == "" {
		return ""
	}

	currentIsCoordinator := IsCoordinatorMode()
	sessionIsCoordinator := sessionMode == SessionModeCoordinator

	if currentIsCoordinator == sessionIsCoordinator {
		return ""
	}

	if sessionIsCoordinator {
		os.Setenv("CLAUDE_CODE_COORDINATOR_MODE", "1")
	} else {
		os.Unsetenv("CLAUDE_CODE_COORDINATOR_MODE")
	}

	slog.Info("coordinator: mode switched to match session",
		slog.String("to", string(sessionMode)))

	if sessionIsCoordinator {
		return "Entered coordinator mode to match resumed session."
	}
	return "Exited coordinator mode to match resumed session."
}

// isSimpleMode checks if CLAUDE_CODE_SIMPLE is enabled.
// Aligned with TS isEnvTruthy(process.env.CLAUDE_CODE_SIMPLE).
func isSimpleMode() bool {
	v := os.Getenv("CLAUDE_CODE_SIMPLE")
	return v == "1" || strings.EqualFold(v, "true")
}

// internalWorkerTools are tools in ASYNC_AGENT_ALLOWED_TOOLS that should NOT
// be listed in the coordinator's worker tools context (they are internal plumbing).
// Aligned with TS coordinatorMode.ts INTERNAL_WORKER_TOOLS.
var internalWorkerTools = map[string]bool{
	"TeamCreate":      true,
	"TeamDelete":      true,
	"SendMessage":     true,
	"SyntheticOutput": true,
}

// computeWorkerTools returns the sorted list of tools visible to coordinator
// workers. Dynamically derived from AsyncAgentAllowedTools minus internal
// tools, aligned with TS getCoordinatorUserContext().
func computeWorkerTools() []string {
	var tools []string
	for name := range AsyncAgentAllowedTools {
		if !internalWorkerTools[name] {
			tools = append(tools, name)
		}
	}
	// Stable output for deterministic prompts.
	sort.Strings(tools)
	return tools
}

// computeSimpleWorkerTools returns the reduced tool list for CLAUDE_CODE_SIMPLE mode.
// Aligned with TS: isEnvTruthy(CLAUDE_CODE_SIMPLE) → [Bash, Read, FileEdit].
func computeSimpleWorkerTools() []string {
	return []string{"Bash", "FileEdit", "Read"}
}

// GetCoordinatorUserContext returns the worker tools context block for injection
// into the system prompt. Returns empty map when not in coordinator mode.
// Aligned with TS getCoordinatorUserContext().
func GetCoordinatorUserContext(mcpClientNames []string, scratchpadDir string) map[string]string {
	if !IsCoordinatorMode() {
		return nil
	}

	// Dynamically compute worker tools from AsyncAgentAllowedTools minus internal tools.
	// Aligned with TS: Array.from(ASYNC_AGENT_ALLOWED_TOOLS).filter(…).sort().join(', ')
	var workerTools []string
	if isSimpleMode() {
		workerTools = computeSimpleWorkerTools()
	} else {
		workerTools = computeWorkerTools()
	}

	content := fmt.Sprintf("Workers spawned via the Task tool have access to these tools: %s",
		strings.Join(workerTools, ", "))

	if len(mcpClientNames) > 0 {
		content += fmt.Sprintf("\n\nWorkers also have access to MCP tools from connected MCP servers: %s",
			strings.Join(mcpClientNames, ", "))
	}

	if scratchpadDir != "" {
		content += fmt.Sprintf("\n\nScratchpad directory: %s\n"+
			"Workers can read and write here without permission prompts. "+
			"Use this for durable cross-worker knowledge — structure files however fits the work.",
			scratchpadDir)
	}

	return map[string]string{"workerToolsContext": content}
}

// CoordinatorConfig configures the coordinator mode.
type CoordinatorConfig struct {
	// MaxWorkers is the maximum number of concurrent worker agents.
	MaxWorkers int
	// ScratchpadDir is the shared directory for coordination files.
	ScratchpadDir string
	// WorkDir is the base working directory.
	WorkDir string
	// DefaultModel is the model for worker agents.
	DefaultModel string
	// MaxTurnsPerWorker is the max turns each worker gets.
	MaxTurnsPerWorker int
	// ForceAsync forces all workers to run asynchronously.
	ForceAsync bool
	// WorkerToolRestrictions limits which tools workers can use.
	WorkerToolRestrictions []string
	// InjectWorktreeNotice injects worktree awareness into worker prompts.
	InjectWorktreeNotice bool
}

// CoordinatorState tracks the state of a coordinator session.
type CoordinatorState struct {
	mu sync.RWMutex

	Config  CoordinatorConfig
	Workers map[string]*CoordinatorWorker
	Plan    *CoordinatorPlan
	Status  CoordinatorStatus
	StartAt time.Time
}

// CoordinatorStatus represents the lifecycle state of the coordinator.
type CoordinatorStatus string

const (
	CoordStatusIdle     CoordinatorStatus = "idle"
	CoordStatusPlanning CoordinatorStatus = "planning"
	CoordStatusRunning  CoordinatorStatus = "running"
	CoordStatusDone     CoordinatorStatus = "done"
	CoordStatusFailed   CoordinatorStatus = "failed"
)

// CoordinatorWorker tracks a single worker agent within the coordinator.
type CoordinatorWorker struct {
	AgentID     string
	Task        string
	Description string
	Status      AsyncAgentStatus
	Result      *AgentRunResult
	WorktreeDir string
	StartedAt   time.Time
	FinishedAt  time.Time
}

// CoordinatorPlan holds the decomposed task plan.
type CoordinatorPlan struct {
	Title       string
	Description string
	WorkItems   []WorkItem
	CreatedAt   time.Time
}

// WorkItem is a single unit of work in the coordinator plan.
type WorkItem struct {
	ID          string   `json:"id"`
	Task        string   `json:"task"`
	Description string   `json:"description"`
	Priority    int      `json:"priority"`
	DependsOn   []string `json:"depends_on,omitempty"`
	Assigned    bool     `json:"assigned"`
	AgentID     string   `json:"agent_id,omitempty"`
}

// CoordinatorMode manages the coordinator lifecycle.
type CoordinatorMode struct {
	mu    sync.RWMutex
	state *CoordinatorState

	runner       *AgentRunner
	asyncManager *AsyncLifecycleManager
	loader       *AgentLoader
}

// NewCoordinatorMode creates a coordinator mode manager.
func NewCoordinatorMode(
	runner *AgentRunner,
	asyncManager *AsyncLifecycleManager,
	loader *AgentLoader,
	cfg CoordinatorConfig,
) *CoordinatorMode {
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = 4
	}
	if cfg.MaxTurnsPerWorker <= 0 {
		cfg.MaxTurnsPerWorker = 100
	}

	return &CoordinatorMode{
		state: &CoordinatorState{
			Config:  cfg,
			Workers: make(map[string]*CoordinatorWorker),
			Status:  CoordStatusIdle,
			StartAt: time.Now(),
		},
		runner:       runner,
		asyncManager: asyncManager,
		loader:       loader,
	}
}

// IsActive returns whether coordinator mode is currently active.
func (cm *CoordinatorMode) IsActive() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.state.Status == CoordStatusRunning || cm.state.Status == CoordStatusPlanning
}

// SetPlan sets the coordinator's work plan.
func (cm *CoordinatorMode) SetPlan(plan *CoordinatorPlan) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.state.Plan = plan
	cm.state.Status = CoordStatusPlanning
}

// SpawnWorker launches a worker agent for a work item.
func (cm *CoordinatorMode) SpawnWorker(ctx context.Context, item WorkItem) (string, error) {
	cm.mu.Lock()
	if cm.state.Status == CoordStatusIdle {
		cm.state.Status = CoordStatusRunning
	}

	// Check worker limit.
	activeCount := 0
	for _, w := range cm.state.Workers {
		if w.Status == AsyncStatusRunning || w.Status == AsyncStatusPending {
			activeCount++
		}
	}
	if activeCount >= cm.state.Config.MaxWorkers {
		cm.mu.Unlock()
		return "", fmt.Errorf("max workers (%d) reached", cm.state.Config.MaxWorkers)
	}
	cm.mu.Unlock()

	// Build worker agent definition.
	workerDef := AgentDefinition{
		AgentType:  "coordinator-worker",
		Source:     SourceBuiltIn,
		Background: true,
		Isolation:  IsolationWorktree,
		MaxTurns:   cm.state.Config.MaxTurnsPerWorker,
		Model:      cm.state.Config.DefaultModel,
	}

	params := RunAgentParams{
		AgentDef:      &workerDef,
		Task:          item.Task,
		WorkDir:       cm.state.Config.WorkDir,
		Background:    true,
		IsolationMode: IsolationWorktree,
	}

	if cm.asyncManager == nil {
		return "", fmt.Errorf("async manager not configured for coordinator mode")
	}

	agentID, err := cm.asyncManager.Launch(ctx, params)
	if err != nil {
		return "", fmt.Errorf("spawn worker: %w", err)
	}

	cm.mu.Lock()
	cm.state.Workers[agentID] = &CoordinatorWorker{
		AgentID:     agentID,
		Task:        item.Task,
		Description: item.Description,
		Status:      AsyncStatusRunning,
		StartedAt:   time.Now(),
	}
	cm.mu.Unlock()

	slog.Info("coordinator: spawned worker",
		slog.String("agent_id", agentID),
		slog.String("task", item.Description),
	)

	return agentID, nil
}

// GetWorkerStatus returns the status of a specific worker.
func (cm *CoordinatorMode) GetWorkerStatus(agentID string) (*CoordinatorWorker, error) {
	cm.mu.RLock()
	w, ok := cm.state.Workers[agentID]
	cm.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("worker not found: %s", agentID)
	}

	// Sync status from async manager.
	if cm.asyncManager != nil {
		status, err := cm.asyncManager.GetStatus(agentID)
		if err == nil {
			w.Status = status
		}
	}

	return w, nil
}

// AllWorkers returns all worker states.
func (cm *CoordinatorMode) AllWorkers() []*CoordinatorWorker {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	workers := make([]*CoordinatorWorker, 0, len(cm.state.Workers))
	for _, w := range cm.state.Workers {
		workers = append(workers, w)
	}
	return workers
}

// WaitAll waits for all workers to complete with a timeout.
func (cm *CoordinatorMode) WaitAll(timeout time.Duration) error {
	cm.mu.RLock()
	ids := make([]string, 0, len(cm.state.Workers))
	for id := range cm.state.Workers {
		ids = append(ids, id)
	}
	cm.mu.RUnlock()

	if cm.asyncManager == nil {
		return fmt.Errorf("async manager not configured")
	}

	for _, id := range ids {
		result, err := cm.asyncManager.Wait(id, timeout)
		if err != nil {
			slog.Warn("coordinator: worker wait failed",
				slog.String("agent_id", id),
				slog.Any("err", err))
			continue
		}

		cm.mu.Lock()
		if w, ok := cm.state.Workers[id]; ok {
			w.Result = result
			w.FinishedAt = time.Now()
			if result.Error != nil {
				w.Status = AsyncStatusFailed
			} else {
				w.Status = AsyncStatusDone
			}
		}
		cm.mu.Unlock()
	}

	return nil
}

// CollectResults returns the results from all completed workers.
func (cm *CoordinatorMode) CollectResults() map[string]*AgentRunResult {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	results := make(map[string]*AgentRunResult)
	for id, w := range cm.state.Workers {
		if w.Result != nil {
			results[id] = w.Result
		}
	}
	return results
}

// Finish marks the coordinator as done.
func (cm *CoordinatorMode) Finish() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	allDone := true
	anyFailed := false
	for _, w := range cm.state.Workers {
		if w.Status == AsyncStatusRunning || w.Status == AsyncStatusPending {
			allDone = false
		}
		if w.Status == AsyncStatusFailed {
			anyFailed = true
		}
	}

	if !allDone {
		cm.state.Status = CoordStatusRunning
		return
	}

	if anyFailed {
		cm.state.Status = CoordStatusFailed
	} else {
		cm.state.Status = CoordStatusDone
	}
}

// EnsureScratchpad creates the shared scratchpad directory if needed.
func (cm *CoordinatorMode) EnsureScratchpad() (string, error) {
	dir := cm.state.Config.ScratchpadDir
	if dir == "" {
		dir = filepath.Join(cm.state.Config.WorkDir, ".coordinator-scratchpad")
		cm.state.Config.ScratchpadDir = dir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create scratchpad: %w", err)
	}
	return dir, nil
}

// WriteScratchpad writes content to a named file in the scratchpad.
func (cm *CoordinatorMode) WriteScratchpad(name, content string) error {
	dir, err := cm.EnsureScratchpad()
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
}

// ReadScratchpad reads a named file from the scratchpad.
func (cm *CoordinatorMode) ReadScratchpad(name string) (string, error) {
	dir := cm.state.Config.ScratchpadDir
	if dir == "" {
		return "", fmt.Errorf("scratchpad not initialized")
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// FormatSummary returns a human-readable summary of the coordinator state.
func (cm *CoordinatorMode) FormatSummary() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	summary := fmt.Sprintf("Coordinator Status: %s\n", cm.state.Status)
	summary += fmt.Sprintf("Workers: %d\n\n", len(cm.state.Workers))

	for _, w := range cm.state.Workers {
		line := fmt.Sprintf("  [%s] %s - %s", w.Status, truncID(w.AgentID), w.Description)
		if w.Status == AsyncStatusDone && w.Result != nil {
			dur := w.FinishedAt.Sub(w.StartedAt).Round(time.Second)
			line += fmt.Sprintf(" (completed in %s)", dur)
		}
		summary += line + "\n"
	}

	return summary
}

// ── Coordinator System Prompt ──────────────────────────────────────────────
// Aligned with claude-code-main's coordinatorMode system prompt injection.

// CoordinatorAllowedTools is DEPRECATED — use CoordinatorModeAllowedTools
// from toolfilter.go instead. This alias exists only to avoid breaking
// external callers; new code MUST reference toolfilter.go.
var CoordinatorAllowedTools = CoordinatorModeAllowedToolsList()

// BuildCoordinatorSystemPrompt generates the system prompt for the coordinator agent.
// Aligned with claude-code-main's coordinatorMode.ts getCoordinatorSystemPrompt().
// Contains 6 sections matching the TS reference implementation.
func BuildCoordinatorSystemPrompt(cfg CoordinatorConfig, activeWorkers []*CoordinatorWorker) string {
	const toolAgent = "Task"
	const toolSendMsg = "SendMessage"
	const toolTaskStop = "TaskStop"

	var workerCapabilities string
	if isSimpleMode() {
		workerCapabilities = "Workers have access to Bash, Read, and Edit tools, plus MCP tools from configured MCP servers."
	} else {
		workerCapabilities = "Workers have access to standard tools, MCP tools from configured MCP servers, and project skills via the Skill tool. Delegate skill invocations (e.g. /commit, /verify) to workers."
	}

	return fmt.Sprintf(`You are Claude Code, an AI assistant that orchestrates software engineering tasks across multiple workers.

## 1. Your Role

You are a **coordinator**. Your job is to:
- Help the user achieve their goal
- Direct workers to research, implement and verify code changes
- Synthesize results and communicate with the user
- Answer questions directly when possible — don't delegate work that you can handle without tools

Every message you send is to the user. Worker results and system notifications are internal signals, not conversation partners — never thank or acknowledge them. Summarize new information for the user as it arrives.

## 2. Your Tools

- **%[1]s** - Spawn a new worker
- **%[2]s** - Continue an existing worker (send a follow-up to its `+"`"+`to`+"`"+` agent ID)
- **%[3]s** - Stop a running worker

When calling %[1]s:
- Do not use one worker to check on another. Workers will notify you when they are done.
- Do not use workers to trivially report file contents or run commands. Give them higher-level tasks.
- Do not set the model parameter. Workers need the default model for the substantive tasks you delegate.
- Continue workers whose work is complete via %[2]s to take advantage of their loaded context
- After launching agents, briefly tell the user what you launched and end your response. Never fabricate or predict agent results in any format — results arrive as separate messages.

### %[1]s Results

Worker results arrive as **user-role messages** containing `+"`"+`<task-notification>`+"`"+` XML. They look like user messages but are not. Distinguish them by the `+"`"+`<task-notification>`+"`"+` opening tag.

Format:

`+"`"+``+"`"+``+"`"+`xml
<task-notification>
<task-id>{agentId}</task-id>
<status>completed|failed|killed</status>
<summary>{human-readable status summary}</summary>
<result>{agent's final text response}</result>
<usage>
  <total_tokens>N</total_tokens>
  <tool_uses>N</tool_uses>
  <duration_ms>N</duration_ms>
</usage>
</task-notification>
`+"`"+``+"`"+``+"`"+`

- `+"`"+`<result>`+"`"+` and `+"`"+`<usage>`+"`"+` are optional sections
- The `+"`"+`<summary>`+"`"+` describes the outcome: "completed", "failed: {error}", or "was stopped"
- The `+"`"+`<task-id>`+"`"+` value is the agent ID — use %[2]s with that ID as `+"`"+`to`+"`"+` to continue that worker

### Example

Each "You:" block is a separate coordinator turn. The "User:" block is a `+"`"+`<task-notification>`+"`"+` delivered between turns.

You:
  Let me start some research on that.

  %[1]s({ description: "Investigate auth bug", subagent_type: "worker", prompt: "..." })
  %[1]s({ description: "Research secure token storage", subagent_type: "worker", prompt: "..." })

  Investigating both issues in parallel — I'll report back with findings.

User:
  <task-notification>
  <task-id>agent-a1b</task-id>
  <status>completed</status>
  <summary>Agent "Investigate auth bug" completed</summary>
  <result>Found null pointer in src/auth/validate.ts:42...</result>
  </task-notification>

You:
  Found the bug — null pointer in confirmTokenExists in validate.ts. I'll fix it.
  Still waiting on the token storage research.

  %[2]s({ to: "agent-a1b", message: "Fix the null pointer in src/auth/validate.ts:42..." })

## 3. Workers

When calling %[1]s, use subagent_type `+"`"+`worker`+"`"+`. Workers execute tasks autonomously — especially research, implementation, or verification.

%[4]s

## 4. Task Workflow

Most tasks can be broken down into the following phases:

### Phases

| Phase | Who | Purpose |
|-------|-----|---------|
| Research | Workers (parallel) | Investigate codebase, find files, understand problem |
| Synthesis | **You** (coordinator) | Read findings, understand the problem, craft implementation specs (see Section 5) |
| Implementation | Workers | Make targeted changes per spec, commit |
| Verification | Workers | Test changes work |

### Concurrency

**Parallelism is your superpower. Workers are async. Launch independent workers concurrently whenever possible — don't serialize work that can run simultaneously and look for opportunities to fan out. When doing research, cover multiple angles. To launch workers in parallel, make multiple tool calls in a single message.**

Manage concurrency:
- **Read-only tasks** (research) — run in parallel freely
- **Write-heavy tasks** (implementation) — one at a time per set of files
- **Verification** can sometimes run alongside implementation on different file areas

### What Real Verification Looks Like

Verification means **proving the code works**, not confirming it exists. A verifier that rubber-stamps weak work undermines everything.

- Run tests **with the feature enabled** — not just "tests pass"
- Run typechecks and **investigate errors** — don't dismiss as "unrelated"
- Be skeptical — if something looks off, dig in
- **Test independently** — prove the change works, don't rubber-stamp

### Handling Worker Failures

When a worker reports failure (tests failed, build errors, file not found):
- Continue the same worker with %[2]s — it has the full error context
- If a correction attempt fails, try a different approach or report to the user

### Stopping Workers

Use %[3]s to stop a worker you sent in the wrong direction — for example, when you realize mid-flight that the approach is wrong, or the user changes requirements after you launched the worker. Pass the `+"`"+`task_id`+"`"+` from the %[1]s tool's launch result. Stopped workers can be continued with %[2]s.

`+"`"+``+"`"+``+"`"+`
// Launched a worker to refactor auth to use JWT
%[1]s({ description: "Refactor auth to JWT", subagent_type: "worker", prompt: "Replace session-based auth with JWT..." })
// ... returns task_id: "agent-x7q" ...

// User clarifies: "Actually, keep sessions — just fix the null pointer"
%[3]s({ task_id: "agent-x7q" })

// Continue with corrected instructions
%[2]s({ to: "agent-x7q", message: "Stop the JWT refactor. Instead, fix the null pointer in src/auth/validate.ts:42..." })
`+"`"+``+"`"+``+"`"+`

## 5. Writing Worker Prompts

**Workers can't see your conversation.** Every prompt must be self-contained with everything the worker needs. After research completes, you always do two things: (1) synthesize findings into a specific prompt, and (2) choose whether to continue that worker via %[2]s or spawn a fresh one.

### Always synthesize — your most important job

When workers report research findings, **you must understand them before directing follow-up work**. Read the findings. Identify the approach. Then write a prompt that proves you understood by including specific file paths, line numbers, and exactly what to change.

Never write "based on your findings" or "based on the research." These phrases delegate understanding to the worker instead of doing it yourself. You never hand off understanding to another worker.

`+"`"+``+"`"+``+"`"+`
// Anti-pattern — lazy delegation (bad whether continuing or spawning)
%[1]s({ prompt: "Based on your findings, fix the auth bug", ... })
%[1]s({ prompt: "The worker found an issue in the auth module. Please fix it.", ... })

// Good — synthesized spec (works with either continue or spawn)
%[1]s({ prompt: "Fix the null pointer in src/auth/validate.ts:42. The user field on Session (src/auth/types.ts:15) is undefined when sessions expire but the token remains cached. Add a null check before user.id access — if null, return 401 with 'Session expired'. Commit and report the hash.", ... })
`+"`"+``+"`"+``+"`"+`

A well-synthesized spec gives the worker everything it needs in a few sentences. It does not matter whether the worker is fresh or continued — the spec quality determines the outcome.

### Add a purpose statement

Include a brief purpose so workers can calibrate depth and emphasis:

- "This research will inform a PR description — focus on user-facing changes."
- "I need this to plan an implementation — report file paths, line numbers, and type signatures."
- "This is a quick check before we merge — just verify the happy path."

### Choose continue vs. spawn by context overlap

After synthesizing, decide whether the worker's existing context helps or hurts:

| Situation | Mechanism | Why |
|-----------|-----------|-----|
| Research explored exactly the files that need editing | **Continue** (%[2]s) with synthesized spec | Worker already has the files in context AND now gets a clear plan |
| Research was broad but implementation is narrow | **Spawn fresh** (%[1]s) with synthesized spec | Avoid dragging along exploration noise; focused context is cleaner |
| Correcting a failure or extending recent work | **Continue** | Worker has the error context and knows what it just tried |
| Verifying code a different worker just wrote | **Spawn fresh** | Verifier should see the code with fresh eyes, not carry implementation assumptions |
| First implementation attempt used the wrong approach entirely | **Spawn fresh** | Wrong-approach context pollutes the retry; clean slate avoids anchoring on the failed path |
| Completely unrelated task | **Spawn fresh** | No useful context to reuse |

There is no universal default. Think about how much of the worker's context overlaps with the next task. High overlap -> continue. Low overlap -> spawn fresh.

### Continue mechanics

When continuing a worker with %[2]s, it has full context from its previous run:
`+"`"+``+"`"+``+"`"+`
// Continuation — worker finished research, now give it a synthesized implementation spec
%[2]s({ to: "xyz-456", message: "Fix the null pointer in src/auth/validate.ts:42. The user field is undefined when Session.expired is true but the token is still cached. Add a null check before accessing user.id — if null, return 401 with 'Session expired'. Commit and report the hash." })
`+"`"+``+"`"+``+"`"+`

`+"`"+``+"`"+``+"`"+`
// Correction — worker just reported test failures from its own change, keep it brief
%[2]s({ to: "xyz-456", message: "Two tests still failing at lines 58 and 72 — update the assertions to match the new error message." })
`+"`"+``+"`"+``+"`"+`

### Prompt tips

**Good examples:**

1. Implementation: "Fix the null pointer in src/auth/validate.ts:42. The user field can be undefined when the session expires. Add a null check and return early with an appropriate error. Commit and report the hash."

2. Precise git operation: "Create a new branch from main called 'fix/session-expiry'. Cherry-pick only commit abc123 onto it. Push and create a draft PR targeting main. Add anthropics/claude-code as reviewer. Report the PR URL."

3. Correction (continued worker, short): "The tests failed on the null check you added — validate.test.ts:58 expects 'Invalid session' but you changed it to 'Session expired'. Fix the assertion. Commit and report the hash."

**Bad examples:**

1. "Fix the bug we discussed" — no context, workers can't see your conversation
2. "Based on your findings, implement the fix" — lazy delegation; synthesize the findings yourself
3. "Create a PR for the recent changes" — ambiguous scope: which changes? which branch? draft?
4. "Something went wrong with the tests, can you look?" — no error message, no file path, no direction

Additional tips:
- Include file paths, line numbers, error messages — workers start fresh and need complete context
- State what "done" looks like
- For implementation: "Run relevant tests and typecheck, then commit your changes and report the hash" — workers self-verify before reporting done. This is the first layer of QA; a separate verification worker is the second layer.
- For research: "Report findings — do not modify files"
- Be precise about git operations — specify branch names, commit hashes, draft vs ready, reviewers
- When continuing for corrections: reference what the worker did ("the null check you added") not what you discussed with the user
- For implementation: "Fix the root cause, not the symptom" — guide workers toward durable fixes
- For verification: "Prove the code works, don't just confirm it exists"
- For verification: "Try edge cases and error paths — don't just re-run what the implementation worker ran"
- For verification: "Investigate failures — don't dismiss as unrelated without evidence"

## 6. Example Session

User: "There's a null pointer in the auth module. Can you fix it?"

You:
  Let me investigate first.

  %[1]s({ description: "Investigate auth bug", subagent_type: "worker", prompt: "Investigate the auth module in src/auth/. Find where null pointer exceptions could occur around session handling and token validation... Report specific file paths, line numbers, and types involved. Do not modify files." })
  %[1]s({ description: "Research auth tests", subagent_type: "worker", prompt: "Find all test files related to src/auth/. Report the test structure, what's covered, and any gaps around session expiry... Do not modify files." })

  Investigating from two angles — I'll report back with findings.

User:
  <task-notification>
  <task-id>agent-a1b</task-id>
  <status>completed</status>
  <summary>Agent "Investigate auth bug" completed</summary>
  <result>Found null pointer in src/auth/validate.ts:42. The user field on Session is undefined when the session expires but ...</result>
  </task-notification>

You:
  Found the bug — null pointer in validate.ts:42.

  %[2]s({ to: "agent-a1b", message: "Fix the null pointer in src/auth/validate.ts:42. Add a null check before accessing user.id — if null, ... Commit and report the hash." })

  Fix is in progress.

User:
  How's it going?

You:
  Fix for the null pointer is in progress. Still waiting to hear back about the test suite.
`, toolAgent, toolSendMsg, toolTaskStop, workerCapabilities)
}

// BuildWorkerWorktreeNotice generates the worktree notice injected into worker prompts.
// Aligned with claude-code-main's worktree notice for coordinator workers.
func BuildWorkerWorktreeNotice(worktreeDir, mainBranch string) string {
	var sb strings.Builder
	sb.WriteString("## Worktree Notice\n\n")
	sb.WriteString(fmt.Sprintf("You are working in a git worktree at: %s\n", worktreeDir))
	sb.WriteString(fmt.Sprintf("The main branch is: %s\n\n", mainBranch))
	sb.WriteString("Important:\n")
	sb.WriteString("- Only modify files within your worktree.\n")
	sb.WriteString("- Do NOT switch branches or merge.\n")
	sb.WriteString("- Your changes will be merged by the coordinator when you complete.\n")
	sb.WriteString("- If you encounter merge conflicts, document them and let the coordinator handle it.\n")
	return sb.String()
}

// SpawnWorkerForced launches a worker with forced async + worktree + tool restrictions.
// This is the coordinator-mode-specific spawn that enforces all constraints.
func (cm *CoordinatorMode) SpawnWorkerForced(ctx context.Context, item WorkItem, parentContext *SubagentContext) (string, error) {
	cm.mu.Lock()
	if cm.state.Status == CoordStatusIdle {
		cm.state.Status = CoordStatusRunning
	}

	activeCount := 0
	for _, w := range cm.state.Workers {
		if w.Status == AsyncStatusRunning || w.Status == AsyncStatusPending {
			activeCount++
		}
	}
	if activeCount >= cm.state.Config.MaxWorkers {
		cm.mu.Unlock()
		return "", fmt.Errorf("max workers (%d) reached", cm.state.Config.MaxWorkers)
	}
	cm.mu.Unlock()

	workerDef := AgentDefinition{
		AgentType:  "coordinator-worker",
		Source:     SourceBuiltIn,
		Background: true,
		Isolation:  IsolationWorktree,
		MaxTurns:   cm.state.Config.MaxTurnsPerWorker,
		Model:      cm.state.Config.DefaultModel,
	}

	// Apply tool restrictions from coordinator config.
	if len(cm.state.Config.WorkerToolRestrictions) > 0 {
		workerDef.AllowedTools = cm.state.Config.WorkerToolRestrictions
	}

	params := RunAgentParams{
		AgentDef:      &workerDef,
		Task:          item.Task,
		Description:   item.Description,
		WorkDir:       cm.state.Config.WorkDir,
		Background:    true,
		IsolationMode: IsolationWorktree,
		IsCoordinator: true,
		ParentContext: parentContext,
	}

	if cm.asyncManager == nil {
		return "", fmt.Errorf("async manager not configured for coordinator mode")
	}

	agentID, err := cm.asyncManager.Launch(ctx, params)
	if err != nil {
		return "", fmt.Errorf("spawn worker: %w", err)
	}

	cm.mu.Lock()
	cm.state.Workers[agentID] = &CoordinatorWorker{
		AgentID:     agentID,
		Task:        item.Task,
		Description: item.Description,
		Status:      AsyncStatusRunning,
		StartedAt:   time.Now(),
	}
	cm.mu.Unlock()

	slog.Info("coordinator: spawned forced-async worker",
		slog.String("agent_id", agentID),
		slog.String("task", item.Description),
	)

	return agentID, nil
}

// PollWorkers syncs worker statuses from the async manager and returns
// newly completed worker IDs.
func (cm *CoordinatorMode) PollWorkers() []string {
	if cm.asyncManager == nil {
		return nil
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	var completed []string
	for id, w := range cm.state.Workers {
		if w.Status == AsyncStatusDone || w.Status == AsyncStatusFailed || w.Status == AsyncStatusCancelled {
			continue
		}

		status, err := cm.asyncManager.GetStatus(id)
		if err != nil {
			continue
		}

		if status != w.Status {
			w.Status = status
			if status == AsyncStatusDone || status == AsyncStatusFailed || status == AsyncStatusCancelled {
				w.FinishedAt = time.Now()
				if result, err := cm.asyncManager.GetResult(id); err == nil {
					w.Result = result
				}
				completed = append(completed, id)
			}
		}
	}
	return completed
}

// FormatWorkerResults returns a formatted summary of all completed worker results.
func (cm *CoordinatorMode) FormatWorkerResults(maxCharsPerWorker int) string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var sb strings.Builder
	sb.WriteString("## Worker Results\n\n")

	for _, w := range cm.state.Workers {
		sb.WriteString(fmt.Sprintf("### Worker %s: %s\n", truncID(w.AgentID), w.Description))
		sb.WriteString(fmt.Sprintf("Status: %s\n", w.Status))

		if w.Result != nil {
			output := w.Result.Output
			if maxCharsPerWorker > 0 && len(output) > maxCharsPerWorker {
				output = TruncateSubagentOutput(output, maxCharsPerWorker)
			}
			sb.WriteString(output)
			sb.WriteString("\n")
		} else if w.Status == AsyncStatusFailed {
			sb.WriteString("(failed with no output)\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
