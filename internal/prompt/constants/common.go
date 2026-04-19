package constants

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Common date helpers ported from constants/common.ts.
// [P1.T2] TS anchor: constants/common.ts

// GetLocalISODate returns the local date in YYYY-MM-DD format.
// Respects CLAUDE_CODE_OVERRIDE_DATE env (ant-only testing hook).
func GetLocalISODate() string {
	if override := os.Getenv("CLAUDE_CODE_OVERRIDE_DATE"); override != "" {
		return override
	}
	now := time.Now()
	return fmt.Sprintf("%04d-%02d-%02d", now.Year(), int(now.Month()), now.Day())
}

// sessionStartDate caches the date at first call — aligned with TS memoize(getLocalISODate).
var (
	sessionDateOnce sync.Once
	sessionDate     string
)

// GetSessionStartDate returns the memoized local ISO date captured once at
// session start. Prevents cache-busting at midnight (TS: memoize(getLocalISODate)).
func GetSessionStartDate() string {
	sessionDateOnce.Do(func() {
		sessionDate = GetLocalISODate()
	})
	return sessionDate
}

// ResetSessionStartDate clears the memoized date — for testing only.
func ResetSessionStartDate() {
	sessionDateOnce = sync.Once{}
	sessionDate = ""
}

// GetLocalMonthYear returns "Month YYYY" (e.g. "February 2026") in the
// local timezone. Changes monthly, not daily — used in tool prompts to
// minimize cache busting.
func GetLocalMonthYear() string {
	var d time.Time
	if override := os.Getenv("CLAUDE_CODE_OVERRIDE_DATE"); override != "" {
		if parsed, err := time.Parse("2006-01-02", override); err == nil {
			d = parsed
		} else {
			d = time.Now()
		}
	} else {
		d = time.Now()
	}
	return d.Format("January 2006")
}
