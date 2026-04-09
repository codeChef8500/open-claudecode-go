package ide

import (
	"context"
	"encoding/json"
	"log/slog"
)

// ────────────────────────────────────────────────────────────────────────────
// VS Code bridge methods — aligned with claude-code-main bridge/.
//
// These handlers implement the bridge protocol that the VS Code extension
// uses to communicate with the agent engine.
// ────────────────────────────────────────────────────────────────────────────

// RegisterVSCodeHandlers adds VS Code-specific method handlers to the bridge.
func RegisterVSCodeHandlers(b *Bridge) {
	b.Register("textDocument/didOpen", handleDidOpen)
	b.Register("textDocument/didChange", handleDidChange)
	b.Register("textDocument/didClose", handleDidClose)
	b.Register("workspace/applyEdit", handleApplyEdit)
	b.Register("window/showMessage", handleShowMessage)
	b.Register("agent/status", handleAgentStatus)
	b.Register("agent/cancel", handleAgentCancel)
	b.Register("agent/getContext", handleGetContext)
	b.Register("agent/setSelection", handleSetSelection)
	b.Register("agent/openFile", handleOpenFile)
	b.Register("agent/showDiff", handleShowDiff)
}

// ── Document sync ────────────────────────────────────────────────────────────

func handleDidOpen(_ context.Context, req *IDERequest) *IDEResponse {
	params, _ := req.Params.(map[string]interface{})
	uri, _ := params["uri"].(string)
	slog.Debug("ide: textDocument/didOpen", slog.String("uri", uri))
	return &IDEResponse{Result: map[string]string{"status": "ok"}}
}

func handleDidChange(_ context.Context, req *IDERequest) *IDEResponse {
	params, _ := req.Params.(map[string]interface{})
	uri, _ := params["uri"].(string)
	slog.Debug("ide: textDocument/didChange", slog.String("uri", uri))
	return &IDEResponse{Result: map[string]string{"status": "ok"}}
}

func handleDidClose(_ context.Context, req *IDERequest) *IDEResponse {
	params, _ := req.Params.(map[string]interface{})
	uri, _ := params["uri"].(string)
	slog.Debug("ide: textDocument/didClose", slog.String("uri", uri))
	return &IDEResponse{Result: map[string]string{"status": "ok"}}
}

// ── Workspace edits ──────────────────────────────────────────────────────────

func handleApplyEdit(_ context.Context, req *IDERequest) *IDEResponse {
	params, _ := req.Params.(map[string]interface{})
	label, _ := params["label"].(string)
	slog.Info("ide: workspace/applyEdit", slog.String("label", label))
	// The actual edit is applied by the IDE extension; we just acknowledge.
	return &IDEResponse{Result: map[string]bool{"applied": true}}
}

// ── Window messages ──────────────────────────────────────────────────────────

func handleShowMessage(_ context.Context, req *IDERequest) *IDEResponse {
	params, _ := req.Params.(map[string]interface{})
	msg, _ := params["message"].(string)
	slog.Info("ide: window/showMessage", slog.String("message", msg))
	return &IDEResponse{Result: map[string]string{"status": "ok"}}
}

// ── Agent control ────────────────────────────────────────────────────────────

func handleAgentStatus(_ context.Context, _ *IDERequest) *IDEResponse {
	return &IDEResponse{Result: map[string]interface{}{
		"status": "running",
	}}
}

func handleAgentCancel(_ context.Context, _ *IDERequest) *IDEResponse {
	slog.Info("ide: agent/cancel requested")
	return &IDEResponse{Result: map[string]string{"status": "cancelled"}}
}

func handleGetContext(_ context.Context, _ *IDERequest) *IDEResponse {
	return &IDEResponse{Result: map[string]interface{}{
		"context": "default",
	}}
}

// ── Selection & navigation ───────────────────────────────────────────────────

func handleSetSelection(_ context.Context, req *IDERequest) *IDEResponse {
	params, _ := req.Params.(map[string]interface{})
	uri, _ := params["uri"].(string)
	slog.Debug("ide: agent/setSelection", slog.String("uri", uri))
	return &IDEResponse{Result: map[string]string{"status": "ok"}}
}

func handleOpenFile(_ context.Context, req *IDERequest) *IDEResponse {
	params, _ := req.Params.(map[string]interface{})
	uri, _ := params["uri"].(string)
	slog.Info("ide: agent/openFile", slog.String("uri", uri))
	return &IDEResponse{Result: map[string]string{"status": "ok"}}
}

func handleShowDiff(_ context.Context, req *IDERequest) *IDEResponse {
	params, _ := req.Params.(map[string]interface{})
	uri, _ := params["uri"].(string)
	slog.Info("ide: agent/showDiff", slog.String("uri", uri))
	return &IDEResponse{Result: map[string]string{"status": "ok"}}
}

// ── Outbound notifications to IDE ────────────────────────────────────────────

// NotifyProgress sends a progress update to the IDE.
func (b *Bridge) NotifyProgress(taskID string, message string, percentage int) {
	payload, _ := json.Marshal(map[string]interface{}{
		"method": "agent/progress",
		"params": map[string]interface{}{
			"taskId":     taskID,
			"message":    message,
			"percentage": percentage,
		},
	})
	b.Send(&IDEResponse{Result: json.RawMessage(payload)})
}

// NotifyFileChanged tells the IDE that a file was modified by the agent.
func (b *Bridge) NotifyFileChanged(uri string, reason string) {
	payload, _ := json.Marshal(map[string]interface{}{
		"method": "agent/fileChanged",
		"params": map[string]interface{}{
			"uri":    uri,
			"reason": reason,
		},
	})
	b.Send(&IDEResponse{Result: json.RawMessage(payload)})
}

// NotifyToolUse tells the IDE about a tool invocation for inline display.
func (b *Bridge) NotifyToolUse(toolName string, input map[string]interface{}) {
	payload, _ := json.Marshal(map[string]interface{}{
		"method": "agent/toolUse",
		"params": map[string]interface{}{
			"tool":  toolName,
			"input": input,
		},
	})
	b.Send(&IDEResponse{Result: json.RawMessage(payload)})
}

// NotifyStatus sends an agent status update to the IDE.
func (b *Bridge) NotifyStatus(status string, details map[string]interface{}) {
	payload, _ := json.Marshal(map[string]interface{}{
		"method": "agent/statusUpdate",
		"params": map[string]interface{}{
			"status":  status,
			"details": details,
		},
	})
	b.Send(&IDEResponse{Result: json.RawMessage(payload)})
}
