package bash

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ────────────────────────────────────────────────────────────────────────────
// PersistentShell maintains a long-running shell process so that environment
// variables, directory changes, and other shell state persist across tool
// invocations.
//
// Aligned with claude-code-main tools/BashTool/BashPersistentShell.ts.
// ────────────────────────────────────────────────────────────────────────────

// sentinel used to detect command completion in output.
const sentinel = "__AGENT_CMD_DONE__"

// PersistentShell wraps a long-running shell process.
type PersistentShell struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  io.ReadCloser
	workDir string
	alive   bool
}

// NewPersistentShell starts a persistent shell in the given working directory.
func NewPersistentShell(workDir string) (*PersistentShell, error) {
	shell := defaultShell()
	args := defaultShellArgs()

	cmd := exec.Command(shell, args...)
	cmd.Dir = workDir
	cmd.Env = os.Environ()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("persistent shell: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("persistent shell: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("persistent shell: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("persistent shell: start: %w", err)
	}

	ps := &PersistentShell{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		stderr:  stderr,
		workDir: workDir,
		alive:   true,
	}

	slog.Info("persistent shell: started",
		slog.String("shell", shell),
		slog.String("workDir", workDir),
		slog.Int("pid", cmd.Process.Pid))

	return ps, nil
}

// Execute runs a command in the persistent shell and returns output.
func (ps *PersistentShell) Execute(ctx context.Context, command string, timeoutMs int) (*Output, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if !ps.alive {
		return nil, fmt.Errorf("persistent shell: process not running")
	}

	if timeoutMs <= 0 {
		timeoutMs = defaultTimeout
	}

	// Write command followed by sentinel echo so we can detect completion.
	wrappedCmd := fmt.Sprintf("%s\necho %s $?\n", command, sentinel)
	if _, err := io.WriteString(ps.stdin, wrappedCmd); err != nil {
		ps.alive = false
		return nil, fmt.Errorf("persistent shell: write: %w", err)
	}

	// Read stdout until sentinel line appears.
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	var stdoutBuf strings.Builder
	var stderrBuf strings.Builder
	exitCode := 0

	// Read stderr in background.
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		scanner := bufio.NewScanner(ps.stderr)
		for scanner.Scan() {
			line := scanner.Text()
			stderrBuf.WriteString(line)
			stderrBuf.WriteString("\n")
		}
	}()

	// Read stdout line by line until sentinel.
	scanner := bufio.NewScanner(ps.stdout)
	for {
		select {
		case <-ctx.Done():
			return &Output{
				Stdout:      stdoutBuf.String(),
				Stderr:      stderrBuf.String(),
				ExitCode:    -1,
				Interrupted: true,
			}, nil
		default:
		}

		if !scanner.Scan() {
			ps.alive = false
			break
		}
		line := scanner.Text()

		if strings.HasPrefix(line, sentinel) {
			// Parse exit code from sentinel line.
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				fmt.Sscanf(parts[1], "%d", &exitCode)
			}
			break
		}
		stdoutBuf.WriteString(line)
		stdoutBuf.WriteString("\n")
	}

	return &Output{
		Stdout:   strings.TrimRight(stdoutBuf.String(), "\n"),
		Stderr:   strings.TrimRight(stderrBuf.String(), "\n"),
		ExitCode: exitCode,
	}, nil
}

// Restart kills the current shell and starts a new one.
func (ps *PersistentShell) Restart() error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.cmd != nil && ps.cmd.Process != nil {
		_ = ps.cmd.Process.Kill()
		_ = ps.cmd.Wait()
	}
	ps.alive = false

	// Start new shell.
	shell := defaultShell()
	args := defaultShellArgs()

	cmd := exec.Command(shell, args...)
	cmd.Dir = ps.workDir
	cmd.Env = os.Environ()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	ps.cmd = cmd
	ps.stdin = stdin
	ps.stdout = stdout
	ps.stderr = stderr
	ps.alive = true

	slog.Info("persistent shell: restarted", slog.Int("pid", cmd.Process.Pid))
	return nil
}

// Close terminates the persistent shell.
func (ps *PersistentShell) Close() error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.stdin != nil {
		_ = ps.stdin.Close()
	}
	if ps.cmd != nil && ps.cmd.Process != nil {
		_ = ps.cmd.Process.Kill()
		return ps.cmd.Wait()
	}
	ps.alive = false
	return nil
}

// IsAlive reports whether the shell process is still running.
func (ps *PersistentShell) IsAlive() bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.alive
}

// ── Platform defaults ────────────────────────────────────────────────────────

func defaultShell() string {
	if runtime.GOOS == "windows" {
		if ps, err := exec.LookPath("pwsh.exe"); err == nil {
			return ps
		}
		return "powershell.exe"
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "/bin/sh"
}

func defaultShellArgs() []string {
	if runtime.GOOS == "windows" {
		return []string{"-NoProfile", "-NonInteractive", "-Command", "-"}
	}
	return []string{"-i"}
}
