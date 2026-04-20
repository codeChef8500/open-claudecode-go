package engine

import (
	"testing"
)

func TestPermDenialTracker_RecordDenial(t *testing.T) {
	tracker := NewPermDenialTracker()

	// Record a denial with SDK-compatible name mapping
	tracker.RecordDenial("BashTool", "tu-1", map[string]interface{}{"command": "rm -rf /"})

	denials := tracker.Denials()
	if len(denials) != 1 {
		t.Fatalf("denials len = %d, want 1", len(denials))
	}
	if denials[0].ToolName != "Bash" {
		t.Errorf("tool_name = %s, want Bash", denials[0].ToolName)
	}
	if denials[0].ToolUseID != "tu-1" {
		t.Errorf("tool_use_id = %s, want tu-1", denials[0].ToolUseID)
	}
	if denials[0].ToolInput["command"] != "rm -rf /" {
		t.Errorf("tool_input missing command field")
	}
}

func TestPermDenialTracker_NoDenials(t *testing.T) {
	tracker := NewPermDenialTracker()
	if len(tracker.Denials()) != 0 {
		t.Errorf("denials len = %d, want 0", len(tracker.Denials()))
	}
}

func TestPermDenialTracker_MultipleDenials(t *testing.T) {
	tracker := NewPermDenialTracker()

	tracker.RecordDenial("EditTool", "tu-a", nil)
	tracker.RecordDenial("WriteTool", "tu-b", nil)

	denials := tracker.Denials()
	if len(denials) != 2 {
		t.Fatalf("denials len = %d, want 2", len(denials))
	}
	if denials[0].ToolName != "Edit" {
		t.Errorf("denial[0].tool_name = %s, want Edit", denials[0].ToolName)
	}
	if denials[1].ToolName != "Write" {
		t.Errorf("denial[1].tool_name = %s, want Write", denials[1].ToolName)
	}
}

func TestPermDenialTracker_Reset(t *testing.T) {
	tracker := NewPermDenialTracker()
	tracker.RecordDenial("BashTool", "tu-1", nil)

	if len(tracker.Denials()) != 1 {
		t.Fatalf("expected 1 denial before reset")
	}
	tracker.Reset()
	if len(tracker.Denials()) != 0 {
		t.Errorf("denials len = %d, want 0 after reset", len(tracker.Denials()))
	}
}

func TestPermDenialTracker_DenialsSnapshot(t *testing.T) {
	tracker := NewPermDenialTracker()
	tracker.RecordDenial("BashTool", "tu-1", nil)

	snap := tracker.Denials()
	// Adding more denials should not affect the snapshot
	tracker.RecordDenial("EditTool", "tu-2", nil)

	if len(snap) != 1 {
		t.Errorf("snapshot should still be 1, got %d", len(snap))
	}
	if len(tracker.Denials()) != 2 {
		t.Errorf("tracker should have 2, got %d", len(tracker.Denials()))
	}
}

func TestSdkCompatToolName(t *testing.T) {
	checks := map[string]string{
		"BashTool":      "Bash",
		"EditTool":      "Edit",
		"GlobTool":      "Glob",
		"GrepTool":      "Grep",
		"ReadTool":      "Read",
		"WriteTool":     "Write",
		"MultiEditTool": "MultiEdit",
		"AgentTool":     "Task",
		"WebFetchTool":  "WebFetch",
		"WebSearchTool": "WebSearch",
		"CustomMCP":     "CustomMCP", // passthrough
		"UnknownTool":   "UnknownTool",
	}
	for input, want := range checks {
		got := SdkCompatToolName(input)
		if got != want {
			t.Errorf("SdkCompatToolName(%s) = %s, want %s", input, got, want)
		}
	}
}
