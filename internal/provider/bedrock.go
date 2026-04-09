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

// Bedrock provider — aligned with claude-code-main's Bedrock provider support.
//
// Connects to AWS Bedrock to invoke Anthropic Claude models via the
// AWS Bedrock Runtime API. Requires AWS credentials (via environment
// variables, IAM role, or AWS config).

// BedrockConfig holds AWS Bedrock connection configuration.
type BedrockConfig struct {
	// Region is the AWS region (e.g., "us-east-1").
	Region string `json:"region"`
	// ModelID is the Bedrock model ID (e.g., "anthropic.claude-sonnet-4-20250514-v1:0").
	ModelID string `json:"model_id"`
	// AccessKeyID is the AWS access key (optional — uses default credential chain if empty).
	AccessKeyID string `json:"access_key_id,omitempty"`
	// SecretAccessKey is the AWS secret key.
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	// SessionToken is the AWS session token (for temporary credentials).
	SessionToken string `json:"session_token,omitempty"`
	// EndpointURL overrides the default Bedrock endpoint.
	EndpointURL string `json:"endpoint_url,omitempty"`
	// MaxTokens is the default max output tokens.
	MaxTokens int `json:"max_tokens,omitempty"`
}

// BedrockProvider implements ModelCaller for AWS Bedrock.
type BedrockProvider struct {
	config     BedrockConfig
	httpClient *http.Client
}

// NewBedrockProvider creates a new Bedrock provider.
func NewBedrockProvider(config BedrockConfig) (*BedrockProvider, error) {
	if config.Region == "" {
		config.Region = os.Getenv("AWS_REGION")
		if config.Region == "" {
			config.Region = os.Getenv("AWS_DEFAULT_REGION")
		}
		if config.Region == "" {
			return nil, fmt.Errorf("bedrock: AWS region is required")
		}
	}

	if config.ModelID == "" {
		config.ModelID = "anthropic.claude-sonnet-4-20250514-v1:0"
	}

	if config.MaxTokens == 0 {
		config.MaxTokens = 8192
	}

	// Use env credentials if not explicitly configured.
	if config.AccessKeyID == "" {
		config.AccessKeyID = os.Getenv("AWS_ACCESS_KEY_ID")
		config.SecretAccessKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
		config.SessionToken = os.Getenv("AWS_SESSION_TOKEN")
	}

	return &BedrockProvider{
		config:     config,
		httpClient: &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

// CallModel implements engine.ModelCaller for Bedrock.
// Currently returns an error indicating the provider needs AWS SDK integration.
func (p *BedrockProvider) CallModel(ctx context.Context, params engine.CallParams) (<-chan *engine.StreamEvent, error) {
	ch := make(chan *engine.StreamEvent, 1)

	go func() {
		defer close(ch)

		// Build the Bedrock InvokeModel request body.
		reqBody := p.buildRequest(params)

		slog.Debug("bedrock: invoking model",
			"model", p.config.ModelID,
			"region", p.config.Region,
			"max_tokens", params.MaxTokens)

		_ = reqBody
		_ = ctx

		// TODO: Implement actual AWS Bedrock API call with SigV4 signing.
		// This requires either the AWS SDK or manual SigV4 request signing.
		// For now, return an error indicating the provider is a stub.
		ch <- &engine.StreamEvent{
			Type:  engine.EventError,
			Error: "bedrock provider: not yet fully implemented — requires AWS SDK integration for SigV4 signing",
		}
	}()

	return ch, nil
}

// bedrockRequest is the request body for Bedrock InvokeModel.
type bedrockRequest struct {
	AnthropicVersion string                   `json:"anthropic_version"`
	MaxTokens        int                      `json:"max_tokens"`
	System           string                   `json:"system,omitempty"`
	Messages         []map[string]interface{} `json:"messages"`
	Temperature      float64                  `json:"temperature,omitempty"`
	StopSequences    []string                 `json:"stop_sequences,omitempty"`
}

// buildRequest converts CallParams to a Bedrock request body.
func (p *BedrockProvider) buildRequest(params engine.CallParams) []byte {
	req := bedrockRequest{
		AnthropicVersion: "bedrock-2023-05-31",
		MaxTokens:        params.MaxTokens,
		Temperature:      params.Temperature,
		StopSequences:    params.StopSequences,
	}

	if req.MaxTokens == 0 {
		req.MaxTokens = p.config.MaxTokens
	}

	// System prompt.
	if len(params.SystemPromptParts) > 0 {
		var parts []string
		for _, sp := range params.SystemPromptParts {
			parts = append(parts, sp.Content)
		}
		req.System = joinStrings(parts, "\n\n")
	} else if params.SystemPrompt != "" {
		req.System = params.SystemPrompt
	}

	// Convert messages.
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
		req.Messages = append(req.Messages, m)
	}

	body, _ := json.Marshal(req)
	return body
}

func joinStrings(parts []string, sep string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += sep
		}
		result += p
	}
	return result
}
