// Package feature provides a central gate for compile-time/runtime feature
// flags used by the prompt builder and the query engine.
//
// Mirrors claude-code-main's feature() helper (see src/** for usage) with a
// Go-idiomatic API: the flag registry is a fixed enum of 17 known flags and
// each flag has a deterministic environment-variable mapping.
//
// Priority (highest → lowest):
//  1. Programmatic override via SetOverride(flag, value).
//  2. Environment variable (CLAUDE_CODE_<FLAG>).
//  3. Default (false).
//
// The default-off policy matches the TS "3P default" behavior: external
// builds have every feature disabled unless the end user opts in via env.
package feature

import (
	"os"
	"strings"
	"sync"
)

// Flag identifies a known feature gate. Names mirror the TS literals used in
// feature('<NAME>') call sites.
type Flag string

// Known flags — keep in sync with constants/prompts.ts and the full TS tree.
const (
	// Proactive / autonomous work
	FlagProactive   Flag = "PROACTIVE"
	FlagKairos      Flag = "KAIROS"
	FlagKairosBrief Flag = "KAIROS_BRIEF"

	// Compact / tool-result lifecycle
	FlagCachedMicrocompact Flag = "CACHED_MICROCOMPACT"

	// Skill search / verification agent
	FlagExperimentalSkillSearch Flag = "EXPERIMENTAL_SKILL_SEARCH"
	FlagVerificationAgent       Flag = "VERIFICATION_AGENT"

	// Budgets
	FlagTokenBudget Flag = "TOKEN_BUDGET"

	// Coordinator / multi-agent
	FlagCoordinatorMode Flag = "COORDINATOR_MODE"

	// Attribution / classification
	FlagCommitAttribution    Flag = "COMMIT_ATTRIBUTION"
	FlagTranscriptClassifier Flag = "TRANSCRIPT_CLASSIFIER"
	FlagBashClassifier       Flag = "BASH_CLASSIFIER"

	// Thinking
	FlagUltrathink Flag = "ULTRATHINK"

	// Telemetry
	FlagEnhancedTelemetryBeta Flag = "ENHANCED_TELEMETRY_BETA"
	FlagPerfettoTracing       Flag = "PERFETTO_TRACING"

	// Misc
	FlagShotStats            Flag = "SHOT_STATS"
	FlagSlowOperationLogging Flag = "SLOW_OPERATION_LOGGING"
	FlagVoiceMode            Flag = "VOICE_MODE"
)

// AllFlags returns the canonical ordered list of known flags.
// Tests rely on this ordering; do not reorder without updating tests.
func AllFlags() []Flag {
	return []Flag{
		FlagProactive,
		FlagKairos,
		FlagKairosBrief,
		FlagCachedMicrocompact,
		FlagExperimentalSkillSearch,
		FlagVerificationAgent,
		FlagTokenBudget,
		FlagCoordinatorMode,
		FlagCommitAttribution,
		FlagTranscriptClassifier,
		FlagBashClassifier,
		FlagUltrathink,
		FlagEnhancedTelemetryBeta,
		FlagPerfettoTracing,
		FlagShotStats,
		FlagSlowOperationLogging,
		FlagVoiceMode,
	}
}

// EnvVar returns the canonical environment variable name for a flag.
// Convention: "CLAUDE_CODE_" + flag literal (e.g. PROACTIVE → CLAUDE_CODE_PROACTIVE).
func EnvVar(f Flag) string {
	return "CLAUDE_CODE_" + string(f)
}

var (
	overrideMu sync.RWMutex
	overrides  = map[Flag]bool{}
	hasOverrid = map[Flag]bool{}
)

// SetOverride installs a programmatic override for a flag (takes precedence
// over the environment).  Primarily for tests; production code should rely on
// env vars.
func SetOverride(f Flag, v bool) {
	overrideMu.Lock()
	defer overrideMu.Unlock()
	overrides[f] = v
	hasOverrid[f] = true
}

// ClearOverride removes an override, so the flag resolves from env again.
func ClearOverride(f Flag) {
	overrideMu.Lock()
	defer overrideMu.Unlock()
	delete(overrides, f)
	delete(hasOverrid, f)
}

// ClearAllOverrides drops every override. Useful for test teardown.
func ClearAllOverrides() {
	overrideMu.Lock()
	defer overrideMu.Unlock()
	overrides = map[Flag]bool{}
	hasOverrid = map[Flag]bool{}
}

// IsEnabled reports whether a feature flag is currently enabled.
// Default is false. The env value is parsed as a semantic boolean: "1",
// "true", "yes", "on" (case-insensitive) → true; anything else → false.
func IsEnabled(f Flag) bool {
	overrideMu.RLock()
	if hasOverrid[f] {
		v := overrides[f]
		overrideMu.RUnlock()
		return v
	}
	overrideMu.RUnlock()

	raw, ok := os.LookupEnv(EnvVar(f))
	if !ok {
		return false
	}
	return parseBool(raw)
}

// Snapshot returns a map of flag → enabled for all known flags. Useful for
// telemetry / debug dumps.
func Snapshot() map[Flag]bool {
	out := make(map[Flag]bool, len(AllFlags()))
	for _, f := range AllFlags() {
		out[f] = IsEnabled(f)
	}
	return out
}

// parseBool matches TS semanticBoolean helpers: truthy ∈ {1,true,yes,on}.
func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on", "y", "t":
		return true
	}
	return false
}
