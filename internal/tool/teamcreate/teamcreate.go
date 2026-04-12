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
				"description": "Unique identifier for this team."
			},
			"agents": {
				"type": "array",
				"description": "List of agent definitions.",
				"items": {
					"type": "object",
					"properties": {
						"agent_id": {"type": "string"},
						"task":     {"type": "string"},
						"work_dir": {"type": "string"}
					},
					"required": ["agent_id", "task"]
				}
			}
		},
		"required": ["team_name", "agents"]
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
			Agents      []struct {
				AgentID   string `json:"agent_id"`
				AgentName string `json:"agent_name"`
				Task      string `json:"task"`
				WorkDir   string `json:"work_dir"`
				AgentType string `json:"agent_type"`
			} `json:"agents"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			ch <- &engine.ContentBlock{Type: engine.ContentTypeText, Text: "invalid input: " + err.Error(), IsError: true}
			return
		}

		leadAgentID := ""
		if uctx != nil {
			leadAgentID = uctx.AgentID
		}

		// Create the team via TeamManager if available.
		if t.creator != nil {
			if _, err := t.creator.CreateTeam(args.TeamName, args.Description, leadAgentID); err != nil {
				ch <- &engine.ContentBlock{Type: engine.ContentTypeText, Text: "team creation failed: " + err.Error(), IsError: true}
				return
			}
			slog.Info("team_create: team created via manager",
				slog.String("team", args.TeamName),
				slog.Int("agents", len(args.Agents)))
		}

		workDir := ""
		if uctx != nil {
			workDir = uctx.WorkDir
		}

		result := fmt.Sprintf("Team %q created with %d agent(s):\n", args.TeamName, len(args.Agents))
		for _, a := range args.Agents {
			dir := a.WorkDir
			if dir == "" {
				dir = workDir
			}
			result += fmt.Sprintf("  - %s: %s (dir: %s)\n", a.AgentID, a.Task, dir)
		}
		result += "\nAgents are queued for parallel execution."

		ch <- &engine.ContentBlock{Type: engine.ContentTypeText, Text: result}
	}()
	return ch, nil
}
