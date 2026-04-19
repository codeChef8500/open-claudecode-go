package sections

import (
	"testing"
)

func TestSystemPromptSection_Basic(t *testing.T) {
	s := SystemPromptSection("test", func() string { return "hello" })
	if s.Name != "test" {
		t.Errorf("Name = %q", s.Name)
	}
	if s.CacheBreak {
		t.Error("should not be CacheBreak")
	}
}

func TestDANGEROUSUncachedSection(t *testing.T) {
	s := DANGEROUSUncachedSystemPromptSection("mcp", func() string { return "data" }, "mcp changes per turn")
	if !s.CacheBreak {
		t.Error("DANGEROUS section should have CacheBreak=true")
	}
}

func TestResolveSections_Cached(t *testing.T) {
	ClearAll()
	calls := 0
	s := SystemPromptSection("cached_test", func() string {
		calls++
		return "value"
	})

	// First resolve: should call Compute
	out := ResolveSections([]Section{s})
	if out[0] != "value" {
		t.Errorf("first resolve = %q", out[0])
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}

	// Second resolve: should serve from cache
	out2 := ResolveSections([]Section{s})
	if out2[0] != "value" {
		t.Errorf("second resolve = %q", out2[0])
	}
	if calls != 1 {
		t.Errorf("expected 1 call (cached), got %d", calls)
	}

	ClearAll()
}

func TestResolveSections_UncachedAlwaysRecomputes(t *testing.T) {
	ClearAll()
	calls := 0
	s := DANGEROUSUncachedSystemPromptSection("volatile", func() string {
		calls++
		return "dynamic"
	}, "test")

	ResolveSections([]Section{s})
	ResolveSections([]Section{s})
	if calls != 2 {
		t.Errorf("DANGEROUS section should compute twice, got %d", calls)
	}

	ClearAll()
}

func TestClearAll_InvalidatesCache(t *testing.T) {
	ClearAll()
	calls := 0
	s := SystemPromptSection("cleartest", func() string {
		calls++
		return "v"
	})

	ResolveSections([]Section{s})
	if calls != 1 {
		t.Fatalf("pre-clear calls = %d", calls)
	}

	ClearAll()

	ResolveSections([]Section{s})
	if calls != 2 {
		t.Errorf("post-clear should recompute, calls = %d", calls)
	}

	ClearAll()
}

func TestResolveSections_EmptyValue(t *testing.T) {
	ClearAll()
	s := SystemPromptSection("empty", func() string { return "" })
	out := ResolveSections([]Section{s})
	if out[0] != "" {
		t.Errorf("expected empty string, got %q", out[0])
	}
	ClearAll()
}

func TestResolveSections_Multiple(t *testing.T) {
	ClearAll()
	secs := []Section{
		SystemPromptSection("a", func() string { return "alpha" }),
		DANGEROUSUncachedSystemPromptSection("b", func() string { return "beta" }, "test"),
		SystemPromptSection("c", func() string { return "" }),
	}
	out := ResolveSections(secs)
	if len(out) != 3 {
		t.Fatalf("len = %d", len(out))
	}
	if out[0] != "alpha" || out[1] != "beta" || out[2] != "" {
		t.Errorf("out = %v", out)
	}
	ClearAll()
}
