package games

import (
	"path/filepath"
	"sort"
	"strings"
)

/*
 * Blizzard's installed games, read from what the installer already registered.
 *
 * Battle.net's own `product.db` is the obvious source and is the wrong one: it
 * is an unschema'd protobuf, so reading it means either a generated schema
 * nobody publishes or byte-scraping a format Blizzard can change without
 * telling anyone. The Windows uninstall registry carries the same facts —
 * display name, install location — in a format that is documented, stable, and
 * already how every other program on the machine finds them.
 *
 * Still a local read: no sign-in, no key, no network (ADR 0066).
 *
 * **No artwork**, and here that is a decision rather than a limitation.
 * Battle.net caches images, but only as content-addressed blobs in a browser
 * cache with nothing tying one to a game — 280 of them on the machine this was
 * built against, none attributable. A game's executable icon *is* available and
 * was deliberately not used: it is a 256px square beside a 2:3 poster, and a
 * grid of mismatched squares is worse than the lettered placeholder Steam games
 * already fall back to.
 */

/*
 * InstalledProgram is one uninstall-registry entry, in the only shape this
 * package cares about.
 *
 * A plain struct so the rules below are pure: the Windows layer reads the
 * registry and hands over a slice, and every decision about what counts as a
 * game is tested against fixtures on any operating system.
 */
type InstalledProgram struct {
	// Key is the registry subkey name, which is what the entry is identified by
	// — stable across reinstalls and not translated, unlike the display name.
	Key string
	// Name is the DisplayName shown in Windows' own list.
	Name string
	// InstallLocation is the directory, which may be empty: the registry does
	// not require it and some installers omit it.
	InstallLocation string
	Publisher       string
	// EstimatedSize is the registry's own figure, in kilobytes. Approximate by
	// construction — it is what the installer claimed, not what is on disk.
	EstimatedSizeKB int64
}

/*
 * blizzardLaunchers are entries that are Blizzard's and are not games.
 *
 * Battle.net itself is the one that matters, and it is exactly the shape of
 * mistake Steam's redistributables deny list exists for: it is published by
 * Blizzard Entertainment, it has an install location, and by every other rule
 * here it is a game. Offering it would put a tile on the grid whose Play button
 * opens the launcher.
 *
 * Matched on the registry key rather than the display name, because the name is
 * localised and the key is not.
 */
var blizzardLaunchers = map[string]bool{
	"Battle.net": true,
	"Agent":      true,
}

/*
 * BlizzardGames picks the games out of a machine's installed programs.
 *
 * Pure, so the rules are a table test. The publisher check is the whole filter
 * — Blizzard's installers all write it, and matching on it rather than on a
 * list of known titles means a game released tomorrow is found without a code
 * change.
 */
func BlizzardGames(programs []InstalledProgram) []Game {
	var games []Game
	for _, p := range programs {
		if !strings.EqualFold(strings.TrimSpace(p.Publisher), "Blizzard Entertainment") {
			continue
		}
		if blizzardLaunchers[p.Key] {
			continue
		}
		/*
		 * An entry with no install location is not something that can be
		 * launched or measured, so it is not offered. Blizzard writes one for
		 * every game; an entry without one is a leftover from an uninstall that
		 * did not finish, which is a real state and a confusing tile.
		 */
		if strings.TrimSpace(p.InstallLocation) == "" {
			continue
		}
		name := strings.TrimSpace(p.Name)
		if name == "" {
			name = p.Key
		}
		games = append(games, Game{
			ID:          BattleNetID(p.Key),
			Source:      SourceBattleNet,
			Name:        name,
			InstallPath: filepath.Clean(p.InstallLocation),
			// Kilobytes in the registry, bytes everywhere here. Approximate,
			// and marked as such rather than silently presented as measured.
			SizeBytes: p.EstimatedSizeKB * 1024,
			// Battle.net records no last-played time on disk.
			LastPlayed: 0,
		})
	}
	sort.SliceStable(games, func(i, j int) bool {
		return strings.ToLower(games[i].Name) < strings.ToLower(games[j].Name)
	})
	return games
}
