package media

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

/*
 * Comics (ADR 0101): what a comic's filename says.
 *
 * A comic book archive is a zip of page images with .cbz on the end. Its name
 * is usually "Series 012 (2014) (Digital) (Group).cbz": the bracketed tags at
 * the end are release notes, and the one that is a four-digit year is the
 * year. The title keeps everything before the tags, issue number included,
 * because "Saga 012" is what somebody looks for, and not run through clean(),
 * whose vocabulary is a video convention.
 *
 * Only .cbz. A .cbr is a RAR archive, which the standard library cannot open,
 * and a .cb7 is 7-Zip; neither is worth a dependency before somebody asks.
 */

// IsComic reports whether a path is a file a comics library indexes.
func IsComic(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".cbz")
}

var (
	reComicTag  = regexp.MustCompile(`\s*[\(\[]([^\)\]]*)[\)\]]\s*$`)
	reComicYear = regexp.MustCompile(`^(19|20)\d\d$`)
)

// ComicTitle is a comic's title and year from its filename.
func ComicTitle(path string) (string, int) {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base = strings.ReplaceAll(base, "_", " ")
	year := 0
	for {
		m := reComicTag.FindStringSubmatchIndex(base)
		if m == nil || m[0] == 0 {
			break
		}
		tag := strings.TrimSpace(base[m[2]:m[3]])
		if year == 0 && reComicYear.MatchString(tag) {
			year, _ = strconv.Atoi(tag)
		}
		base = base[:m[0]]
	}
	title := strings.TrimSpace(strings.Join(strings.Fields(base), " "))
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return title, year
}

// ComicSeries is the folder a comic sits in, when that is not the library's
// root: comics are kept a folder per series far more often than not, and the
// folder's name is the series' name as its owner spells it.
func ComicSeries(root, path string) string {
	dir := filepath.Dir(filepath.Clean(path))
	if filepath.Clean(root) == dir {
		return ""
	}
	return filepath.Base(dir)
}
