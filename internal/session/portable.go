package session

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Portable session storage — aligned with claude-code-main's session
// export/import capabilities.
//
// Provides a cross-platform JSON format for exporting and importing
// sessions, enabling migration between machines and environments.

// PortableSession is the cross-platform session export format.
type PortableSession struct {
	// FormatVersion identifies this portable format version.
	FormatVersion string `json:"format_version"`
	// ExportedAt is when the export was created.
	ExportedAt time.Time `json:"exported_at"`
	// Metadata is the session metadata.
	Metadata PortableMetadata `json:"metadata"`
	// Entries is the full session transcript.
	Entries []PortableEntry `json:"entries"`
	// Environment captures the original runtime environment.
	Environment *PortableEnvironment `json:"environment,omitempty"`
}

const portableFormatVersion = "1.0"

// PortableMetadata is the session metadata in portable format.
type PortableMetadata struct {
	ID        string    `json:"id"`
	Summary   string    `json:"summary"`
	WorkDir   string    `json:"work_dir"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	TurnCount int       `json:"turn_count"`
	Model     string    `json:"model,omitempty"`
}

// PortableEntry is a single transcript entry in portable format.
type PortableEntry struct {
	Type      string          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// PortableEnvironment captures the runtime environment for context.
type PortableEnvironment struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Hostname  string `json:"hostname,omitempty"`
	GoVer     string `json:"go_version,omitempty"`
	GitBranch string `json:"git_branch,omitempty"`
}

// ExportSession exports a session to portable JSON format.
func (s *Storage) ExportSession(sessionID string) (*PortableSession, error) {
	meta, err := s.LoadMeta(sessionID)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	entries, err := s.ReadTranscript(sessionID)
	if err != nil {
		return nil, fmt.Errorf("read transcript: %w", err)
	}

	portable := &PortableSession{
		FormatVersion: portableFormatVersion,
		ExportedAt:    time.Now(),
		Metadata: PortableMetadata{
			ID:        meta.ID,
			Summary:   meta.Summary,
			WorkDir:   meta.WorkDir,
			CreatedAt: meta.CreatedAt,
			UpdatedAt: meta.UpdatedAt,
			TurnCount: meta.TurnCount,
		},
	}

	for _, e := range entries {
		payloadJSON, err := json.Marshal(e.Payload)
		if err != nil {
			continue
		}
		portable.Entries = append(portable.Entries, PortableEntry{
			Type:      string(e.Type),
			Timestamp: e.Timestamp,
			Payload:   payloadJSON,
		})
	}

	return portable, nil
}

// ExportSessionToFile exports a session to a JSON file.
func (s *Storage) ExportSessionToFile(sessionID, filePath string) error {
	portable, err := s.ExportSession(sessionID)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(portable, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal portable session: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// ImportSession imports a session from portable JSON format.
// Returns the new session ID.
func (s *Storage) ImportSession(portable *PortableSession) (string, error) {
	if portable == nil {
		return "", fmt.Errorf("nil portable session")
	}

	// Create a new session with the imported metadata.
	sessionID := portable.Metadata.ID
	if sessionID == "" {
		sessionID = fmt.Sprintf("imported-%d", time.Now().UnixNano())
	}

	meta := &SessionMetadata{
		ID:        sessionID,
		Summary:   portable.Metadata.Summary,
		WorkDir:   portable.Metadata.WorkDir,
		CreatedAt: portable.Metadata.CreatedAt,
		UpdatedAt: portable.Metadata.UpdatedAt,
		TurnCount: portable.Metadata.TurnCount,
	}

	if err := s.SaveMeta(meta); err != nil {
		return "", fmt.Errorf("save session metadata: %w", err)
	}

	// Write transcript entries.
	for _, pe := range portable.Entries {
		entry := &TranscriptEntry{
			Type:      TranscriptEntryType(pe.Type),
			Timestamp: pe.Timestamp,
		}

		// Unmarshal payload back to interface{}.
		if len(pe.Payload) > 0 {
			var payload interface{}
			if err := json.Unmarshal(pe.Payload, &payload); err == nil {
				entry.Payload = payload
			}
		}

		if err := s.AppendEntry(sessionID, entry); err != nil {
			return sessionID, fmt.Errorf("append transcript entry: %w", err)
		}
	}

	return sessionID, nil
}

// ImportSessionFromFile imports a session from a JSON file.
func (s *Storage) ImportSessionFromFile(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	var portable PortableSession
	if err := json.Unmarshal(data, &portable); err != nil {
		return "", fmt.Errorf("parse portable session: %w", err)
	}

	return s.ImportSession(&portable)
}
