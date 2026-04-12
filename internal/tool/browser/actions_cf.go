package browser

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
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
		title := page.MustInfo().Title
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
		title := page.MustInfo().Title
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
				info := page.MustInfo()
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
func clickTurnstileCheckbox(page *rod.Page) error {
	// Try to find Turnstile iframe and click the checkbox inside it
	_, err := page.Eval(`() => {
		let iframes = document.querySelectorAll('iframe[src*="challenges.cloudflare.com"]');
		if (iframes.length === 0) return false;
		// Simulate click on the iframe area (Turnstile checkbox is typically centered)
		let rect = iframes[0].getBoundingClientRect();
		let x = rect.left + rect.width / 2;
		let y = rect.top + rect.height / 2;
		let evt = new MouseEvent('click', { clientX: x, clientY: y, bubbles: true });
		iframes[0].dispatchEvent(evt);
		return true;
	}`)
	return err
}
