package games

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

/*
 * LaunchURI builds the URI that starts a game.
 *
 * The rule from ADR 0066 is that the page names a game and never names a URI:
 * it hands over an id, the client re-scans to confirm that id is installed, and
 * only then does this turn it into a URI. Nothing from the page reaches the
 * shell. With three launchers the rule is unchanged — what grew is the number
 * of shapes the far side can take, and each is built here and nowhere else.
 *
 * Every branch validates before it formats. A launcher's id space is the only
 * thing that makes the validation possible, which is why the id carries its
 * source: without it there is no way to know which alphabet is legal.
 */
func LaunchURI(id string) (string, error) {
	source, own, ok := SplitID(id)
	if !ok {
		return "", fmt.Errorf("not a game id: %q", id)
	}

	switch source {
	case SourceSteam:
		/*
		 * rungameid takes a number, so anything else is either a bug or an
		 * attempt to smuggle something into the URI, and both deserve the same
		 * refusal.
		 */
		if !isDigits(own) {
			return "", fmt.Errorf("not a Steam app id: %q", own)
		}
		return "steam://rungameid/" + own, nil

	case SourceEpic:
		/*
		 * Epic's AppName is a 32-character hex catalogue id. Checked rather
		 * than trusted: it is the only part of this URI that did not come from
		 * a constant, and it is followed by query parameters — a value carrying
		 * `&` or `#` would rewrite what the launcher is being asked to do.
		 *
		 * `silent=true` suppresses the launcher window, which is the behaviour
		 * somebody pressing Play on a television expects; without it the game
		 * starts behind Epic's own UI.
		 */
		if !isHex(own) {
			return "", fmt.Errorf("not an Epic app name: %q", own)
		}
		return "com.epicgames.launcher://apps/" + own + "?action=launch&silent=true", nil

	case SourceBattleNet:
		/*
		 * Battle.net is the one that cannot be launched by URI here.
		 *
		 * Its protocol handler is registered — `battlenet://` reaches
		 * Battle.net.exe — but the code in the URI is not the identifier
		 * anything on disk records: Hearthstone is `hs_beta` in `product.db`
		 * and `WTCG` in a URI, and the mapping is undocumented and per-game. A
		 * table of guesses would fail silently for anything not in it, and fail
		 * *differently* as Blizzard renames things.
		 *
		 * So a Blizzard game is started from its own directory instead — see
		 * BattleNetLaunchTarget, which re-derives an executable from the
		 * install path rather than building a URI at all.
		 */
		return "", fmt.Errorf("a Battle.net game is launched from its folder, not a URI")

	case SourceXbox:
		/*
		 * A packaged game is started by its application user model id, through
		 * the shell's apps folder — the same thing the Start menu does. Not a
		 * URL: there is no protocol for it, and the caller hands this to
		 * explorer.exe rather than to the URL handler (XboxShellTarget).
		 *
		 * The id is validated again here, not trusted because it was valid
		 * when read: this is the last step before a shell, and the check is
		 * cheap.
		 */
		if !validAUMID.MatchString(own) {
			return "", fmt.Errorf("not an Xbox app id: %q", own)
		}
		return `shell:AppsFolder\` + own, nil

	case SourceGOG, SourceEA:
		/*
		 * Started by a file, like Battle.net: GOG through Galaxy or its play
		 * task, EA by the executable its own shortcut runs. See
		 * ExecutableTarget and GalaxyArgs.
		 */
		return "", fmt.Errorf("a %s game is launched from its folder, not a URI", source.Label())
	}
	return "", fmt.Errorf("unknown launcher for %q", id)
}

/*
 * ExecutableTarget is the file to run for a game started by an executable,
 * checked the way every path from data is checked before it reaches a process
 * (ADR 0066): an absolute .exe that exists, inside the game's install folder.
 * The registry and the info file that named it can be written by any
 * installer, and this is the last step before a process starts.
 */
func ExecutableTarget(g Game) (string, error) {
	if g.Executable == "" {
		return "", fmt.Errorf("%s: no executable recorded for %s", g.Source.Label(), g.Name)
	}
	root, err := filepath.Abs(g.InstallPath)
	if err != nil || g.InstallPath == "" {
		return "", fmt.Errorf("%s: install path %q", g.Source.Label(), g.InstallPath)
	}
	exe, err := filepath.Abs(g.Executable)
	if err != nil {
		return "", fmt.Errorf("%s: executable %q", g.Source.Label(), g.Executable)
	}
	rel, err := filepath.Rel(root, exe)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%s: %q is outside %q", g.Source.Label(), exe, root)
	}
	if !strings.EqualFold(filepath.Ext(exe), ".exe") {
		return "", fmt.Errorf("%s: %q is not an executable", g.Source.Label(), exe)
	}
	if st, err := os.Stat(exe); err != nil || st.IsDir() {
		return "", fmt.Errorf("%s: %q is not there", g.Source.Label(), exe)
	}
	return exe, nil
}

// isHex reports whether s is a non-empty run of hexadecimal digits, which is
// the shape of an Epic AppName.
func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}
