package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/wall-ai/agent-engine/internal/engine"
)

// Vertex AI provider — aligned with claude-code-main's Vertex AI provider support.
//
// Connects to Google Cloud Vertex AI to invoke Anthropic Claude models via
// the Vertex AI API. Requires GCP credentials (via ADC, service account,
// or explicit token).

// VertexConfig holds GCP Vertex AI connection configuration.
type VertexConfig struct {
	// ProjectID is the GCP project ID.
	ProjectID string `json:"project_id"`
	// Region is the GCP region (e.g., "us-east5").
	Region string `json:"region"`
	// ModelID is the Vertex AI model ID (e.g., "claude-sonnet-4@20250514").
	ModelID string `json:"model_id"`
	// AccessToken is a GCP access token (optional — uses ADC if empty).
	AccessToken string `json:"access_token,omitempty"`
	// EndpointURL overrides the default Vertex AI endpoint.
	EndpointURL string `json:"endpoint_url,omitempty"`
	// MaxTokens is the default max output tokens.
	MaxTokens int `json:"max_tokens,omitempty"`
}

// VertexProvider implements ModelCaller for GCP Vertex AI.
type VertexProvider struct {
	config     VertexConfig
	httpClient *http.Client
}

// NewVertexProvider creates a new Vertex AI provider.
func NewVertexProvider(config VertexConfig) (*VertexProvider, error) {
	if config.ProjectID == "" {
		config.ProjectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
		if config.ProjectID == "" {
			config.ProjectID = os.Getenv("GCLOUD_PROJECT")
		}
		if config.ProjectID == "" {
			return nil, fmt.Errorf("vertex: GCP project ID is required")
		}
	}

	if config.Region == "" {
		config.Region = os.Getenv("GOOGLE_CLOUD_REGION")
		if config.Region == "" {
			config.Region = "us-east5"
		}
	}

	if config.ModelID == "" {
		config.ModelID = "claude-sonnet-4@20250514"
	}

	if config.MaxTokens == 0 {
		config.MaxTokens = 8192
	}

	return &VertexProvider{
		config:     config,
		httpClient: &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

// CallModel implements engine.ModelCaller for Vertex AI.
// Currently returns an error indicating the provider needs GCP auth integration.
func (p *VertexProvider) CallModel(ctx context.Context, params engine.CallParams) (<-chan *engine.StreamEvent, error) {
	ch := make(chan *engine.StreamEvent, 1)

	go func() {
		defer close(ch)

		reqBody := p.buildRequest(params)

		slog.Debug("vertex: invoking model",
			"model", p.config.ModelID,
			"project", p.config.ProjectID,
			"region", p.config.Region,
			"max_tokens", params.MaxTokens)

		_ = reqBody
		_ = ctx

		// TODO: Implement actual Vertex AI API call with GCP ADC authentication.
		// This requires either the Google Cloud SDK or manual OAuth2 token acquisition.
		// For now, return an error indicating the provider is a stub.
		ch <- &engine.StreamEvent{
			Type:  engine.EventError,
			Error: "vertex provider: not yet fully implemented — requires GCP authentication integration",
		}
	}()

	return ch, nil
}

// Endpoint returns the Vertex AI API endpoint URL.
func (p *VertexProvider) Endpoint() string {
	if p.config.EndpointURL != "" {
		return p.config.EndpointURL
	}
	return fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/anthropic/models/%s:streamRawPredict",
		p.config.Region,
		p.config.ProjectID,
		p.config.Region,
		p.config.ModelID,
	)
}

// buildRequest converts CallParams to a Vertex AI request body.
func (p *VertexProvider) buildRequest(params engine.CallParams) []byte {
	req := map[string]interface{}{
		"anthropic_version": "vertex-2023-10-16",
		"max_tokens":        params.MaxTokens,
	}

	if req["max_tokens"] == 0 {
		req["max_tokens"] = p.config.MaxTokens
	}

	if params.Temperature > 0 {
		req["temperature"] = params.Temperature
	}

	if len(params.StopSequences) > 0 {
		req["stop_sequences"] = params.StopSequences
	}

	// System prompt.
	if len(params.SystemPromptParts) > 0 {
		var parts []string
		for _, sp := range params.SystemPromptParts {
			parts = append(parts, sp.Content)
		}
		req["system"] = joinStrings(parts, "\n\n")
	} else if params.SystemPrompt != "" {
		req["system"] = params.SystemPrompt
	}

	// Convert messages.
	var messages []map[string]interface{}
	for _, msg := range params.Messages {
		m := map[string]interface{}{
			"role": string(msg.Role),
		}
		var content []map[string]interface{}
		for _, block := range msg.Content {
			switch block.Type {
			case engine.ContentTypeText:
				content = append(content, map[string]interface{}{
					"type": "text",
					"text": block.Text,
				})
			case engine.ContentTypeToolUse:
				content = append(content, map[string]interface{}{
					"type":  "tool_use",
					"id":    block.ToolUseID,
					"name":  block.ToolName,
					"input": block.Input,
				})
			case engine.ContentTypeToolResult:
				content = append(content, map[string]interface{}{
					"type":        "tool_result",
					"tool_use_id": block.ToolUseID,
					"content":     block.Text,
					"is_error":    block.IsError,
				})
			}
		}
		m["content"] = content
		messages = append(messages, m)
	}
	req["messages"] = messages

	body, _ := json.Marshal(req)
	return body
}
