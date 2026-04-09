package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
)

// Prompt hook execution — aligned with claude-code-main execPromptHook.ts.
//
// A prompt hook sends a rendered template to an LLM for evaluation.
// The LLM response is parsed as JSON (HookJSONOutput) to determine the
// hook decision (approve/block/ask) or other actions.

// promptTemplateData provides the template context for prompt hook rendering.
type promptTemplateData struct {
	Event     string `json:"event"`
	ToolName  string `json:"tool_name,omitempty"`
	ToolID    string `json:"tool_id,omitempty"`
	Input     string `json:"input,omitempty"`
	Output    string `json:"output,omitempty"`
	SessionID string `json:"session_id"`
	CWD       string `json:"cwd"`
}

// runPromptHook evaluates a prompt-type hook by rendering the template,
// sending it to the configured PromptEvaluator, and parsing the LLM response.
func (e *Executor) runPromptHook(ctx context.Context, cfg HookConfig, input *HookInput) SyncHookResponse {
	if e.promptEvaluator == nil {
		return SyncHookResponse{
			Error: fmt.Errorf("prompt hook %q: no PromptEvaluator configured", cfg.Source),
		}
	}

	// Build template data from hook input.
	data := promptTemplateData{
		Event:     string(input.Event),
		SessionID: input.SessionID,
		CWD:       input.CWD,
	}
	if input.PreToolUse != nil {
		data.ToolName = input.PreToolUse.ToolName
		data.ToolID = input.PreToolUse.ToolID
		data.Input = string(input.PreToolUse.Input)
	}
	if input.PostToolUse != nil {
		data.ToolName = input.PostToolUse.ToolName
		data.ToolID = input.PostToolUse.ToolID
		data.Input = string(input.PostToolUse.Input)
		data.Output = input.PostToolUse.Output
	}

	// Render the prompt template.
	rendered, err := renderPromptTemplate(cfg.PromptTemplate, data)
	if err != nil {
		return SyncHookResponse{
			Error: fmt.Errorf("prompt hook template render: %w", err),
		}
	}

	// Wrap with system instruction so the LLM returns structured JSON.
	fullPrompt := promptHookSystemInstruction + "\n\n" + rendered

	// Call the LLM evaluator.
	out, err := e.promptEvaluator(ctx, fullPrompt)
	if err != nil {
		return SyncHookResponse{
			Error: fmt.Errorf("prompt hook evaluation failed: %w", err),
		}
	}
	if out == nil {
		return SyncHookResponse{}
	}

	return parseHookOutput(*out)
}

// renderPromptTemplate renders a Go text/template with the given data.
func renderPromptTemplate(tmplStr string, data promptTemplateData) (string, error) {
	if tmplStr == "" {
		// Default template: serialize all data as context.
		inputJSON, _ := json.MarshalIndent(data, "", "  ")
		return fmt.Sprintf("Evaluate this hook event and respond with a JSON decision:\n\n%s", string(inputJSON)), nil
	}

	tmpl, err := template.New("hook").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

// promptHookSystemInstruction tells the LLM how to respond.
const promptHookSystemInstruction = `You are a hook evaluator. Given a hook event, evaluate whether it should proceed.

Respond with a JSON object. Valid fields:
- "decision": "approve" | "block" | "ask" (for PreToolUse hooks)
- "reason": string explaining your decision
- "continue": true | false (for other hooks)
- "shouldStop": true | false
- "stopReason": string
- "additionalContext": string (injected as system message)

` + "Respond ONLY with valid JSON, no markdown fencing."

// DefaultPromptEvaluator returns a no-op evaluator that always approves.
// Replace with a real LLM-backed evaluator at runtime.
func DefaultPromptEvaluator() PromptEvaluator {
	return func(_ context.Context, _ string) (*HookJSONOutput, error) {
		cont := true
		return &HookJSONOutput{
			Decision: "approve",
			Continue: &cont,
		}, nil
	}
}

// ParsePromptEvaluatorResponse parses a raw LLM text response into HookJSONOutput.
// It handles markdown-fenced JSON and plain JSON.
func ParsePromptEvaluatorResponse(text string) (*HookJSONOutput, error) {
	text = strings.TrimSpace(text)

	// Strip markdown JSON fencing if present.
	if strings.HasPrefix(text, "```json") {
		text = strings.TrimPrefix(text, "```json")
		if idx := strings.LastIndex(text, "```"); idx >= 0 {
			text = text[:idx]
		}
		text = strings.TrimSpace(text)
	} else if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```")
		if idx := strings.LastIndex(text, "```"); idx >= 0 {
			text = text[:idx]
		}
		text = strings.TrimSpace(text)
	}

	var out HookJSONOutput
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, fmt.Errorf("parse LLM hook response: %w (raw: %.200s)", err, text)
	}
	return &out, nil
}
