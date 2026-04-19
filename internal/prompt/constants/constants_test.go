package constants

import (
	"strings"
	"testing"
)

// ── cyberrisk ──────────────────────────────────────────────────────────────

func TestCyberRiskInstruction_NonEmpty(t *testing.T) {
	if CyberRiskInstruction == "" {
		t.Fatal("CyberRiskInstruction must not be empty")
	}
	if !strings.HasPrefix(CyberRiskInstruction, "IMPORTANT:") {
		t.Error("CyberRiskInstruction should start with IMPORTANT:")
	}
}

// ── xml_tags ───────────────────────────────────────────────────────────────

func TestTickTag(t *testing.T) {
	if TickTag != "tick" {
		t.Errorf("TickTag = %q, want %q", TickTag, "tick")
	}
}

func TestTerminalOutputTags_Length(t *testing.T) {
	if got, want := len(TerminalOutputTags), 6; got != want {
		t.Errorf("TerminalOutputTags length = %d, want %d", got, want)
	}
}

func TestForkDirectivePrefix(t *testing.T) {
	if ForkDirectivePrefix != "Your directive: " {
		t.Errorf("ForkDirectivePrefix = %q", ForkDirectivePrefix)
	}
}

func TestCommonHelpArgs(t *testing.T) {
	if len(CommonHelpArgs) != 3 {
		t.Errorf("CommonHelpArgs length = %d, want 3", len(CommonHelpArgs))
	}
}

func TestCommonInfoArgs(t *testing.T) {
	if len(CommonInfoArgs) != 13 {
		t.Errorf("CommonInfoArgs length = %d, want 13", len(CommonInfoArgs))
	}
}

// ── model_ids ──────────────────────────────────────────────────────────────

func TestFrontierModelName(t *testing.T) {
	if FrontierModelName == "" {
		t.Fatal("FrontierModelName must not be empty")
	}
}

func TestClaude45Or46ModelIDs(t *testing.T) {
	for _, tier := range []string{"opus", "sonnet", "haiku"} {
		if _, ok := Claude45Or46ModelIDs[tier]; !ok {
			t.Errorf("Claude45Or46ModelIDs missing tier %q", tier)
		}
	}
}

// ── boundary ───────────────────────────────────────────────────────────────

func TestSystemPromptDynamicBoundary(t *testing.T) {
	if SystemPromptDynamicBoundary != "__SYSTEM_PROMPT_DYNAMIC_BOUNDARY__" {
		t.Errorf("boundary = %q", SystemPromptDynamicBoundary)
	}
}

func TestClaudeCodeDocsMapURL(t *testing.T) {
	if !strings.HasPrefix(ClaudeCodeDocsMapURL, "https://") {
		t.Errorf("docs map URL should be https, got %q", ClaudeCodeDocsMapURL)
	}
}

// ── common (date helpers) ──────────────────────────────────────────────────

func TestGetLocalISODate_Format(t *testing.T) {
	d := GetLocalISODate()
	if len(d) != 10 || d[4] != '-' || d[7] != '-' {
		t.Errorf("GetLocalISODate() = %q, expected YYYY-MM-DD format", d)
	}
}

func TestGetLocalISODate_Override(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OVERRIDE_DATE", "2099-12-31")
	if got := GetLocalISODate(); got != "2099-12-31" {
		t.Errorf("GetLocalISODate with override = %q, want 2099-12-31", got)
	}
}

func TestGetSessionStartDate_Memoized(t *testing.T) {
	ResetSessionStartDate()
	t.Setenv("CLAUDE_CODE_OVERRIDE_DATE", "2000-01-01")
	first := GetSessionStartDate()
	t.Setenv("CLAUDE_CODE_OVERRIDE_DATE", "2099-12-31")
	second := GetSessionStartDate()
	if first != second {
		t.Errorf("memoized date changed: %q → %q", first, second)
	}
	ResetSessionStartDate()
}

func TestGetLocalMonthYear(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OVERRIDE_DATE", "2026-02-15")
	got := GetLocalMonthYear()
	if got != "February 2026" {
		t.Errorf("GetLocalMonthYear = %q, want %q", got, "February 2026")
	}
}
