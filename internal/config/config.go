// Package config resolves where LANcast keeps its state and what it listens on.
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Config is the fully resolved runtime configuration.
type Config struct {
	Addr    string // listen address, e.g. ":8080"
	DataDir string // directory holding lancast.db
}

// DefaultDataDir returns the per-user data directory. It creates nothing.
func DefaultDataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(base, "LANcast"), nil
}

// Resolve fills in defaults and ensures the data directory exists.
func Resolve(addr, dataDir string) (Config, error) {
	if dataDir == "" {
		d, err := DefaultDataDir()
		if err != nil {
			return Config{}, err
		}
		dataDir = d
	}
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return Config{}, fmt.Errorf("resolve data dir %q: %w", dataDir, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return Config{}, fmt.Errorf("create data dir %q: %w", abs, err)
	}
	return Config{Addr: addr, DataDir: abs}, nil
}

// DBPath is the location of the SQLite database file.
func (c Config) DBPath() string {
	return filepath.Join(c.DataDir, "lancast.db")
}
