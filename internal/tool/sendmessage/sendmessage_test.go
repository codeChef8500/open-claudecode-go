package sendmessage

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	agentswarm "github.com/wall-ai/agent-engine/internal/agent/swarm"
	"github.com/wall-ai/agent-engine/internal/engine"
)

type testSender struct {
	sentFrom      string
	sentTo        string
	sentText      string
	sentPriority  string
	envelopeFrom  string
	envelopeTo    string
	envelope      *agentswarm.MailboxEnvelope
	broadcastFrom string
	broadcastTeam string
	broadcastText string
}

func (s *testSender) Send(from, to, text string, priority string, replyTo string) (string, error) {
	s.sentFrom = from
	s.sentTo = to
	s.sentText = text
	s.sentPriority = priority
	return "msg-1", nil
}

func (s *testSender) Broadcast(from, teamName, text string) error {
	s.broadcastFrom = from
	s.broadcastTeam = teamName
	s.broadcastText = text
	return nil
}

func (s *testSender) TeamMembers(teamName string) []string {
	return []string{"worker@test-team"}
}

func (s *testSender) SendEnvelope(from, to string, env *agentswarm.MailboxEnvelope) error {
	s.envelopeFrom = from
	s.envelopeTo = to
	s.envelope = env
	return nil
}

func readSingleTextBlock(t *testing.T, ch <-chan *engine.ContentBlock) string {
	t.Helper()
	var blocks []*engine.ContentBlock
	for block := range ch {
		blocks = append(blocks, block)
	}
	require.Len(t, blocks, 1)
	return blocks[0].Text
}

func TestSendMessage_UsesResolvedTargetForDirectSend(t *testing.T) {
	sender := &testSender{}
	tool := NewWithAllDeps(sender, nil, func(name string) string {
		if name == "researcher" {
			return "researcher@test-team"
		}
		return ""
	})

	input, err := json.Marshal(Input{
		Message: "hello",
		To:      "researcher",
	})
	require.NoError(t, err)

	ch, err := tool.Call(context.Background(), input, &engine.UseContext{AgentID: "leader@test-team"})
	require.NoError(t, err)
	text := readSingleTextBlock(t, ch)

	assert.Equal(t, "leader@test-team", sender.sentFrom)
	assert.Equal(t, "researcher@test-team", sender.sentTo)
	assert.Equal(t, "hello", sender.sentText)
	assert.Contains(t, text, "researcher@test-team")
}

func TestSendMessage_StructuredPlanApprovalRequest(t *testing.T) {
	sender := &testSender{}
	tool := NewWithAllDeps(sender, nil, nil)

	input, err := json.Marshal(Input{
		Message:     "proposed plan",
		To:          "worker@test-team",
		MessageType: "plan_approval_request",
	})
	require.NoError(t, err)

	ch, err := tool.Call(context.Background(), input, &engine.UseContext{AgentID: "leader@test-team"})
	require.NoError(t, err)
	text := readSingleTextBlock(t, ch)

	require.NotNil(t, sender.envelope)
	assert.Equal(t, "leader@test-team", sender.envelopeFrom)
	assert.Equal(t, "worker@test-team", sender.envelopeTo)
	assert.Equal(t, agentswarm.MessageTypePlanApprovalRequest, sender.envelope.Type)
	assert.Contains(t, text, "Structured message sent")

	var payload agentswarm.PlanApprovalRequestPayload
	require.NoError(t, sender.envelope.DecodePayload(&payload))
	assert.Equal(t, "proposed plan", payload.PlanText)
	assert.Equal(t, "leader@test-team", payload.AgentName)
}

func TestSendMessage_StructuredAliases(t *testing.T) {
	t.Run("plan rejection maps to denied approval response", func(t *testing.T) {
		sender := &testSender{}
		tool := NewWithAllDeps(sender, nil, nil)
		approved := true
		input, err := json.Marshal(Input{
			Message:     "needs revision",
			To:          "worker@test-team",
			MessageType: "plan_rejection",
			Approved:    &approved,
		})
		require.NoError(t, err)

		ch, err := tool.Call(context.Background(), input, &engine.UseContext{AgentID: "leader@test-team"})
		require.NoError(t, err)
		_ = readSingleTextBlock(t, ch)

		require.NotNil(t, sender.envelope)
		assert.Equal(t, agentswarm.MessageTypePlanApprovalResponse, sender.envelope.Type)
		var payload agentswarm.PlanApprovalResponsePayload
		require.NoError(t, sender.envelope.DecodePayload(&payload))
		assert.False(t, payload.Approved)
		assert.Equal(t, "needs revision", payload.Feedback)
	})

	t.Run("shutdown rejected alias maps to shutdown rejected envelope", func(t *testing.T) {
		sender := &testSender{}
		tool := NewWithAllDeps(sender, nil, nil)
		approved := false
		input, err := json.Marshal(Input{
			Message:     "still working",
			To:          "worker@test-team",
			MessageType: "shutdown_rejected",
			Approved:    &approved,
		})
		require.NoError(t, err)

		ch, err := tool.Call(context.Background(), input, &engine.UseContext{AgentID: "leader@test-team"})
		require.NoError(t, err)
		_ = readSingleTextBlock(t, ch)

		require.NotNil(t, sender.envelope)
		assert.Equal(t, agentswarm.MessageTypeShutdownRejected, sender.envelope.Type)
		var payload agentswarm.ShutdownRejectedPayload
		require.NoError(t, sender.envelope.DecodePayload(&payload))
		assert.Equal(t, "still working", payload.Reason)
	})
}

func TestSendMessage_StructuredSendCallback(t *testing.T) {
	sender := &testSender{}
	tool := NewWithAllDeps(sender, nil, nil)
	var got StructuredSendEvent
	tool.SetStructuredSendCallback(func(ev StructuredSendEvent) {
		got = ev
	})

	input, err := json.Marshal(Input{
		Message:     "done here",
		To:          "worker@test-team",
		MessageType: "shutdown_request",
	})
	require.NoError(t, err)

	ch, err := tool.Call(context.Background(), input, &engine.UseContext{AgentID: "leader@test-team"})
	require.NoError(t, err)
	_ = readSingleTextBlock(t, ch)

	assert.Equal(t, "shutdown_request", got.MessageType)
	assert.Equal(t, "leader@test-team", got.From)
	assert.Equal(t, "worker@test-team", got.To)
	assert.Equal(t, "done here", got.Message)
}
