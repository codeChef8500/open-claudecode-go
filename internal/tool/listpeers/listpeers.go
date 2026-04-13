package listpeers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/wall-ai/agent-engine/internal/agent"
	"github.com/wall-ai/agent-engine/internal/engine"
	"github.com/wall-ai/agent-engine/internal/tool"
)

// ListPeersTool lists all currently active peer agents in the session.
type ListPeersTool struct {
	tool.BaseTool
	asyncMgr *agent.AsyncLifecycleManager
}

func New() *ListPeersTool { return &ListPeersTool{} }

// NewWithManager creates a ListPeersTool wired to an AsyncLifecycleManager.
func NewWithManager(mgr *agent.AsyncLifecycleManager) *ListPeersTool {
	return &ListPeersTool{asyncMgr: mgr}
}

func (t *ListPeersTool) Name() string           { return "list_peers" }
func (t *ListPeersTool) UserFacingName() string { return "ListPeers" }
func (t *ListPeersTool) Description() string {
	return "List all currently active peer agents and their statuses."
}
func (t *ListPeersTool) IsReadOnly(_ json.RawMessage) bool        { return true }
func (t *ListPeersTool) IsConcurrencySafe(_ json.RawMessage) bool { return true }
func (t *ListPeersTool) MaxResultSizeChars() int                  { return 4000 }
func (t *ListPeersTool) IsEnabled(_ *tool.UseContext) bool        { return true }
func (t *ListPeersTool) IsSearchOrRead(_ json.RawMessage) engine.SearchOrReadInfo {
	return engine.SearchOrReadInfo{IsSearch: true}
}

func (t *ListPeersTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func (t *ListPeersTool) Prompt(_ *tool.UseContext) string { return "" }

func (t *ListPeersTool) CheckPermissions(_ context.Context, _ json.RawMessage, _ *tool.UseContext) error {
	return nil
}

func (t *ListPeersTool) Call(_ context.Context, _ json.RawMessage, uctx *tool.UseContext) (<-chan *engine.ContentBlock, error) {
	ch := make(chan *engine.ContentBlock, 1)
	go func() {
		defer close(ch)

		var sb strings.Builder
		sb.WriteString("Peer agents:\n")

		if uctx != nil && uctx.AgentID != "" {
			sb.WriteString(fmt.Sprintf("  - [self] %s (current)\n", uctx.AgentID))
		}

		if t.asyncMgr != nil {
			infos := t.asyncMgr.ListPeerInfos()
			if len(infos) == 0 {
				sb.WriteString("  (no background agents)\n")
			} else {
				for _, info := range infos {
					durStr := ""
					if info.Duration > 0 {
						durStr = fmt.Sprintf(", %s", info.Duration.Round(time.Second))
					} else if !info.StartedAt.IsZero() {
						durStr = fmt.Sprintf(", %s", time.Since(info.StartedAt).Round(time.Second))
					}
					sb.WriteString(fmt.Sprintf("  - [%s] %s%s\n", info.Status, info.AgentID, durStr))
				}
			}
		} else {
			sb.WriteString("  (no agent tracking available)\n")
		}

		ch <- &engine.ContentBlock{Type: engine.ContentTypeText, Text: sb.String()}
	}()
	return ch, nil
}
