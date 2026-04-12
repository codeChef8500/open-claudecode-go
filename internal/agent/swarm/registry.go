package swarm

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"sync"
)

// ── Backend Registry ─────────────────────────────────────────────────────────
//
// Detects the available pane backend and caches it for the session.
// Aligned with claude-code-main's registry.ts:
//   auto-detection order: tmux (inside) > tmux (external) > in-process fallback

// BackendMode specifies which backend selection strategy to use.
type BackendMode string

const (
	BackendModeAuto      BackendMode = "auto"
	BackendModeInProcess BackendMode = "in-process"
	BackendModeTmux      BackendMode = "tmux"
)

// BackendRegistry caches detected backends and provides executor resolution.
type BackendRegistry struct {
	mu       sync.Mutex
	mode     BackendMode
	detected *BackendDetectionResult
	// Executors keyed by BackendType.
	executors map[BackendType]TeammateExecutor
}

// NewBackendRegistry creates a registry with the specified mode.
func NewBackendRegistry(mode BackendMode) *BackendRegistry {
	if mode == "" {
		mode = BackendModeAuto
	}
	return &BackendRegistry{
		mode:      mode,
		executors: make(map[BackendType]TeammateExecutor),
	}
}

// RegisterExecutor registers a TeammateExecutor for the given backend type.
func (r *BackendRegistry) RegisterExecutor(bt BackendType, exec TeammateExecutor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executors[bt] = exec
}

// GetExecutor returns the TeammateExecutor for the given backend type.
func (r *BackendRegistry) GetExecutor(bt BackendType) (TeammateExecutor, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.executors[bt]
	return e, ok
}

// ResolveExecutor returns the appropriate executor based on mode and detection.
func (r *BackendRegistry) ResolveExecutor() (TeammateExecutor, BackendType, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch r.mode {
	case BackendModeInProcess:
		if e, ok := r.executors[BackendInProcess]; ok {
			return e, BackendInProcess, nil
		}
		return nil, "", fmt.Errorf("in-process executor not registered")

	case BackendModeTmux:
		if e, ok := r.executors[BackendTmux]; ok {
			return e, BackendTmux, nil
		}
		return nil, "", fmt.Errorf("tmux executor not registered")

	default: // auto
		result := r.detectLocked()
		if result.BackendType == BackendTmux {
			if e, ok := r.executors[BackendTmux]; ok {
				return e, BackendTmux, nil
			}
		}
		// Fallback to in-process.
		if e, ok := r.executors[BackendInProcess]; ok {
			return e, BackendInProcess, nil
		}
		return nil, "", fmt.Errorf("no executor available (detected: %s)", result.BackendType)
	}
}

// Detect performs backend detection (cached after first call).
func (r *BackendRegistry) Detect() *BackendDetectionResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.detectLocked()
}

func (r *BackendRegistry) detectLocked() *BackendDetectionResult {
	if r.detected != nil {
		return r.detected
	}

	result := detectBackend()
	r.detected = result

	slog.Info("swarm: backend detected",
		slog.String("type", string(result.BackendType)),
		slog.Bool("inside_tmux", result.IsInsideTmux),
		slog.String("setup_msg", result.SetupMessage))

	return result
}

// Mode returns the configured backend mode.
func (r *BackendRegistry) Mode() BackendMode {
	return r.mode
}

// ── Detection logic ──────────────────────────────────────────────────────────

func detectBackend() *BackendDetectionResult {
	// 1. Check if we're already inside tmux.
	if os.Getenv("TMUX") != "" {
		return &BackendDetectionResult{
			BackendType:  BackendTmux,
			IsInsideTmux: true,
		}
	}

	// 2. Check if tmux is available externally.
	if runtime.GOOS != "windows" {
		if _, err := exec.LookPath("tmux"); err == nil {
			return &BackendDetectionResult{
				BackendType:  BackendTmux,
				IsInsideTmux: false,
			}
		}
	}

	// 3. Fallback to in-process.
	msg := ""
	if runtime.GOOS == "windows" {
		msg = "tmux is not available on Windows; using in-process backend"
	}
	return &BackendDetectionResult{
		BackendType:  BackendInProcess,
		IsInsideTmux: false,
		SetupMessage: msg,
	}
}

// ── TeammateRegistry ─────────────────────────────────────────────────────────
//
// Tracks all active in-process teammates for lifecycle management.
// Aligned with claude-code-main's in-process teammate tracking in AppState.

// TeammateRegistry manages active teammate executors across all backends.
type TeammateRegistry struct {
	mu        sync.RWMutex
	teammates map[string]*ActiveTeammate // agentID → ActiveTeammate
}

// ActiveTeammate tracks a running teammate regardless of backend.
type ActiveTeammate struct {
	Identity    TeammateIdentity
	BackendType BackendType
	Cancel      func() // context cancel function
	PaneID      string // tmux pane ID (empty for in-process)
}

// NewTeammateRegistry creates a new teammate registry.
func NewTeammateRegistry() *TeammateRegistry {
	return &TeammateRegistry{
		teammates: make(map[string]*ActiveTeammate),
	}
}

// Register adds a teammate to the registry.
func (tr *TeammateRegistry) Register(at *ActiveTeammate) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.teammates[at.Identity.AgentID] = at
}

// Unregister removes a teammate from the registry.
func (tr *TeammateRegistry) Unregister(agentID string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	delete(tr.teammates, agentID)
}

// Get returns a teammate by agent ID.
func (tr *TeammateRegistry) Get(agentID string) (*ActiveTeammate, bool) {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	at, ok := tr.teammates[agentID]
	return at, ok
}

// All returns all active teammates.
func (tr *TeammateRegistry) All() []*ActiveTeammate {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	result := make([]*ActiveTeammate, 0, len(tr.teammates))
	for _, at := range tr.teammates {
		result = append(result, at)
	}
	return result
}

// Count returns the number of active teammates.
func (tr *TeammateRegistry) Count() int {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	return len(tr.teammates)
}

// StopAll cancels all teammates and clears the registry.
func (tr *TeammateRegistry) StopAll() {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	for id, at := range tr.teammates {
		if at.Cancel != nil {
			at.Cancel()
		}
		slog.Info("swarm: stopped teammate", slog.String("agent_id", id))
	}
	tr.teammates = make(map[string]*ActiveTeammate)
}

// AgentIDs returns all registered agent IDs.
func (tr *TeammateRegistry) AgentIDs() []string {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	ids := make([]string, 0, len(tr.teammates))
	for id := range tr.teammates {
		ids = append(ids, id)
	}
	return ids
}
