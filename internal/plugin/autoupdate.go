package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"golang.org/x/mod/semver"
)

// Plugin auto-update notification — aligned with claude-code-main's plugin
// update checking logic.
//
// On startup (or periodically), checks installed plugins against a registry
// to detect available updates. Notifies the user without auto-installing.

const (
	defaultRegistryURL  = "https://registry.npmjs.org"
	updateCheckInterval = 24 * time.Hour
	updateCheckTimeout  = 10 * time.Second
)

// UpdateInfo describes an available update for a plugin.
type UpdateInfo struct {
	PluginName     string `json:"plugin_name"`
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	Description    string `json:"description,omitempty"`
	ReleaseNotes   string `json:"release_notes,omitempty"`
	UpdateURL      string `json:"update_url,omitempty"`
}

// UpdateChecker checks for plugin updates.
type UpdateChecker struct {
	mu          sync.Mutex
	registryURL string
	httpClient  *http.Client
	lastCheck   time.Time
	cache       []UpdateInfo
}

// NewUpdateChecker creates a new plugin update checker.
func NewUpdateChecker() *UpdateChecker {
	return &UpdateChecker{
		registryURL: defaultRegistryURL,
		httpClient:  &http.Client{Timeout: updateCheckTimeout},
	}
}

// SetRegistryURL configures a custom registry URL.
func (uc *UpdateChecker) SetRegistryURL(url string) {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	uc.registryURL = url
}

// CheckUpdates checks all installed plugins for available updates.
// Returns only plugins with newer versions available.
func (uc *UpdateChecker) CheckUpdates(ctx context.Context, installed []PluginManifest) ([]UpdateInfo, error) {
	uc.mu.Lock()
	// Return cached results if recent.
	if time.Since(uc.lastCheck) < updateCheckInterval && len(uc.cache) > 0 {
		cached := uc.cache
		uc.mu.Unlock()
		return cached, nil
	}
	uc.mu.Unlock()

	var updates []UpdateInfo
	for _, p := range installed {
		if p.Version == "" || p.Name == "" {
			continue
		}

		latest, err := uc.fetchLatestVersion(ctx, p.Name)
		if err != nil {
			slog.Debug("plugin update check failed", "plugin", p.Name, "error", err)
			continue
		}

		if latest == nil || latest.Version == "" {
			continue
		}

		// Compare versions using semver.
		currentV := ensureSemverPrefix(p.Version)
		latestV := ensureSemverPrefix(latest.Version)

		if semver.IsValid(currentV) && semver.IsValid(latestV) {
			if semver.Compare(currentV, latestV) < 0 {
				updates = append(updates, UpdateInfo{
					PluginName:     p.Name,
					CurrentVersion: p.Version,
					LatestVersion:  latest.Version,
					Description:    latest.Description,
				})
			}
		}
	}

	// Cache results.
	uc.mu.Lock()
	uc.cache = updates
	uc.lastCheck = time.Now()
	uc.mu.Unlock()

	return updates, nil
}

// HasUpdates returns true if any updates are available (using cached data).
func (uc *UpdateChecker) HasUpdates() bool {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	return len(uc.cache) > 0
}

// CachedUpdates returns the most recently fetched update info.
func (uc *UpdateChecker) CachedUpdates() []UpdateInfo {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	cp := make([]UpdateInfo, len(uc.cache))
	copy(cp, uc.cache)
	return cp
}

// registryPackageInfo is a subset of the npm registry package response.
type registryPackageInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// fetchLatestVersion queries the registry for the latest version of a package.
func (uc *UpdateChecker) fetchLatestVersion(ctx context.Context, name string) (*registryPackageInfo, error) {
	url := fmt.Sprintf("%s/%s/latest", uc.registryURL, name)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := uc.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // package not in registry
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	var info registryPackageInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, err
	}

	return &info, nil
}

// ensureSemverPrefix adds "v" prefix if missing for semver comparison.
func ensureSemverPrefix(version string) string {
	if version == "" {
		return ""
	}
	if version[0] != 'v' {
		return "v" + version
	}
	return version
}
