package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTP webhook hook execution — aligned with claude-code-main execHttpHook.ts.
//
// An HTTP hook sends the hook payload as JSON to a configured URL endpoint
// and parses the response as a HookJSONOutput.

const (
	httpHookDefaultTimeout = 30 * time.Second
	httpHookMaxResponseBody = 1 << 20 // 1 MB
)

// runHTTPHook sends the hook payload to an HTTP endpoint and parses the response.
func runHTTPHook(ctx context.Context, cfg HookConfig, input *HookInput) SyncHookResponse {
	if cfg.URL == "" {
		return SyncHookResponse{
			Error: fmt.Errorf("http hook: URL is required"),
		}
	}

	// SSRF guard: block private/internal URLs.
	if err := validateHookURL(cfg.URL); err != nil {
		return SyncHookResponse{
			Error: fmt.Errorf("http hook SSRF guard: %w", err),
		}
	}

	// Serialize input to JSON body.
	body, err := json.Marshal(input)
	if err != nil {
		return SyncHookResponse{
			Error: fmt.Errorf("http hook: marshal input: %w", err),
		}
	}

	method := cfg.Method
	if method == "" {
		method = http.MethodPost
	}

	req, err := http.NewRequestWithContext(ctx, method, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return SyncHookResponse{
			Error: fmt.Errorf("http hook: create request: %w", err),
		}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "open-claudecode-go/hooks")

	// Apply custom headers.
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{
		Timeout: httpHookDefaultTimeout,
		// Use SSRF-safe transport that blocks private IP connections.
		Transport: &ssrfSafeTransport{base: http.DefaultTransport},
	}

	resp, err := client.Do(req)
	if err != nil {
		return SyncHookResponse{
			Error: fmt.Errorf("http hook %q: request failed: %w", cfg.URL, err),
		}
	}
	defer resp.Body.Close()

	// Read response body (limited).
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, httpHookMaxResponseBody))
	if err != nil {
		return SyncHookResponse{
			Error: fmt.Errorf("http hook %q: read response: %w", cfg.URL, err),
		}
	}

	// Non-2xx status is treated as a block.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SyncHookResponse{
			Decision: "block",
			Error:    fmt.Errorf("http hook %q returned status %d: %s", cfg.URL, resp.StatusCode, string(respBody)),
		}
	}

	// Parse JSON response.
	output := strings.TrimSpace(string(respBody))
	if output == "" {
		return SyncHookResponse{} // no output = no opinion
	}

	var raw HookJSONOutput
	if err := json.Unmarshal([]byte(output), &raw); err != nil {
		return SyncHookResponse{
			Error: fmt.Errorf("http hook %q: parse response: %w (raw: %.200s)", cfg.URL, err, output),
		}
	}

	return parseHookOutput(raw)
}

// ────────────────────────────────────────────────────────────────────────────
// SSRF Guard — prevents hooks from accessing internal/private networks
// Aligned with claude-code-main ssrfGuard.ts
// ────────────────────────────────────────────────────────────────────────────

// validateHookURL checks that a URL is not targeting a private/internal address.
func validateHookURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Only allow http/https schemes.
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme %q: only http/https allowed", u.Scheme)
	}

	host := u.Hostname()

	// Block localhost variants.
	lower := strings.ToLower(host)
	if lower == "localhost" || lower == "0.0.0.0" || lower == "[::1]" || lower == "::1" {
		return fmt.Errorf("blocked localhost address: %s", host)
	}

	// Resolve hostname to IP and check for private ranges.
	ips, err := net.LookupHost(host)
	if err != nil {
		// If DNS fails, allow — the HTTP client will fail anyway.
		return nil
	}

	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if isPrivateIP(ip) {
			return fmt.Errorf("blocked private IP %s for host %s", ipStr, host)
		}
	}

	return nil
}

// isPrivateIP checks if an IP address is in a private/reserved range.
func isPrivateIP(ip net.IP) bool {
	// RFC 1918 private ranges.
	privateRanges := []struct {
		network string
		mask    int
	}{
		{"10.0.0.0", 8},
		{"172.16.0.0", 12},
		{"192.168.0.0", 16},
		{"127.0.0.0", 8},        // loopback
		{"169.254.0.0", 16},     // link-local
		{"::1", 128},            // IPv6 loopback
		{"fe80::", 10},          // IPv6 link-local
		{"fc00::", 7},           // IPv6 unique local
	}

	for _, r := range privateRanges {
		_, cidr, err := net.ParseCIDR(fmt.Sprintf("%s/%d", r.network, r.mask))
		if err != nil {
			continue
		}
		if cidr.Contains(ip) {
			return true
		}
	}

	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate()
}

// ssrfSafeTransport wraps an http.RoundTripper and blocks connections to private IPs.
type ssrfSafeTransport struct {
	base http.RoundTripper
}

func (t *ssrfSafeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()

	// Re-check at connection time (DNS may resolve differently than validation).
	ips, err := net.LookupHost(host)
	if err == nil {
		for _, ipStr := range ips {
			ip := net.ParseIP(ipStr)
			if ip != nil && isPrivateIP(ip) {
				return nil, fmt.Errorf("SSRF blocked: %s resolves to private IP %s", host, ipStr)
			}
		}
	}

	return t.base.RoundTrip(req)
}
