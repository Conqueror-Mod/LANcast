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
	KindTrack   Kind = "track"
	// Pictures (ADR 0028): a gallery is a folder, a photo is a file. Not
	// `album` — that kind is music's, and one string meaning two media types
	// would collide in every helper that switches on kind.
	KindGallery Kind = "gallery"
	KindPhoto   Kind = "photo"
	KindOther   Kind = "other"
)

// Library kinds, as stored on a library row and accepted by the API. Spelled
// once here so the scanner and the gate below cannot disagree about them.
const (
	LibraryMovie = "movie"
	LibraryShow  = "show"
	LibraryMusic = "music"
	// A library of images (ADR 0028). Spelled "picture" rather than "photo"
	// because the leaf kind is `photo`, and a library kind that reads identically
	// to an item kind is a bug waiting to be written by autocomplete.
	LibraryPicture = "picture"
	LibraryOther   = "other"
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

// audioExts are the music containers a library scans (ADR 0024).
//
// Listed by what ffprobe reads, not by what a browser plays: MP3, AAC, FLAC,
// Opus, Vorbis and WAV direct-play, while WMA and AIFF are transcoded — that is
// the playback decision's business, not the scanner's. A file the server cannot
// deliver at all is still better indexed and reported than silently skipped.
//
// `.m4a` covers both AAC and ALAC; they are distinguished by codec at probe
// time, which matters because ALAC plays only on Safari.
var audioExts = map[string]bool{
	".mp3": true, ".flac": true, ".m4a": true, ".aac": true, ".ogg": true,
	".oga": true, ".opus": true, ".wav": true, ".aiff": true, ".aif": true,
	".wma": true, ".alac": true,
}

// imageExts are the picture formats a library scans (ADR 0028).
//
// Listed by what can be *indexed*, not by what this build can decode: heic and
// heif need ffmpeg where the rest need only the standard library and
// golang.org/x/image. A phone backup is mostly heic, and a library that omitted
// them would be missing most of someone's photos with no explanation — the
// decoder's limits are the thumbnail worker's problem, reported there, not a
// reason for the scanner to pretend a file is not on disk.
var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true,
	".bmp": true, ".tiff": true, ".tif": true, ".heic": true, ".heif": true,
}

// IsVideo reports whether a path looks like a playable video file.
func IsVideo(path string) bool {
	return videoExts[strings.ToLower(filepath.Ext(path))]
}

// IsAudio reports whether a path looks like a music file.
func IsAudio(path string) bool {
	return audioExts[strings.ToLower(filepath.Ext(path))]
}

// IsImage reports whether a path looks like a picture file.
func IsImage(path string) bool {
	return imageExts[strings.ToLower(filepath.Ext(path))]
}

// IsScannable reports whether a library of this kind should index the file.
//
// The gate asks what the library is *for* rather than what the file *is*,
// because both mistakes are real: a movie library that absorbs the MP3s in a
// soundtrack folder fills the grid with tracks, and a music library that
// indexes the MKV a band shipped with an album produces an item nothing can
// group. An unknown library kind takes video, which is what every library did
// before music existed.
func IsScannable(path, libKind string) bool {
	// A switch rather than the two-branch form this had: "music takes audio,
	// everything else takes video" stopped being extensible the moment a third
	// media type existed, and the default is the part that must stay explicit —
	// an unknown library kind takes video, which is what every library did
	// before music or pictures existed.
	switch libKind {
	case LibraryMusic:
		return IsAudio(path)
	case LibraryPicture:
		return IsImage(path)
	default:
		return IsVideo(path)
	}
}

var (
	/*
	 * S01E02, s1e2, S01.E02, 1x02.
	 *
	 * `\b` before the marker is load-bearing, not tidiness. Without it the `s9`
	 * inside **ds9** matched: `star.trek.ds9.e099.apocalypse.rising.mkv` read as
	 * *season 9, episode 99* of a series called "star trek d" — the series name
	 * truncated at the false marker. Every show abbreviated to letters ending in
	 * s + a digit hits this, and it fails silently, producing a confident wrong
	 * answer rather than no answer.
	 *
	 * The optional trailing range consumes the second half of a double episode
	 * so it does not land in the title. `S01E01-E02 - Emissary` was titled
	 * "E02 Emissary", and `s01e001-002.emissary` before that was "002 emissary".
	 * Non-capturing, so the submatch indices the caller reads stay put.
	 */
	reSeasonEp = regexp.MustCompile(`(?i)(?:\bs(?:eason)?[\s._-]*(\d{1,2})[\s._-]*(?:e|ep|episode|x)[\s._-]*(\d{1,3})|\b(\d{1,2})x(\d{1,3})\b)(?:[\s._-]*[-–][\s._-]*(?:e|ep)?\d{1,3})?`)
	// Years are matched by two separate patterns, deliberately. A bracketed year
	// is an explicit statement and always wins; Go's regexp is leftmost-match
	// rather than alternation-priority, so a single combined pattern would read
	// "Blade Runner 2049 (2017)" as year 2049.
	reYearBracket = regexp.MustCompile(`[\(\[]((?:19|20)\d{2})[\)\]]`)
	reYearBare    = regexp.MustCompile(`[\s._-]((?:19|20)\d{2})(?:[\s._-]|$)`)
	// Release-group noise: everything from the first quality marker onward is junk.
	reNoise = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|480p|4k|uhd|hdr|sdr|bluray|blu-ray|bdrip|brrip|dvdrip|webrip|web-dl|webdl|hdtv|remux|x264|x265|h264|h265|hevc|avc|xvid|divx|aac|ac3|eac3|dts|dts-hd|truehd|atmos|ddp5|dd5|10bit|8bit|proper|repack|extended|unrated|remastered|imax|multi)\b`)
	// Trailing text is allowed after the number ("Season 1 - Star Trek Deep
	// Space Nine"), but only behind a separator — "S3rvant" must not read as
	// season 3. A folder that starts with the marker and immediately hits a
	// letter fails the match and falls through to being treated as the show
	// folder itself, which is what stopped this from matching at all before.
	reSeasonDir = regexp.MustCompile(`(?i)^(?:season|series|s)[\s._-]*(\d{1,2})(?:[\s._-]+.*)?$`)
	/*
	 * The same marker at the *end* of a folder or series name: "BMS S01",
	 * "Spider-Noir Season 1".
	 *
	 * A real library had both. `Blue Mountain State/BMS S01/S01E01 …mkv` put the
	 * show under a series called "BMS S01" — the filename has nothing before its
	 * marker, so the series came from the folder, and the folder was not
	 * recognised as a season. The show's other two seasons, whose filenames do
	 * carry the show name, grouped correctly under "Blue Mountain State", so one
	 * show appeared twice under two names.
	 *
	 * A separator before the marker is required, so "S3rvant" and "Terminator 2"
	 * are untouched, and at least one character has to precede it or a plain
	 * "Season 1" folder would be read as a *name* ending in a marker rather than
	 * as the season folder it is — that case belongs to reSeasonDir above.
	 */
	reSeasonSuffix = regexp.MustCompile(`(?i)^(.+?)[\s._-]+(?:season|series|s)[\s._-]*(\d{1,2})$`)
	reSpaces       = regexp.MustCompile(`\s+`)
	// Explicit grouping markers. Deliberately narrow — no roman numerals
	// (ambiguous with sequels: "Part II" vs a second film), no "Vol"/"CD" (a
	// different concept — one work split for size, which plays as a single item
	// and is not modelled here).
	rePart    = regexp.MustCompile(`(?i)\b(?:part|pt\.?)[\s._-]*(\d{1,2}|one|two|three|four|five|six|seven|eight|nine)\b`)
	reChapter = regexp.MustCompile(`(?i)\b(?:chapter|ch\.?)[\s._-]*(\d{1,2}|one|two|three|four|five|six|seven|eight|nine)\b`)
	// An ordinal marker with no season — "Storm of the Century E2", "…Episode 3",
	// "…Part 2", "…Chapter 1". In a show library every one of these means a
	// miniseries part (an episode), where in a movie library Part means a film
	// work and Chapter a serial. The bare "e" only matches adjacent to digits at
	// a word boundary, so "Se7en" and "WALL-E" are safe.
	reShowOrdinal = regexp.MustCompile(`(?i)\b(?:part[\s._-]*|pt\.?[\s._-]*|chapter[\s._-]*|ch\.?[\s._-]*|episode[\s._-]*|ep[\s._-]*|e)(\d{1,2}|one|two|three|four|five|six|seven|eight|nine)\b`)
)

// LibShow is the library kind that marks a library as television, which biases
// how ambiguous filenames are read (a bare "E2" is an episode, not a film).
const LibShow = "show"

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

// markerOf finds an explicit ordinal marker (Part N, Chapter N) in a filename
// and splits off the work title before it. Shared by PartOf and ChapterOf so the
// two cannot drift in how they parse a number or trim a title.
func markerOf(path string, re *regexp.Regexp) (work string, num int, ok bool) {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	/*
	 * The marker is looked for in the tidied name first, and in the raw name
	 * only if that finds nothing.
	 *
	 * `stripNoise` cuts everything from the first quality marker onward, which
	 * is right for a title and wrong for a marker that sits *after* one. Scene
	 * names routinely put it there:
	 *
	 *	Storm.Of.The.Century.[1999].DVDRip.XviD.EP2-BLiTZKRiEG
	 *
	 * cut at `DVDRip`, taking `EP2` with it — so a three-part miniseries became
	 * three films in a television library, each separately matched against
	 * TMDB's *movie* data, and each landing in the review queue with nothing to
	 * fix. Found on a real library.
	 *
	 * Ordered as a fallback rather than searching the raw name outright,
	 * because that is strictly additive: every name that resolves today
	 * resolves identically, and only the ones that currently find nothing get a
	 * second look. A release tag containing something the pattern would read as
	 * an ordinal therefore cannot change an answer that already works.
	 */
	search := stripNoise(base)
	loc := re.FindStringSubmatchIndex(search)
	if loc == nil {
		search = base
		loc = re.FindStringSubmatchIndex(search)
	}
	if loc == nil {
		return "", 0, false
	}
	base = search
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
	//
	// Noise is stripped here rather than only up front, because on the fallback
	// path above the name still carries it: the work title of
	// "…[1999].DVDRip.XviD.EP2" is "Storm Of The Century", not "Storm Of The
	// Century DVDRip XviD". A second call is free on the ordinary path, where
	// the text has already been through it.
	raw := stripNoise(base[:loc[0]])
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
// libKind is the owning library's kind ("movie", "show", …); it biases the
// reading of ambiguous names — in a show library a bare "E2" is a miniseries
// episode, where in a movie library the same name is left a film.
func Parse(root, path, libKind string) Info {
	base := stripGroupPrefix(path, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	info := Info{Kind: KindOther}

	// A music library reads its files by a different set of conventions
	// entirely, and none of the season/episode/year heuristics below apply to
	// them (ADR 0024).
	if libKind == LibraryMusic {
		return ParseTrack(root, path)
	}

	// A picture is its filename, verbatim (ADR 0028). Not run through `clean`,
	// which strips release-group noise, years and quality markers — every one of
	// those is a video-naming convention, and applying them to
	// "openart-f81b7650ced542cdb5b37d8916f0bc92_raw" produces a different
	// meaningless string with less information in it. A UUID is a poor title; a
	// tidied UUID is a poor title that has been lied about.
	if libKind == LibraryPicture {
		return Info{Kind: KindPhoto, Title: base}
	}

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
		// Whichever source it came from, the series is the show — not the show
		// plus the season it happens to be in.
		info.Series = stripSeasonSuffix(info.Series)
		if info.Title == "" {
			info.Title = "Episode " + strconv.Itoa(info.Episode)
		}
		return info
	}

	// In a show library an ordinal marker with no season — "Storm of the Century
	// E2", "…Part 2", "…Chapter 1" — is a TV episode (of season 1), not a
	// same-named film or a movie work. This is what lets a miniseries match
	// against TMDB's TV data: episode and show kinds search /tv, where a movie
	// kind searches /movie and can only ever find the wrong, same-named film.
	// Everything in a show library is television, so Part here means an episode,
	// not the multi-part film work it means in a movie library. Gated to show
	// libraries so a movie's odd name never trips it.
	if libKind == LibShow {
		if series, episode, ok := markerOf(path, reShowOrdinal); ok {
			info.Kind = KindEpisode
			info.Series = series
			info.Season = 1
			info.Episode = episode
			info.Title = "Episode " + strconv.Itoa(episode)
			return info
		}
	}

	info.Kind = KindMovie
	name := stripNoise(base)
	if year, cut, ok := findYear(name); ok {
		info.Year = year
		name = name[:cut]
	} else if year := yearFromDir(root, path); year != 0 {
		info.Year = year
	}
	// The edition marker comes off after cleaning, so "Alien.DC" has become
	// "Alien DC" and the suffix is a word rather than a separator away.
	info.Title = stripEditionSuffix(clean(name))
	if info.Title == "" {
		info.Title = clean(base)
	}
	return info
}

// yearFromDir reads a release year off the file's immediate parent directory,
// for the "Title (Year)/Title.ext" layout where the year is stated once — on
// the folder — and the filename repeats only the title.
//
// Without this the year is not merely missing but capping: an absent year
// scores half credit, which holds the weighted total strictly below the
// auto-accept threshold however exact the title, so every film in such a
// library lands in review permanently and no amount of popularity rescues it.
// See internal/meta.ScoreBreakdown.
//
// Only the immediate parent is read. Walking further up reaches collection
// folders — "Alien(1986-2024)", "Marvel Comics" — where a year belongs to the
// set rather than to this film, and would be stamped onto every film beneath
// it. The filename still wins outright when it carries a year of its own.
func yearFromDir(root, path string) int {
	dir := filepath.Dir(path)

	// The root itself names the library, not the film. "Movies (2024)/Dredd.mp4"
	// must not become a 2024 film.
	rootAbs, rerr := filepath.Abs(root)
	dirAbs, derr := filepath.Abs(dir)
	if rerr == nil && derr == nil && rootAbs == dirAbs {
		return 0
	}

	year, _, ok := findYear(stripNoise(filepath.Base(dir)))
	if !ok {
		return 0
	}
	return year
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
	if isSeasonDir(filepath.Base(dir)) {
		return dir
	}
	return ""
}

// isSeasonDir reports whether a folder name is a season folder, in either of the
// two shapes a real library uses: the marker leading ("Season 1 - Show Name") or
// trailing ("BMS S01"). One predicate so the walk and SeasonDir cannot disagree
// about what a season folder is — two answers to that question is how a show
// ends up listed twice under two names.
func isSeasonDir(name string) bool {
	return reSeasonDir.MatchString(name) || reSeasonSuffix.MatchString(name)
}

/*
 * stripSeasonSuffix removes a trailing season marker from a *series* name.
 *
 * The series is often read from the filename text before the episode marker,
 * and a release named `Spider-Noir.Season.1.S01E01.1080p…` leaves
 * "Spider Noir Season 1" — which searches a provider as a show by that name and
 * finds nothing. The season is already known from the marker that followed it;
 * repeating it in the title is noise, not information.
 *
 * Never strips the whole name: a folder called exactly "Season 1" is a season
 * folder, handled above, and a series name that is nothing but a marker is not
 * improved by becoming empty.
 */
func stripSeasonSuffix(series string) string {
	if m := reSeasonSuffix.FindStringSubmatch(series); m != nil {
		if trimmed := strings.TrimSpace(m[1]); trimmed != "" {
			return trimmed
		}
	}
	return series
}

func showDirWalk(root, path string) string {
	dir := filepath.Dir(path)
	rootAbs := filepath.Clean(root)

	// The last season-marked folder passed on the way up. When the walk reaches
	// the root without finding a plain folder, that one *is* the show: the
	// `Show S01`, `Show S02` layout sitting directly under the library root has
	// no folder above it to name the series, and treating it as a season with
	// no show produced no show at all (ADR 0037 — a twenty-season series became
	// twenty shows before, and zero shows if this falls back to nothing).
	//
	// The season marker still comes off the *name* via stripSeasonSuffix, so
	// "Show S01" and "Show S02" resolve to one series either way.
	lastSeason := ""

	for i := 0; i < 4; i++ {
		if filepath.Clean(dir) == rootAbs || dir == filepath.Dir(dir) {
			break
		}
		name := filepath.Base(dir)
		if !isSeasonDir(name) {
			return dir
		}
		lastSeason = dir
		dir = filepath.Dir(dir)
	}
	return lastSeason
}

/*
 * stripGroupPrefix drops a scene release group from the *front* of a filename,
 * and only when the folder around it agrees.
 *
 * The shape is "veto-beavis.and.butthead.do.america.1996.1080p.bluray.x264.mkv"
 * inside a folder called "Beavis.And.Butthead.Do.America.1996...-VETO". The
 * group is a trailing marker on the folder and a *leading* one on the file, and
 * stripNoise only knows about trailing ones — so the title came out as "veto
 * beavis and butthead do america".
 *
 * The naive fix is a catastrophe. Stripping any leading "word-" turns
 * "Spider-Man.2002.1080p.mkv" into "Man 2002", and there is nothing in the
 * filename alone that separates the two cases: both are a word, a hyphen, and a
 * dotted title.
 *
 * The folder is what separates them. A release group appears at the end of the
 * directory it produced, so the prefix is only removed when the containing
 * folder actually ends with it. "Spider-Man" is never inside a folder ending
 * "-Spider", and a mismatch leaves the name exactly as it was.
 */
func stripGroupPrefix(path, base string) string {
	i := strings.Index(base, "-")
	// Nothing before the hyphen, nothing after it, or a dot inside the
	// candidate: not a group name.
	if i <= 0 || i+1 >= len(base) || strings.Contains(base[:i], ".") {
		return base
	}
	group := strings.ToLower(base[:i])
	dir := strings.ToLower(filepath.Base(filepath.Dir(path)))
	if strings.HasSuffix(dir, "-"+group) {
		return base[i+1:]
	}
	return base
}

// stripNoise drops everything from the first release-quality marker onward.
func stripNoise(s string) string {
	if loc := reNoise.FindStringIndex(s); loc != nil {
		return s[:loc[0]]
	}
	return s
}

/*
 * reEditionSuffix matches an edition marker at the *end* of a title.
 *
 * "Alien DC" and "Alien Resurrection SE" are a director's cut and a special
 * edition, and they were matching nothing — two posterless tiles sorted apart
 * from the films they are editions of.
 *
 * Anchored to the end, and that is not a stylistic choice. `reNoise` strips
 * from a marker *onward* wherever it appears, and adding `dc` to it would turn
 * "DC League of Super-Pets" into an empty title — the marker is the first word.
 * At the end of a title those two letters are an edition; at the front they are
 * a name. The anchor is the only thing that tells them apart.
 */
var reEditionSuffix = regexp.MustCompile(`(?i)[\s\-]+(` +
	`dc|se|ee|uncut|theatrical|final cut|ultimate edition|` +
	`directors cut|director's cut|special edition|extended edition` +
	`)$`)

/*
 * stripEditionSuffix removes a trailing edition marker so an edition matches
 * the work it is an edition of.
 *
 * It does **not** group the two — "Alien" and "Alien DC" remain two rows, and
 * both now match, where before one of them matched nothing. Modelling editions
 * as one work with several files is a real feature and a larger one; this is
 * the half that stops the library lying about what the file is.
 *
 * Never strips the whole title: a film actually called "Uncut" keeps its name.
 */
func stripEditionSuffix(s string) string {
	out := reEditionSuffix.ReplaceAllString(s, "")
	if strings.TrimSpace(out) == "" {
		return s
	}
	return out
}

// clean turns "Some.Movie_Title--" into "Some Movie Title".
func clean(s string) string {
	s = strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(s)
	s = reSpaces.ReplaceAllString(s, " ")
	return stripPairedQuotes(strings.Trim(s, " -[](){}"))
}

/*
 * stripPairedQuotes removes quotation marks that wrap the whole title.
 *
 * A real library had a film titled `"Wuthering Heights"` — quotes included —
 * which is not only wrong on the tile but sorts to the very front of the grid,
 * because a quote character orders before every letter.
 *
 * Only a *matching pair* is removed, and that is the whole care in this
 * function. Trimming quote characters off both ends would turn `'71` — a 2014
 * film — into `71`, and a leading apostrophe is the one case where the
 * character is part of the name rather than around it. A pair is evidence
 * somebody wrapped the title; a single mark is evidence of nothing.
 */
func stripPairedQuotes(s string) string {
	pairs := [][2]string{
		{`"`, `"`}, {`'`, `'`}, {"“", "”"}, {"‘", "’"},
	}
	for _, p := range pairs {
		if len(s) > len(p[0])+len(p[1]) &&
			strings.HasPrefix(s, p[0]) && strings.HasSuffix(s, p[1]) {
			return strings.TrimSpace(s[len(p[0]) : len(s)-len(p[1])])
		}
	}
	return s
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

// reTrackNo matches a leading track number: "01 Title", "1-02 Title",
// "03. Title", "04 - Title", and a bare "07". The disc-number form ("1-02") is
// common on multi-disc rips and would otherwise be read as part of the title.
//
// The trailing separator is optional only at end of input, so a file named just
// "07.mp3" yields track 7 rather than a title of "07". That ordering matters:
// without a number, "01" through "10" sort as strings and an album plays in the
// wrong order. The cost is that a song genuinely titled "22" reads as track 22
// until its tags are read — an acceptable trade for a fallback that tags
// outrank, and only for one to three digits, so an album called "1984" is
// untouched.
var reTrackNo = regexp.MustCompile(`^\s*(?:(\d{1,2})\s*[-.]\s*)?(\d{1,3})(?:\s*(?:[-.)\]]|\s)\s*|$)`)

// ParseTrack reads what a music file's path alone can tell us: a track number,
// a disc number, and a title, with the album and artist taken from the folders
// above it.
//
// This is the *fallback*. Per ADR 0024 the authority for music is the embedded
// tag — ID3v2, Vorbis comment, MP4 atom — because unlike a film's filename it
// is written by the tagger rather than guessed by a ripper. This runs when the
// tags are missing, and everything it produces is expected to be overwritten by
// them.
//
// The layout assumed is the near-universal one: Artist/Album/NN Title.ext.
func ParseTrack(root, path string) Info {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	info := Info{Kind: KindTrack}

	if m := reTrackNo.FindStringSubmatch(base); m != nil {
		if m[1] != "" {
			info.Season, _ = strconv.Atoi(m[1]) // disc
		}
		info.Episode, _ = strconv.Atoi(m[2]) // track
		base = base[len(m[0]):]
	}

	info.Title = clean(base)
	if info.Title == "" {
		// A file named only by its number still needs something to show.
		info.Title = "Track " + strconv.Itoa(info.Episode)
	}

	// Album from the containing folder, artist from the one above it. Series
	// carries the album so it groups exactly as a show's episodes do.
	info.Series = albumFromDirs(root, path)
	return info
}

// albumFromDirs returns the folder immediately containing the track, which by
// convention is the album. Empty when the track sits at the library root, where
// there is no album to name.
func albumFromDirs(root, path string) string {
	rel, err := filepath.Rel(root, filepath.Dir(path))
	if err != nil || rel == "." || rel == "" {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	return clean(parts[len(parts)-1])
}
