package teamcreate

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/wall-ai/agent-engine/internal/engine"
	"github.com/wall-ai/agent-engine/internal/tool"
)

// TeamCreator abstracts the team creation API so we don't import agent directly.
type TeamCreator interface {
	CreateTeam(name, description, leadAgentID string) (interface{}, error)
	AddMember(teamName, agentID, agentType, role string) error
	TeamMemberIDs(teamName string) []string
}

// TeamCreateTool creates a named team of sub-agents.
type TeamCreateTool struct {
	tool.BaseTool
	creator TeamCreator
}

func New() *TeamCreateTool { return &TeamCreateTool{} }

// NewWithCreator creates a TeamCreateTool wired to a TeamCreator.
func NewWithCreator(c TeamCreator) *TeamCreateTool {
	return &TeamCreateTool{creator: c}
}

func (t *TeamCreateTool) Name() string           { return "team_create" }
func (t *TeamCreateTool) UserFacingName() string { return "TeamCreate" }
func (t *TeamCreateTool) Description() string {
	return "Create a named team of parallel sub-agents. Each agent receives an independent task and works concurrently."
}
func (t *TeamCreateTool) IsReadOnly(_ json.RawMessage) bool        { return false }
func (t *TeamCreateTool) IsConcurrencySafe(_ json.RawMessage) bool { return false }
func (t *TeamCreateTool) MaxResultSizeChars() int                  { return 4000 }
func (t *TeamCreateTool) IsEnabled(_ *tool.UseContext) bool        { return true }

func (t *TeamCreateTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"team_name": {
				"type": "string",
				"description": "Name for the new team to create."
			},
			"description": {
				"type": "string",
				"description": "Team description/purpose."
			},
			"agent_type": {
				"type": "string",
				"description": "Type/role of the team lead (e.g. researcher, test-runner)."
			}
		},
		"required": ["team_name"]
	}`)
}

func (t *TeamCreateTool) Prompt(_ *tool.UseContext) string { return "" }

func (t *TeamCreateTool) CheckPermissions(_ context.Context, _ json.RawMessage, _ *tool.UseContext) error {
	return nil
}

func (t *TeamCreateTool) Call(_ context.Context, input json.RawMessage, uctx *tool.UseContext) (<-chan *engine.ContentBlock, error) {
	ch := make(chan *engine.ContentBlock, 2)
	go func() {
		defer close(ch)

		var args struct {
			TeamName    string `json:"team_name"`
			Description string `json:"description"`
			AgentType   string `json:"agent_type"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			ch <- &engine.ContentBlock{Type: engine.ContentTypeText, Text: "invalid input: " + err.Error(), IsError: true}
			return
		}

		if args.TeamName == "" {
			ch <- &engine.ContentBlock{Type: engine.ContentTypeText, Text: "team_name is required", IsError: true}
			return
		}

		leadAgentID := fmt.Sprintf("team-lead@%s", args.TeamName)
		if uctx != nil && uctx.AgentID != "" {
			leadAgentID = uctx.AgentID
		}

		// Create the team via TeamManager if available.
		if t.creator != nil {
			if _, err := t.creator.CreateTeam(args.TeamName, args.Description, leadAgentID); err != nil {
				ch <- &engine.ContentBlock{Type: engine.ContentTypeText, Text: "team creation failed: " + err.Error(), IsError: true}
				return
			}
		}

		slog.Info("team_create: team registered",
			slog.String("team", args.TeamName),
			slog.String("lead", leadAgentID))

		// Return metadata only — no agent spawning.
		// Aligned with TS TeamCreateTool: only registers team metadata.
		// Actual agent spawning is done via the Task tool with name + team_name.
		resultJSON, _ := json.Marshal(map[string]string{
			"team_name":     args.TeamName,
			"lead_agent_id": leadAgentID,
			"status":        "registered",
			"note":          "Team registered. Use the Task tool with name + team_name parameters to spawn teammates.",
		})

		ch <- &engine.ContentBlock{Type: engine.ContentTypeText, Text: string(resultJSON)}
	}()
	return ch, nil
}
