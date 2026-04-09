package util

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Remote managed settings — aligned with claude-code-main's remote settings
// sync for enterprise deployments.
//
// Fetches configuration from a remote endpoint and merges with local config.
// Remote settings take precedence for enterprise-managed keys, while user
// preferences are preserved for non-managed keys.

// RemoteSettingsConfig configures the remote settings fetcher.
type RemoteSettingsConfig struct {
	// URL is the remote settings endpoint.
	URL string `json:"url"`
	// AuthToken is the bearer token for authentication.
	AuthToken string `json:"auth_token,omitempty"`
	// RefreshInterval is how often to re-fetch (default 5 minutes).
	RefreshInterval time.Duration `json:"refresh_interval,omitempty"`
	// Timeout is the HTTP request timeout (default 10 seconds).
	Timeout time.Duration `json:"timeout,omitempty"`
	// TenantID is the enterprise tenant identifier.
	TenantID string `json:"tenant_id,omitempty"`
}

// RemoteSettings holds the fetched remote configuration.
type RemoteSettings struct {
	// Values is the key-value settings map.
	Values map[string]interface{} `json:"values"`
	// ManagedKeys lists keys that are enterprise-managed (cannot be overridden locally).
	ManagedKeys []string `json:"managed_keys,omitempty"`
	// FetchedAt is when the settings were last fetched.
	FetchedAt time.Time `json:"fetched_at"`
	// Version is the remote settings version (for change detection).
	Version string `json:"version,omitempty"`
}

// RemoteSettingsManager fetches and caches remote settings.
type RemoteSettingsManager struct {
	mu         sync.RWMutex
	config     RemoteSettingsConfig
	httpClient *http.Client
	current    *RemoteSettings
	stopCh     chan struct{}
	running    bool
}

// NewRemoteSettingsManager creates a new remote settings manager.
func NewRemoteSettingsManager(config RemoteSettingsConfig) *RemoteSettingsManager {
	if config.RefreshInterval == 0 {
		config.RefreshInterval = 5 * time.Minute
	}
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}

	return &RemoteSettingsManager{
		config:     config,
		httpClient: &http.Client{Timeout: config.Timeout},
		stopCh:     make(chan struct{}),
	}
}

// Fetch retrieves the latest settings from the remote endpoint.
func (m *RemoteSettingsManager) Fetch(ctx context.Context) (*RemoteSettings, error) {
	if m.config.URL == "" {
		return nil, fmt.Errorf("remote settings: URL not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.config.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "open-claudecode-go/settings")
	if m.config.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+m.config.AuthToken)
	}
	if m.config.TenantID != "" {
		req.Header.Set("X-Tenant-ID", m.config.TenantID)
	}

	// Send current version for conditional fetch.
	m.mu.RLock()
	if m.current != nil && m.current.Version != "" {
		req.Header.Set("If-None-Match", m.current.Version)
	}
	m.mu.RUnlock()

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch remote settings: %w", err)
	}
	defer resp.Body.Close()

	// 304 Not Modified — use cached.
	if resp.StatusCode == http.StatusNotModified {
		m.mu.RLock()
		cached := m.current
		m.mu.RUnlock()
		return cached, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("remote settings returned %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var settings RemoteSettings
	if err := json.Unmarshal(body, &settings); err != nil {
		return nil, fmt.Errorf("parse remote settings: %w", err)
	}

	settings.FetchedAt = time.Now()
	if etag := resp.Header.Get("ETag"); etag != "" {
		settings.Version = etag
	}

	// Cache the result.
	m.mu.Lock()
	m.current = &settings
	m.mu.Unlock()

	slog.Debug("remote settings fetched",
		"keys", len(settings.Values),
		"managed", len(settings.ManagedKeys),
		"version", settings.Version)

	return &settings, nil
}

// Current returns the most recently fetched settings (nil if never fetched).
func (m *RemoteSettingsManager) Current() *RemoteSettings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// Get returns a single setting value, checking remote first, then falling back.
func (m *RemoteSettingsManager) Get(key string) (interface{}, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.current == nil {
		return nil, false
	}
	val, ok := m.current.Values[key]
	return val, ok
}

// IsManaged reports whether a key is enterprise-managed (cannot be overridden locally).
func (m *RemoteSettingsManager) IsManaged(key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.current == nil {
		return false
	}
	for _, k := range m.current.ManagedKeys {
		if k == key {
			return true
		}
	}
	return false
}

// MergeWith merges remote settings into a local settings map.
// Managed keys from remote override local values.
// Non-managed keys use local values if present, remote otherwise.
func (m *RemoteSettingsManager) MergeWith(local map[string]interface{}) map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]interface{})

	// Start with local values.
	for k, v := range local {
		result[k] = v
	}

	if m.current == nil {
		return result
	}

	// Build managed key set.
	managed := make(map[string]bool, len(m.current.ManagedKeys))
	for _, k := range m.current.ManagedKeys {
		managed[k] = true
	}

	// Apply remote values.
	for k, v := range m.current.Values {
		if managed[k] {
			// Managed keys always override local.
			result[k] = v
		} else if _, exists := result[k]; !exists {
			// Non-managed keys only fill in missing local values.
			result[k] = v
		}
	}

	return result
}

// StartPeriodicFetch begins background periodic fetching.
func (m *RemoteSettingsManager) StartPeriodicFetch(ctx context.Context) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()

	go func() {
		// Initial fetch.
		if _, err := m.Fetch(ctx); err != nil {
			slog.Warn("initial remote settings fetch failed", "error", err)
		}

		ticker := time.NewTicker(m.config.RefreshInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if _, err := m.Fetch(ctx); err != nil {
					slog.Debug("periodic remote settings fetch failed", "error", err)
				}
			case <-m.stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop halts periodic fetching.
func (m *RemoteSettingsManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		close(m.stopCh)
		m.running = false
	}
}
