package engine

import "os"

// ────────────────────────────────────────────────────────────────────────────
// [P8.T7] ResolveThinkingConfig — mirrors TS QueryEngine.ts:L278-282
// ────────────────────────────────────────────────────────────────────────────

// ResolveThinkingConfig returns the effective thinking config for a turn.
// If the user explicitly provided one, use it. Otherwise, check
// shouldEnableThinkingByDefault() and return adaptive or disabled.
func ResolveThinkingConfig(userConfig *ThinkingConfig) ThinkingConfig {
	if userConfig != nil {
		return *userConfig
	}
	if shouldEnableThinkingByDefault() {
		return ThinkingConfig{
			Enabled:        true,
			BudgetTokens:   thinkingDefaultBudget,
			AdaptiveBudget: true,
		}
	}
	return ThinkingConfig{Enabled: false}
}

// shouldEnableThinkingByDefault checks whether thinking should be enabled
// by default. TS checks getConfigValue("enableThinking") but we simplify
// to an env var check for now.
func shouldEnableThinkingByDefault() bool {
	v := os.Getenv("CLAUDE_CODE_DISABLE_THINKING")
	return v == "" || v == "0" || v == "false"
}
