package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ── OAuth 2.0 + PKCE for MCP servers ────────────────────────────────────────
// Aligned with claude-code-main src/services/mcp/auth.ts core flow:
//   1. Discover OAuth metadata from server
//   2. Register client dynamically (RFC 7591) if needed
//   3. Generate PKCE challenge
//   4. Open browser for authorization
//   5. Listen on localhost for callback
//   6. Exchange code for tokens
//   7. Persist tokens for reuse
//   8. Refresh tokens on expiry

const (
	authRequestTimeout  = 30 * time.Second
	callbackTimeout     = 5 * time.Minute
	defaultRedirectPath = "/oauth/callback"
)

// OAuthTokens holds the tokens returned by the authorization server.
type OAuthTokens struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// OAuthClientInfo holds the dynamically registered client credentials.
type OAuthClientInfo struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
	RedirectURI  string `json:"redirect_uri,omitempty"`
}

// OAuthServerMetadata is the authorization server metadata (RFC 8414).
type OAuthServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint,omitempty"`
	ScopesSupported                   []string `json:"scopes_supported,omitempty"`
	ResponseTypesSupported            []string `json:"response_types_supported,omitempty"`
	GrantTypesSupported               []string `json:"grant_types_supported,omitempty"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported,omitempty"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported,omitempty"`
}

// OAuthState holds the in-progress OAuth flow state.
type OAuthState struct {
	ServerURL    string
	Metadata     *OAuthServerMetadata
	ClientInfo   *OAuthClientInfo
	Tokens       *OAuthTokens
	CodeVerifier string
	State        string
}

// OAuthStorage defines the interface for persisting OAuth credentials.
type OAuthStorage interface {
	// SaveTokens persists tokens for a server URL.
	SaveTokens(serverURL string, tokens *OAuthTokens) error
	// LoadTokens loads tokens for a server URL, or nil if not found.
	LoadTokens(serverURL string) (*OAuthTokens, error)
	// SaveClientInfo persists the dynamically registered client info.
	SaveClientInfo(serverURL string, info *OAuthClientInfo) error
	// LoadClientInfo loads client info, or nil if not found.
	LoadClientInfo(serverURL string) (*OAuthClientInfo, error)
}

// OAuthProvider manages OAuth authentication for MCP servers.
type OAuthProvider struct {
	mu      sync.Mutex
	storage OAuthStorage
	// openBrowser is called to open the authorization URL in the user's browser.
	// Callers should inject a function appropriate for their platform.
	openBrowser func(url string) error
}

// NewOAuthProvider creates an OAuthProvider with the given storage backend.
func NewOAuthProvider(storage OAuthStorage, browserFn func(string) error) *OAuthProvider {
	if browserFn == nil {
		browserFn = func(url string) error {
			slog.Info("mcp oauth: please open this URL in your browser", slog.String("url", url))
			return nil
		}
	}
	return &OAuthProvider{storage: storage, openBrowser: browserFn}
}

// Authenticate performs the full OAuth flow for an MCP server URL.
// Returns an OAuthState with valid tokens.
func (p *OAuthProvider) Authenticate(ctx context.Context, serverURL string) (*OAuthState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	state := &OAuthState{ServerURL: serverURL}

	// Try loading existing tokens.
	if tokens, err := p.storage.LoadTokens(serverURL); err == nil && tokens != nil {
		state.Tokens = tokens
		return state, nil
	}

	// Step 1: Discover OAuth metadata.
	meta, err := discoverOAuthMetadata(ctx, serverURL)
	if err != nil {
		return nil, fmt.Errorf("mcp oauth: metadata discovery: %w", err)
	}
	state.Metadata = meta

	// Step 2: Load or register client.
	clientInfo, err := p.storage.LoadClientInfo(serverURL)
	if err != nil || clientInfo == nil {
		clientInfo, err = dynamicClientRegistration(ctx, meta, serverURL)
		if err != nil {
			return nil, fmt.Errorf("mcp oauth: client registration: %w", err)
		}
		if err := p.storage.SaveClientInfo(serverURL, clientInfo); err != nil {
			slog.Warn("mcp oauth: failed to save client info", slog.Any("err", err))
		}
	}
	state.ClientInfo = clientInfo

	// Step 3: Generate PKCE verifier + challenge.
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return nil, fmt.Errorf("mcp oauth: PKCE generation: %w", err)
	}
	state.CodeVerifier = verifier
	state.State = generateRandomState()

	// Step 4: Start local callback server.
	listener, port, err := findAvailablePort()
	if err != nil {
		return nil, fmt.Errorf("mcp oauth: no available port: %w", err)
	}
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d%s", port, defaultRedirectPath)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := startCallbackServer(listener, state.State, codeCh, errCh)

	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	// Step 5: Build authorization URL and open browser.
	authURL := buildAuthorizationURL(meta.AuthorizationEndpoint, clientInfo.ClientID,
		redirectURI, state.State, challenge)

	slog.Info("mcp oauth: opening browser for authorization",
		slog.String("server", serverURL))
	if err := p.openBrowser(authURL); err != nil {
		slog.Warn("mcp oauth: failed to open browser", slog.Any("err", err))
		fmt.Fprintf(os.Stderr, "\nPlease open this URL to authenticate:\n%s\n\n", authURL)
	}

	// Step 6: Wait for authorization code.
	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return nil, fmt.Errorf("mcp oauth: callback error: %w", err)
	case <-time.After(callbackTimeout):
		return nil, fmt.Errorf("mcp oauth: authorization timeout (waited %v)", callbackTimeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Step 7: Exchange code for tokens.
	tokens, err := exchangeCodeForTokens(ctx, meta.TokenEndpoint,
		clientInfo.ClientID, clientInfo.ClientSecret,
		code, redirectURI, verifier)
	if err != nil {
		return nil, fmt.Errorf("mcp oauth: token exchange: %w", err)
	}
	state.Tokens = tokens

	// Step 8: Persist tokens.
	if err := p.storage.SaveTokens(serverURL, tokens); err != nil {
		slog.Warn("mcp oauth: failed to save tokens", slog.Any("err", err))
	}

	return state, nil
}

// RefreshTokens attempts to refresh expired tokens.
func (p *OAuthProvider) RefreshTokens(ctx context.Context, serverURL string) (*OAuthTokens, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	tokens, err := p.storage.LoadTokens(serverURL)
	if err != nil || tokens == nil || tokens.RefreshToken == "" {
		return nil, fmt.Errorf("mcp oauth: no refresh token available")
	}

	meta, err := discoverOAuthMetadata(ctx, serverURL)
	if err != nil {
		return nil, fmt.Errorf("mcp oauth: metadata discovery for refresh: %w", err)
	}

	clientInfo, err := p.storage.LoadClientInfo(serverURL)
	if err != nil || clientInfo == nil {
		return nil, fmt.Errorf("mcp oauth: no client info for refresh")
	}

	newTokens, err := refreshTokens(ctx, meta.TokenEndpoint,
		clientInfo.ClientID, clientInfo.ClientSecret, tokens.RefreshToken)
	if err != nil {
		return nil, err
	}

	// Preserve refresh token if server didn't issue a new one.
	if newTokens.RefreshToken == "" {
		newTokens.RefreshToken = tokens.RefreshToken
	}

	if err := p.storage.SaveTokens(serverURL, newTokens); err != nil {
		slog.Warn("mcp oauth: failed to save refreshed tokens", slog.Any("err", err))
	}

	return newTokens, nil
}

// ── Discovery ────────────────────────────────────────────────────────────────

// discoverOAuthMetadata fetches the OAuth authorization server metadata.
// Tries RFC 9728 (protected resource → auth server) first, then RFC 8414.
func discoverOAuthMetadata(ctx context.Context, serverURL string) (*OAuthServerMetadata, error) {
	ctx, cancel := context.WithTimeout(ctx, authRequestTimeout)
	defer cancel()

	parsed, err := url.Parse(serverURL)
	if err != nil {
		return nil, err
	}

	// Try /.well-known/oauth-authorization-server (RFC 8414).
	wellKnownURL := fmt.Sprintf("%s://%s/.well-known/oauth-authorization-server",
		parsed.Scheme, parsed.Host)
	if parsed.Path != "" && parsed.Path != "/" {
		wellKnownURL += parsed.Path
	}

	resp, err := httpGetJSON(ctx, wellKnownURL)
	if err == nil {
		defer resp.Body.Close()
		var meta OAuthServerMetadata
		if err := json.NewDecoder(resp.Body).Decode(&meta); err == nil && meta.AuthorizationEndpoint != "" {
			return &meta, nil
		}
	}

	// Fallback: try without path.
	wellKnownURL = fmt.Sprintf("%s://%s/.well-known/oauth-authorization-server",
		parsed.Scheme, parsed.Host)
	resp, err = httpGetJSON(ctx, wellKnownURL)
	if err != nil {
		return nil, fmt.Errorf("metadata discovery failed: %w", err)
	}
	defer resp.Body.Close()

	var meta OAuthServerMetadata
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("parse metadata: %w", err)
	}
	if meta.AuthorizationEndpoint == "" {
		return nil, fmt.Errorf("metadata missing authorization_endpoint")
	}
	return &meta, nil
}

// ── Dynamic Client Registration (RFC 7591) ──────────────────────────────────

func dynamicClientRegistration(ctx context.Context, meta *OAuthServerMetadata, serverURL string) (*OAuthClientInfo, error) {
	if meta.RegistrationEndpoint == "" {
		// No registration endpoint — assume public client.
		return &OAuthClientInfo{ClientID: "claude-code-mcp"}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, authRequestTimeout)
	defer cancel()

	payload := map[string]interface{}{
		"client_name":                  "Claude Code (agent-engine)",
		"redirect_uris":               []string{"http://127.0.0.1/oauth/callback"},
		"grant_types":                  []string{"authorization_code", "refresh_token"},
		"response_types":              []string{"code"},
		"token_endpoint_auth_method":  "none",
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, meta.RegistrationEndpoint,
		strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registration failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse registration response: %w", err)
	}
	return &OAuthClientInfo{
		ClientID:     result.ClientID,
		ClientSecret: result.ClientSecret,
	}, nil
}

// ── PKCE (RFC 7636) ─────────────────────────────────────────────────────────

func generatePKCE() (verifier, challenge string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge, nil
}

func generateRandomState() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

// ── Authorization URL ────────────────────────────────────────────────────────

func buildAuthorizationURL(endpoint, clientID, redirectURI, state, challenge string) string {
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	if strings.Contains(endpoint, "?") {
		return endpoint + "&" + params.Encode()
	}
	return endpoint + "?" + params.Encode()
}

// ── Local callback server ────────────────────────────────────────────────────

func findAvailablePort() (net.Listener, int, error) {
	// Try a range of ports commonly used for OAuth callbacks.
	for _, port := range []int{9876, 9877, 9878, 9879, 9880, 0} {
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		if port == 0 {
			addr = "127.0.0.1:0"
		}
		l, err := net.Listen("tcp", addr)
		if err == nil {
			return l, l.Addr().(*net.TCPAddr).Port, nil
		}
	}
	return nil, 0, fmt.Errorf("no available port for OAuth callback")
}

func startCallbackServer(listener net.Listener, expectedState string, codeCh chan<- string, errCh chan<- error) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc(defaultRedirectPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		if errMsg := q.Get("error"); errMsg != "" {
			desc := q.Get("error_description")
			errCh <- fmt.Errorf("authorization error: %s (%s)", errMsg, desc)
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, "<html><body><h1>Authorization Failed</h1><p>%s</p></body></html>", errMsg)
			return
		}

		state := q.Get("state")
		if state != expectedState {
			errCh <- fmt.Errorf("state mismatch: expected %q, got %q", expectedState, state)
			http.Error(w, "State mismatch", http.StatusBadRequest)
			return
		}

		code := q.Get("code")
		if code == "" {
			errCh <- fmt.Errorf("no authorization code in callback")
			http.Error(w, "Missing code", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, "<html><body><h1>Authorization Successful</h1><p>You can close this window.</p></body></html>")
		codeCh <- code
	})

	srv := &http.Server{Handler: mux}
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Debug("mcp oauth: callback server error", slog.Any("err", err))
		}
	}()
	return srv
}

// ── Token exchange ──────────────────────────────────────────────────────────

func exchangeCodeForTokens(ctx context.Context, tokenEndpoint, clientID, clientSecret, code, redirectURI, verifier string) (*OAuthTokens, error) {
	ctx, cancel := context.WithTimeout(ctx, authRequestTimeout)
	defer cancel()

	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"code_verifier": {verifier},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint,
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if clientSecret != "" {
		req.SetBasicAuth(clientID, clientSecret)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token exchange failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokens OAuthTokens
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	return &tokens, nil
}

func refreshTokens(ctx context.Context, tokenEndpoint, clientID, clientSecret, refreshToken string) (*OAuthTokens, error) {
	ctx, cancel := context.WithTimeout(ctx, authRequestTimeout)
	defer cancel()

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint,
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if clientSecret != "" {
		req.SetBasicAuth(clientID, clientSecret)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token refresh failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokens OAuthTokens
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return nil, fmt.Errorf("parse refresh response: %w", err)
	}
	return &tokens, nil
}

// ── File-based storage ──────────────────────────────────────────────────────

// FileOAuthStorage persists OAuth tokens and client info to the filesystem.
// Tokens are stored in ~/.claude/oauth/<server_hash>/tokens.json.
type FileOAuthStorage struct {
	baseDir string
}

// NewFileOAuthStorage creates a FileOAuthStorage rooted at the given directory.
// If dir is empty, defaults to ~/.claude/oauth.
func NewFileOAuthStorage(dir string) *FileOAuthStorage {
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".claude", "oauth")
	}
	return &FileOAuthStorage{baseDir: dir}
}

func (s *FileOAuthStorage) serverDir(serverURL string) string {
	h := sha256.Sum256([]byte(serverURL))
	hash := base64.RawURLEncoding.EncodeToString(h[:8])
	return filepath.Join(s.baseDir, hash)
}

func (s *FileOAuthStorage) SaveTokens(serverURL string, tokens *OAuthTokens) error {
	dir := s.serverDir(serverURL)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(tokens, "", "  ")
	return os.WriteFile(filepath.Join(dir, "tokens.json"), data, 0o600)
}

func (s *FileOAuthStorage) LoadTokens(serverURL string) (*OAuthTokens, error) {
	path := filepath.Join(s.serverDir(serverURL), "tokens.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var tokens OAuthTokens
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, err
	}
	return &tokens, nil
}

func (s *FileOAuthStorage) SaveClientInfo(serverURL string, info *OAuthClientInfo) error {
	dir := s.serverDir(serverURL)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(info, "", "  ")
	return os.WriteFile(filepath.Join(dir, "client.json"), data, 0o600)
}

func (s *FileOAuthStorage) LoadClientInfo(serverURL string) (*OAuthClientInfo, error) {
	path := filepath.Join(s.serverDir(serverURL), "client.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var info OAuthClientInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// ── HTTP helper ──────────────────────────────────────────────────────────────

func httpGetJSON(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return resp, nil
}
