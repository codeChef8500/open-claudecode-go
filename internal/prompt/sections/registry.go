// Package sections implements the system-prompt section registry.
//
// [P1.T3] TS anchor: constants/systemPromptSections.ts (all 69 lines).
//
// A Section is a named, lazily-computed fragment of the system prompt.
// Two flavours:
//
//   - systemPromptSection       → memoized (cached until ClearAll)
//   - DANGEROUS uncached section → recomputed every turn (breaks prompt cache)
//
// Callers build a []Section list in getSystemPrompt and hand it to
// ResolveSections to get the final []string prompt fragments.
package sections

// ComputeFn is a function that produces a section's text (or "" for nil).
type ComputeFn func() string

// Section is a named system prompt fragment with a compute function.
type Section struct {
	Name       string
	Compute    ComputeFn
	CacheBreak bool // true → DANGEROUS uncached (recompute every turn)
}

// SystemPromptSection creates a memoized section.
// Computed once, cached until ClearAll() (i.e. /clear or /compact).
func SystemPromptSection(name string, compute ComputeFn) Section {
	return Section{Name: name, Compute: compute, CacheBreak: false}
}

// DANGEROUSUncachedSystemPromptSection creates a volatile section that
// recomputes every turn. This WILL break the prompt cache when the value
// changes.  `reason` is documentation-only (matches TS _reason param).
func DANGEROUSUncachedSystemPromptSection(name string, compute ComputeFn, reason string) Section {
	_ = reason // documentation only — kept for grep-ability
	return Section{Name: name, Compute: compute, CacheBreak: true}
}

// ResolveSections resolves all sections, returning their string values
// (empty string for nil / disabled sections). Cached sections are served
// from the shared cache; DANGEROUS uncached sections always re-run Compute.
func ResolveSections(secs []Section) []string {
	out := make([]string, len(secs))
	for i, s := range secs {
		if !s.CacheBreak {
			if v, ok := cacheGet(s.Name); ok {
				out[i] = v
				continue
			}
		}
		val := s.Compute()
		cacheSet(s.Name, val)
		out[i] = val
	}
	return out
}
