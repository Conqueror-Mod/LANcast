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
	/*
	 * OpenAtLogin is deliberately absent.
	 *
	 * It used to live here and was **written and never read**: the run key is
	 * what actually starts anything at login, the settings page reads that, and
	 * this field only ever recorded what somebody last asked for through this
	 * one program. The tray writes the run key too and never touched this file,
	 * so the two drifted — found on a real install as `open_at_login: true`
	 * beside no run-key entry at all.
	 *
	 * A second copy of a fact, kept by one of its two owners, can only ever go
	 * out of date. The registry is the fact.
	 */
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

	/*
	 * Window is where the window was when it last closed: which screen, and
	 * where on it.
	 *
	 * Here rather than in server settings for the reason the package comment
	 * gives, and more obviously than anything else in this file. "Which of my
	 * three monitors" is a fact about a desk, not about an account — putting it
	 * in /api/settings would move one household member's window onto another
	 * person's screen, and mean nothing at all to a phone in the kitchen.
	 *
	 * Stored as an opaque record rather than interpreted here: the rules about
	 * unplugged monitors and windows larger than their new screen belong to the
	 * window, and this package's job is to remember, not to decide.
	 */
	Window *WindowPlacement `json:"window,omitempty"`
}

/*
 * WindowPlacement is the last position of the desktop window.
 *
 * A copy of clientwindow.Placement rather than the type itself, because
 * desktopprefs is the file format and clientwindow is the behaviour: a
 * preferences package that imported the window package would make the JSON
 * shape hostage to a Win32 refactor, and this file is written to disk and read
 * by later versions.
 *
 * The position is relative to its monitor's work area, which is what lets a
 * rearranged desk still put the window where it was *on that screen*.
 */
type WindowPlacement struct {
	Monitor   string `json:"monitor,omitempty"`
	X         int    `json:"x"`
	Y         int    `json:"y"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Maximized bool   `json:"maximized,omitempty"`
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
