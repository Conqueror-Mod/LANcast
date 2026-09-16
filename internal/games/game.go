// Package games reads what the Steam client has installed on *this* machine.
//
// It exists in the desktop client rather than the server, and that is the whole
// decision (ADR 0066): a game is installed on one PC, cannot be streamed, and
// cannot be launched from the phone in the kitchen. The server holds no games
// table and serves no games endpoint, so nothing here has a route.
//
// Everything is a local file read. There is no sign-in, no API key and no
// network: Steam's own sign-in answers *which games you own*, which is a
// different question from the one this package asks, and answering it would
// mean a credential and a phone-home for a fact that is already on the disk.
package games

import (
	"sort"
	"strings"
)

// Status says why a list is empty, because "Steam is not installed here" and
// "Steam is installed and has nothing in it" are different sentences and an
// empty grid says neither. A reader that cannot tell them apart makes a missing
// Steam look like a broken LANcast.
type Status string

const (
	// StatusOK means Steam was found and read. The list may still be empty.
	StatusOK Status = "ok"
	// StatusNotInstalled means no Steam installation was found on this machine.
	StatusNotInstalled Status = "not-installed"
	// StatusError means Steam was found and could not be read.
	StatusError Status = "error"
)

// Game is one installed Steam title.
//
// ID is the Steam appid as its decimal string. It stays a string all the way to
// the page and back: it is an identifier rather than a quantity, nothing here
// does arithmetic on it, and JSON numbers through a web view binding are a
// float64 round-trip this does not need.
type Game struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Source is which launcher this came from. Carried so the page can say so
	// and so a launch can be routed back to the right one — an id alone is not
	// enough once more than one launcher is read.
	Source      Source `json:"source"`
	SizeBytes   int64  `json:"size_bytes"`
	LastPlayed  int64  `json:"last_played,omitempty"` // Unix seconds, 0 when never
	InstallPath string `json:"install_path"`
	// PosterPath and HeaderPath are Steam's own cached artwork on this disk,
	// empty when it has not cached any. They are paths *here*, in the client
	// process; the page is sent image bytes and never a path (ADR 0066).
	PosterPath string `json:"-"`
	HeaderPath string `json:"-"`
}

// Result is one scan: what was found and whether finding it worked.
type Result struct {
	Status Status `json:"status"`
	Games  []Game `json:"games"`
	// Err is a message for the page when Status is StatusError. A string rather
	// than an error because this crosses the binding into JavaScript.
	Err string `json:"error,omitempty"`
}

// Scan reads this machine's Steam installation.
//
// Locating Steam is the only part that is OS-specific; everything after it is
// ScanRoot, which is a directory away from being testable.
func Scan() Result {
	return merge(scanSteam(), scanEpic(), scanBattleNet())
}

// scanSteam is the Steam half of Scan, unchanged by the arrival of the others.
func scanSteam() Result {
	root, ok := steamRoot()
	if !ok {
		return Result{Status: StatusNotInstalled}
	}
	return ScanRoot(root)
}

func scanEpic() Result {
	dir, ok := epicManifestDir()
	if !ok {
		return Result{Status: StatusNotInstalled}
	}
	res, err := ScanEpicManifests(dir)
	if err != nil {
		return Result{Status: StatusError, Err: err.Error()}
	}
	return res
}

func scanBattleNet() Result {
	programs := installedPrograms()
	games := BlizzardGames(programs)
	/*
	 * No Blizzard entries at all reads as "not installed" rather than as an
	 * empty list, and the distinction is the same one Status exists for: a
	 * machine with no Battle.net and a Battle.net with nothing in it are
	 * different sentences.
	 *
	 * Unlike Steam and Epic there is no directory to look for — the uninstall
	 * registry is always there — so the absence of entries is the only evidence
	 * available.
	 */
	if len(games) == 0 {
		return Result{Status: StatusNotInstalled}
	}
	return Result{Status: StatusOK, Games: games}
}

/*
 * merge combines the readers into one answer.
 *
 * The status rules are the whole of it, and they matter more than the
 * concatenation: a person with Steam and no Epic must not be told anything is
 * wrong, and a person with none of the three must not be shown an empty grid
 * that looks like a broken LANcast.
 *
 *   any reader succeeded        -> ok, with everything that was found
 *   none succeeded, one errored -> error, naming what went wrong
 *   none succeeded, none errored-> not-installed
 *
 * An error from one reader while another worked is deliberately *not* surfaced:
 * the grid has games in it, and a banner about a launcher the person may not
 * even use would be noise. It is in the client log either way.
 */
func merge(results ...Result) Result {
	out := Result{Status: StatusNotInstalled}
	var firstErr string
	for _, r := range results {
		switch r.Status {
		case StatusOK:
			out.Status = StatusOK
			out.Games = append(out.Games, r.Games...)
		case StatusError:
			if firstErr == "" {
				firstErr = r.Err
			}
		}
	}
	if out.Status != StatusOK && firstErr != "" {
		return Result{Status: StatusError, Err: firstErr}
	}
	if out.Status != StatusOK {
		return out
	}
	// One order for the merged list, by name then id, so two launchers cannot
	// make the grid order depend on which reader ran first.
	sort.SliceStable(out.Games, func(i, j int) bool {
		a, b := strings.ToLower(out.Games[i].Name), strings.ToLower(out.Games[j].Name)
		if a != b {
			return a < b
		}
		return out.Games[i].ID < out.Games[j].ID
	})
	return out
}

// ScanRoot reads a Steam installation rooted at dir.
//
// Exported and directory-taking so the rules above it are exercised against a
// fixture tree rather than against whatever happens to be installed on the
// machine running the tests.
func ScanRoot(dir string) Result {
	if dir == "" {
		return Result{Status: StatusNotInstalled}
	}
	games, err := scanLibraries(dir)
	if err != nil {
		return Result{Status: StatusError, Err: err.Error()}
	}
	// Sorted by name here rather than in the page, so that the order is the same
	// in the grid, in a test, and in anything that reads this next. The page
	// re-sorts when the viewer asks for last-played or size.
	sort.SliceStable(games, func(i, j int) bool {
		a, b := strings.ToLower(games[i].Name), strings.ToLower(games[j].Name)
		if a != b {
			return a < b
		}
		return games[i].ID < games[j].ID
	})
	return Result{Status: StatusOK, Games: games}
}
