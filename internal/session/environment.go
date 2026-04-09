package session

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ── Session Environment Snapshot ─────────────────────────────────────────────
// Aligned with claude-code-main src/services/session/sessionEnvironment.ts
//
// Captures a snapshot of the user's environment at session start so that
// environment changes during long sessions can be detected and communicated
// to the model.

// SessionEnvironment records the environment context at session start.
type SessionEnvironment struct {
	// System information.
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Hostname string `json:"hostname,omitempty"`

	// Working directory.
	WorkDir string `json:"workDir"`

	// Shell information.
	Shell        string `json:"shell,omitempty"`
	ShellVersion string `json:"shellVersion,omitempty"`

	// Git context.
	GitBranch string `json:"gitBranch,omitempty"`
	GitRemote string `json:"gitRemote,omitempty"`
	GitRoot   string `json:"gitRoot,omitempty"`

	// Timestamp.
	CapturedAt time.Time `json:"capturedAt"`

	// Selected environment variables (redacted keys only for security).
	EnvKeys []string `json:"envKeys,omitempty"`

	// Go runtime info.
	GoVersion string `json:"goVersion,omitempty"`
}

// CaptureEnvironment takes a snapshot of the current environment.
func CaptureEnvironment(workDir string) *SessionEnvironment {
	hostname, _ := os.Hostname()
	env := &SessionEnvironment{
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		Hostname:   hostname,
		WorkDir:    workDir,
		CapturedAt: time.Now(),
		GoVersion:  runtime.Version(),
	}

	// Shell.
	env.Shell = detectShell()
	env.ShellVersion = detectShellVersion(env.Shell)

	// Git context.
	env.GitBranch = gitOutput(workDir, "rev-parse", "--abbrev-ref", "HEAD")
	env.GitRemote = gitOutput(workDir, "config", "--get", "remote.origin.url")
	env.GitRoot = gitOutput(workDir, "rev-parse", "--show-toplevel")

	// Environment variable keys (not values — security).
	env.EnvKeys = environmentKeys()

	return env
}

// SaveEnvironment writes the environment snapshot to the session directory.
func SaveEnvironment(sessionDir string, env *SessionEnvironment) error {
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(sessionDir, "environment.json")
	return os.WriteFile(path, data, 0o644)
}

// LoadEnvironment reads the environment snapshot from the session directory.
func LoadEnvironment(sessionDir string) (*SessionEnvironment, error) {
	path := filepath.Join(sessionDir, "environment.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var env SessionEnvironment
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	return &env, nil
}

// EnvironmentDiff compares two environment snapshots and returns a human-readable
// summary of changes.
func EnvironmentDiff(old, current *SessionEnvironment) string {
	if old == nil || current == nil {
		return ""
	}
	var diffs []string

	if old.WorkDir != current.WorkDir {
		diffs = append(diffs, fmt.Sprintf("Working directory changed: %s → %s", old.WorkDir, current.WorkDir))
	}
	if old.GitBranch != current.GitBranch {
		diffs = append(diffs, fmt.Sprintf("Git branch changed: %s → %s", old.GitBranch, current.GitBranch))
	}
	if old.GitRemote != current.GitRemote {
		diffs = append(diffs, fmt.Sprintf("Git remote changed: %s → %s", old.GitRemote, current.GitRemote))
	}

	if len(diffs) == 0 {
		return ""
	}
	return strings.Join(diffs, "\n")
}

// ── helpers ──────────────────────────────────────────────────────────────────

func detectShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return filepath.Base(s)
	}
	if s := os.Getenv("COMSPEC"); s != "" {
		return filepath.Base(s)
	}
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	return "sh"
}

func detectShellVersion(shell string) string {
	if shell == "" {
		return ""
	}
	var cmd *exec.Cmd
	switch {
	case strings.Contains(shell, "bash"):
		cmd = exec.Command("bash", "--version")
	case strings.Contains(shell, "zsh"):
		cmd = exec.Command("zsh", "--version")
	case strings.Contains(shell, "fish"):
		cmd = exec.Command("fish", "--version")
	case strings.Contains(shell, "powershell"), strings.Contains(shell, "pwsh"):
		cmd = exec.Command("pwsh", "-Version")
	default:
		return ""
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	first := strings.Split(strings.TrimSpace(string(out)), "\n")[0]
	return first
}

func gitOutput(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func environmentKeys() []string {
	envs := os.Environ()
	keys := make([]string, 0, len(envs))
	for _, e := range envs {
		if idx := strings.IndexByte(e, '='); idx > 0 {
			keys = append(keys, e[:idx])
		}
	}
	return keys
}
