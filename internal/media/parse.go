// Package media turns filesystem paths into best-guess metadata.
//
// This is pure guessing from names, and it is intentionally the only place
// that guesses. Real metadata (TMDB, NFO) arrives at M2 and overwrites these
// fields; keeping the heuristics isolated means providers can replace them
// without touching the scanner or the store.
//
// The normalizers here (clean, SortTitle) are also what M2 confidence scoring
// uses. Do not write a second one — two normalizers that disagree is a bug
// factory.
package media

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Kind classifies an item well enough for the UI to group it.
type Kind string

const (
	KindMovie   Kind = "movie"
	KindEpisode Kind = "episode"
	KindOther   Kind = "other"
)

// Info is what we could infer from a path alone.
type Info struct {
	Kind    Kind
	Title   string
	Year    int
	Series  string
	Season  int
	Episode int
}

var videoExts = map[string]bool{
	".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".mov": true,
	".wmv": true, ".ts": true, ".m2ts": true, ".mpg": true, ".mpeg": true,
	".webm": true, ".flv": true, ".ogv": true, ".divx": true, ".vob": true,
}

// IsVideo reports whether a path looks like a playable video file.
func IsVideo(path string) bool {
	return videoExts[strings.ToLower(filepath.Ext(path))]
}

var (
	// S01E02, s1e2, S01.E02, 1x02
	reSeasonEp = regexp.MustCompile(`(?i)(?:s(?:eason)?[\s._-]*(\d{1,2})[\s._-]*(?:e|ep|episode|x)[\s._-]*(\d{1,3})|\b(\d{1,2})x(\d{1,3})\b)`)
	// Years are matched by two separate patterns, deliberately. A bracketed year
	// is an explicit statement and always wins; Go's regexp is leftmost-match
	// rather than alternation-priority, so a single combined pattern would read
	// "Blade Runner 2049 (2017)" as year 2049.
	reYearBracket = regexp.MustCompile(`[\(\[]((?:19|20)\d{2})[\)\]]`)
	reYearBare    = regexp.MustCompile(`[\s._-]((?:19|20)\d{2})(?:[\s._-]|$)`)
	// Release-group noise: everything from the first quality marker onward is junk.
	reNoise     = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|480p|4k|uhd|hdr|sdr|bluray|blu-ray|bdrip|brrip|dvdrip|webrip|web-dl|webdl|hdtv|remux|x264|x265|h264|h265|hevc|avc|xvid|divx|aac|ac3|eac3|dts|dts-hd|truehd|atmos|ddp5|dd5|10bit|8bit|proper|repack|extended|unrated|remastered|imax|multi)\b`)
	reSeasonDir = regexp.MustCompile(`(?i)^(?:season|series|s)[\s._-]*(\d{1,2})$`)
	reSpaces    = regexp.MustCompile(`\s+`)
)

// Parse infers metadata for a media file. root is the library root, used to
// derive series names from directory layout when the filename is uninformative.
func Parse(root, path string) Info {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	info := Info{Kind: KindOther}

	if m := reSeasonEp.FindStringSubmatch(base); m != nil {
		info.Kind = KindEpisode
		if m[1] != "" {
			info.Season, _ = strconv.Atoi(m[1])
			info.Episode, _ = strconv.Atoi(m[2])
		} else {
			info.Season, _ = strconv.Atoi(m[3])
			info.Episode, _ = strconv.Atoi(m[4])
		}

		loc := reSeasonEp.FindStringIndex(base)
		info.Series = clean(base[:loc[0]])
		info.Title = clean(stripNoise(base[loc[1]:]))

		// "Show/Season 01/S01E02.mkv" leaves nothing before the marker; walk up
		// the directory tree for the series name instead.
		if info.Series == "" {
			info.Series = seriesFromDirs(root, path)
		}
		if info.Title == "" {
			info.Title = "Episode " + strconv.Itoa(info.Episode)
		}
		return info
	}

	info.Kind = KindMovie
	name := stripNoise(base)
	if year, cut, ok := findYear(name); ok {
		info.Year = year
		name = name[:cut]
	}
	info.Title = clean(name)
	if info.Title == "" {
		info.Title = clean(base)
	}
	return info
}

// findYear returns the release year and the index at which the title ends.
//
// A bracketed year wins outright — "Blade Runner 2049 (2017)" is unambiguous
// and must not resolve to 2049. Failing that, the *last* bare year is used, on
// the reasoning that a trailing year is far more often a release date than a
// leading one is, and titles containing a number tend to carry it early.
func findYear(s string) (year, cut int, ok bool) {
	if loc := reYearBracket.FindStringSubmatchIndex(s); loc != nil {
		y, err := strconv.Atoi(s[loc[2]:loc[3]])
		return y, loc[0], err == nil
	}
	all := reYearBare.FindAllStringSubmatchIndex(s, -1)
	if len(all) == 0 {
		return 0, 0, false
	}
	last := all[len(all)-1]
	y, err := strconv.Atoi(s[last[2]:last[3]])
	return y, last[0], err == nil
}

// seriesFromDirs walks up from the file looking for the first ancestor that
// isn't a "Season N" folder, stopping at the library root.
func seriesFromDirs(root, path string) string {
	dir := filepath.Dir(path)
	rootAbs := filepath.Clean(root)
	for i := 0; i < 4; i++ {
		if filepath.Clean(dir) == rootAbs || dir == filepath.Dir(dir) {
			break
		}
		name := filepath.Base(dir)
		if !reSeasonDir.MatchString(name) {
			return clean(name)
		}
		dir = filepath.Dir(dir)
	}
	return ""
}

// stripNoise drops everything from the first release-quality marker onward.
func stripNoise(s string) string {
	if loc := reNoise.FindStringIndex(s); loc != nil {
		return s[:loc[0]]
	}
	return s
}

// clean turns "Some.Movie_Title--" into "Some Movie Title".
func clean(s string) string {
	s = strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(s)
	s = reSpaces.ReplaceAllString(s, " ")
	return strings.Trim(s, " -[](){}")
}

// SortTitle normalizes for alphabetical ordering: lowercased, leading article
// moved off the front so "The Matrix" files under M.
func SortTitle(title string) string {
	t := strings.ToLower(strings.TrimSpace(title))
	for _, a := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(t, a) {
			return strings.TrimPrefix(t, a)
		}
	}
	return t
}
