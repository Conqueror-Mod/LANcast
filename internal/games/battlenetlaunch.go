package games

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

/*
 * Starting a Blizzard game.
 *
 * The other two launchers take a URI. Battle.net cannot, and the reason is
 * worth stating rather than leaving as an inconsistency: the code its protocol
 * expects is not the identifier anything on disk records. Hearthstone is
 * `hs_beta` in `product.db`, `Hearthstone` in the uninstall registry, and
 * `WTCG` in a `battlenet://` URI. That last mapping is undocumented, per-game,
 * and changes when Blizzard renames a product — a table of it would fail
 * silently for every game not in it, which is the worst shape this could take:
 * Play does nothing, and nothing says why.
 *
 * What *is* on disk is the game's own launcher, beside the game, named by the
 * installer. That is what the Start Menu shortcut runs, so it is also what the
 * person would have run themselves.
 */

/*
 * BattleNetLaunchTarget finds the executable to start for an installed game.
 *
 * The containment rule from ADR 0066 applies exactly as it does everywhere else
 * a path from data becomes a path on disk: the result must be inside the install
 * directory. The install path came from the registry, which any installer can
 * write to, and this function's output is handed to the process launcher.
 *
 * Preference order, and each step is a real case:
 *
 *  1. a `* Launcher.exe` — what Blizzard's own Start Menu shortcut runs, and
 *     what performs the game's update check before starting it
 *  2. an executable named after the game's directory — the common layout
 *  3. exactly one executable in the directory, if there is only one
 *
 * Anything else returns an error rather than guessing. A directory with four
 * executables and no obvious launcher is one where picking wrong starts an
 * uninstaller, and "I could not tell which" is a better answer than that.
 */
func BattleNetLaunchTarget(installPath string) (string, error) {
	root, err := filepath.Abs(installPath)
	if err != nil {
		return "", fmt.Errorf("battle.net: install path: %w", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", fmt.Errorf("battle.net: read install directory: %w", err)
	}

	var exes []string
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".exe") {
			continue
		}
		exes = append(exes, e.Name())
	}
	if len(exes) == 0 {
		return "", fmt.Errorf("battle.net: no executable in %s", root)
	}

	pick := ""
	for _, name := range exes {
		if strings.HasSuffix(strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name))), " launcher") {
			pick = name
			break
		}
	}
	if pick == "" {
		want := strings.ToLower(filepath.Base(root)) + ".exe"
		for _, name := range exes {
			if strings.EqualFold(name, want) {
				pick = name
				break
			}
		}
	}
	if pick == "" && len(exes) == 1 {
		pick = exes[0]
	}
	if pick == "" {
		return "", fmt.Errorf("battle.net: could not tell which of %d executables in %s starts the game",
			len(exes), root)
	}

	target := filepath.Join(root, pick)
	if !withinDir(root, target) {
		// Unreachable via a directory listing, and checked anyway: this is the
		// boundary where a name becomes something the machine runs.
		return "", fmt.Errorf("battle.net: %s escapes its install directory", pick)
	}
	return target, nil
}

// withinDir reports whether target is inside root, comparing cleaned absolute
// paths. Case-insensitively, because this is Windows and `C:\Games` and
// `c:\games` are one directory.
func withinDir(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return !filepath.IsAbs(rel)
}
