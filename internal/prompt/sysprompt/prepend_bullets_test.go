package sysprompt

import (
	"strings"
	"testing"
)

func TestPrependBullets_SingleStrings(t *testing.T) {
	got := PrependBullets("alpha", "beta")
	want := []string{" - alpha", " - beta"}
	assertEqualSlice(t, got, want)
}

func TestPrependBullets_NestedSlice(t *testing.T) {
	got := PrependBullets([]string{"sub1", "sub2"})
	want := []string{"  - sub1", "  - sub2"}
	assertEqualSlice(t, got, want)
}

func TestPrependBullets_Mixed(t *testing.T) {
	got := PrependBullets("top", []string{"nested1", "nested2"}, "another")
	want := []string{
		" - top",
		"  - nested1",
		"  - nested2",
		" - another",
	}
	assertEqualSlice(t, got, want)
}

func TestPrependBullets_Empty(t *testing.T) {
	got := PrependBullets()
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestPrependBullets_EmptySubSlice(t *testing.T) {
	got := PrependBullets("a", []string{}, "b")
	want := []string{" - a", " - b"}
	assertEqualSlice(t, got, want)
}

// ── TS parity: verify exact spacing ────────────────────────────────────────

func TestPrependBullets_TopLevelSpacing(t *testing.T) {
	lines := PrependBullets("x")
	if !strings.HasPrefix(lines[0], " - ") {
		t.Errorf("top-level should start with ' - ', got %q", lines[0])
	}
}

func TestPrependBullets_NestedSpacing(t *testing.T) {
	lines := PrependBullets([]string{"y"})
	if !strings.HasPrefix(lines[0], "  - ") {
		t.Errorf("nested should start with '  - ', got %q", lines[0])
	}
}

func assertEqualSlice(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %d, want %d\ngot:  %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q, want %q", i, got[i], want[i])
		}
	}
}
