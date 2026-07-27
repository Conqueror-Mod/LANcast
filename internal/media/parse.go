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
	// Explicit grouping markers. Deliberately narrow — no roman numerals
	// (ambiguous with sequels: "Part II" vs a second film), no "Vol"/"CD" (a
	// different concept — one work split for size, which plays as a single item
	// and is not modelled here).
	rePart    = regexp.MustCompile(`(?i)\b(?:part|pt\.?)[\s._-]*(\d{1,2}|one|two|three|four|five|six|seven|eight|nine)\b`)
	reChapter = regexp.MustCompile(`(?i)\b(?:chapter|ch\.?)[\s._-]*(\d{1,2}|one|two|three|four|five|six|seven|eight|nine)\b`)
	// A miniseries part marker: "Episode 2", "Ep 2", or a bare "E2" (adjacent, no
	// separator — so "Se7en" and "Wall-E" are safe). Season-numbered files
	// (S01E02) are episodes of a show and handled by reSeasonEp before this.
	reEpisodeMark = regexp.MustCompile(`(?i)\b(?:episode[\s._-]*|ep[\s._-]*|e)(\d{1,2})\b`)
)

var numberWords = map[string]int{
	"one": 1, "two": 2, "three": 3, "four": 4, "five": 5,
	"six": 6, "seven": 7, "eight": 8, "nine": 9,
}

// PartOf reports whether a filename names an explicit part of a larger work —
// "Baahubali Part 1", "Storm of the Century Pt. 2" — returning the work title
// (the stem before the marker) and the part number.
//
// It is only a signal: a lone part is still just a movie. Grouping into a
// multi-part work happens in the scanner, and only when two or more files share
// a work title (ADR 0017), which is what keeps a standalone film that merely
// has "Part" in its name from being torn into pieces.
func PartOf(path string) (work string, part int, ok bool) {
	return markerOf(path, rePart)
}

// ChapterOf is PartOf for theatrical serials — "Batman Chapter 1", the chaptered
// 1940s serials ADR 0017 calls out. Same grouping rules; a distinct marker so a
// serial's pieces are labelled chapters, not parts.
func ChapterOf(path string) (work string, chapter int, ok bool) {
	return markerOf(path, reChapter)
}

// EpisodeMarkerOf detects a miniseries part named with a bare episode marker —
// "Storm of the Century E2", "Storm of the Century Episode 3" — that carries no
// season and so is not a show episode (reSeasonEp needs S<n>E<n>). Same grouping
// rules as PartOf; the scanner folds these into a serial.
func EpisodeMarkerOf(path string) (work string, episode int, ok bool) {
	return markerOf(path, reEpisodeMark)
}

// markerOf finds an explicit ordinal marker (Part N, Chapter N, E N) in a
// filename and splits off the work title before it. Shared by the detectors so
// they cannot drift in how they parse a number or trim a title.
func markerOf(path string, re *regexp.Regexp) (work string, num int, ok bool) {
	base := stripNoise(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	loc := re.FindStringSubmatchIndex(base)
	if loc == nil {
		return "", 0, false
	}
	token := strings.ToLower(base[loc[2]:loc[3]])
	if n, err := strconv.Atoi(token); err == nil {
		num = n
	} else {
		num = numberWords[token]
	}
	if num == 0 {
		return "", 0, false
	}

	// Drop a trailing bracketed or bare year so "Movie (2020) Part 1" and
	// "Movie (2020) Part 2" group under the same work title. Done before clean,
	// which would otherwise strip the brackets the year detector keys on.
	raw := base[:loc[0]]
	if _, cut, hasYear := findYear(raw); hasYear {
		raw = raw[:cut]
	}
	work = clean(raw)
	if work == "" {
		// Nothing identifies the work — "Part 1.mkv" alone cannot be grouped
		// with confidence, so it stays an ordinary movie.
		return "", 0, false
	}
	return work, num, true
}

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
	if dir := showDirWalk(root, path); dir != "" {
		return clean(filepath.Base(dir))
	}
	return ""
}

// ShowDir returns the directory that owns an episode's show — the first
// ancestor that is not a "Season N" folder, without climbing above the library
// root. It is what a show media_item uses as its path, so tvshow.nfo lands in
// the right place (ADR 0010). Empty when the file sits directly in the root,
// which is not a show layout.
func ShowDir(root, path string) string {
	return showDirWalk(root, path)
}

// SeasonDir returns the immediate parent directory of an episode when that
// parent is a "Season N" folder, and empty otherwise — a show whose episodes
// sit loose in the show folder has no season directory, and the caller
// synthesizes a season identity instead.
func SeasonDir(path string) string {
	dir := filepath.Dir(path)
	if reSeasonDir.MatchString(filepath.Base(dir)) {
		return dir
	}
	return ""
}

func showDirWalk(root, path string) string {
	dir := filepath.Dir(path)
	rootAbs := filepath.Clean(root)
	for i := 0; i < 4; i++ {
		if filepath.Clean(dir) == rootAbs || dir == filepath.Dir(dir) {
			break
		}
		name := filepath.Base(dir)
		if !reSeasonDir.MatchString(name) {
			return dir
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
