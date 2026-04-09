// Package voice provides the framework for voice-based interaction.
//
// Aligned with claude-code-main voice/ — enables voice input/output
// for the agent using speech-to-text and text-to-speech services.
package voice

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// ────────────────────────────────────────────────────────────────────────────
// Voice framework — basic structure for future voice integration
// ────────────────────────────────────────────────────────────────────────────

// ProviderType identifies the voice service provider.
type ProviderType string

const (
	ProviderWhisper ProviderType = "whisper"    // OpenAI Whisper STT
	ProviderNative  ProviderType = "native"     // OS native TTS/STT
	ProviderNone    ProviderType = "none"       // disabled
)

// Config configures voice capabilities.
type Config struct {
	Provider   ProviderType `json:"provider"`
	Language   string       `json:"language,omitempty"`
	APIKey     string       `json:"api_key,omitempty"`
	BaseURL    string       `json:"base_url,omitempty"`
	TTSEnabled bool         `json:"tts_enabled,omitempty"`
	STTEnabled bool         `json:"stt_enabled,omitempty"`
}

// STTResult is the result of a speech-to-text transcription.
type STTResult struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
	Language   string  `json:"language,omitempty"`
	DurationMs int64   `json:"duration_ms"`
}

// TTSRequest describes text to be spoken.
type TTSRequest struct {
	Text  string `json:"text"`
	Voice string `json:"voice,omitempty"`
	Speed float64 `json:"speed,omitempty"`
}

// STTProvider transcribes audio to text.
type STTProvider interface {
	Transcribe(ctx context.Context, audioData []byte, format string) (*STTResult, error)
	Close() error
}

// TTSProvider converts text to speech audio.
type TTSProvider interface {
	Synthesize(ctx context.Context, req TTSRequest) ([]byte, error)
	Close() error
}

// Manager coordinates voice input/output.
type Manager struct {
	mu     sync.Mutex
	cfg    Config
	stt    STTProvider
	tts    TTSProvider
	active bool
}

// NewManager creates a voice manager with the given configuration.
func NewManager(cfg Config) *Manager {
	return &Manager{cfg: cfg}
}

// Start initializes voice providers.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cfg.Provider == ProviderNone {
		slog.Info("voice: disabled")
		return nil
	}

	slog.Info("voice: initializing",
		slog.String("provider", string(m.cfg.Provider)),
		slog.Bool("stt", m.cfg.STTEnabled),
		slog.Bool("tts", m.cfg.TTSEnabled))

	// TODO: Initialize STT/TTS providers based on config.
	// This is a placeholder framework for future implementation.

	m.active = true
	return nil
}

// Transcribe converts audio data to text.
func (m *Manager) Transcribe(ctx context.Context, audioData []byte, format string) (*STTResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.active || m.stt == nil {
		return nil, fmt.Errorf("voice: STT not available")
	}
	return m.stt.Transcribe(ctx, audioData, format)
}

// Speak converts text to audio.
func (m *Manager) Speak(ctx context.Context, text string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.active || m.tts == nil {
		return nil, fmt.Errorf("voice: TTS not available")
	}
	return m.tts.Synthesize(ctx, TTSRequest{Text: text})
}

// IsActive reports whether voice is enabled and running.
func (m *Manager) IsActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active
}

// Close shuts down voice providers.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.active = false
	var firstErr error
	if m.stt != nil {
		if err := m.stt.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if m.tts != nil {
		if err := m.tts.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
