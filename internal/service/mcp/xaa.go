package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// XAA (Extended Authentication & Authorization) — aligned with claude-code-main
// xaa.ts and xaaIdpLogin.ts.
//
// XAA extends the standard OAuth flow with enterprise IdP (Identity Provider)
// integration. It supports:
//   - SAML and OIDC IdP discovery
//   - IdP-initiated login flows
//   - Token exchange (RFC 8693) for cross-domain auth
//   - Enterprise SSO session management

// ────────────────────────────────────────────────────────────────────────────
// Types
// ────────────────────────────────────────────────────────────────────────────

// XAAConfig holds enterprise IdP configuration.
type XAAConfig struct {
	// IdPURL is the IdP's base URL (e.g., "https://sso.example.com").
	IdPURL string `json:"idp_url"`
	// IdPType is the federation protocol: "saml", "oidc".
	IdPType string `json:"idp_type"`
	// ClientID is the application client ID registered with the IdP.
	ClientID string `json:"client_id"`
	// TenantID is the enterprise tenant identifier.
	TenantID string `json:"tenant_id,omitempty"`
	// Audience is the target API audience for token exchange.
	Audience string `json:"audience,omitempty"`
	// Scopes are the requested OAuth scopes.
	Scopes []string `json:"scopes,omitempty"`
	// TokenExchangeURL is the RFC 8693 token exchange endpoint.
	TokenExchangeURL string `json:"token_exchange_url,omitempty"`
}

// XAASession holds the active XAA session state.
type XAASession struct {
	mu           sync.RWMutex
	config       XAAConfig
	idpToken     string
	accessToken  string
	refreshToken string
	expiresAt    time.Time
	subject      string // authenticated user identity
}

// IdPMetadata describes the IdP's capabilities discovered via well-known endpoints.
type IdPMetadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint,omitempty"`
	JWKSUri               string `json:"jwks_uri,omitempty"`
	EndSessionEndpoint    string `json:"end_session_endpoint,omitempty"`
}

// ────────────────────────────────────────────────────────────────────────────
// XAA Client
// ────────────────────────────────────────────────────────────────────────────

// XAAClient manages enterprise IdP authentication flows.
type XAAClient struct {
	config     XAAConfig
	session    *XAASession
	httpClient *http.Client
	metadata   *IdPMetadata
}

// NewXAAClient creates a new XAA client with the given configuration.
func NewXAAClient(config XAAConfig) *XAAClient {
	return &XAAClient{
		config: config,
		session: &XAASession{
			config: config,
		},
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// DiscoverIdP fetches the IdP metadata from the well-known endpoint.
func (c *XAAClient) DiscoverIdP(ctx context.Context) (*IdPMetadata, error) {
	wellKnownURL := c.config.IdPURL
	switch c.config.IdPType {
	case "oidc":
		wellKnownURL += "/.well-known/openid-configuration"
	case "saml":
		wellKnownURL += "/.well-known/saml-metadata"
	default:
		wellKnownURL += "/.well-known/openid-configuration"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnownURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create IdP discovery request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("IdP discovery request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("IdP discovery returned %d: %s", resp.StatusCode, string(body))
	}

	var meta IdPMetadata
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("parse IdP metadata: %w", err)
	}

	c.metadata = &meta
	slog.Debug("XAA: discovered IdP", "issuer", meta.Issuer)
	return &meta, nil
}

// StartIdPLogin initiates the IdP login flow. It returns an authorization URL
// that should be opened in a browser.
func (c *XAAClient) StartIdPLogin(ctx context.Context, redirectURI string) (string, error) {
	if c.metadata == nil {
		if _, err := c.DiscoverIdP(ctx); err != nil {
			return "", err
		}
	}

	params := url.Values{
		"response_type": {"code"},
		"client_id":     {c.config.ClientID},
		"redirect_uri":  {redirectURI},
		"scope":         {joinScopes(c.config.Scopes)},
	}
	if c.config.TenantID != "" {
		params.Set("tenant", c.config.TenantID)
	}

	authURL := c.metadata.AuthorizationEndpoint + "?" + params.Encode()
	return authURL, nil
}

// ExchangeIdPCode exchanges the authorization code from the IdP callback
// for tokens.
func (c *XAAClient) ExchangeIdPCode(ctx context.Context, code, redirectURI string) error {
	if c.metadata == nil {
		return fmt.Errorf("IdP metadata not discovered")
	}

	params := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirectURI},
		"client_id":    {c.config.ClientID},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.metadata.TokenEndpoint, nil)
	if err != nil {
		return fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.URL.RawQuery = params.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("token exchange returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("parse token response: %w", err)
	}

	c.session.mu.Lock()
	c.session.idpToken = tokenResp.IDToken
	c.session.accessToken = tokenResp.AccessToken
	c.session.refreshToken = tokenResp.RefreshToken
	if tokenResp.ExpiresIn > 0 {
		c.session.expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}
	c.session.mu.Unlock()

	slog.Debug("XAA: IdP tokens obtained", "expires_in", tokenResp.ExpiresIn)
	return nil
}

// TokenExchange performs RFC 8693 token exchange, swapping the IdP token
// for a target API access token.
func (c *XAAClient) TokenExchange(ctx context.Context) (string, error) {
	c.session.mu.RLock()
	idpToken := c.session.idpToken
	c.session.mu.RUnlock()

	if idpToken == "" {
		return "", fmt.Errorf("no IdP token available — run IdP login first")
	}

	exchangeURL := c.config.TokenExchangeURL
	if exchangeURL == "" {
		if c.metadata != nil {
			exchangeURL = c.metadata.TokenEndpoint
		} else {
			return "", fmt.Errorf("no token exchange URL configured")
		}
	}

	params := url.Values{
		"grant_type":           {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token":        {idpToken},
		"subject_token_type":   {"urn:ietf:params:oauth:token-type:id_token"},
		"requested_token_type": {"urn:ietf:params:oauth:token-type:access_token"},
	}
	if c.config.Audience != "" {
		params.Set("audience", c.config.Audience)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, exchangeURL, nil)
	if err != nil {
		return "", fmt.Errorf("create exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.URL.RawQuery = params.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("token exchange returned %d: %s", resp.StatusCode, string(body))
	}

	var exchangeResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&exchangeResp); err != nil {
		return "", fmt.Errorf("parse exchange response: %w", err)
	}

	slog.Debug("XAA: token exchange successful", "expires_in", exchangeResp.ExpiresIn)
	return exchangeResp.AccessToken, nil
}

// IsAuthenticated reports whether the session has a valid (non-expired) token.
func (c *XAAClient) IsAuthenticated() bool {
	c.session.mu.RLock()
	defer c.session.mu.RUnlock()
	return c.session.accessToken != "" &&
		(c.session.expiresAt.IsZero() || time.Now().Before(c.session.expiresAt))
}

// AccessToken returns the current access token, or empty if not authenticated.
func (c *XAAClient) AccessToken() string {
	c.session.mu.RLock()
	defer c.session.mu.RUnlock()
	return c.session.accessToken
}

// Logout clears the session state.
func (c *XAAClient) Logout() {
	c.session.mu.Lock()
	defer c.session.mu.Unlock()
	c.session.idpToken = ""
	c.session.accessToken = ""
	c.session.refreshToken = ""
	c.session.expiresAt = time.Time{}
	c.session.subject = ""
}

func joinScopes(scopes []string) string {
	if len(scopes) == 0 {
		return "openid profile email"
	}
	result := ""
	for i, s := range scopes {
		if i > 0 {
			result += " "
		}
		result += s
	}
	return result
}
