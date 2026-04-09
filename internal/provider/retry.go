package provider

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

// ────────────────────────────────────────────────────────────────────────────
// Retry config & constants — aligned with claude-code-main withRetry.ts.
// ────────────────────────────────────────────────────────────────────────────

const (
	// max529Retries is the maximum consecutive 529 errors before triggering
	// a fallback model switch. Aligned with TS MAX_529_RETRIES = 3.
	max529Retries = 3

	// baseDelayMs is the starting delay for exponential backoff.
	// Aligned with TS BASE_DELAY_MS = 500.
	baseDelayMs = 500

	// floorOutputTokens is the minimum output tokens to preserve on context overflow.
	// Aligned with TS FLOOR_OUTPUT_TOKENS = 3000.
	floorOutputTokens = 3000
)

// RetryConfig controls the exponential-backoff retry behaviour.
type RetryConfig struct {
	// MaxAttempts is the total number of tries (including the first).  Default 4.
	MaxAttempts int
	// BaseDelay is the initial back-off interval.  Default 500 ms.
	BaseDelay time.Duration
	// MaxDelay caps the back-off interval.  Default 32 s.
	MaxDelay time.Duration
	// JitterFraction adds ±JitterFraction*delay random jitter.  Default 0.25.
	JitterFraction float64
	// FallbackModel is the model to switch to after max529Retries 529 errors.
	FallbackModel string
	// Max529Retries caps consecutive 529 errors before fallback. Default 3.
	Max529Retries int
}

// DefaultRetryConfig returns a sensible production retry config.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:    11, // 10 retries + 1 initial, aligned with TS DEFAULT_MAX_RETRIES=10
		BaseDelay:      500 * time.Millisecond,
		MaxDelay:       32 * time.Second,
		JitterFraction: 0.25,
		Max529Retries:  max529Retries,
	}
}

// FallbackTriggeredError is returned when the retry loop triggers a model
// fallback after repeated 529 errors. Aligned with TS FallbackTriggeredError.
type FallbackTriggeredError struct {
	OriginalModel string
	FallbackModel string
}

func (e *FallbackTriggeredError) Error() string {
	return fmt.Sprintf("model fallback triggered: %s -> %s", e.OriginalModel, e.FallbackModel)
}

// WithRetry calls fn up to cfg.MaxAttempts times, backing off between attempts.
// It stops immediately on non-retryable errors.
// If a server-supplied Retry-After value is available it is honoured.
// On repeated 529 errors, triggers FallbackTriggeredError if FallbackModel is set.
func WithRetry[T any](ctx context.Context, cfg RetryConfig, fn func() (T, error)) (T, error) {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 11
	}
	if cfg.BaseDelay == 0 {
		cfg.BaseDelay = 500 * time.Millisecond
	}
	if cfg.MaxDelay == 0 {
		cfg.MaxDelay = 32 * time.Second
	}
	if cfg.JitterFraction == 0 {
		cfg.JitterFraction = 0.25
	}
	if cfg.Max529Retries <= 0 {
		cfg.Max529Retries = max529Retries
	}

	var zero T
	var lastErr error
	var consecutive529 int
	delay := cfg.BaseDelay

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		if ctx.Err() != nil {
			return zero, ctx.Err()
		}

		val, err := fn()
		if err == nil {
			return val, nil
		}
		lastErr = err

		apiErr := CategorizeRetryableAPIError(err)
		if apiErr == nil {
			// Non-retryable: give up immediately.
			return zero, err
		}

		// Track consecutive 529 errors for fallback logic.
		if apiErr.Category == ErrCatOverloaded {
			consecutive529++
			if consecutive529 >= cfg.Max529Retries && cfg.FallbackModel != "" {
				return zero, &FallbackTriggeredError{
					OriginalModel: "current",
					FallbackModel: cfg.FallbackModel,
				}
			}
		} else {
			consecutive529 = 0
		}

		if attempt == cfg.MaxAttempts {
			break
		}

		// Determine sleep duration.
		sleep := delay
		if apiErr.RetryAfterSecs > 0 {
			sleep = time.Duration(apiErr.RetryAfterSecs) * time.Second
		}
		sleep = addJitter(sleep, cfg.JitterFraction)
		if sleep > cfg.MaxDelay {
			sleep = cfg.MaxDelay
		}

		slog.Info("provider: retrying after error",
			slog.Int("attempt", attempt),
			slog.Int("consecutive_529", consecutive529),
			slog.String("category", string(apiErr.Category)),
			slog.Duration("backoff", sleep))

		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(sleep):
		}

		// Exponential back-off.
		delay *= 2
		if delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}
	}
	return zero, lastErr
}

// ParseRetryAfterHeader extracts the Retry-After value from an HTTP response header.
// Returns 0 if the header is missing or unparseable.
// Aligned with TS getRetryAfter / getRetryAfterMs.
func ParseRetryAfterHeader(h http.Header) int {
	val := h.Get("Retry-After")
	if val == "" {
		return 0
	}
	secs, err := strconv.Atoi(val)
	if err == nil && secs > 0 {
		return secs
	}
	return 0
}

// ParseRateLimitResetHeader extracts the anthropic-ratelimit-unified-reset header.
// Returns duration until reset, or 0 if not present.
// Aligned with TS getRateLimitResetDelayMs.
func ParseRateLimitResetHeader(h http.Header) time.Duration {
	val := h.Get("anthropic-ratelimit-unified-reset")
	if val == "" {
		return 0
	}
	resetUnixSec, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return 0
	}
	delayMs := resetUnixSec*1000 - float64(time.Now().UnixMilli())
	if delayMs <= 0 {
		return 0
	}
	// Cap at 6 hours (aligned with TS PERSISTENT_RESET_CAP_MS).
	const maxResetDuration = 6 * time.Hour
	d := time.Duration(delayMs) * time.Millisecond
	if d > maxResetDuration {
		d = maxResetDuration
	}
	return d
}

// GetRetryDelay computes the delay for a given retry attempt, optionally
// using a server-supplied Retry-After value.
// Aligned with TS getRetryDelay.
func GetRetryDelay(attempt int, retryAfterSecs int, maxDelay time.Duration) time.Duration {
	if retryAfterSecs > 0 {
		return time.Duration(retryAfterSecs) * time.Second
	}
	base := time.Duration(baseDelayMs) * time.Millisecond
	for i := 1; i < attempt; i++ {
		base *= 2
		if base > maxDelay {
			base = maxDelay
			break
		}
	}
	jitter := time.Duration(rand.Float64() * 0.25 * float64(base)) //nolint:gosec
	return base + jitter
}

// addJitter multiplies d by a random factor in [1-f, 1+f].
func addJitter(d time.Duration, f float64) time.Duration {
	if f <= 0 {
		return d
	}
	delta := float64(d) * f
	jitter := (rand.Float64()*2 - 1) * delta //nolint:gosec
	return time.Duration(float64(d) + jitter)
}
