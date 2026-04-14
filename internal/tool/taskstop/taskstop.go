package taskstop

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/wall-ai/agent-engine/internal/engine"
	"github.com/wall-ai/agent-engine/internal/tool"
)

type Input struct {
	TaskID  string `json:"task_id,omitempty"`
	ShellID string `json:"shell_id,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

type TaskStopTool struct{ tool.BaseTool }

func New() *TaskStopTool { return &TaskStopTool{} }

func (t *TaskStopTool) Name() string           { return "TaskStop" }
func (t *TaskStopTool) UserFacingName() string { return "task_stop" }
func (t *TaskStopTool) Description() string {
	return "Stop a running background task by task ID."
}
func (t *TaskStopTool) IsReadOnly(_ json.RawMessage) bool        { return false }
func (t *TaskStopTool) IsConcurrencySafe(_ json.RawMessage) bool { return false }
func (t *TaskStopTool) MaxResultSizeChars() int                  { return 0 }
func (t *TaskStopTool) IsEnabled(uctx *tool.UseContext) bool {
	return uctx != nil
}

func (t *TaskStopTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type":"object",
		"properties":{
			"task_id":{"type":"string","description":"Task ID to stop."},
			"shell_id":{"type":"string","description":"Deprecated alias for task_id."},
			"reason":{"type":"string","description":"Optional reason for stopping the task."}
		},
		"anyOf":[{"required":["task_id"]},{"required":["shell_id"]}]
	}`)
}

func (t *TaskStopTool) Prompt(_ *tool.UseContext) string { return "" }

func (t *TaskStopTool) CheckPermissions(_ context.Context, input json.RawMessage, _ *tool.UseContext) error {
	var in Input
	if err := json.Unmarshal(input, &in); err != nil {
		return fmt.Errorf("invalid input: %w", err)
	}
	if in.TaskID == "" && in.ShellID == "" {
		return fmt.Errorf("task_id or shell_id must be provided")
	}
	return nil
}

func (t *TaskStopTool) Call(_ context.Context, input json.RawMessage, uctx *tool.UseContext) (<-chan *engine.ContentBlock, error) {
	var in Input
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}
	taskID := in.TaskID
	if taskID == "" {
		taskID = in.ShellID
	}

	ch := make(chan *engine.ContentBlock, 2)
	go func() {
		defer close(ch)
		if taskID == "" {
			ch <- &engine.ContentBlock{Type: engine.ContentTypeText, Text: `{"error":"task_id or shell_id must be provided"}`, IsError: true}
			return
		}
		if uctx == nil || uctx.StopTask == nil {
			ch <- &engine.ContentBlock{Type: engine.ContentTypeText, Text: `{"error":"task stopping is not available in this context"}`, IsError: true}
			return
		}
		if err := uctx.StopTask(taskID); err != nil {
			ch <- &engine.ContentBlock{Type: engine.ContentTypeText, Text: fmt.Sprintf(`{"error":%q}`, err.Error()), IsError: true}
			return
		}
		msg := fmt.Sprintf(`{"status":"stopped","task_id":%q}`, taskID)
		if in.Reason != "" {
			msg = fmt.Sprintf(`{"status":"stopped","task_id":%q,"reason":%q}`, taskID, in.Reason)
		}
		ch <- &engine.ContentBlock{Type: engine.ContentTypeText, Text: msg}
	}()
	return ch, nil
}
