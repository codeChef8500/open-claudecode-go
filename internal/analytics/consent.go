package analytics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ────────────────────────────────────────────────────────────────────────────
// Analytics consent — aligned with claude-code-main analytics/consent.
//
// Users can opt-in/out of analytics collection. The consent state is stored
// in ~/.claude/analytics_consent.json.
// ────────────────────────────────────────────────────────────────────────────

// ConsentStatus represents the user's analytics consent state.
type ConsentStatus string

const (
	ConsentGranted   ConsentStatus = "granted"
	ConsentDenied    ConsentStatus = "denied"
	ConsentUndecided ConsentStatus = "undecided"
)

// consentState is the persisted consent data.
type consentState struct {
	Status    ConsentStatus `json:"status"`
	Timestamp int64         `json:"timestamp,omitempty"`
}

var (
	consentMu     sync.Mutex
	consentCached *consentState
)

// consentPath returns the path to the consent file.
func consentPath() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".claude", "analytics_consent.json")
}

// GetConsent returns the current analytics consent status.
func GetConsent() ConsentStatus {
	consentMu.Lock()
	defer consentMu.Unlock()

	if consentCached != nil {
		return consentCached.Status
	}

	path := consentPath()
	if path == "" {
		return ConsentUndecided
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ConsentUndecided
	}

	var state consentState
	if err := json.Unmarshal(data, &state); err != nil {
		return ConsentUndecided
	}

	consentCached = &state
	return state.Status
}

// SetConsent updates the analytics consent status.
func SetConsent(status ConsentStatus) error {
	consentMu.Lock()
	defer consentMu.Unlock()

	state := consentState{
		Status:    status,
		Timestamp: currentTimeMs(),
	}

	path := consentPath()
	if path == "" {
		return nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	consentCached = &state
	return os.WriteFile(path, data, 0600)
}

// IsAnalyticsEnabled returns true if analytics collection is allowed.
// Analytics is enabled when consent is granted OR undecided (opt-out model).
func IsAnalyticsEnabled() bool {
	return GetConsent() != ConsentDenied
}

// currentTimeMs returns current time in milliseconds.
func currentTimeMs() int64 {
	return nowFunc().UnixMilli()
}

// nowFunc is the time source, replaceable in tests.
var nowFunc = time.Now
