package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// ── MCP Elicitation (server → client user-input requests) ────────────────────
// Aligned with claude-code-main src/services/mcp/elicitationHandler.ts
//
// MCP servers can request user input via elicitation/create. The server sends
// a JSON-RPC request with params describing what input it needs; the client
// presents a prompt to the user and returns the result.

const (
	MethodElicitCreate   = "elicitation/create"
	MethodElicitComplete = "notifications/elicitation/complete"
)

// ElicitationMode is "form" or "url".
type ElicitationMode string

const (
	ElicitModeForm ElicitationMode = "form"
	ElicitModeURL  ElicitationMode = "url"
)

// ElicitAction is the user's response to an elicitation.
type ElicitAction string

const (
	ElicitActionAccept  ElicitAction = "accept"
	ElicitActionDecline ElicitAction = "decline"
	ElicitActionCancel  ElicitAction = "cancel"
)

// ElicitRequestParams are the parameters of an elicitation/create request.
type ElicitRequestParams struct {
	Mode          ElicitationMode        `json:"mode"`
	Message       string                 `json:"message,omitempty"`
	Title         string                 `json:"title,omitempty"`
	URL           string                 `json:"url,omitempty"`
	ElicitationID string                 `json:"elicitationId,omitempty"`
	Schema        map[string]interface{} `json:"requestedSchema,omitempty"`
}

// ElicitResult is the client's response to an elicitation.
type ElicitResult struct {
	Action  ElicitAction           `json:"action"`
	Content map[string]interface{} `json:"content,omitempty"`
}

// ElicitationHandler is called when an MCP server requests user input.
// Implementations should present the request to the user and return a result.
type ElicitationHandler interface {
	HandleElicitation(ctx context.Context, serverName string, params *ElicitRequestParams) (*ElicitResult, error)
}

// ElicitationHandlerFunc adapts a function to the ElicitationHandler interface.
type ElicitationHandlerFunc func(ctx context.Context, serverName string, params *ElicitRequestParams) (*ElicitResult, error)

func (f ElicitationHandlerFunc) HandleElicitation(ctx context.Context, serverName string, params *ElicitRequestParams) (*ElicitResult, error) {
	return f(ctx, serverName, params)
}

// DefaultElicitationHandler auto-declines all elicitations (for non-interactive use).
type DefaultElicitationHandler struct{}

func (DefaultElicitationHandler) HandleElicitation(_ context.Context, serverName string, params *ElicitRequestParams) (*ElicitResult, error) {
	slog.Info("mcp: elicitation auto-declined (non-interactive mode)",
		slog.String("server", serverName),
		slog.String("message", params.Message))
	return &ElicitResult{Action: ElicitActionDecline}, nil
}

// CLIElicitationHandler prompts the user in the terminal.
type CLIElicitationHandler struct {
	// AskFn is called to prompt the user. It receives the message and returns
	// true (accept) or false (decline).
	AskFn func(message string) (bool, error)
}

func (h *CLIElicitationHandler) HandleElicitation(_ context.Context, serverName string, params *ElicitRequestParams) (*ElicitResult, error) {
	if h.AskFn == nil {
		return &ElicitResult{Action: ElicitActionDecline}, nil
	}

	prompt := fmt.Sprintf("[MCP %s] %s", serverName, params.Message)
	if params.URL != "" {
		prompt += fmt.Sprintf("\nURL: %s", params.URL)
	}

	accepted, err := h.AskFn(prompt)
	if err != nil {
		return &ElicitResult{Action: ElicitActionCancel}, nil
	}
	if accepted {
		return &ElicitResult{Action: ElicitActionAccept}, nil
	}
	return &ElicitResult{Action: ElicitActionDecline}, nil
}

// ── Elicitation event queue (for UI-based handlers) ─────────────────────────

// ElicitationEvent represents a pending elicitation in a queue.
type ElicitationEvent struct {
	ServerName string
	RequestID  interface{}
	Params     *ElicitRequestParams
	Respond    func(*ElicitResult)
	Completed  bool // set by completion notification
}

// ElicitationQueue manages pending elicitation requests.
type ElicitationQueue struct {
	mu    sync.Mutex
	queue []*ElicitationEvent
}

// NewElicitationQueue creates an empty queue.
func NewElicitationQueue() *ElicitationQueue {
	return &ElicitationQueue{}
}

// Push adds an elicitation event to the queue.
func (q *ElicitationQueue) Push(e *ElicitationEvent) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.queue = append(q.queue, e)
}

// Pop removes and returns the first event, or nil if empty.
func (q *ElicitationQueue) Pop() *ElicitationEvent {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.queue) == 0 {
		return nil
	}
	e := q.queue[0]
	q.queue = q.queue[1:]
	return e
}

// Len returns the number of pending events.
func (q *ElicitationQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.queue)
}

// MarkCompleted sets completed=true on a matching URL elicitation by server+elicitationId.
func (q *ElicitationQueue) MarkCompleted(serverName, elicitationID string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, e := range q.queue {
		if e.ServerName == serverName &&
			e.Params.Mode == ElicitModeURL &&
			e.Params.ElicitationID == elicitationID {
			e.Completed = true
			return true
		}
	}
	return false
}

// ── Elicitation config ──────────────────────────────────────────────────────

// ElicitationConfig configures elicitation behavior per-server.
// Aligned with TS ElicitationConfig.
type ElicitationConfig struct {
	// Enabled controls whether elicitations are allowed from this server.
	Enabled bool
	// Timeout is the max duration to wait for user input. 0 = no timeout.
	Timeout time.Duration
	// MaxFormFields limits form elicitation fields. Default: 20.
	MaxFormFields int
}

// DefaultElicitationConfig returns sensible defaults.
func DefaultElicitationConfig() ElicitationConfig {
	return ElicitationConfig{
		Enabled:       true,
		Timeout:       5 * time.Minute,
		MaxFormFields: 20,
	}
}

// ── Schema validation ───────────────────────────────────────────────────────

// ValidateElicitSchema validates that a form elicitation request has a valid
// JSON schema. Returns an error if validation fails.
// Aligned with TS validateElicitSchema.
func ValidateElicitSchema(params *ElicitRequestParams) error {
	if params.Mode != ElicitModeForm {
		return nil
	}
	if params.Schema == nil || len(params.Schema) == 0 {
		return fmt.Errorf("form elicitation requires a requestedSchema")
	}

	schemaType, _ := params.Schema["type"].(string)
	if schemaType != "object" {
		return fmt.Errorf("requestedSchema.type must be 'object', got %q", schemaType)
	}

	props, _ := params.Schema["properties"].(map[string]interface{})
	if props == nil || len(props) == 0 {
		return fmt.Errorf("requestedSchema.properties must be non-empty")
	}

	return nil
}

// ValidateElicitResponse validates the user's response against the schema.
// Checks required fields and basic type constraints.
func ValidateElicitResponse(schema map[string]interface{}, content map[string]interface{}) error {
	required, _ := schema["required"].([]interface{})
	for _, r := range required {
		key, ok := r.(string)
		if !ok {
			continue
		}
		if _, exists := content[key]; !exists {
			return fmt.Errorf("missing required field: %s", key)
		}
	}

	props, _ := schema["properties"].(map[string]interface{})
	for key, val := range content {
		propSchema, ok := props[key].(map[string]interface{})
		if !ok {
			continue
		}
		expectedType, _ := propSchema["type"].(string)
		if expectedType == "" {
			continue
		}
		switch expectedType {
		case "string":
			if _, ok := val.(string); !ok {
				return fmt.Errorf("field %s: expected string", key)
			}
		case "number":
			switch val.(type) {
			case float64, int, int64:
				// ok
			default:
				return fmt.Errorf("field %s: expected number", key)
			}
		case "boolean":
			if _, ok := val.(bool); !ok {
				return fmt.Errorf("field %s: expected boolean", key)
			}
		}
	}
	return nil
}

// ── Registration helper ─────────────────────────────────────────────────────

// RegisterElicitationHandler wires an ElicitationHandler into a Client so that
// incoming elicitation/create requests are handled.
func RegisterElicitationHandler(client *Client, handler ElicitationHandler) {
	RegisterElicitationHandlerWithConfig(client, handler, DefaultElicitationConfig())
}

// RegisterElicitationHandlerWithConfig wires an ElicitationHandler with config.
func RegisterElicitationHandlerWithConfig(client *Client, handler ElicitationHandler, cfg ElicitationConfig) {
	serverName := client.Name()
	existingHandler := client.notificationHandler

	client.SetNotificationHandler(func(method string, params json.RawMessage) {
		switch method {
		case MethodElicitCreate:
			if !cfg.Enabled {
				slog.Info("mcp: elicitation disabled for server",
					slog.String("server", serverName))
				sendElicitDecline(client, params)
				return
			}
			go handleElicitRequest(client, serverName, params, handler, cfg)

		case MethodElicitComplete:
			var p struct {
				ElicitationID string `json:"elicitationId"`
			}
			_ = json.Unmarshal(params, &p)
			slog.Debug("mcp: elicitation completion notification",
				slog.String("server", serverName),
				slog.String("elicitationId", p.ElicitationID))

		default:
			// Delegate to existing handler if any.
			if existingHandler != nil {
				existingHandler(method, params)
			}
		}
	})
}

func handleElicitRequest(client *Client, serverName string, rawParams json.RawMessage, handler ElicitationHandler, cfg ElicitationConfig) {
	var req struct {
		ID     interface{}         `json:"id"`
		Params ElicitRequestParams `json:"params"`
	}
	if err := json.Unmarshal(rawParams, &req); err != nil {
		slog.Warn("mcp: invalid elicitation request", slog.String("server", serverName), slog.Any("err", err))
		return
	}

	slog.Debug("mcp: elicitation request received",
		slog.String("server", serverName),
		slog.String("mode", string(req.Params.Mode)),
		slog.String("message", req.Params.Message))

	// Validate form schema if present.
	if err := ValidateElicitSchema(&req.Params); err != nil {
		slog.Warn("mcp: elicitation schema validation failed",
			slog.String("server", serverName), slog.Any("err", err))
		sendElicitError(client, req.ID, err.Error())
		return
	}

	// Apply timeout.
	ctx := context.Background()
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	result, err := handler.HandleElicitation(ctx, serverName, &req.Params)
	if err != nil {
		slog.Warn("mcp: elicitation handler error",
			slog.String("server", serverName), slog.Any("err", err))
		result = &ElicitResult{Action: ElicitActionCancel}
	}

	// Validate response against schema if form mode with accept.
	if result.Action == ElicitActionAccept && req.Params.Mode == ElicitModeForm && req.Params.Schema != nil {
		if err := ValidateElicitResponse(req.Params.Schema, result.Content); err != nil {
			slog.Warn("mcp: elicitation response validation failed",
				slog.String("server", serverName), slog.Any("err", err))
			// Don't reject — just log the validation failure.
		}
	}

	// Send the response back to the server.
	resultJSON, _ := json.Marshal(result)
	resp := &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  resultJSON,
	}
	data, _ := json.Marshal(resp)
	client.mu.Lock()
	tr := client.transport
	client.mu.Unlock()
	if tr != nil {
		_ = tr.Send(data)
	}
}

// sendElicitDecline sends an automatic decline response.
func sendElicitDecline(client *Client, rawParams json.RawMessage) {
	var req struct {
		ID interface{} `json:"id"`
	}
	if err := json.Unmarshal(rawParams, &req); err != nil {
		return
	}
	result := &ElicitResult{Action: ElicitActionDecline}
	resultJSON, _ := json.Marshal(result)
	resp := &Response{JSONRPC: "2.0", ID: req.ID, Result: resultJSON}
	data, _ := json.Marshal(resp)
	client.mu.Lock()
	tr := client.transport
	client.mu.Unlock()
	if tr != nil {
		_ = tr.Send(data)
	}
}

// sendElicitError sends an error response for invalid requests.
func sendElicitError(client *Client, id interface{}, message string) {
	resp := &Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: -32602, Message: message},
	}
	data, _ := json.Marshal(resp)
	client.mu.Lock()
	tr := client.transport
	client.mu.Unlock()
	if tr != nil {
		_ = tr.Send(data)
	}
}
