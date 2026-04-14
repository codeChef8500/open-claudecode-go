package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Peer address management for cross-process agent communication.
// Aligned with claude-code-main's peerAddress.ts / bridge communication.
//
// When agents run in separate processes (e.g. tmux panes, containers),
// they need a way to discover and connect to each other. Peer addresses
// use a file-based registry under .claude/peers/.

// PeerAddress represents a resolvable address for an agent peer.
type PeerAddress struct {
	AgentID    string    `json:"agent_id"`
	AgentName  string    `json:"agent_name,omitempty"`
	TeamName   string    `json:"team_name,omitempty"`
	Protocol   string    `json:"protocol"`              // "uds" | "tcp" | "inprocess"
	Address    string    `json:"address"`                // socket path or host:port
	PID        int       `json:"pid,omitempty"`          // process ID
	SessionID  string    `json:"session_id,omitempty"`   // session identifier
	RegisterAt time.Time `json:"registered_at"`
	LastSeen   time.Time `json:"last_seen"`
}

// PeerAddressRegistry manages peer address registration and discovery.
type PeerAddressRegistry struct {
	baseDir string // .claude/peers/ directory
}

// NewPeerAddressRegistry creates a peer address registry.
func NewPeerAddressRegistry(baseDir string) *PeerAddressRegistry {
	return &PeerAddressRegistry{baseDir: baseDir}
}

// peersDir returns the directory for peer address files.
func (r *PeerAddressRegistry) peersDir() string {
	return filepath.Join(r.baseDir, ".claude", "peers")
}

// peerPath returns the file path for a specific peer.
func (r *PeerAddressRegistry) peerPath(agentID string) string {
	// Sanitize agentID for use as filename.
	safe := strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(agentID)
	return filepath.Join(r.peersDir(), safe+".json")
}

// Register publishes this agent's address for other processes to discover.
func (r *PeerAddressRegistry) Register(addr PeerAddress) error {
	dir := r.peersDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create peers dir: %w", err)
	}

	addr.RegisterAt = time.Now()
	addr.LastSeen = time.Now()

	data, err := json.MarshalIndent(addr, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal peer address: %w", err)
	}

	path := r.peerPath(addr.AgentID)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write peer address: %w", err)
	}

	slog.Debug("peer_address: registered",
		slog.String("agent_id", addr.AgentID),
		slog.String("protocol", addr.Protocol),
		slog.String("address", addr.Address),
	)

	return nil
}

// Unregister removes a peer's address file.
func (r *PeerAddressRegistry) Unregister(agentID string) error {
	path := r.peerPath(agentID)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove peer address: %w", err)
	}
	return nil
}

// Lookup finds a peer by agent ID.
func (r *PeerAddressRegistry) Lookup(agentID string) (*PeerAddress, error) {
	path := r.peerPath(agentID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read peer address: %w", err)
	}

	var addr PeerAddress
	if err := json.Unmarshal(data, &addr); err != nil {
		return nil, fmt.Errorf("parse peer address: %w", err)
	}

	return &addr, nil
}

// LookupByName finds a peer by agent name (searching all registered peers).
func (r *PeerAddressRegistry) LookupByName(agentName string) (*PeerAddress, error) {
	entries, err := os.ReadDir(r.peersDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(r.peersDir(), e.Name()))
		if err != nil {
			continue
		}
		var addr PeerAddress
		if err := json.Unmarshal(data, &addr); err != nil {
			continue
		}
		if addr.AgentName == agentName {
			return &addr, nil
		}
	}

	return nil, nil
}

// ListAll returns all registered peer addresses.
func (r *PeerAddressRegistry) ListAll() ([]*PeerAddress, error) {
	entries, err := os.ReadDir(r.peersDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var peers []*PeerAddress
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(r.peersDir(), e.Name()))
		if err != nil {
			continue
		}
		var addr PeerAddress
		if err := json.Unmarshal(data, &addr); err != nil {
			continue
		}
		peers = append(peers, &addr)
	}

	return peers, nil
}

// Touch updates the LastSeen timestamp for a peer.
func (r *PeerAddressRegistry) Touch(agentID string) error {
	addr, err := r.Lookup(agentID)
	if err != nil || addr == nil {
		return err
	}
	addr.LastSeen = time.Now()
	return r.Register(*addr)
}

// CleanupStale removes peer address files that haven't been seen within maxAge.
func (r *PeerAddressRegistry) CleanupStale(maxAge time.Duration) (int, error) {
	peers, err := r.ListAll()
	if err != nil {
		return 0, err
	}

	cutoff := time.Now().Add(-maxAge)
	removed := 0
	for _, p := range peers {
		if p.LastSeen.Before(cutoff) {
			if err := r.Unregister(p.AgentID); err != nil {
				slog.Warn("peer_address: cleanup failed",
					slog.String("agent_id", p.AgentID),
					slog.Any("err", err),
				)
				continue
			}
			removed++
		}
	}

	return removed, nil
}

// ── UDS Listener ────────────────────────────────────────────────────────────

// UDSSocketPath returns the Unix domain socket path for an agent.
func UDSSocketPath(baseDir, agentID string) string {
	safe := strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(agentID)
	return filepath.Join(baseDir, ".claude", "sockets", safe+".sock")
}

// CreateUDSListener creates a Unix domain socket listener for IPC.
// On Windows, this falls back to TCP on localhost with an ephemeral port.
func CreateUDSListener(baseDir, agentID string) (net.Listener, string, error) {
	// Try UDS first (Unix/macOS/Linux).
	sockPath := UDSSocketPath(baseDir, agentID)
	sockDir := filepath.Dir(sockPath)
	if err := os.MkdirAll(sockDir, 0o755); err != nil {
		return nil, "", fmt.Errorf("create socket dir: %w", err)
	}

	// Remove stale socket.
	_ = os.Remove(sockPath)

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		// Fallback: TCP on localhost.
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, "", fmt.Errorf("listen: %w", err)
		}
		return ln, ln.Addr().String(), nil
	}

	return ln, sockPath, nil
}

// CleanupUDSSocket removes a stale UDS socket file.
func CleanupUDSSocket(baseDir, agentID string) {
	sockPath := UDSSocketPath(baseDir, agentID)
	_ = os.Remove(sockPath)
}
