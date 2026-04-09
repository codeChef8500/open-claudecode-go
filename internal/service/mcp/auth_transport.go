package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// AuthTransport wraps another Transport and injects OAuth Bearer tokens
// into HTTP-based transports. If a connection attempt returns a 401/403,
// it triggers the OAuth flow and retries.
type AuthTransport struct {
	inner    Transport
	provider *OAuthProvider
	serverURL string

	mu     sync.Mutex
	tokens *OAuthTokens
}

// NewAuthTransport wraps a transport with OAuth authentication.
// If provider is nil, the inner transport is used directly (no auth).
func NewAuthTransport(inner Transport, provider *OAuthProvider, serverURL string) Transport {
	if provider == nil {
		return inner
	}
	return &AuthTransport{
		inner:     inner,
		provider:  provider,
		serverURL: serverURL,
	}
}

// Start initializes the inner transport. For SSE/HTTP transports, it first
// attempts to load cached tokens and inject them as headers.
func (t *AuthTransport) Start(ctx context.Context) error {
	// Try to load cached tokens before starting.
	if state, err := t.provider.Authenticate(ctx, t.serverURL); err == nil && state.Tokens != nil {
		t.mu.Lock()
		t.tokens = state.Tokens
		t.mu.Unlock()

		// Inject Authorization header into the inner transport if supported.
		t.injectAuthHeader(state.Tokens.AccessToken)
	}

	err := t.inner.Start(ctx)
	if err != nil && isAuthError(err) {
		// Connection failed with auth error — trigger full OAuth flow.
		slog.Info("mcp auth: transport start requires authentication",
			slog.String("server", t.serverURL))

		state, authErr := t.provider.Authenticate(ctx, t.serverURL)
		if authErr != nil {
			return fmt.Errorf("mcp auth: authentication failed: %w", authErr)
		}

		t.mu.Lock()
		t.tokens = state.Tokens
		t.mu.Unlock()

		t.injectAuthHeader(state.Tokens.AccessToken)

		// Retry the connection.
		return t.inner.Start(ctx)
	}
	return err
}

// Send delegates to the inner transport.
func (t *AuthTransport) Send(msg []byte) error {
	return t.inner.Send(msg)
}

// Receive delegates to the inner transport.
func (t *AuthTransport) Receive() <-chan []byte {
	return t.inner.Receive()
}

// Close delegates to the inner transport.
func (t *AuthTransport) Close() error {
	return t.inner.Close()
}

// injectAuthHeader adds the Bearer token to the inner transport's headers
// if it supports header injection.
func (t *AuthTransport) injectAuthHeader(token string) {
	switch inner := t.inner.(type) {
	case *SSETransport:
		inner.mu.Lock()
		if inner.headers == nil {
			inner.headers = make(map[string]string)
		}
		inner.headers["Authorization"] = "Bearer " + token
		inner.mu.Unlock()
	case *HTTPTransport:
		inner.mu.Lock()
		if inner.headers == nil {
			inner.headers = make(map[string]string)
		}
		inner.headers["Authorization"] = "Bearer " + token
		inner.mu.Unlock()
	}
}

// isAuthError checks if an error indicates authentication is required.
func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return containsAny(msg, "401", "403", "unauthorized", "Unauthorized", "forbidden")
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
