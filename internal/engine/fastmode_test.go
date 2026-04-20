package engine

import "testing"

func TestGetFastModeState_Nil(t *testing.T) {
	if got := GetFastModeState("claude-sonnet-4-6", nil); got != FastModeOff {
		t.Errorf("got %s, want off", got)
	}
}

func TestGetFastModeState_False(t *testing.T) {
	f := false
	if got := GetFastModeState("claude-sonnet-4-6", &f); got != FastModeOff {
		t.Errorf("got %s, want off", got)
	}
}

func TestGetFastModeState_True(t *testing.T) {
	tr := true
	if got := GetFastModeState("claude-sonnet-4-6", &tr); got != FastModeOn {
		t.Errorf("got %s, want on", got)
	}
}
