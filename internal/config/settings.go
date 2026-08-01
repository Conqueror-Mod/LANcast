package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Settings is the user-editable runtime configuration.
//
// The TMDB key lives here rather than in the database because it is a secret:
// the file is written 0600, and the API never echoes the value back.
type Settings struct {
	TMDBKey string `json:"tmdb_key,omitempty"`
	// OpenSubtitlesKey enables subtitle search. Optional, like the TMDB key:
	// without it LANcast still serves embedded and sidecar subtitles.
	OpenSubtitlesKey string `json:"opensubtitles_key,omitempty"`
	// OMDbKey enables external ratings (Rotten Tomatoes / Metacritic / IMDb via
	// OMDb, ADR 0019). Optional and write-only like the other keys; without it
	// the rating pass never runs and nothing leaves the machine.
	OMDbKey    string  `json:"omdb_key,omitempty"`
	RatePerSec float64 `json:"rate_per_sec,omitempty"`
	WriteNFO   bool    `json:"write_nfo"`
	AutoEnrich bool    `json:"auto_enrich"`

	// HardwareEncoder is "auto", "off", or a specific ffmpeg encoder name.
	// Auto takes the fastest encoder that passed a real test encode.
	HardwareEncoder string `json:"hardware_encoder,omitempty"`

	// PasswordHash is the bcrypt hash guarding the instance. Empty means the
	// server is unconfigured and will bind to loopback only.
	PasswordHash string `json:"password_hash,omitempty"`

	// TLSCertFile and TLSKeyFile point at a PEM certificate and private key the
	// operator supplies (bring-your-own-cert). When both are set, LANcast serves
	// HTTPS with them. When empty and the server binds beyond loopback, a
	// self-signed certificate is generated and persisted instead (ADR 0014).
	TLSCertFile string `json:"tls_cert_file,omitempty"`
	TLSKeyFile  string `json:"tls_key_file,omitempty"`
}

// CustomTLS reports whether the operator supplied their own certificate and key.
func (s Settings) CustomTLS() bool { return s.TLSCertFile != "" && s.TLSKeyFile != "" }

// Secured reports whether a password has been set.
func (s Settings) Secured() bool { return s.PasswordHash != "" }

// Defaults returns the settings a fresh install starts with.
//
// AutoEnrich is on because metadata arriving by itself is the expected
// behavior; it is a no-op without a key. WriteNFO is off because writing into
// someone's media folders is not something to do unasked.
func Defaults() Settings {
	return Settings{RatePerSec: 5, WriteNFO: false, AutoEnrich: true, HardwareEncoder: "auto"}
}

// SettingsStore reads and writes the settings file.
type SettingsStore struct {
	path string
	mu   sync.RWMutex
	cur  Settings
}

// LoadSettings opens (or initializes) the settings file in dir.
func LoadSettings(dir string) (*SettingsStore, error) {
	s := &SettingsStore{
		path: filepath.Join(dir, "config.json"),
		cur:  Defaults(),
	}

	raw, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return s, nil // defaults; nothing written until something is set
	case err != nil:
		return nil, fmt.Errorf("read settings: %w", err)
	}

	loaded := Defaults()
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return nil, fmt.Errorf("parse settings %s: %w", s.path, err)
	}
	if loaded.RatePerSec <= 0 {
		loaded.RatePerSec = Defaults().RatePerSec
	}
	s.cur = loaded
	return s, nil
}

// Get returns a copy of the current settings.
func (s *SettingsStore) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

// Set replaces the settings and persists them with 0600 permissions.
func (s *SettingsStore) Set(next Settings) error {
	if next.RatePerSec <= 0 {
		next.RatePerSec = Defaults().RatePerSec
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	body, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}

	// Write via a temp file so a crash never leaves a truncated config, and
	// create it 0600 from the outset rather than chmod-ing after the secret is
	// already on disk.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replace settings: %w", err)
	}

	s.cur = next
	return nil
}

// Path is the settings file location, for diagnostics.
func (s *SettingsStore) Path() string { return s.path }
