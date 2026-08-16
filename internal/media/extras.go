package media

/*
 * Extras: the video files in a movie library that are not the movie.
 *
 * A trailer, a featurette, a deleted scene and a five-second `sample.mkv` are
 * all playable video sitting inside a film's own folder, and every one of them
 * was being imported as a *film* — with a title parsed from its filename, a
 * tile in the grid, and a line in the library count. A library reporting 1,381
 * films against a real 1,192 is what that looks like from outside.
 *
 * The conventions here are not invented: they are the layout Plex and Kodi both
 * document, which is what the tools that produced these folders were written
 * against. Guessing lives in this package (CLAUDE.md), and this is guessing —
 * so it is written to be wrong in the safe direction, which for an exclusion
 * rule means importing something questionable rather than discarding something
 * real.
 */

import (
	"path/filepath"
	"strings"
)

/*
 * extraDirs are the subfolder names that hold a film's extras.
 *
 * Compared after normalization, so "Behind The Scenes", "behind the scenes" and
 * "Behind.The.Scenes" are one name.
 */
var extraDirs = map[string]bool{
	"behindthescenes": true,
	"deletedscenes":   true,
	"featurettes":     true,
	"interviews":      true,
	"scenes":          true,
	"shorts":          true,
	"trailers":        true,
	"extras":          true,
	"other":           true,
	// Not "specials": in a shows library that is season zero, which is real
	// content with real episodes, and discarding it would lose a Christmas
	// special somebody went looking for.
}

/*
 * extraSuffixes are the filename markers for an extra that sits beside the film
 * rather than in a folder of its own — `The Film-trailer.mkv`.
 */
var extraSuffixes = []string{
	"-trailer", "-sample", "-featurette", "-deleted", "-behindthescenes",
	"-interview", "-scene", "-short", "-other",
}

/*
 * IsExtra reports whether a video file is a film's extra rather than a work.
 *
 * `root` is the library location the file was found under, and it matters for
 * one reason that is the whole subtlety of this function: **a folder named
 * "Shorts" or "Trailers" directly inside a library root is a category, not an
 * extras folder.** A library organised as `Movies/Shorts/…` is somebody's
 * collection of short films, and discarding it would be a far worse bug than
 * the one this fixes. So an extras folder must have a film folder above it —
 * at least two path segments below the root — which is exactly where the
 * convention puts it: `Movies/The Film (2011)/Trailers/…`.
 *
 * The filename rules carry no such condition, because `sample.mkv` is junk
 * wherever it is found.
 */
func IsExtra(root, path string) bool {
	if isExtraName(path) {
		return true
	}

	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil || strings.HasPrefix(rel, "..") {
		// Not under this root. Say no rather than guess: the caller has handed
		// us a pair that does not go together, and inventing an answer would
		// discard a file on the strength of a mismatch.
		return false
	}

	parts := strings.Split(filepath.ToSlash(rel), "/")
	// The last element is the file; anything before it is a directory. Start at
	// index 1 so a directory sitting immediately under the root is never an
	// extras folder, which is what protects a `Shorts` category.
	for i := 1; i < len(parts)-1; i++ {
		if extraDirs[normalizeDirName(parts[i])] {
			return true
		}
	}
	return false
}

// isExtraName covers the file-level conventions: a bare `sample`, and the
// `-trailer` family of suffixes.
func isExtraName(path string) bool {
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if base == "sample" || base == "trailer" {
		return true
	}
	// `.sample.` appears mid-name in some rips: "Film.2011.sample.mkv".
	if strings.HasSuffix(base, ".sample") || strings.HasSuffix(base, " sample") {
		return true
	}
	for _, suf := range extraSuffixes {
		if strings.HasSuffix(base, suf) {
			return true
		}
	}
	return false
}

// normalizeDirName strips the separators release tools scatter through folder
// names, so "Behind.The.Scenes" and "Behind The Scenes" compare equal. It is
// deliberately not SortTitle: that one is for *titles*, and leading-article
// stripping would turn "Other" into something else entirely.
func normalizeDirName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch r {
		case ' ', '.', '_', '-':
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
