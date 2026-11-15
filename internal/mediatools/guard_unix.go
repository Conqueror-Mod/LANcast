//go:build unix

package mediatools

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// ServiceGuard is the guard a service applies to its tools: owner and mode
// bits, for a process running as its own effective user.
func ServiceGuard() Guard { return NewModeGuard(systemStatter{}, uint32(os.Geteuid())) }

type systemStatter struct{}

func (systemStatter) Stat(path string) (FileMode, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return FileMode{}, nil
	}
	if err != nil {
		return FileMode{}, fmt.Errorf("read %s: %w", path, err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return FileMode{}, fmt.Errorf("read the owner of %s", path)
	}
	return FileMode{Exists: true, UID: st.Uid, Mode: info.Mode()}, nil
}

func (systemStatter) Resolve(path string) (string, error) { return filepath.EvalSymlinks(path) }

// Protect creates dir if needed, readable by all and writable by its owner
// only, and refuses a folder somebody else owns.
func Protect(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		return fmt.Errorf("protect %s: %w", dir, err)
	}
	return nil
}

// ownByAdministrators has nothing to do on Unix: a copy belongs to whoever
// made it, root at install, which the guard accepts.
func ownByAdministrators(string) error { return nil }
