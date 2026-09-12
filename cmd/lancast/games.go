package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"lancast/internal/childproc"
	"lancast/internal/desktop"
	"lancast/internal/desktopprefs"
	"lancast/internal/games"
)

/*
 * The games bindings (ADR 0066).
 *
 * They live in the client rather than behind an API because a game is installed
 * on *this* machine: the server cannot see it, cannot stream it, and — running
 * as a service in session 0, with no desktop — could not put a game window on
 * anybody's screen if it tried.
 *
 * Three rules run through everything below.
 *
 * The setting is checked in the process, not in the page. A page that called
 * these with games switched off is refused here, because "the page will not
 * ask" is not a boundary.
 *
 * The page names a game, never a URI and never a path. It sends an app id; the
 * client re-scans, finds that id among what is *actually installed*, and builds
 * the URI or the path itself. The worst a compromised page can do is start a
 * game somebody already has.
 *
 * Artwork goes out as bytes, never as a location on disk.
 */

// maxArtBytes caps one image. Steam's library art is a couple of hundred
// kilobytes; anything far larger is not a poster, and a data URI has to be held
// in memory as a string on both sides of the binding.
const maxArtBytes = 4 << 20

// gamesBindings are the window functions behind the Games tab.
func gamesBindings(dir string) map[string]any {
	return map[string]any{
		/*
		 * lancastGames lists what Steam has installed here.
		 *
		 * Artwork is deliberately *not* included. A library of two hundred
		 * games would be twenty megabytes of base64 through a single binding
		 * call, most of it for tiles nobody scrolled to; the page asks for one
		 * image at a time with lancastGameArt instead, and the flags below tell
		 * it which games have one to ask for.
		 */
		"lancastGames": func() map[string]any {
			if !gamesEnabled(dir) {
				return map[string]any{"status": "disabled"}
			}
			res := games.Scan()
			prefs, prefsErr := games.LoadPrefs(dir)

			list := make([]map[string]any, 0, len(res.Games))
			for _, g := range res.Games {
				list = append(list, map[string]any{
					"id":           g.ID,
					"name":         g.Name,
					"size_bytes":   g.SizeBytes,
					"last_played":  g.LastPlayed,
					"install_path": g.InstallPath,
					"has_poster":   g.PosterPath != "",
					"has_header":   g.HeaderPath != "",
					"hidden":       prefs.IsHidden(g.ID),
					"favourite":    prefs.IsFavourite(g.ID),
					// Empty means nobody has been asked which display this one
					// should open on, which is what raises the picker. A game
					// answered with "wherever it opens" carries the default
					// sentinel instead, and is never asked again.
					"display": prefs.DisplayFor(g.ID),
				})
			}
			out := map[string]any{"status": string(res.Status), "games": list}
			if res.Err != "" {
				out["error"] = res.Err
			} else if prefsErr != nil {
				// Worth saying rather than swallowing: the games are right and
				// the hidden ones are about to reappear.
				out["error"] = prefsErr.Error()
			}
			return out
		},

		// lancastGameArt returns one cached image as a data URI. kind is
		// "poster" or "header"; anything else is refused rather than guessed,
		// since a guess here would be a third way to name a file.
		"lancastGameArt": func(id, kind string) map[string]any {
			if !gamesEnabled(dir) {
				return map[string]any{"ok": false, "error": "games are switched off"}
			}
			g, ok := installedGame(id)
			if !ok {
				return map[string]any{"ok": false, "error": "that game is not installed"}
			}
			var path string
			switch kind {
			case "poster":
				path = g.PosterPath
			case "header":
				path = g.HeaderPath
			default:
				return map[string]any{"ok": false, "error": "unknown image"}
			}
			if path == "" {
				// Ordinary: Steam caches artwork lazily, so a game installed and
				// never looked at has none. The page draws a placeholder.
				return map[string]any{"ok": true, "uri": ""}
			}
			st, err := os.Stat(path)
			if err != nil || st.Size() > maxArtBytes {
				return map[string]any{"ok": true, "uri": ""}
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return map[string]any{"ok": true, "uri": ""}
			}
			return map[string]any{
				"ok":  true,
				"uri": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(raw),
			}
		},

		// lancastLaunchGame starts a game by app id.
		"lancastLaunchGame": func(id string) map[string]any {
			if !gamesEnabled(dir) {
				return map[string]any{"ok": false, "error": "games are switched off"}
			}
			// The rescan is the check. Whatever the page believes it is looking
			// at, only an app id that is installed on this disk right now gets
			// as far as a URI.
			g, ok := installedGame(id)
			if !ok {
				return map[string]any{"ok": false, "error": "that game is not installed"}
			}
			uri, err := games.LaunchURI(id)
			if err != nil {
				return map[string]any{"ok": false, "error": err.Error()}
			}
			if err := desktop.OpenBrowser(uri); err != nil {
				return map[string]any{"ok": false, "error": err.Error()}
			}
			/*
			 * Started only once the URI is away, and read here rather than
			 * taken from the page.
			 *
			 * Here, because the watcher's first act is to write down every
			 * window that already exists — anything it finds after that is a
			 * candidate. Starting it before the launch would widen that gap for
			 * no gain; starting it on a launch that failed would leave it
			 * hunting for ninety seconds and possibly moving somebody's
			 * unrelated window.
			 */
			if prefs, err := games.LoadPrefs(dir); err == nil {
				if device := prefs.DisplayFor(id); device != "" && device != games.DisplayDefault {
					moveGameToDisplay(device, g.Name)
				}
			}
			return map[string]any{"ok": true}
		},

		// lancastOpenGameFolder reveals a game's install directory.
		"lancastOpenGameFolder": func(id string) map[string]any {
			if !gamesEnabled(dir) {
				return map[string]any{"ok": false, "error": "games are switched off"}
			}
			g, ok := installedGame(id)
			if !ok {
				return map[string]any{"ok": false, "error": "that game is not installed"}
			}
			// The path came from the scan, which already refused any installdir
			// escaping its library folder. The page never supplied it.
			if err := openFolder(g.InstallPath); err != nil {
				return map[string]any{"ok": false, "error": err.Error()}
			}
			return map[string]any{"ok": true}
		},

		/*
		 * lancastSetGameFlags records hidden and favourite for one game.
		 *
		 * Both flags every time, like lancastDesktopSet: the page sends what the
		 * game's state should now be, so there is one way to write this file and
		 * nothing can half-apply a change.
		 *
		 * No rescan here, deliberately. Hiding is a note about a game rather
		 * than an act on the machine, and a game uninstalled today may be back
		 * next week — dropping the note because the game is currently absent
		 * would quietly unhide it.
		 */
		"lancastSetGameFlags": func(id string, hidden, favourite bool) map[string]any {
			if !gamesEnabled(dir) {
				return map[string]any{"ok": false, "error": "games are switched off"}
			}
			if id == "" {
				return map[string]any{"ok": false, "error": "no game named"}
			}
			prefs, err := games.LoadPrefs(dir)
			if err != nil {
				// Defaults came back with the error. Writing over an unreadable
				// file is the recovery: the alternative is a list nobody can
				// ever change again.
				prefs = games.Prefs{}
			}
			prefs.Set(id, hidden, favourite)
			if err := games.SavePrefs(dir, prefs); err != nil {
				return map[string]any{"ok": false, "error": err.Error()}
			}
			return map[string]any{"ok": true}
		},

		// lancastDisplays lists the screens a game can be sent to, with the one
		// this window is on marked — "not the one I am reading this on" is the
		// usual answer, and a list of device names would not let anybody say it.
		"lancastDisplays": func() map[string]any {
			if !gamesEnabled(dir) {
				return map[string]any{"ok": false, "error": "games are switched off"}
			}
			return map[string]any{"ok": true, "displays": availableDisplays()}
		},

		/*
		 * lancastSetGameDisplay records which screen a game should open on.
		 *
		 * The device name is checked against the screens actually attached
		 * rather than stored as given. It is the same rule as everywhere else
		 * here — the page names a thing, the client decides whether that thing
		 * exists — and it also keeps the file honest: an unplugged monitor's
		 * name would sit in games.json for ever, quietly meaning nothing.
		 *
		 * Two values that are not devices are allowed. The default sentinel is
		 * a real answer, "leave this one wherever it opens", and it stops the
		 * picker coming back. An empty string forgets the answer entirely,
		 * which is how the detail page asks to be asked again.
		 */
		"lancastSetGameDisplay": func(id, device string) map[string]any {
			if !gamesEnabled(dir) {
				return map[string]any{"ok": false, "error": "games are switched off"}
			}
			if id == "" {
				return map[string]any{"ok": false, "error": "no game named"}
			}
			if device != "" && device != games.DisplayDefault {
				known := false
				for _, d := range availableDisplays() {
					if d.Device == device {
						known = true
						break
					}
				}
				if !known {
					return map[string]any{"ok": false, "error": "no such display is attached"}
				}
			}
			prefs, err := games.LoadPrefs(dir)
			if err != nil {
				prefs = games.Prefs{}
			}
			prefs.SetDisplay(id, device)
			if err := games.SavePrefs(dir, prefs); err != nil {
				return map[string]any{"ok": false, "error": err.Error()}
			}
			return map[string]any{"ok": true}
		},
	}
}

// gamesEnabled reports whether the games setting is on, read fresh each call so
// that switching it off takes effect on the next call rather than at the next
// launch. Unreadable preferences mean off: this fails closed, because the cost
// of being wrong in the other direction is listing somebody's games against
// their setting.
func gamesEnabled(dir string) bool {
	prefs, err := desktopprefs.Load(dir)
	if err != nil {
		return false
	}
	return prefs.Games
}

// installedGame finds one installed game by app id.
//
// A whole scan for one game, every time, and that is the point rather than an
// oversight: it is the freshness check that makes an id from the page safe to
// act on. Reading a few dozen small files is cheap next to launching a game.
func installedGame(id string) (games.Game, bool) {
	res := games.Scan()
	if res.Status != games.StatusOK {
		return games.Game{}, false
	}
	for _, g := range res.Games {
		if g.ID == id {
			return g, true
		}
	}
	return games.Game{}, false
}

// openFolder shows a directory in the system file manager.
//
// Explorer by name on Windows rather than through the URL handler: this is a
// path, not a URI, and turning a Windows path into a file:// URL correctly is a
// second escaping problem for no gain.
func openFolder(path string) error {
	if path == "" {
		return fmt.Errorf("that game has no install folder")
	}
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		// An install folder the scan believed in and the disk does not: a drive
		// unplugged since, most likely.
		return fmt.Errorf("that folder is not there any more")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	childproc.Hide(cmd)
	// Explorer exits non-zero even when it opens the window, so this is
	// fire-and-forget like OpenBrowser: Start reports "could not run it at all",
	// which is the only failure worth showing.
	return cmd.Start()
}
