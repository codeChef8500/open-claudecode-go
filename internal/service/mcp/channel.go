package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
)

// Channel system — aligned with claude-code-main channelNotification.ts,
// channelPermissions.ts, and channelAllowlist.ts.
//
// MCP servers can push notifications through named channels. The channel system
// manages subscriptions, permission allowlists, and message delivery.

// ────────────────────────────────────────────────────────────────────────────
// Channel notification types
// ────────────────────────────────────────────────────────────────────────────

// ChannelMessage is a notification pushed from an MCP server through a channel.
type ChannelMessage struct {
	// Channel is the channel name (e.g., "status", "progress", "error").
	Channel string `json:"channel"`
	// ServerName is the MCP server that sent the message.
	ServerName string `json:"server_name"`
	// Data is the notification payload.
	Data json.RawMessage `json:"data,omitempty"`
	// Text is a human-readable summary (optional).
	Text string `json:"text,omitempty"`
	// Level indicates severity: "info", "warning", "error".
	Level string `json:"level,omitempty"`
}

// ChannelHandler processes channel messages.
type ChannelHandler func(msg ChannelMessage)

// ────────────────────────────────────────────────────────────────────────────
// Channel permission allowlist
// ────────────────────────────────────────────────────────────────────────────

// ChannelPermission defines what a server is allowed to do on a channel.
type ChannelPermission string

const (
	ChannelPermRead  ChannelPermission = "read"
	ChannelPermWrite ChannelPermission = "write"
	ChannelPermAdmin ChannelPermission = "admin"
)

// ChannelAllowlistEntry maps a server to its allowed channels and permissions.
type ChannelAllowlistEntry struct {
	ServerName  string              `json:"server_name"`
	Channels    []string            `json:"channels"`
	Permissions []ChannelPermission `json:"permissions"`
}

// ────────────────────────────────────────────────────────────────────────────
// Channel Manager
// ────────────────────────────────────────────────────────────────────────────

// ChannelManager manages MCP server notification channels.
type ChannelManager struct {
	mu            sync.RWMutex
	handlers      map[string][]ChannelHandler    // channel → handlers
	allowlist     map[string]*ChannelAllowlistEntry // serverName → entry
	messageBuffer []ChannelMessage
	bufferSize    int
}

// NewChannelManager creates a new channel manager.
func NewChannelManager() *ChannelManager {
	return &ChannelManager{
		handlers:   make(map[string][]ChannelHandler),
		allowlist:  make(map[string]*ChannelAllowlistEntry),
		bufferSize: 100,
	}
}

// Subscribe registers a handler for messages on the given channel.
// Returns an unsubscribe function.
func (cm *ChannelManager) Subscribe(channel string, handler ChannelHandler) func() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.handlers[channel] = append(cm.handlers[channel], handler)
	idx := len(cm.handlers[channel]) - 1

	return func() {
		cm.mu.Lock()
		defer cm.mu.Unlock()
		handlers := cm.handlers[channel]
		if idx < len(handlers) {
			cm.handlers[channel] = append(handlers[:idx], handlers[idx+1:]...)
		}
	}
}

// SubscribeAll registers a handler for messages on all channels.
func (cm *ChannelManager) SubscribeAll(handler ChannelHandler) func() {
	return cm.Subscribe("*", handler)
}

// Publish sends a message through a channel. It checks the allowlist
// before delivery.
func (cm *ChannelManager) Publish(_ context.Context, msg ChannelMessage) error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Check allowlist.
	if !cm.isAllowed(msg.ServerName, msg.Channel, ChannelPermWrite) {
		return fmt.Errorf("server %q not allowed to write to channel %q", msg.ServerName, msg.Channel)
	}

	if msg.Level == "" {
		msg.Level = "info"
	}

	// Buffer the message.
	cm.bufferMessage(msg)

	// Deliver to channel-specific handlers.
	for _, h := range cm.handlers[msg.Channel] {
		h(msg)
	}

	// Deliver to wildcard handlers.
	for _, h := range cm.handlers["*"] {
		h(msg)
	}

	return nil
}

// SetAllowlist configures the channel permissions for a server.
func (cm *ChannelManager) SetAllowlist(entry ChannelAllowlistEntry) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.allowlist[entry.ServerName] = &entry
}

// RemoveAllowlist removes channel permissions for a server.
func (cm *ChannelManager) RemoveAllowlist(serverName string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.allowlist, serverName)
}

// GetAllowlist returns the allowlist entry for a server (nil if not found).
func (cm *ChannelManager) GetAllowlist(serverName string) *ChannelAllowlistEntry {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	entry := cm.allowlist[serverName]
	if entry == nil {
		return nil
	}
	cp := *entry
	return &cp
}

// RecentMessages returns the most recent buffered messages, optionally
// filtered by channel.
func (cm *ChannelManager) RecentMessages(channel string, limit int) []ChannelMessage {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if limit <= 0 {
		limit = 20
	}

	var result []ChannelMessage
	for i := len(cm.messageBuffer) - 1; i >= 0 && len(result) < limit; i-- {
		msg := cm.messageBuffer[i]
		if channel == "" || channel == "*" || msg.Channel == channel {
			result = append(result, msg)
		}
	}

	// Reverse to chronological order.
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}

// HandleNotification processes an MCP server notification and routes it
// through the channel system.
func (cm *ChannelManager) HandleNotification(ctx context.Context, serverName string, method string, params json.RawMessage) {
	// Extract channel from notification method.
	channel := notificationMethodToChannel(method)
	if channel == "" {
		return
	}

	msg := ChannelMessage{
		Channel:    channel,
		ServerName: serverName,
		Data:       params,
		Level:      "info",
	}

	if err := cm.Publish(ctx, msg); err != nil {
		slog.Debug("channel notification dropped", "server", serverName, "channel", channel, "error", err)
	}
}

// isAllowed checks if a server has the given permission on a channel.
// If no allowlist entry exists, all channels are allowed (open by default).
func (cm *ChannelManager) isAllowed(serverName, channel string, perm ChannelPermission) bool {
	entry := cm.allowlist[serverName]
	if entry == nil {
		return true // no restrictions
	}

	// Check channel is in the allowed list.
	channelOK := false
	for _, c := range entry.Channels {
		if c == "*" || c == channel {
			channelOK = true
			break
		}
	}
	if !channelOK {
		return false
	}

	// Check permission.
	for _, p := range entry.Permissions {
		if p == perm || p == ChannelPermAdmin {
			return true
		}
	}
	return false
}

// bufferMessage adds a message to the ring buffer (caller must hold at least RLock).
func (cm *ChannelManager) bufferMessage(msg ChannelMessage) {
	cm.messageBuffer = append(cm.messageBuffer, msg)
	if len(cm.messageBuffer) > cm.bufferSize {
		cm.messageBuffer = cm.messageBuffer[len(cm.messageBuffer)-cm.bufferSize:]
	}
}

// notificationMethodToChannel maps MCP notification methods to channel names.
func notificationMethodToChannel(method string) string {
	switch method {
	case MethodToolsListChanged:
		return "tools_changed"
	case MethodResourcesListChanged:
		return "resources_changed"
	default:
		// Generic notifications: use method as channel name.
		if len(method) > 0 {
			return method
		}
		return ""
	}
}
