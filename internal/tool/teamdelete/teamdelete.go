package teamdelete

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/wall-ai/agent-engine/internal/engine"
	"github.com/wall-ai/agent-engine/internal/tool"
)

// TeamDeleter abstracts team deletion so we don't import agent directly.
type TeamDeleter interface {
	DeleteTeam(teamName string) error
	FinishTeam(teamName string) error
}

// TeamDeleteTool cancels and removes a running team.
type TeamDeleteTool struct {
	tool.BaseTool
	deleter TeamDeleter
}

func New() *TeamDeleteTool { return &TeamDeleteTool{} }

// NewWithDeleter creates a TeamDeleteTool wired to a TeamDeleter.
func NewWithDeleter(d TeamDeleter) *TeamDeleteTool {
	return &TeamDeleteTool{deleter: d}
}

func (t *TeamDeleteTool) Name() string           { return "team_delete" }
func (t *TeamDeleteTool) UserFacingName() string { return "TeamDelete" }
func (t *TeamDeleteTool) Description() string {
	return "Cancel all agents in a team and remove the team from the registry."
}
func (t *TeamDeleteTool) IsReadOnly(_ json.RawMessage) bool        { return false }
func (t *TeamDeleteTool) IsConcurrencySafe(_ json.RawMessage) bool { return false }
func (t *TeamDeleteTool) MaxResultSizeChars() int                  { return 500 }
func (t *TeamDeleteTool) IsEnabled(_ *tool.UseContext) bool        { return true }

func (t *TeamDeleteTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"team_name": {
				"type": "string",
				"description": "The team to delete."
			}
		},
		"required": ["team_name"]
	}`)
}

func (t *TeamDeleteTool) Prompt(_ *tool.UseContext) string { return "" }

func (t *TeamDeleteTool) CheckPermissions(_ context.Context, _ json.RawMessage, _ *tool.UseContext) error {
	return nil
}

func (t *TeamDeleteTool) Call(_ context.Context, input json.RawMessage, _ *tool.UseContext) (<-chan *engine.ContentBlock, error) {
	ch := make(chan *engine.ContentBlock, 1)
	go func() {
		defer close(ch)

		var args struct {
			TeamName string `json:"team_name"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			ch <- &engine.ContentBlock{Type: engine.ContentTypeText, Text: "invalid input: " + err.Error(), IsError: true}
			return
		}

		if t.deleter != nil {
			// First mark as finished, then delete.
			_ = t.deleter.FinishTeam(args.TeamName)
			if err := t.deleter.DeleteTeam(args.TeamName); err != nil {
				ch <- &engine.ContentBlock{Type: engine.ContentTypeText, Text: "team deletion failed: " + err.Error(), IsError: true}
				return
			}
			slog.Info("team_delete: team deleted via manager", slog.String("team", args.TeamName))
		}

		ch <- &engine.ContentBlock{
			Type: engine.ContentTypeText,
			Text: fmt.Sprintf("Team %q cancelled and removed.", args.TeamName),
		}
	}()
	return ch, nil
}
