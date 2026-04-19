package sysprompt

import (
	"strings"
	"testing"
)

// ── P4.T1 env info ─────────────────────────────────────────────────────────

func TestComputeSimpleEnvInfo_Basic(t *testing.T) {
	s := ComputeSimpleEnvInfo(EnvInfoOpts{
		CWD:       "/home/user/project",
		IsGit:     true,
		Platform:  "linux",
		Shell:     "bash",
		OSVersion: "Linux 6.6.4",
		ModelID:   "claude-sonnet-4-6",
	})
	if !strings.HasPrefix(s, "# Environment") {
		t.Error("missing header")
	}
	if !strings.Contains(s, "/home/user/project") {
		t.Error("missing CWD")
	}
	if !strings.Contains(s, "Is a git repository: Yes") {
		t.Error("missing git info")
	}
	if !strings.Contains(s, "Claude Code is available") {
		t.Error("missing Claude Code availability line")
	}
}

func TestComputeSimpleEnvInfo_Worktree(t *testing.T) {
	s := ComputeSimpleEnvInfo(EnvInfoOpts{
		CWD:        "/wt/branch",
		IsWorktree: true,
		Platform:   "darwin",
		Shell:      "zsh",
		OSVersion:  "Darwin 25.3.0",
	})
	if !strings.Contains(s, "git worktree") {
		t.Error("should mention worktree")
	}
}

func TestComputeSimpleEnvInfo_Undercover(t *testing.T) {
	s := ComputeSimpleEnvInfo(EnvInfoOpts{
		CWD:          "/tmp",
		IsAnt:        true,
		IsUndercover: true,
		Platform:     "linux",
		Shell:        "bash",
		OSVersion:    "Linux 6.6",
	})
	if strings.Contains(s, "Claude Code is available") {
		t.Error("undercover should suppress model/product lines")
	}
}

func TestComputeSimpleEnvInfo_Windows(t *testing.T) {
	s := ComputeSimpleEnvInfo(EnvInfoOpts{
		CWD:       "C:\\Users\\test",
		Platform:  "win32",
		Shell:     "powershell",
		OSVersion: "Windows 11",
	})
	if !strings.Contains(s, "Unix shell syntax") {
		t.Error("Windows should have shell hint about Unix syntax")
	}
}

func TestComputeSimpleEnvInfo_AdditionalDirs(t *testing.T) {
	s := ComputeSimpleEnvInfo(EnvInfoOpts{
		CWD:                   "/main",
		AdditionalWorkingDirs: []string{"/extra1", "/extra2"},
		Platform:              "linux",
		Shell:                 "bash",
		OSVersion:             "Linux 6",
	})
	if !strings.Contains(s, "Additional working directories") {
		t.Error("should list additional dirs")
	}
	if !strings.Contains(s, "/extra1") {
		t.Error("missing extra dir")
	}
}

// ── P4.T1 knowledge cutoff ────────────────────────────────────────────────

func TestGetKnowledgeCutoff(t *testing.T) {
	cases := map[string]string{
		"claude-sonnet-4-6-20260101": "August 2025",
		"claude-opus-4-6":            "May 2025",
		"claude-opus-4-5-20251001":   "May 2025",
		"claude-haiku-4-5-20251001":  "February 2025",
		"claude-opus-4-20250115":     "January 2025",
		"claude-sonnet-4-20250115":   "January 2025",
		"unknown-model":              "",
	}
	for model, want := range cases {
		if got := GetKnowledgeCutoff(model); got != want {
			t.Errorf("GetKnowledgeCutoff(%q) = %q, want %q", model, got, want)
		}
	}
}

// ── P4.T2 enhance system prompt ────────────────────────────────────────────

func TestEnhanceSystemPromptWithEnvDetails(t *testing.T) {
	existing := []string{"base prompt"}
	out := EnhanceSystemPromptWithEnvDetails(existing, EnhanceOpts{
		EnvInfo: "env info here",
	})
	if len(out) < 3 {
		t.Fatalf("expected at least 3 elements, got %d", len(out))
	}
	if out[0] != "base prompt" {
		t.Error("should preserve existing prompt")
	}
	if !strings.Contains(out[1], "absolute file paths") {
		t.Error("missing agent notes")
	}
	if out[len(out)-1] != "env info here" {
		t.Error("missing env info at end")
	}
}

func TestEnhanceSystemPromptWithEnvDetails_DiscoverSkills(t *testing.T) {
	out := EnhanceSystemPromptWithEnvDetails([]string{"base"}, EnhanceOpts{
		DiscoverSkillsEnabled:  true,
		DiscoverSkillsToolName: "DiscoverSkills",
		EnabledToolNames:       map[string]bool{"DiscoverSkills": true},
		EnvInfo:                "env",
	})
	found := false
	for _, s := range out {
		if strings.Contains(s, "DiscoverSkills") {
			found = true
			break
		}
	}
	if !found {
		t.Error("should include discover skills guidance")
	}
}
