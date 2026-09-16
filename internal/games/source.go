package games

import "strings"

/*
 * Which launcher a game came from, and how an id says so.
 *
 * Steam was the only reader when ADR 0066 landed, so an id was a bare Steam
 * appid. With three readers that cannot hold: hidden and favourite flags are
 * stored per id, and Battle.net keys its entries on a display name — nothing
 * stops one of those colliding with a Steam appid, and a collision here hides
 * the wrong game with no error anywhere.
 *
 * So an id carries its source: `steam:440`, `epic:a26f991a…`,
 * `battlenet:Hearthstone`. One string, still opaque to the page, still the only
 * thing the page ever sends back.
 */

// Source names a launcher.
type Source string

const (
	SourceSteam     Source = "steam"
	SourceEpic      Source = "epic"
	SourceBattleNet Source = "battlenet"
)

// Label is what a person reads when a tile says where a game came from.
func (s Source) Label() string {
	switch s {
	case SourceSteam:
		return "Steam"
	case SourceEpic:
		return "Epic Games"
	case SourceBattleNet:
		return "Battle.net"
	}
	return string(s)
}

// SteamID, EpicID and BattleNetID build a namespaced id. Three functions rather
// than one taking a Source so that a caller cannot pass the wrong constant for
// the reader it is in — the compiler cannot catch that, and a test would only
// catch it if somebody thought to write one.
func SteamID(appid string) string   { return string(SourceSteam) + ":" + appid }
func EpicID(appName string) string  { return string(SourceEpic) + ":" + appName }
func BattleNetID(key string) string { return string(SourceBattleNet) + ":" + key }

/*
 * SplitID returns the source and the launcher's own id.
 *
 * An id with no prefix is a **Steam appid written before ids were namespaced**.
 * Reading it as Steam is what keeps somebody's hidden and favourite games from
 * silently resetting on upgrade: those flags live in a per-client JSON file
 * that nothing migrates, and an unrecognised id is indistinguishable from a
 * game that has been uninstalled.
 */
func SplitID(id string) (Source, string, bool) {
	source, rest, found := strings.Cut(id, ":")
	if !found {
		if id == "" {
			return "", "", false
		}
		return SourceSteam, id, true
	}
	switch Source(source) {
	case SourceSteam, SourceEpic, SourceBattleNet:
		if rest == "" {
			return "", "", false
		}
		return Source(source), rest, true
	}
	return "", "", false
}
