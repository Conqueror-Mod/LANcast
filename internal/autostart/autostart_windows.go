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

func enabled(t Target) (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, fmt.Errorf("autostart: %w", err)
	}
	defer k.Close()

	v, _, err := k.GetStringValue(t.value)
	if err == registry.ErrNotExist {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("autostart: %w", err)
	}
	/*
	 * The value has to name *this* target's executable.
	 *
	 * A non-empty value used to be the whole test, which was true while there
	 * was one value for one program. It is not true across the change that gave
	 * each target its own name: an install upgraded mid-life can still carry a
	 * `LANcast` entry the tray wrote, pointing at the server, and reporting that
	 * as "the client starts at login" is exactly the confusion being removed.
	 *
	 * Asked as "is this the other program's entry?" rather than "is it exactly
	 * mine?": the path legitimately varies — a moved install, a build run from
	 * a terminal, a test binary — and demanding an exact executable would
	 * report a perfectly good entry as absent.
	 */
	if strings.TrimSpace(v) == "" {
		return false, nil
	}
	return !strings.Contains(strings.ToLower(v), strings.ToLower(t.foreign)), nil
}

func enable(t Target, args ...string) error {
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

	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("autostart: %w", err)
	}
	defer k.Close()

	if err := k.SetStringValue(t.value, cmd); err != nil {
		return fmt.Errorf("autostart: %w", err)
	}

	/*
	 * Clear a legacy shared entry that meant this target.
	 *
	 * Before each target owned a name, both wrote `LANcast`. The tray writing
	 * its own name now would leave that old entry behind, and two entries
	 * launching the same program at login is a worse bug than the one being
	 * fixed. Only removed when it names this target's executable: an entry
	 * naming the *other* program belongs to the other target and is not this
	 * one's to delete.
	 */
	if t.value != Client.value {
		if v, _, err := k.GetStringValue(Client.value); err == nil &&
			strings.Contains(strings.ToLower(v), strings.ToLower(t.own)) {
			_ = k.DeleteValue(Client.value)
		}
	}
	return nil
}

func disable(t Target) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil
		}
		return fmt.Errorf("autostart: %w", err)
	}
	defer k.Close()

	if err := k.DeleteValue(t.value); err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("autostart: %w", err)
	}
	// And the legacy shared entry, when it named this target. Leaving it would
	// have a switch turned off still starting the program at login, which is
	// the failure people report as the setting not working.
	if t.value != Client.value {
		if v, _, err := k.GetStringValue(Client.value); err == nil &&
			strings.Contains(strings.ToLower(v), strings.ToLower(t.own)) {
			_ = k.DeleteValue(Client.value)
		}
	}
	return nil
}
