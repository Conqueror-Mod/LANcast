//go:build windows

package autostart

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// runKey is the per-user run key. HKCU, never HKLM: this is one person's
// preference on one machine, it needs no elevation, and writing the machine-wide
// key would start LANcast for every account on the computer — including ones
// that have never heard of it.
const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func enabled() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, fmt.Errorf("autostart: %w", err)
	}
	defer k.Close()

	v, _, err := k.GetStringValue(Name)
	if err == registry.ErrNotExist {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("autostart: %w", err)
	}
	return strings.TrimSpace(v) != "", nil
}

func enable(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("autostart: %w", err)
	}

	// Quoted, always. A path with a space in it — "C:\Program Files\LANcast\…",
	// which is the default install location — is parsed as a command plus
	// arguments without quotes, so the unquoted form silently launches the wrong
	// thing or nothing at all.
	cmd := `"` + exe + `"`
	for _, a := range args {
		cmd += " " + a
	}

	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("autostart: %w", err)
	}
	defer k.Close()

	if err := k.SetStringValue(Name, cmd); err != nil {
		return fmt.Errorf("autostart: %w", err)
	}
	return nil
}

func disable() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil
		}
		return fmt.Errorf("autostart: %w", err)
	}
	defer k.Close()

	if err := k.DeleteValue(Name); err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("autostart: %w", err)
	}
	return nil
}
