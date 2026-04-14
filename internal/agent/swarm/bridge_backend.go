package swarm

import (
	"context"
	"fmt"
)

// BridgeBackendImpl is a placeholder executor for future cross-process bridge
// routing. It currently shares the external mailbox path used by tmux/uds
// workers so the routing layer can already distinguish BackendBridge.
type BridgeBackendImpl struct {
	registry *TeammateRegistry
	fileMB   *FileMailboxRegistry
}

// BridgeBackendConfig configures the bridge backend.
type BridgeBackendConfig struct {
	Registry *TeammateRegistry
	FileMB   *FileMailboxRegistry
}

// NewBridgeBackend creates a new bridge backend skeleton.
func NewBridgeBackend(cfg BridgeBackendConfig) *BridgeBackendImpl {
	return &BridgeBackendImpl{
		registry: cfg.Registry,
		fileMB:   cfg.FileMB,
	}
}

// Spawn is not implemented yet for bridge workers.
func (b *BridgeBackendImpl) Spawn(_ context.Context, config TeammateSpawnConfig) (*TeammateSpawnResult, error) {
	agentID := config.Identity.AgentID
	if agentID == "" {
		agentID = FormatAgentID(config.Identity.AgentName, config.Identity.TeamName)
		config.Identity.AgentID = agentID
	}
	return nil, fmt.Errorf("bridge backend spawn not implemented for %s", agentID)
}

// SendMessage delivers a message via the shared external mailbox path.
func (b *BridgeBackendImpl) SendMessage(agentID string, message TeammateMessage) error {
	if b.fileMB == nil {
		return fmt.Errorf("file mailbox not configured for bridge backend")
	}
	name, _ := ParseAgentID(agentID)
	if name == "" {
		name = agentID
	}
	env, err := NewEnvelope(message.From, agentID, message.MessageType, PlainTextPayload{Text: message.Content})
	if err != nil {
		return err
	}
	return b.fileMB.Send(message.From, name, env)
}

// Terminate requests graceful shutdown over the external mailbox.
func (b *BridgeBackendImpl) Terminate(agentID string, reason string) error {
	if b.fileMB == nil {
		return fmt.Errorf("file mailbox not configured for bridge backend")
	}
	name, _ := ParseAgentID(agentID)
	if name == "" {
		name = agentID
	}
	env, err := NewEnvelope(TeamLeadName, agentID, MessageTypeShutdownRequest, ShutdownRequestPayload{Reason: reason})
	if err != nil {
		return err
	}
	return b.fileMB.Send(TeamLeadName, name, env)
}

// Kill is not implemented yet for bridge workers.
func (b *BridgeBackendImpl) Kill(agentID string) error {
	return fmt.Errorf("bridge backend kill not implemented for %s", agentID)
}

// IsActive reports whether the agent is known to the registry.
func (b *BridgeBackendImpl) IsActive(agentID string) bool {
	if b.registry == nil {
		return false
	}
	_, ok := b.registry.Get(agentID)
	return ok
}

// Type returns the backend type.
func (b *BridgeBackendImpl) Type() BackendType {
	return BackendBridge
}
