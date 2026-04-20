package engine

import "testing"

func TestIsAtBlockingLimit(t *testing.T) {
	if IsAtBlockingLimit(90000, 100000) {
		t.Error("90% should not be at blocking limit")
	}
	if !IsAtBlockingLimit(95000, 100000) {
		t.Error("95% should be at blocking limit")
	}
	if !IsAtBlockingLimit(100000, 100000) {
		t.Error("100% should be at blocking limit")
	}
	if IsAtBlockingLimit(100, 0) {
		t.Error("0 context window should not block")
	}
}

func TestShouldSkipBlockingLimitCheck(t *testing.T) {
	// Compaction just happened → skip
	if !ShouldSkipBlockingLimitCheck(true, "sdk", false, false, false) {
		t.Error("should skip after compaction")
	}
	// Compact query source → skip
	if !ShouldSkipBlockingLimitCheck(false, QuerySourceCompact, false, false, false) {
		t.Error("should skip for compact source")
	}
	// Reactive + auto → skip
	if !ShouldSkipBlockingLimitCheck(false, "sdk", true, true, false) {
		t.Error("should skip when reactive+auto enabled")
	}
	// Collapse + auto → skip
	if !ShouldSkipBlockingLimitCheck(false, "sdk", false, true, true) {
		t.Error("should skip when collapse+auto enabled")
	}
	// Normal case → don't skip
	if ShouldSkipBlockingLimitCheck(false, "sdk", false, false, false) {
		t.Error("should not skip in normal case")
	}
}

func TestTaskBudgetRemainingTracker(t *testing.T) {
	tracker := NewTaskBudgetRemainingTracker(100000)

	// Before any compaction, remaining is nil
	if tracker.GetRemaining() != nil {
		t.Error("expected nil remaining before compaction")
	}

	// First compaction: 60000 tokens
	tracker.RecordCompaction(60000)
	rem := tracker.GetRemaining()
	if rem == nil || *rem != 40000 {
		t.Errorf("expected 40000 remaining, got %v", rem)
	}

	// Second compaction: 30000 more
	tracker.RecordCompaction(30000)
	rem = tracker.GetRemaining()
	if rem == nil || *rem != 10000 {
		t.Errorf("expected 10000 remaining, got %v", rem)
	}

	// Third compaction exceeding remaining
	tracker.RecordCompaction(20000)
	rem = tracker.GetRemaining()
	if rem == nil || *rem != 0 {
		t.Errorf("expected 0 remaining, got %v", rem)
	}
}
