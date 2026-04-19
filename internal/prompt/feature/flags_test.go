package feature

import (
	"testing"
)

func TestAllFlagsCount(t *testing.T) {
	flags := AllFlags()
	if got, want := len(flags), 17; got != want {
		t.Fatalf("AllFlags count = %d, want %d", got, want)
	}
}

func TestAllFlagsUnique(t *testing.T) {
	seen := map[Flag]struct{}{}
	for _, f := range AllFlags() {
		if _, dup := seen[f]; dup {
			t.Errorf("duplicate flag: %s", f)
		}
		seen[f] = struct{}{}
	}
}

func TestEnvVarMapping(t *testing.T) {
	cases := map[Flag]string{
		FlagProactive:               "CLAUDE_CODE_PROACTIVE",
		FlagKairos:                  "CLAUDE_CODE_KAIROS",
		FlagCachedMicrocompact:      "CLAUDE_CODE_CACHED_MICROCOMPACT",
		FlagExperimentalSkillSearch: "CLAUDE_CODE_EXPERIMENTAL_SKILL_SEARCH",
		FlagVoiceMode:               "CLAUDE_CODE_VOICE_MODE",
	}
	for f, want := range cases {
		if got := EnvVar(f); got != want {
			t.Errorf("EnvVar(%s) = %q, want %q", f, got, want)
		}
	}
}

func TestIsEnabled_DefaultOff(t *testing.T) {
	ClearAllOverrides()
	for _, f := range AllFlags() {
		// Env might leak in from the dev machine; only assert if the env is
		// unset.
		t.Setenv(EnvVar(f), "")
	}
	for _, f := range AllFlags() {
		if IsEnabled(f) {
			t.Errorf("flag %s should default to false, got true", f)
		}
	}
}

func TestIsEnabled_EnvTruthy(t *testing.T) {
	ClearAllOverrides()
	for _, raw := range []string{"1", "true", "yes", "on", "TRUE", " YES "} {
		t.Setenv(EnvVar(FlagProactive), raw)
		if !IsEnabled(FlagProactive) {
			t.Errorf("env=%q should enable PROACTIVE", raw)
		}
	}
}

func TestIsEnabled_EnvFalsy(t *testing.T) {
	ClearAllOverrides()
	for _, raw := range []string{"", "0", "false", "no", "off", "random"} {
		t.Setenv(EnvVar(FlagKairos), raw)
		if IsEnabled(FlagKairos) {
			t.Errorf("env=%q should disable KAIROS", raw)
		}
	}
}

func TestOverrideTakesPrecedence(t *testing.T) {
	t.Cleanup(ClearAllOverrides)
	t.Setenv(EnvVar(FlagTokenBudget), "1")
	SetOverride(FlagTokenBudget, false)
	if IsEnabled(FlagTokenBudget) {
		t.Error("SetOverride(false) should beat env=1")
	}
	SetOverride(FlagTokenBudget, true)
	t.Setenv(EnvVar(FlagTokenBudget), "0")
	if !IsEnabled(FlagTokenBudget) {
		t.Error("SetOverride(true) should beat env=0")
	}
	ClearOverride(FlagTokenBudget)
	if IsEnabled(FlagTokenBudget) {
		t.Error("after ClearOverride, env=0 should disable")
	}
}

func TestSnapshotCoversAllFlags(t *testing.T) {
	ClearAllOverrides()
	snap := Snapshot()
	if len(snap) != len(AllFlags()) {
		t.Fatalf("snapshot size = %d, want %d", len(snap), len(AllFlags()))
	}
	for _, f := range AllFlags() {
		if _, ok := snap[f]; !ok {
			t.Errorf("snapshot missing flag %s", f)
		}
	}
}
