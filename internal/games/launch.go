package games

import "fmt"

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
	}
	return "", fmt.Errorf("unknown launcher for %q", id)
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
