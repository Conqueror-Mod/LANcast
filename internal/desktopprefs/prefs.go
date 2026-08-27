// Package desktopprefs holds the desktop client's own lifecycle preferences.
//
// These are deliberately not server settings. GET/PUT /api/settings is
// machine-wide and shared — TMDB keys, ffmpeg, NFO writing — and applies to the
// server everyone on the LAN talks to. "Close to tray" is a property of one
// person's desktop on one machine. Putting it in server settings would mean one
// household member's window preference silently changing another's, and a phone
// in the kitchen carrying a setting about a tray it does not have
// (docs/desktop-lifecycle-plan.md).
//
// So: per user, per machine, a small JSON file beside the window's own profile.
// The server never learns about any of it.
package desktopprefs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// FileName is the preferences file inside the client's directory.
const FileName = "desktop.json"

// Prefs are the desktop lifecycle options.
//
// Both default to off, and that is a decision rather than an accident: a ✕ that
// does not close and an app that starts itself at login are both things a person
// should opt into. Surprising background behaviour is a bug here even when it is
// convenient.
type Prefs struct {
	// CloseToTray hides the window instead of quitting when it is closed.
	CloseToTray bool `json:"close_to_tray"`
	// OpenAtLogin starts the client when the user signs in to Windows.
	OpenAtLogin bool `json:"open_at_login"`
	/*
	 * DevTools opens the web view's inspector with the window.
	 *
	 * Off by default like the others, and for a sharper reason than surprise:
	 * an always-on inspector in a media player is a support liability, and an
	 * unreachable one is why client faults in this project have been diagnosed
	 * by inference — reading the server log and deducing what the page must
	 * have done. Both of those are worse than a switch somebody has to find.
	 *
	 * It takes effect at the next launch, because the browser arguments are
	 * read when the web view environment is created and there is no supported
	 * way to add one to a running environment. The UI says so rather than
	 * appearing not to work.
	 */
	DevTools bool `json:"devtools"`
}

// Load reads preferences from dir.
//
// A missing file is not an error — it is the first run, and the answer is the
// defaults. A malformed file is not an error either: it is recovered from by
// returning defaults, because refusing to open the app over an unreadable
// preference would be trading the whole product for one tickbox. The caller is
// told, so the failure has a voice, but it is not fatal.
func Load(dir string) (Prefs, error) {
	if dir == "" {
		return Prefs{}, nil
	}
	raw, err := os.ReadFile(filepath.Join(dir, FileName))
	if os.IsNotExist(err) {
		return Prefs{}, nil
	}
	if err != nil {
		return Prefs{}, fmt.Errorf("desktop preferences: %w", err)
	}
	var p Prefs
	if err := json.Unmarshal(raw, &p); err != nil {
		return Prefs{}, fmt.Errorf("desktop preferences: %s is not readable: %w", FileName, err)
	}
	return p, nil
}

// Save writes preferences to dir, creating it if needed.
//
// Written whole and replaced atomically: a half-written preferences file that
// then fails to parse would silently reset both options, and the user would
// have no way to tell that from the app ignoring them.
func Save(dir string, p Prefs) error {
	if dir == "" {
		return fmt.Errorf("desktop preferences: no directory to write to")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("desktop preferences: %w", err)
	}
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("desktop preferences: %w", err)
	}
	raw = append(raw, '\n')

	final := filepath.Join(dir, FileName)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("desktop preferences: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("desktop preferences: %w", err)
	}
	return nil
}
