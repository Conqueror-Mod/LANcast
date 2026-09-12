package games

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

/*
 * Steam's two on-disk facts.
 *
 * steamapps/libraryfolders.vdf names every folder Steam keeps games in — the
 * one it was installed into and every drive added since. Inside each,
 * steamapps/appmanifest_<appid>.acf is one game.
 *
 * Neither is a published contract. They are the Steam client's own files and
 * Valve may change them; that is why the parsing below is pure and fixture
 * tested, and why a failure to read is reported as StatusError rather than as
 * an empty library.
 */

// stateInstalled is StateFlags bit 4, "fully installed".
//
// This is the single most important check in the package. A game that is
// queued, downloading, or half-updated has a perfectly ordinary manifest with a
// name and a size, and listing it would offer a Play button for something that
// cannot start.
const stateInstalled = 4

/*
 * notGames are appids Steam installs beside games and lists exactly like them.
 *
 * Keyed on id and never on name, for the same reason the MCU collection is
 * keyed on a TMDB keyword id: ids are stable, names are marketing. A rule
 * matching "Steamworks Common Redistributables" stops matching the day it is
 * renamed, and a filter that fails by matching nothing never looks broken — the
 * junk simply comes back.
 *
 * Deliberately short. This is a list of things that are *not games at all*, not
 * a taste filter: hiding a demo or a tool somebody does want is what the
 * per-client hide list is for.
 */
var notGames = map[string]string{
	"228980":  "Steamworks Common Redistributables",
	"1070560": "Steam Linux Runtime 1.0",
	"1391110": "Steam Linux Runtime 2.0",
	"1628350": "Steam Linux Runtime 3.0",
	"1493710": "Proton Experimental",
}

// ParseLibraryFolders returns the library folder paths in a libraryfolders.vdf.
//
// Two shapes, because Steam changed it and old installs keep the old file:
// current Steam writes a block per folder with a "path" inside it, older Steam
// wrote the path as the value itself. Both are accepted; anything that is
// neither is skipped rather than failing the scan, since one unreadable entry
// should not cost the other drives.
func ParseLibraryFolders(r io.Reader) ([]string, error) {
	root, err := parseVDF(r)
	if err != nil {
		return nil, err
	}
	folders := root.sub("libraryfolders")
	if folders == nil {
		// Some installs write the list at the top level.
		folders = root
	}

	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		p = filepath.Clean(p)
		if key := strings.ToLower(p); !seen[key] {
			seen[key] = true
			out = append(out, p)
		}
	}

	folders.eachSub(func(_ string, sub *node) { add(sub.val("path")) })
	for key, v := range folders.vals {
		// The old shape: "0" "D:\\SteamLibrary". Numeric keys only, so that
		// "contentstatsid" and friends are not mistaken for paths.
		if _, err := strconv.Atoi(key); err == nil {
			add(v)
		}
	}
	return out, nil
}

// ParseAppManifest reads one appmanifest_<appid>.acf.
//
// The bool is "this is an installed game worth listing": false covers a
// half-downloaded title, a tool from notGames, and a manifest with no appid or
// no name. An error is returned only when the file is not KeyValues at all.
//
// library is the folder the manifest was found in, and is what the install path
// is resolved against and contained within.
func ParseAppManifest(r io.Reader, library string) (Game, bool, error) {
	root, err := parseVDF(r)
	if err != nil {
		return Game{}, false, err
	}
	app := root.sub("appstate")
	if app == nil {
		return Game{}, false, fmt.Errorf("%w: no AppState block", errVDF)
	}

	id := strings.TrimSpace(app.val("appid"))
	name := strings.TrimSpace(app.val("name"))
	if id == "" || name == "" || !isDigits(id) {
		return Game{}, false, nil
	}
	if _, junk := notGames[id]; junk {
		return Game{}, false, nil
	}

	flags, err := strconv.ParseInt(strings.TrimSpace(app.val("stateflags")), 10, 64)
	if err != nil || flags&stateInstalled == 0 {
		return Game{}, false, nil
	}

	installDir := strings.TrimSpace(app.val("installdir"))
	if installDir == "" {
		return Game{}, false, nil
	}
	path, ok := installPath(library, installDir)
	if !ok {
		// A manifest naming a directory outside its own library folder. The
		// database-row-to-filesystem-path rule, applied to a file Steam wrote:
		// trusted source, still checked, because this path is later handed to
		// Explorer.
		return Game{}, false, nil
	}

	g := Game{
		ID:          id,
		Name:        name,
		SizeBytes:   parseInt(app.val("sizeondisk")),
		LastPlayed:  parseInt(app.val("lastplayed")),
		InstallPath: path,
	}
	return g, true, nil
}

// installPath resolves a manifest's installdir inside its library folder, and
// refuses anything that escapes it. An installdir of "..\\..\\Windows" is a
// path traversal with a Play button on it.
func installPath(library, installDir string) (string, bool) {
	common := filepath.Clean(filepath.Join(library, "steamapps", "common"))
	full := filepath.Clean(filepath.Join(common, installDir))
	if !contained(common, full) {
		return "", false
	}
	return full, true
}

// contained reports whether path is base or lies beneath it, comparing
// case-insensitively because this is Windows and "D:\\Games" and "d:\\games"
// are the same folder.
func contained(base, path string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if strings.HasPrefix(rel, "..") {
		return false
	}
	return !filepath.IsAbs(rel)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// parseInt is "a number or nothing". Steam writes 0, an empty string, and
// occasionally a value too large for the field; none of those is worth failing
// a scan over, because a wrong size is a cosmetic fault and a refused library
// is not.
func parseInt(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// scanLibraries reads every manifest in every library folder under root.
func scanLibraries(root string) ([]Game, error) {
	libs, err := libraryFolders(root)
	if err != nil {
		return nil, err
	}

	var out []Game
	seen := map[string]bool{}
	for _, lib := range libs {
		matches, err := filepath.Glob(filepath.Join(lib, "steamapps", "appmanifest_*.acf"))
		if err != nil {
			continue
		}
		for _, m := range matches {
			f, err := os.Open(m)
			if err != nil {
				// One unreadable manifest is not a broken library: a game being
				// written to while the scan runs is ordinary.
				continue
			}
			g, ok, err := ParseAppManifest(f, lib)
			f.Close()
			if err != nil || !ok {
				continue
			}
			// A game moved between drives can leave a manifest behind in both.
			// First one wins, and libraryfolders order puts Steam's own folder
			// first.
			if seen[g.ID] {
				continue
			}
			seen[g.ID] = true
			g.PosterPath, g.HeaderPath = artworkPaths(root, g.ID)
			out = append(out, g)
		}
	}
	return out, nil
}

// libraryFolders is every folder to look in, always including root itself: a
// fresh install has games before it has a libraryfolders.vdf worth reading, and
// a missing or unreadable list should cost the other drives rather than all of
// them.
func libraryFolders(root string) ([]string, error) {
	out := []string{filepath.Clean(root)}
	seen := map[string]bool{strings.ToLower(filepath.Clean(root)): true}

	f, err := os.Open(filepath.Join(root, "steamapps", "libraryfolders.vdf"))
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, fmt.Errorf("reading Steam's library list: %w", err)
	}
	defer f.Close()

	folders, err := ParseLibraryFolders(f)
	if err != nil {
		return nil, fmt.Errorf("reading Steam's library list: %w", err)
	}
	for _, p := range folders {
		if key := strings.ToLower(p); !seen[key] {
			seen[key] = true
			out = append(out, p)
		}
	}
	return out, nil
}

/*
 * artworkPaths finds Steam's own cached artwork for an appid.
 *
 * Two layouts, because Steam reorganised this cache and an install that
 * predates the change keeps the old one:
 *
 *   appcache/librarycache/<appid>/library_600x900.jpg   (current)
 *   appcache/librarycache/<appid>_library_600x900.jpg   (older)
 *
 * Empty when neither exists, which is ordinary: Steam caches artwork lazily, so
 * a game installed and never viewed in the library may have none. The page
 * draws a lettered placeholder rather than a broken image.
 *
 * No download happens here or anywhere else in this package. A missing poster
 * is a missing poster; fetching one would be the phone-home the whole design
 * avoids.
 */
func artworkPaths(root, appid string) (poster, header string) {
	cache := filepath.Join(root, "appcache", "librarycache")
	poster = firstExisting(
		filepath.Join(cache, appid, "library_600x900.jpg"),
		filepath.Join(cache, appid+"_library_600x900.jpg"),
	)
	header = firstExisting(
		filepath.Join(cache, appid, "header.jpg"),
		filepath.Join(cache, appid, "library_hero.jpg"),
		filepath.Join(cache, appid+"_header.jpg"),
		filepath.Join(cache, appid+"_library_hero.jpg"),
	)
	return poster, header
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Size() > 0 {
			return p
		}
	}
	return ""
}
