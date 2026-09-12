package games

import "fmt"

/*
 * LaunchURI builds the steam:// URI that starts a game.
 *
 * Pure, and the only place a launch URI is ever constructed. The rule from ADR
 * 0066 is that the page names a game and never names a URI: it hands over an
 * appid, the client re-scans to confirm that appid is installed, and then this
 * function turns it into a URI. Nothing from the page reaches the shell.
 *
 * The digits check is the second half of that. rungameid takes a number, so
 * anything else is either a bug or an attempt to smuggle something into the
 * URI, and both deserve the same refusal.
 */
func LaunchURI(appid string) (string, error) {
	if !isDigits(appid) {
		return "", fmt.Errorf("not a Steam app id: %q", appid)
	}
	return "steam://rungameid/" + appid, nil
}
