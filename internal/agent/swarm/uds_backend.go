package swarm

import (
	"context"
	"fmt"
)

// UDSBackendImpl is a placeholder executor for future Unix-domain-socket
// teammate routing. For now it reuses the file mailbox path for message
// delivery so higher layers can route to BackendUDS without special cases.
type UDSBackendImpl struct {
	registry *TeammateRegistry
	fileMB   *FileMailboxRegistry
}

// UDSBackendConfig configures the UDS backend.
type UDSBackendConfig struct {
	Registry *TeammateRegistry
	FileMB   *FileMailboxRegistry
}

// NewUDSBackend creates a new UDS backend skeleton.
func NewUDSBackend(cfg UDSBackendConfig) *UDSBackendImpl {
	return &UDSBackendImpl{
		registry: cfg.Registry,
		fileMB:   cfg.FileMB,
	}
}

// Spawn is not implemented yet for UDS workers.
func (b *UDSBackendImpl) Spawn(_ context.Context, config TeammateSpawnConfig) (*TeammateSpawnResult, error) {
	agentID := config.Identity.AgentID
	if agentID == "" {
		agentID = FormatAgentID(config.Identity.AgentName, config.Identity.TeamName)
		config.Identity.AgentID = agentID
	}
	return nil, fmt.Errorf("uds backend spawn not implemented for %s", agentID)
}

// SendMessage delivers a message via the shared external mailbox path.
func (b *UDSBackendImpl) SendMessage(agentID string, message TeammateMessage) error {
	if b.fileMB == nil {
		return fmt.Errorf("file mailbox not configured for uds backend")
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
func (b *UDSBackendImpl) Terminate(agentID string, reason string) error {
	if b.fileMB == nil {
		return fmt.Errorf("file mailbox not configured for uds backend")
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

// Kill is not implemented yet for UDS workers.
func (b *UDSBackendImpl) Kill(agentID string) error {
	return fmt.Errorf("uds backend kill not implemented for %s", agentID)
}

// IsActive reports whether the agent is known to the registry.
func (b *UDSBackendImpl) IsActive(agentID string) bool {
	if b.registry == nil {
		return false
	}
	_, ok := b.registry.Get(agentID)
	return ok
}

// Type returns the backend type.
func (b *UDSBackendImpl) Type() BackendType {
	return BackendUDS
}
