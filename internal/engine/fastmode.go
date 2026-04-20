package engine

// ────────────────────────────────────────────────────────────────────────────
// [P7.T3] Fast mode state — simplified version of TS utils/fastMode.ts
// ────────────────────────────────────────────────────────────────────────────

// GetFastModeState returns the fast mode toggle state for SDK reporting.
// TS anchor: utils/fastMode.ts:L319-335 (getFastModeState)
//
// The full fast mode subsystem (isFastModeEnabled, isFastModeAvailable,
// isFastModeSupportedByModel, cooldown tracking) is not yet ported.
// This provides a simplified version: if fastMode is explicitly true, return "on";
// otherwise "off". Cooldown is deferred to P10.
func GetFastModeState(model string, fastMode *bool) FastModeState {
	if fastMode != nil && *fastMode {
		return FastModeOn
	}
	return FastModeOff
}
