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
	ID          string `json:"id"`
	Name        string `json:"name"`
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
	root, ok := steamRoot()
	if !ok {
		return Result{Status: StatusNotInstalled}
	}
	return ScanRoot(root)
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
