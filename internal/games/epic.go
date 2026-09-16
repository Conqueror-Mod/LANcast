package games

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

/*
 * Epic's installed games, read from its own manifests.
 *
 * One JSON file per installation under
 * `C:\ProgramData\Epic\EpicGamesLauncher\Data\Manifests`, which is the same
 * shape of local read Steam gets: no sign-in, no key, no network. Epic's
 * account API answers *what you own*, which is a different question from what
 * is on this disk.
 *
 * Easier to read than Steam's, and the parsing is pure JSON rather than a
 * hand-written VDF parser — so the only OS-specific part is knowing where the
 * directory is.
 *
 * **No artwork.** Epic keeps none on disk that can be tied to a game: the
 * launcher's cache holds content-addressed blobs with nothing naming what they
 * are. A tile falls back to the lettered placeholder Steam games already use
 * when Steam has cached nothing. That was the reason this reader was deferred
 * in ADR 0066, and it is a decision rather than a gap — an icon dug out of an
 * executable is a 256px square beside a 2:3 poster, and a wrong picture is
 * worse than an honest letter.
 */

// epicManifest is the part of an `.item` file this reads.
//
// Named fields rather than a map, and only the ones used: a manifest carries
// 52 keys, most of them about patching, and a reader that took the whole
// object would invite somebody to start depending on one of them.
type epicManifest struct {
	DisplayName      string   `json:"DisplayName"`
	AppName          string   `json:"AppName"`
	InstallLocation  string   `json:"InstallLocation"`
	LaunchExecutable string   `json:"LaunchExecutable"`
	InstallSize      int64    `json:"InstallSize"`
	AppCategories    []string `json:"AppCategories"`
	// MainGameAppName is the base game's AppName. On a base game it equals
	// AppName; on downloadable content it names the parent, which is how an
	// add-on is told apart from the game it belongs to.
	MainGameAppName string `json:"MainGameAppName"`
	// IsIncompleteInstall is Epic's own "this is not finished" flag — a
	// download that was interrupted or paused. The equivalent of the Steam
	// state bit, and the reason a half-downloaded game is not offered.
	IsIncompleteInstall bool `json:"bIsIncompleteInstall"`
}

/*
 * ParseEpicManifest reads one manifest and says whether it describes a game
 * that is installed and playable.
 *
 * Pure and byte-taking, so every rule below is a table test against a fixture
 * rather than something that needs Epic on the machine.
 */
func ParseEpicManifest(raw []byte) (Game, bool) {
	var m epicManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Game{}, false
	}
	if m.AppName == "" || m.DisplayName == "" || m.InstallLocation == "" {
		return Game{}, false
	}
	/*
	 * Three rules, each excluding something a real library contains.
	 *
	 * `games` in the categories keeps out the engines, plugins and asset packs
	 * that share this directory — an Unreal Engine install is a manifest here
	 * exactly like a game, and nobody wants to launch it from a television.
	 *
	 * A manifest whose MainGameAppName names something else is downloadable
	 * content. It has its own install location and its own size, and listing it
	 * beside the game would offer a tile that launches the same thing twice.
	 *
	 * And an incomplete install is a download, not a game.
	 */
	if !hasCategory(m.AppCategories, "games") {
		return Game{}, false
	}
	if m.MainGameAppName != "" && m.MainGameAppName != m.AppName {
		return Game{}, false
	}
	if m.IsIncompleteInstall {
		return Game{}, false
	}

	/*
	 * Epic writes the install location with mixed separators — `D:\Epic
	 * Library/NeonAbyss` is verbatim from a real manifest, backslash then
	 * forward slash in one path. Cleaning it is not tidiness: the launch
	 * executable is joined onto it, and a path that half the code agrees on is
	 * the kind of thing that works until somebody compares two of them.
	 */
	install := filepath.Clean(strings.ReplaceAll(m.InstallLocation, "/", string(filepath.Separator)))

	return Game{
		ID:          EpicID(m.AppName),
		Source:      SourceEpic,
		Name:        m.DisplayName,
		SizeBytes:   m.InstallSize,
		InstallPath: install,
		// Epic records no last-played time on disk. Zero means "never" to the
		// grid, which sorts such games last under "last played" — the honest
		// answer rather than inventing a date from a file's mtime.
		LastPlayed: 0,
	}, true
}

// hasCategory reports whether a manifest carries one of Epic's categories.
func hasCategory(cats []string, want string) bool {
	for _, c := range cats {
		if strings.EqualFold(strings.TrimSpace(c), want) {
			return true
		}
	}
	return false
}

/*
 * ScanEpicManifests reads every manifest in a directory.
 *
 * Directory-taking for the same reason ScanRoot is: the rules above are
 * exercised against a fixture tree rather than against whatever happens to be
 * installed on the machine running the tests.
 *
 * A manifest that cannot be parsed is skipped rather than failing the scan.
 * These are files another program writes and rewrites — one being mid-write, or
 * from a newer launcher, is an ordinary condition, and one bad file must not
 * empty the grid.
 */
func ScanEpicManifests(dir string) (Result, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Status: StatusNotInstalled}, nil
		}
		return Result{}, fmt.Errorf("epic: read manifests: %w", err)
	}

	var games []Game
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".item") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if g, ok := ParseEpicManifest(raw); ok {
			games = append(games, g)
		}
	}
	return Result{Status: StatusOK, Games: games}, nil
}
