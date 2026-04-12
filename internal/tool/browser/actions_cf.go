package browser

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// doWaitCFChallenge waits for Cloudflare challenge to complete.
// Detection strategy: title-based + DOM feature detection + Turnstile iframe.
func (t *BrowserTool) doWaitCFChallenge(in *Input) string {
	_, page, err := t.getSessionAndPage(in)
	if err != nil {
		return errStr(err)
	}

	timeout := 30 * time.Second
	if in.CFChallengeTimeout > 0 {
		timeout = time.Duration(in.CFChallengeTimeout) * time.Millisecond
	}

	deadline := time.Now().Add(timeout)

	// Phase 1: Detect challenge presence
	challengeDetected := false
	for time.Now().Before(deadline) {
		title := safeInfo(page).Title
		titleLower := strings.ToLower(title)

		if strings.Contains(titleLower, "just a moment") ||
			strings.Contains(titleLower, "attention required") ||
			strings.Contains(titleLower, "checking your browser") {
			challengeDetected = true
			break
		}

		// Check for Cloudflare DOM markers
		res, _ := page.Eval(`() => {
			return !!(document.querySelector('#challenge-running') ||
				document.querySelector('#challenge-form') ||
				document.querySelector('.cf-browser-verification') ||
				document.querySelector('#cf-challenge-running') ||
				document.querySelector('iframe[src*="challenges.cloudflare.com"]'));
		}`)
		if res != nil && res.Value.Bool() {
			challengeDetected = true
			break
		}

		time.Sleep(500 * time.Millisecond)
	}

	if !challengeDetected {
		return "No Cloudflare challenge detected. Page may already be accessible."
	}

	// Phase 2: Try to click Turnstile checkbox if present
	_ = clickTurnstileCheckbox(page)

	// Phase 3: Wait for challenge to resolve
	for time.Now().Before(deadline) {
		title := safeInfo(page).Title
		titleLower := strings.ToLower(title)

		if !strings.Contains(titleLower, "just a moment") &&
			!strings.Contains(titleLower, "attention required") &&
			!strings.Contains(titleLower, "checking your browser") {
			// Challenge might be done — verify
			res, _ := page.Eval(`() => {
				return !(document.querySelector('#challenge-running') ||
					document.querySelector('#challenge-form') ||
					document.querySelector('.cf-browser-verification'));
			}`)
			if res != nil && res.Value.Bool() {
				info := safeInfo(page)
				return fmt.Sprintf("Cloudflare challenge passed!\n  URL: %s\n  Title: %s", info.URL, info.Title)
			}
		}

		// Retry Turnstile click periodically
		_ = clickTurnstileCheckbox(page)

		time.Sleep(1 * time.Second)
	}

	if in.CFScreenshotOnFail {
		data, _ := page.Screenshot(false, nil)
		if len(data) > 0 {
			return "Cloudflare challenge timed out. Screenshot captured (in memory)."
		}
	}
	return fmt.Sprintf("Cloudflare challenge timed out after %v.", timeout)
}

func (t *BrowserTool) doExtractCFClearance(in *Input) string {
	_, page, err := t.getSessionAndPage(in)
	if err != nil {
		return errStr(err)
	}
	cookies, err := page.Cookies(nil)
	if err != nil {
		return fmt.Sprintf("extract_cf_clearance failed: %v", err)
	}

	for _, c := range cookies {
		if c.Name == "cf_clearance" {
			return fmt.Sprintf("cf_clearance found.\n  value: %s\n  domain: %s\n  expires: %.0f\n  secure: %v",
				truncStr(c.Value, 80), c.Domain, c.Expires, c.Secure)
		}
	}
	return "cf_clearance cookie not found. Challenge may not have completed."
}

func (t *BrowserTool) doVerifyCFClearance(in *Input) string {
	_, page, err := t.getSessionAndPage(in)
	if err != nil {
		return errStr(err)
	}
	cookies, err := page.Cookies(nil)
	if err != nil {
		return fmt.Sprintf("verify_cf_clearance failed: %v", err)
	}

	for _, c := range cookies {
		if c.Name == "cf_clearance" {
			// Check if not expired
			if c.Expires > 0 && float64(c.Expires) < float64(time.Now().Unix()) {
				return "cf_clearance cookie exists but is expired."
			}
			return fmt.Sprintf("cf_clearance is valid.\n  domain: %s\n  expires: %.0f\n  value: %s",
				c.Domain, c.Expires, truncStr(c.Value, 40))
		}
	}
	return "cf_clearance not found — Cloudflare bypass not active."
}

// clickTurnstileCheckbox attempts to find and click the Cloudflare Turnstile checkbox.
// Uses CDP Input.dispatchMouseEvent to click at absolute coordinates, which can
// penetrate cross-origin iframes (unlike JS MouseEvent dispatch).
func clickTurnstileCheckbox(page *rod.Page) error {
	// Get the bounding rect of the Turnstile iframe via JS
	res, err := page.Eval(`() => {
		let iframes = document.querySelectorAll('iframe[src*="challenges.cloudflare.com"]');
		if (iframes.length === 0) return null;
		let rect = iframes[0].getBoundingClientRect();
		return { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 };
	}`)
	if err != nil || res == nil || res.Value.Nil() {
		return fmt.Errorf("turnstile iframe not found")
	}

	x := res.Value.Get("x").Num()
	y := res.Value.Get("y").Num()

	// CDP mouse click at absolute page coordinates (crosses iframe boundary)
	_ = proto.InputDispatchMouseEvent{
		Type:       proto.InputDispatchMouseEventTypeMousePressed,
		X:          x,
		Y:          y,
		Button:     proto.InputMouseButtonLeft,
		ClickCount: 1,
	}.Call(page)
	time.Sleep(50 * time.Millisecond)
	_ = proto.InputDispatchMouseEvent{
		Type:       proto.InputDispatchMouseEventTypeMouseReleased,
		X:          x,
		Y:          y,
		Button:     proto.InputMouseButtonLeft,
		ClickCount: 1,
	}.Call(page)

	return nil
}
