package swarm

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wall-ai/agent-engine/internal/agent"
)

// ── MailboxAdapter — unified interface for in-memory and file mailboxes ──────
//
// This adapter routes messages to the appropriate mailbox backend based on
// the teammate's BackendType:
//   - In-process teammates → agent.MailboxRegistry (in-memory, high-perf)
//   - Tmux teammates → FileMailboxRegistry (file-based, cross-process)

// MailboxAdapter provides a unified message delivery interface
// that satisfies the sendmessage.MailboxSender contract.
type MailboxAdapter interface {
	// Send delivers a message to a single agent.
	Send(from, to, text string, priority string, replyTo string) (string, error)

	// SendEnvelope delivers a structured envelope to a single agent.
	SendEnvelope(from, to string, env *MailboxEnvelope) error

	// Broadcast sends a text message to all members of a team.
	Broadcast(from, teamName, text string) error

	// ReadPending returns unread messages for an agent (sorted by priority).
	ReadPending(agentID string) ([]MailboxEnvelope, error)

	// MarkRead marks a specific message as read.
	MarkRead(agentID, msgID string) error

	// Clear removes all messages for an agent.
	Clear(agentID string) error

	// TeamMembers returns agent IDs in a team (for broadcast).
	TeamMembers(teamName string) []string
}

// ── HybridMailboxAdapter ─────────────────────────────────────────────────────

// BackendResolver maps agent IDs to their backend type.
type BackendResolver func(agentID string) BackendType

// TeamMembersResolver returns agent IDs for a team.
type TeamMembersResolver func(teamName string) []string

// HybridMailboxAdapter routes messages to in-memory or file mailbox
// based on the teammate's backend type.
type HybridMailboxAdapter struct {
	mu sync.RWMutex

	inMemory *agent.MailboxRegistry
	fileMB   *FileMailboxRegistry

	resolveBackend BackendResolver
	resolveMembers TeamMembersResolver
}

// HybridMailboxConfig configures the hybrid mailbox adapter.
type HybridMailboxConfig struct {
	InMemory       *agent.MailboxRegistry
	FileMB         *FileMailboxRegistry
	BackendResolver BackendResolver
	MembersResolver TeamMembersResolver
}

// NewHybridMailboxAdapter creates an adapter that routes between backends.
func NewHybridMailboxAdapter(cfg HybridMailboxConfig) *HybridMailboxAdapter {
	return &HybridMailboxAdapter{
		inMemory:       cfg.InMemory,
		fileMB:         cfg.FileMB,
		resolveBackend: cfg.BackendResolver,
		resolveMembers: cfg.MembersResolver,
	}
}

// Send delivers a text message to a single agent.
func (h *HybridMailboxAdapter) Send(from, to, text string, priority string, replyTo string) (string, error) {
	backend := h.getBackend(to)

	switch backend {
	case BackendTmux:
		return h.sendViaFile(from, to, text, priority, replyTo)
	default:
		return h.sendViaMemory(from, to, text, priority, replyTo)
	}
}

// SendEnvelope delivers a structured envelope to a single agent.
func (h *HybridMailboxAdapter) SendEnvelope(from, to string, env *MailboxEnvelope) error {
	backend := h.getBackend(to)

	switch backend {
	case BackendTmux:
		if h.fileMB == nil {
			return fmt.Errorf("file mailbox not configured for tmux backend")
		}
		_, agentName := ParseAgentID(to)
		if agentName == "" {
			agentName = to
		}
		return h.fileMB.Send(from, agentName, env)

	default:
		// Convert envelope to in-memory message.
		payloadJSON, _ := json.Marshal(env.Payload)
		text := string(payloadJSON)
		if env.Type == MessageTypePlainText {
			var p PlainTextPayload
			_ = json.Unmarshal(env.Payload, &p)
			text = p.Text
		}
		prio := agent.MailboxPriority(env.priorityOrDefault())
		_, err := h.inMemory.Send(from, to, text, prio, "")
		return err
	}
}

// Broadcast sends a text message to all team members.
func (h *HybridMailboxAdapter) Broadcast(from, teamName, text string) error {
	members := h.TeamMembers(teamName)
	var lastErr error
	for _, m := range members {
		if m == from {
			continue
		}
		if _, err := h.Send(from, m, text, PriorityNormal, ""); err != nil {
			slog.Warn("broadcast: send failed",
				slog.String("to", m), slog.Any("err", err))
			lastErr = err
		}
	}
	return lastErr
}

// ReadPending returns unread messages for an agent.
func (h *HybridMailboxAdapter) ReadPending(agentID string) ([]MailboxEnvelope, error) {
	backend := h.getBackend(agentID)

	switch backend {
	case BackendTmux:
		if h.fileMB == nil {
			return nil, nil
		}
		_, agentName := ParseAgentID(agentID)
		if agentName == "" {
			agentName = agentID
		}
		mb, err := h.fileMB.GetOrCreate(agentName)
		if err != nil {
			return nil, err
		}
		return mb.ReadUnread()

	default:
		// Convert in-memory messages to envelopes.
		mb := h.inMemory.GetOrCreate(agentID)
		pending := mb.Read()
		var envs []MailboxEnvelope
		for _, m := range pending {
			envs = append(envs, MailboxEnvelope{
				ID:        m.ID,
				From:      m.From,
				To:        m.To,
				Type:      MessageTypePlainText,
				Timestamp: m.Timestamp,
				Payload:   mustMarshal(PlainTextPayload{Text: m.Text}),
			})
		}
		return envs, nil
	}
}

// MarkRead marks a message as read.
func (h *HybridMailboxAdapter) MarkRead(agentID, msgID string) error {
	backend := h.getBackend(agentID)

	switch backend {
	case BackendTmux:
		if h.fileMB == nil {
			return nil
		}
		_, agentName := ParseAgentID(agentID)
		if agentName == "" {
			agentName = agentID
		}
		mb, err := h.fileMB.GetOrCreate(agentName)
		if err != nil {
			return err
		}
		return mb.MarkRead(msgID)

	default:
		h.inMemory.GetOrCreate(agentID).Ack(msgID)
		return nil
	}
}

// Clear removes all messages for an agent.
func (h *HybridMailboxAdapter) Clear(agentID string) error {
	backend := h.getBackend(agentID)

	switch backend {
	case BackendTmux:
		if h.fileMB == nil {
			return nil
		}
		_, agentName := ParseAgentID(agentID)
		if agentName == "" {
			agentName = agentID
		}
		mb, err := h.fileMB.GetOrCreate(agentName)
		if err != nil {
			return err
		}
		return mb.Clear()

	default:
		h.inMemory.GetOrCreate(agentID).Clear()
		return nil
	}
}

// TeamMembers returns agent IDs for the specified team.
func (h *HybridMailboxAdapter) TeamMembers(teamName string) []string {
	if h.resolveMembers != nil {
		return h.resolveMembers(teamName)
	}
	return nil
}

// ── Internal helpers ─────────────────────────────────────────────────────────

func (h *HybridMailboxAdapter) getBackend(agentID string) BackendType {
	if h.resolveBackend != nil {
		return h.resolveBackend(agentID)
	}
	return BackendInProcess
}

func (h *HybridMailboxAdapter) sendViaMemory(from, to, text, priority, replyTo string) (string, error) {
	if h.inMemory == nil {
		return "", fmt.Errorf("in-memory mailbox not configured")
	}
	prio := agent.MailboxPriority(priority)
	if prio == "" {
		prio = agent.MailboxPriorityNormal
	}
	return h.inMemory.Send(from, to, text, prio, replyTo)
}

func (h *HybridMailboxAdapter) sendViaFile(from, to, text, priority, replyTo string) (string, error) {
	if h.fileMB == nil {
		return "", fmt.Errorf("file mailbox not configured for tmux backend")
	}

	_, agentName := ParseAgentID(to)
	if agentName == "" {
		agentName = to
	}

	env := &MailboxEnvelope{
		ID:        uuid.New().String(),
		From:      from,
		To:        to,
		Type:      MessageTypePlainText,
		Timestamp: time.Now(),
		Payload:   mustMarshal(PlainTextPayload{Text: text}),
	}

	if err := h.fileMB.Send(from, agentName, env); err != nil {
		return "", err
	}
	return env.ID, nil
}

func (e *MailboxEnvelope) priorityOrDefault() string {
	// Not stored in envelope currently; default to normal.
	return PriorityNormal
}

func mustMarshal(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// ── InMemoryOnlyAdapter ─────────────────────────────────────────────────────

// InMemoryOnlyAdapter wraps agent.MailboxRegistry as a MailboxAdapter
// for setups without file-based teammates.
type InMemoryOnlyAdapter struct {
	registry       *agent.MailboxRegistry
	resolveMembers TeamMembersResolver
}

// NewInMemoryOnlyAdapter creates an adapter backed solely by in-memory mailboxes.
func NewInMemoryOnlyAdapter(reg *agent.MailboxRegistry, membersResolver TeamMembersResolver) *InMemoryOnlyAdapter {
	return &InMemoryOnlyAdapter{
		registry:       reg,
		resolveMembers: membersResolver,
	}
}

func (a *InMemoryOnlyAdapter) Send(from, to, text, priority, replyTo string) (string, error) {
	prio := agent.MailboxPriority(priority)
	if prio == "" {
		prio = agent.MailboxPriorityNormal
	}
	return a.registry.Send(from, to, text, prio, replyTo)
}

func (a *InMemoryOnlyAdapter) SendEnvelope(from, to string, env *MailboxEnvelope) error {
	text := ""
	if env.Type == MessageTypePlainText {
		var p PlainTextPayload
		_ = json.Unmarshal(env.Payload, &p)
		text = p.Text
	} else {
		text = string(env.Payload)
	}
	_, err := a.registry.Send(from, to, text, agent.MailboxPriorityNormal, "")
	return err
}

func (a *InMemoryOnlyAdapter) Broadcast(from, teamName, text string) error {
	members := a.TeamMembers(teamName)
	for _, m := range members {
		if m == from {
			continue
		}
		if _, err := a.Send(from, m, text, PriorityNormal, ""); err != nil {
			slog.Warn("broadcast: send failed", slog.String("to", m), slog.Any("err", err))
		}
	}
	return nil
}

func (a *InMemoryOnlyAdapter) ReadPending(agentID string) ([]MailboxEnvelope, error) {
	mb := a.registry.GetOrCreate(agentID)
	pending := mb.Read()
	var envs []MailboxEnvelope
	for _, m := range pending {
		envs = append(envs, MailboxEnvelope{
			ID:        m.ID,
			From:      m.From,
			To:        m.To,
			Type:      MessageTypePlainText,
			Timestamp: m.Timestamp,
			Payload:   mustMarshal(PlainTextPayload{Text: m.Text}),
		})
	}
	return envs, nil
}

func (a *InMemoryOnlyAdapter) MarkRead(agentID, msgID string) error {
	a.registry.GetOrCreate(agentID).Ack(msgID)
	return nil
}

func (a *InMemoryOnlyAdapter) Clear(agentID string) error {
	a.registry.GetOrCreate(agentID).Clear()
	return nil
}

func (a *InMemoryOnlyAdapter) TeamMembers(teamName string) []string {
	if a.resolveMembers != nil {
		return a.resolveMembers(teamName)
	}
	return nil
}
