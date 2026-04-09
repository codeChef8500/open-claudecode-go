package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ── Transport interface ─────────────────────────────────────────────────────

// Transport is the abstraction for communicating with an MCP server.
// All transports must be safe for concurrent use.
type Transport interface {
	// Start initializes the transport connection.
	Start(ctx context.Context) error
	// Send writes a JSON-RPC message to the server.
	Send(msg []byte) error
	// Receive returns a channel that yields incoming JSON-RPC messages.
	Receive() <-chan []byte
	// Close shuts down the transport.
	Close() error
}

// ── Stdio transport ─────────────────────────────────────────────────────────

// StdioTransport communicates with an MCP server via stdin/stdout of a subprocess.
type StdioTransport struct {
	command string
	args    []string
	env     []string

	mu     sync.Mutex
	stdin  io.WriteCloser
	stdout *bufio.Reader
	cmd    *exec.Cmd
	msgCh  chan []byte
	closed chan struct{}
}

// NewStdioTransport creates a transport that spawns a subprocess.
func NewStdioTransport(command string, args, env []string) *StdioTransport {
	return &StdioTransport{
		command: command,
		args:    args,
		env:     env,
		msgCh:   make(chan []byte, 64),
		closed:  make(chan struct{}),
	}
}

// Start spawns the subprocess and starts reading from stdout.
func (t *StdioTransport) Start(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, t.command, t.args...)
	if len(t.env) > 0 {
		cmd.Env = t.env
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdio transport: stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdio transport: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("stdio transport: start: %w", err)
	}

	t.mu.Lock()
	t.cmd = cmd
	t.stdin = stdinPipe
	t.stdout = bufio.NewReader(stdoutPipe)
	t.mu.Unlock()

	go t.readLoop()
	return nil
}

// Send writes a newline-delimited JSON message to the subprocess stdin.
func (t *StdioTransport) Send(msg []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stdin == nil {
		return fmt.Errorf("stdio transport: not started")
	}
	_, err := fmt.Fprintf(t.stdin, "%s\n", msg)
	return err
}

// Receive returns the channel of incoming messages.
func (t *StdioTransport) Receive() <-chan []byte {
	return t.msgCh
}

// Close shuts down the subprocess.
func (t *StdioTransport) Close() error {
	select {
	case <-t.closed:
		return nil
	default:
		close(t.closed)
	}
	t.mu.Lock()
	if t.stdin != nil {
		_ = t.stdin.Close()
	}
	cmd := t.cmd
	t.mu.Unlock()
	if cmd != nil {
		return cmd.Wait()
	}
	return nil
}

func (t *StdioTransport) readLoop() {
	defer close(t.msgCh)
	for {
		select {
		case <-t.closed:
			return
		default:
		}
		line, err := t.stdout.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				slog.Debug("stdio transport: read error", slog.Any("err", err))
			}
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		t.msgCh <- []byte(line)
	}
}

// ── SSE transport ───────────────────────────────────────────────────────────

// SSETransport connects to an MCP server via Server-Sent Events over HTTP.
// Implements the MCP SSE transport specification:
//   - GET to the SSE URL to open an event stream
//   - Server sends an "endpoint" event with the POST URL for messages
//   - Client POSTs JSON-RPC messages to the discovered endpoint
//   - Server streams responses and notifications back over the SSE connection
type SSETransport struct {
	url     string
	headers map[string]string

	mu      sync.Mutex
	msgCh   chan []byte
	closed  chan struct{}
	client  *http.Client
	postURL string        // discovered from SSE endpoint event
	ready   chan struct{} // closed when endpoint is discovered
	ctx     context.Context
	cancel  context.CancelFunc
	baseURL *url.URL // parsed base URL for resolving relative endpoints
}

const (
	sseReadyTimeout   = 30 * time.Second
	sseReconnectDelay = 2 * time.Second
	sseMaxReconnects  = 5
)

// NewSSETransport creates an SSE transport for the given URL.
func NewSSETransport(rawURL string, headers map[string]string) *SSETransport {
	parsed, _ := url.Parse(rawURL)
	return &SSETransport{
		url:     rawURL,
		headers: headers,
		msgCh:   make(chan []byte, 64),
		closed:  make(chan struct{}),
		client:  &http.Client{Timeout: 0}, // no timeout — SSE is long-lived
		ready:   make(chan struct{}),
		baseURL: parsed,
	}
}

// Start connects to the SSE endpoint and begins reading events.
// Blocks until the server's endpoint event is received or timeout.
func (t *SSETransport) Start(ctx context.Context) error {
	t.ctx, t.cancel = context.WithCancel(ctx)

	if err := t.connectSSE(); err != nil {
		return err
	}

	// Wait for endpoint discovery or timeout.
	select {
	case <-t.ready:
		return nil
	case <-time.After(sseReadyTimeout):
		t.Close()
		return fmt.Errorf("sse transport: timeout waiting for endpoint event from %s", t.url)
	case <-ctx.Done():
		t.Close()
		return ctx.Err()
	}
}

func (t *SSETransport) connectSSE() error {
	req, err := http.NewRequestWithContext(t.ctx, http.MethodGet, t.url, nil)
	if err != nil {
		return fmt.Errorf("sse transport: create request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("sse transport: connect: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return fmt.Errorf("sse transport: unexpected status %d", resp.StatusCode)
	}

	go t.readSSELoop(resp.Body)
	return nil
}

// Send posts a JSON-RPC message to the server's message endpoint.
func (t *SSETransport) Send(msg []byte) error {
	// Wait for endpoint to be ready.
	select {
	case <-t.ready:
	case <-t.closed:
		return fmt.Errorf("sse transport: closed")
	}

	t.mu.Lock()
	postURL := t.postURL
	t.mu.Unlock()
	if postURL == "" {
		return fmt.Errorf("sse transport: no message endpoint discovered")
	}

	req, err := http.NewRequest(http.MethodPost, postURL, strings.NewReader(string(msg)))
	if err != nil {
		return fmt.Errorf("sse transport: create post request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("sse transport: post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("sse transport: post status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// Receive returns the channel of incoming messages.
func (t *SSETransport) Receive() <-chan []byte {
	return t.msgCh
}

// Close shuts down the SSE connection.
func (t *SSETransport) Close() error {
	select {
	case <-t.closed:
		return nil
	default:
		close(t.closed)
	}
	if t.cancel != nil {
		t.cancel()
	}
	return nil
}

func (t *SSETransport) readSSELoop(body io.ReadCloser) {
	defer body.Close()

	scanner := bufio.NewScanner(body)
	// Increase scanner buffer for large SSE payloads.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var eventType, eventData string

	for scanner.Scan() {
		select {
		case <-t.closed:
			return
		default:
		}

		line := scanner.Text()
		if line == "" {
			// Empty line = end of event.
			t.processSSEEvent(eventType, eventData)
			eventType = ""
			eventData = ""
			continue
		}

		// SSE comment (keep-alive ping).
		if strings.HasPrefix(line, ":") {
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if eventData != "" {
				eventData += "\n" + data
			} else {
				eventData = data
			}
		} else if line == "data:" {
			// Empty data line per SSE spec.
			if eventData != "" {
				eventData += "\n"
			}
		}
	}

	if err := scanner.Err(); err != nil {
		slog.Debug("sse transport: read error", slog.Any("err", err))
	}

	// Connection dropped — attempt reconnect if not closed.
	select {
	case <-t.closed:
		return
	default:
	}
	slog.Info("sse transport: connection lost, attempting reconnect")
	for attempt := 0; attempt < sseMaxReconnects; attempt++ {
		select {
		case <-t.closed:
			return
		case <-time.After(sseReconnectDelay * time.Duration(1<<uint(attempt))):
		}
		if err := t.connectSSE(); err != nil {
			slog.Warn("sse transport: reconnect failed",
				slog.Int("attempt", attempt+1), slog.Any("err", err))
			continue
		}
		slog.Info("sse transport: reconnected", slog.Int("attempt", attempt+1))
		return // readSSELoop will be started by connectSSE
	}
	slog.Error("sse transport: all reconnect attempts exhausted")
	close(t.msgCh)
}

// resolveEndpointURL resolves a potentially relative endpoint URL against
// the SSE base URL.
func (t *SSETransport) resolveEndpointURL(endpoint string) string {
	if t.baseURL == nil {
		return endpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	// If endpoint is already absolute, use as-is.
	if parsed.IsAbs() {
		return endpoint
	}
	// Resolve relative to the SSE URL's base.
	return t.baseURL.ResolveReference(parsed).String()
}

func (t *SSETransport) processSSEEvent(eventType, data string) {
	if data == "" {
		return
	}
	switch eventType {
	case "endpoint":
		// Server tells us the POST endpoint for messages.
		resolved := t.resolveEndpointURL(strings.TrimSpace(data))
		t.mu.Lock()
		wasEmpty := t.postURL == ""
		t.postURL = resolved
		t.mu.Unlock()
		slog.Debug("sse transport: discovered endpoint", slog.String("url", resolved))
		// Signal readiness on first endpoint discovery.
		if wasEmpty {
			select {
			case <-t.ready:
				// Already closed.
			default:
				close(t.ready)
			}
		}

	case "message":
		// JSON-RPC message from server.
		t.msgCh <- []byte(data)

	default:
		// Unknown event type — treat as a message if it looks like JSON.
		if json.Valid([]byte(data)) {
			t.msgCh <- []byte(data)
		}
	}
}

// ── HTTP Streamable transport ────────────────────────────────────────────────

// HTTPTransport connects to an MCP server via the Streamable HTTP transport.
// Per the MCP spec (2025-03-26), this uses POST for sending and can receive
// responses via SSE or direct JSON. Supports session management via
// Mcp-Session-Id header.
type HTTPTransport struct {
	url     string
	headers map[string]string

	mu        sync.Mutex
	sessionID string
	msgCh     chan []byte
	closed    chan struct{}
	client    *http.Client
}

// NewHTTPTransport creates an HTTP Streamable transport.
func NewHTTPTransport(url string, headers map[string]string) *HTTPTransport {
	return &HTTPTransport{
		url:     url,
		headers: headers,
		msgCh:   make(chan []byte, 64),
		closed:  make(chan struct{}),
		client:  &http.Client{},
	}
}

// Start initializes the HTTP transport (no persistent connection needed).
func (t *HTTPTransport) Start(_ context.Context) error {
	return nil // HTTP is stateless; connection happens on Send.
}

// Send posts a JSON-RPC message and reads the response.
// For SSE responses, the body is read in the background.
func (t *HTTPTransport) Send(msg []byte) error {
	req, err := http.NewRequest(http.MethodPost, t.url, strings.NewReader(string(msg)))
	if err != nil {
		return fmt.Errorf("http transport: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	t.mu.Lock()
	if t.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", t.sessionID)
	}
	t.mu.Unlock()

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("http transport: post: %w", err)
	}

	// Capture session ID from response headers.
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.mu.Lock()
		t.sessionID = sid
		t.mu.Unlock()
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("http transport: status %d: %s", resp.StatusCode, string(body))
	}

	// Read response body as JSON-RPC messages.
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		// SSE response — read events in background; body is closed when done.
		go func() {
			defer resp.Body.Close()
			scanner := bufio.NewScanner(resp.Body)
			scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			var eventData string
			for scanner.Scan() {
				select {
				case <-t.closed:
					return
				default:
				}
				line := scanner.Text()
				if line == "" {
					// End of event — emit if valid JSON.
					if eventData != "" && json.Valid([]byte(eventData)) {
						t.msgCh <- []byte(eventData)
					}
					eventData = ""
					continue
				}
				if strings.HasPrefix(line, "data: ") {
					data := strings.TrimPrefix(line, "data: ")
					if eventData != "" {
						eventData += "\n" + data
					} else {
						eventData = data
					}
				}
			}
			// Flush remaining event data.
			if eventData != "" && json.Valid([]byte(eventData)) {
				t.msgCh <- []byte(eventData)
			}
		}()
	} else {
		// Direct JSON response.
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("http transport: read body: %w", err)
		}
		if len(body) > 0 && json.Valid(body) {
			t.msgCh <- body
		}
	}
	return nil
}

// Receive returns the channel of incoming messages.
func (t *HTTPTransport) Receive() <-chan []byte {
	return t.msgCh
}

// Close shuts down the HTTP transport.
func (t *HTTPTransport) Close() error {
	select {
	case <-t.closed:
		return nil
	default:
		close(t.closed)
	}
	return nil
}

// ── WebSocket transport (scaffolded) ────────────────────────────────────────

// WebSocketTransportConfig holds configuration for a WebSocket transport.
// This is scaffolded; actual WebSocket implementation requires a ws library.
type WebSocketTransportConfig struct {
	URL     string
	Headers map[string]string
}

// NewWebSocketTransportConfig creates a WebSocket transport config.
func NewWebSocketTransportConfig(url string, headers map[string]string) *WebSocketTransportConfig {
	return &WebSocketTransportConfig{URL: url, Headers: headers}
}

// ── Transport factory ───────────────────────────────────────────────────────

// NewTransportFromConfig creates the appropriate transport for a server config.
func NewTransportFromConfig(cfg *ServerConfig) (Transport, error) {
	expanded := cfg.ExpandEnv()
	switch expanded.Transport {
	case TransportStdio, "":
		return NewStdioTransport(expanded.Command, expanded.Args, expanded.Env), nil
	case TransportSSE, TransportSSEIDE:
		return NewSSETransport(expanded.URL, expanded.Headers), nil
	case TransportHTTP:
		return NewHTTPTransport(expanded.URL, expanded.Headers), nil
	default:
		return nil, fmt.Errorf("unsupported transport: %s", expanded.Transport)
	}
}
