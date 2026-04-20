package engine

import "testing"

func TestIncrementQueryTracking_New(t *testing.T) {
	qt := IncrementQueryTracking(nil)
	if qt.ChainID == "" {
		t.Error("expected non-empty chain ID")
	}
	if qt.Depth != 0 {
		t.Errorf("depth = %d, want 0", qt.Depth)
	}
}

func TestIncrementQueryTracking_Existing(t *testing.T) {
	initial := &QueryTracking{ChainID: "chain-1", Depth: 3}
	qt := IncrementQueryTracking(initial)
	if qt.ChainID != "chain-1" {
		t.Errorf("chainID = %s, want chain-1", qt.ChainID)
	}
	if qt.Depth != 4 {
		t.Errorf("depth = %d, want 4", qt.Depth)
	}
}
