package swarm

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileMailbox_WriteAndReadAll(t *testing.T) {
	dir := t.TempDir()
	mb, err := NewFileMailbox(FileMailboxConfig{
		TeamName:  "test-team",
		AgentName: "worker-1",
		BaseDir:   dir,
	})
	require.NoError(t, err)

	// Write two messages.
	env1, _ := NewEnvelope("leader@test-team", "worker-1@test-team", MessageTypePlainText, PlainTextPayload{Text: "Hello"})
	env2, _ := NewEnvelope("leader@test-team", "worker-1@test-team", MessageTypeTaskAssignment, TaskAssignmentPayload{TaskID: "t1", Description: "Find bugs"})
	require.NoError(t, mb.Write(env1))
	require.NoError(t, mb.Write(env2))

	// Read all.
	msgs, err := mb.ReadAll()
	require.NoError(t, err)
	assert.Len(t, msgs, 2)
	assert.Equal(t, MessageTypePlainText, msgs[0].Type)
	assert.Equal(t, MessageTypeTaskAssignment, msgs[1].Type)
}

func TestFileMailbox_ReadUnread(t *testing.T) {
	dir := t.TempDir()
	mb, err := NewFileMailbox(FileMailboxConfig{
		TeamName:  "test-team",
		AgentName: "worker-1",
		BaseDir:   dir,
	})
	require.NoError(t, err)

	env1, _ := NewEnvelope("leader@test-team", "worker-1@test-team", MessageTypePlainText, PlainTextPayload{Text: "msg1"})
	env2, _ := NewEnvelope("leader@test-team", "worker-1@test-team", MessageTypePlainText, PlainTextPayload{Text: "msg2"})
	require.NoError(t, mb.Write(env1))
	require.NoError(t, mb.Write(env2))

	// Mark first as read.
	msgs, _ := mb.ReadAll()
	require.NoError(t, mb.MarkRead(msgs[0].ID))

	// Only second should be unread.
	unread, err := mb.ReadUnread()
	require.NoError(t, err)
	assert.Len(t, unread, 1)
	assert.Equal(t, msgs[1].ID, unread[0].ID)
}

func TestFileMailbox_LeaderPriority(t *testing.T) {
	dir := t.TempDir()
	mb, err := NewFileMailbox(FileMailboxConfig{
		TeamName:  "test-team",
		AgentName: "worker-1",
		BaseDir:   dir,
	})
	require.NoError(t, err)

	// Write peer message first, then leader message.
	peer, _ := NewEnvelope("worker-2@test-team", "worker-1@test-team", MessageTypePlainText, PlainTextPayload{Text: "peer"})
	peer.Timestamp = time.Now()
	leader, _ := NewEnvelope("team-lead@test-team", "worker-1@test-team", MessageTypePlainText, PlainTextPayload{Text: "leader"})
	leader.Timestamp = time.Now().Add(time.Second)
	require.NoError(t, mb.Write(peer))
	require.NoError(t, mb.Write(leader))

	// Read unread: leader should be first despite being written second.
	unread, err := mb.ReadUnread()
	require.NoError(t, err)
	require.Len(t, unread, 2)
	assert.True(t, unread[0].IsFromLeader(), "leader message should come first")
}

func TestFileMailbox_Clear(t *testing.T) {
	dir := t.TempDir()
	mb, err := NewFileMailbox(FileMailboxConfig{
		TeamName:  "test-team",
		AgentName: "worker-1",
		BaseDir:   dir,
	})
	require.NoError(t, err)

	env, _ := NewEnvelope("a", "b", MessageTypePlainText, PlainTextPayload{Text: "x"})
	require.NoError(t, mb.Write(env))

	require.NoError(t, mb.Clear())
	msgs, err := mb.ReadAll()
	require.NoError(t, err)
	assert.Empty(t, msgs)
}

func TestFileMailbox_ConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	mb, err := NewFileMailbox(FileMailboxConfig{
		TeamName:  "test-team",
		AgentName: "worker-1",
		BaseDir:   dir,
	})
	require.NoError(t, err)

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			env, _ := NewEnvelope("sender", "worker-1", MessageTypePlainText, PlainTextPayload{Text: "msg"})
			_ = mb.Write(env)
		}(i)
	}
	wg.Wait()

	msgs, err := mb.ReadAll()
	require.NoError(t, err)
	assert.Len(t, msgs, n)
}

func TestFileMailboxRegistry_SendAndRead(t *testing.T) {
	dir := t.TempDir()
	reg := NewFileMailboxRegistry("test-team", dir)

	env, _ := NewEnvelope("leader", "worker-1", MessageTypePlainText, PlainTextPayload{Text: "hello"})
	require.NoError(t, reg.Send("leader", "worker-1", env))

	mb, err := reg.GetOrCreate("worker-1")
	require.NoError(t, err)
	msgs, err := mb.ReadAll()
	require.NoError(t, err)
	assert.Len(t, msgs, 1)
}

func TestFileMailbox_EnvelopePayloadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	mb, err := NewFileMailbox(FileMailboxConfig{
		TeamName:  "rt-team",
		AgentName: "worker",
		BaseDir:   dir,
	})
	require.NoError(t, err)

	orig := PermissionRequestPayload{
		RequestID:   "req-1",
		ToolName:    "Edit",
		ToolUseID:   "tu-1",
		Input:       `{"file":"main.go"}`,
		Description: "Edit main.go",
		WorkerID:    "worker@rt-team",
		WorkerName:  "worker",
		WorkerColor: "#ff0000",
		TeamName:    "rt-team",
	}
	env, err := NewEnvelope("worker@rt-team", "team-lead@rt-team", MessageTypePermissionRequest, orig)
	require.NoError(t, err)
	require.NoError(t, mb.Write(env))

	msgs, err := mb.ReadAll()
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	var decoded PermissionRequestPayload
	require.NoError(t, msgs[0].DecodePayload(&decoded))
	assert.Equal(t, orig, decoded)
}
