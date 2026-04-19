package prompt

import (
	"os"
	"runtime"
	"strings"
)

// getShell returns the current shell name.
func getShell() string {
	shell := os.Getenv("SHELL")
	if shell == "" && runtime.GOOS == "windows" {
		shell = os.Getenv("ComSpec")
		if shell == "" {
			shell = "cmd.exe"
		}
	}
	if shell == "" {
		return "unknown"
	}
	// Extract short name
	switch {
	case strings.Contains(shell, "zsh"):
		return "zsh"
	case strings.Contains(shell, "bash"):
		return "bash"
	case strings.Contains(shell, "powershell"), strings.Contains(shell, "pwsh"):
		return "powershell"
	default:
		return shell
	}
}

// getOSVersion returns a uname-like OS version string.
func getOSVersion() string {
	if runtime.GOOS == "windows" {
		// Best-effort: use PROCESSOR_ARCHITECTURE + OS env
		ver := os.Getenv("OS")
		if ver == "" {
			ver = "Windows_NT"
		}
		return ver
	}
	// On Unix-like, runtime doesn't expose uname directly — use a placeholder.
	// Production code should call uname(2) via syscall; for now return GOOS.
	return runtime.GOOS + " " + runtime.GOARCH
}
