package media

import (
	"path/filepath"
	"testing"
)

func TestParse(t *testing.T) {
	const root = `D:\Media`

	tests := []struct {
		name string
		path string
		want Info
	}{
		{
			name: "episode with release noise",
			path: `Show.Name.S01E02.The.Title.1080p.WEB-DL.x264-GRP.mkv`,
			want: Info{Kind: KindEpisode, Series: "Show Name", Season: 1, Episode: 2, Title: "The Title"},
		},
		{
			name: "episode in NxNN form",
			path: `Show Name - 1x02 - Title.mkv`,
			want: Info{Kind: KindEpisode, Series: "Show Name", Season: 1, Episode: 2, Title: "Title"},
		},
		{
			name: "series name recovered from directory",
			path: filepath.Join(`Some.Show`, `Season 01`, `S01E05.mkv`),
			want: Info{Kind: KindEpisode, Series: "Some Show", Season: 1, Episode: 5, Title: "Episode 5"},
		},
		{
			name: "movie with year and release noise",
			path: `The.Matrix.1999.1080p.BluRay.x264.mkv`,
			want: Info{Kind: KindMovie, Title: "The Matrix", Year: 1999},
		},
		{
			// The regression this suite exists for: a number in the title must
			// not be harvested as the release year.
			name: "title containing a number, bracketed year",
			path: `Blade Runner 2049 (2017).mkv`,
			want: Info{Kind: KindMovie, Title: "Blade Runner 2049", Year: 2017},
		},
		{
			name: "bracketed year wins over bare year in title",
			path: `2001 A Space Odyssey (1968).mkv`,
			want: Info{Kind: KindMovie, Title: "2001 A Space Odyssey", Year: 1968},
		},
		{
			name: "no year present",
			path: `random-clip.mp4`,
			want: Info{Kind: KindMovie, Title: "random clip"},
		},
		{
			name: "dotted title with bare year",
			path: `Arrival.2016.mkv`,
			want: Info{Kind: KindMovie, Title: "Arrival", Year: 2016},
		},
		{
			name: "season directory with show name in filename",
			path: filepath.Join(`Andor`, `Season 01`, `Andor.S01E07.Announcement.mkv`),
			want: Info{Kind: KindEpisode, Series: "Andor", Season: 1, Episode: 7, Title: "Announcement"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(root, filepath.Join(root, tt.path), "movie")
			if got != tt.want {
				t.Errorf("Parse(%q)\n got: %+v\nwant: %+v", tt.path, got, tt.want)
			}
		})
	}
}

func TestSortTitle(t *testing.T) {
	tests := []struct{ in, want string }{
		{"The Matrix", "matrix"},
		{"A Beautiful Mind", "beautiful mind"},
		{"An American Werewolf", "american werewolf"},
		{"Arrival", "arrival"},
		{"Theodore Rex", "theodore rex"}, // "The" prefix must not match mid-word
	}
	for _, tt := range tests {
		if got := SortTitle(tt.in); got != tt.want {
			t.Errorf("SortTitle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestShowAndSeasonDir(t *testing.T) {
	root := filepath.Join("R", "TV")
	nested := filepath.Join(root, "Andor", "Season 01", "Andor.S01E07.mkv")
	loose := filepath.Join(root, "Firefly", "Firefly.S01E01.mkv")
	atRoot := filepath.Join(root, "Stray.S01E01.mkv")

	if got, want := ShowDir(root, nested), filepath.Join(root, "Andor"); got != want {
		t.Errorf("ShowDir(nested) = %q, want %q", got, want)
	}
	if got, want := ShowDir(root, loose), filepath.Join(root, "Firefly"); got != want {
		t.Errorf("ShowDir(loose) = %q, want %q", got, want)
	}
	// A file directly in the root is not a show layout.
	if got := ShowDir(root, atRoot); got != "" {
		t.Errorf("ShowDir(atRoot) = %q, want empty", got)
	}

	if got, want := SeasonDir(nested), filepath.Join(root, "Andor", "Season 01"); got != want {
		t.Errorf("SeasonDir(nested) = %q, want %q", got, want)
	}
	// No "Season N" folder — the caller must synthesize an identity instead.
	if got := SeasonDir(loose); got != "" {
		t.Errorf("SeasonDir(loose) = %q, want empty", got)
	}
}

// The season number can lead the folder name instead of trailing it — a
// scene-release layout ("Season 1 - Star Trek Deep Space Nine") rather than
// the "Show/Season 01" shape the rest of this file exercises. Before this
// case matched reSeasonDir at all, showDirWalk stopped one level early and
// treated the season folder as the show, producing a fake show per season
// named after the folder rather than the series.
func TestSeasonDirWithTrailingShowName(t *testing.T) {
	root := filepath.Join("Y", "TV Shows")
	nested := filepath.Join(root, "Star Trek Deep Space Nine",
		"Season 1 - Star Trek Deep Space Nine", "s01e005.babel.mkv")

	if got, want := ShowDir(root, nested), filepath.Join(root, "Star Trek Deep Space Nine"); got != want {
		t.Errorf("ShowDir(nested) = %q, want %q", got, want)
	}
	if got, want := SeasonDir(nested), filepath.Join(root, "Star Trek Deep Space Nine", "Season 1 - Star Trek Deep Space Nine"); got != want {
		t.Errorf("SeasonDir(nested) = %q, want %q", got, want)
	}
}

// A show folder that merely starts the way a season marker does must not be
// swallowed by the wider match above: "S3rvant" has no separator between the
// digit and the letter that follows it, so it never reads as season 3.
func TestSeasonDirDoesNotMatchShowNameStartingWithS(t *testing.T) {
	root := filepath.Join("Y", "TV Shows")
	nested := filepath.Join(root, "S3rvant", "S01E01.mkv")

	if got, want := ShowDir(root, nested), filepath.Join(root, "S3rvant"); got != want {
		t.Errorf("ShowDir(nested) = %q, want %q — \"S3rvant\" must not read as a season folder", got, want)
	}
}

func TestPartOf(t *testing.T) {
	tests := []struct {
		name     string
		wantWork string
		wantPart int
		wantOK   bool
	}{
		{"Baahubali Part 1", "Baahubali", 1, true},
		{"Baahubali.Part.2.1080p.BluRay.x264", "Baahubali", 2, true},
		{"Nymphomaniac Part Two", "Nymphomaniac", 2, true},
		{"Storm of the Century Pt. 3", "Storm of the Century", 3, true},
		{"Movie (2020) Part 1", "Movie", 1, true}, // trailing year dropped
		// Not multi-part: no marker, or nothing identifies the work.
		{"Ocean's Eleven", "", 0, false},
		{"2 Fast 2 Furious", "", 0, false},
		{"Part 1", "", 0, false}, // no work title to group on
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			work, part, ok := PartOf(tt.name + ".mkv")
			if ok != tt.wantOK || part != tt.wantPart || work != tt.wantWork {
				t.Errorf("PartOf(%q) = (%q, %d, %v), want (%q, %d, %v)",
					tt.name, work, part, ok, tt.wantWork, tt.wantPart, tt.wantOK)
			}
		})
	}
}

func TestChapterOf(t *testing.T) {
	tests := []struct {
		name     string
		wantWork string
		wantCh   int
		wantOK   bool
	}{
		{"Batman Chapter 1", "Batman", 1, true},
		{"Batman.Chapter.15.1080p.WEBRip", "Batman", 15, true},
		{"The Phantom Ch. 3", "The Phantom", 3, true},
		{"Superman Chapter Four", "Superman", 4, true},
		// A part marker is not a chapter, and vice versa — the two stay disjoint.
		{"Baahubali Part 1", "", 0, false},
		{"Some Movie", "", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			work, ch, ok := ChapterOf(tt.name + ".mkv")
			if ok != tt.wantOK || ch != tt.wantCh || work != tt.wantWork {
				t.Errorf("ChapterOf(%q) = (%q, %d, %v), want (%q, %d, %v)",
					tt.name, work, ch, ok, tt.wantWork, tt.wantCh, tt.wantOK)
			}
		})
	}

	// A part filename must not be seen as a chapter, so the two grouping passes
	// never fight over the same file.
	if _, _, ok := PartOf("Batman Chapter 1.mkv"); ok {
		t.Error("PartOf matched a Chapter filename")
	}
}

/*
 * "ds9" is not season 9.
 *
 * The season marker matched the `s9` inside **ds9**, so
 * `star.trek.ds9.e099.apocalypse.rising.mkv` read as season 9 episode 99 of a
 * series called "star trek d" — the name truncated at the false marker. It
 * fails silently and confidently, which is the worst shape a parse bug takes:
 * no error, a plausible-looking answer, and 78 episodes filed under a season
 * that does not exist.
 *
 * Any show abbreviated to letters ending in s + a digit hits this.
 */
func TestAnAbbreviationIsNotASeasonMarker(t *testing.T) {
	root := filepath.Join("Y", "TV Shows")

	for _, name := range []string{
		"star.trek.ds9.e099.apocalypse.rising.mkv",
		"ds9.e100.the.ship.mkv",
	} {
		t.Run(name, func(t *testing.T) {
			got := Parse(root, filepath.Join(root, "Star Trek Deep Space Nine", name), "show")
			if got.Season == 9 {
				t.Errorf("season 9 from %q — the marker matched inside an abbreviation", name)
			}
			if got.Series == "star trek d" || got.Series == "ds" {
				t.Errorf("series = %q — truncated at a false marker", got.Series)
			}
		})
	}

	// A real marker still works, including at the very start of a name.
	got := Parse(root, filepath.Join(root, "Show", "Season 01", "S01E02.mkv"), "show")
	if got.Kind != KindEpisode || got.Season != 1 || got.Episode != 2 {
		t.Errorf("got %+v, want episode S01E02", got)
	}
}

/*
 * A season marker at the end of a folder name still marks a season.
 *
 * From a real library: `Blue Mountain State/BMS S01/S01E01 …mkv`. The filename
 * has nothing before its marker, so the series comes from the folder — and
 * "BMS S01" was not recognised as a season folder, so it became the show. The
 * same show's other seasons, whose filenames carry the show name, grouped
 * correctly under "Blue Mountain State", so one show appeared twice.
 *
 * Both of that library's naming conventions are checked here, because they have
 * to converge on one series: season 1 carries episode titles and no show name,
 * seasons 2 and 3 carry the show name and no titles.
 */
func TestTwoNamingConventionsInOneShowStillGroupTogether(t *testing.T) {
	root := filepath.Join("Y", "TV Shows")
	show := filepath.Join(root, "Blue Mountain State")

	first := Parse(root, filepath.Join(show, "BMS S01", "S01E01 It's Called Hazing, Look it up.mkv"), "show")
	if first.Series != "Blue Mountain State" {
		t.Errorf("series = %q, want Blue Mountain State — the season folder became the show", first.Series)
	}
	if first.Season != 1 || first.Episode != 1 {
		t.Errorf("got S%dE%d, want S1E1", first.Season, first.Episode)
	}
	if first.Title != "It's Called Hazing, Look it up" {
		t.Errorf("title = %q, want the episode title kept", first.Title)
	}

	later := Parse(root, filepath.Join(show, "BMS S02",
		"Blue Mountain State S02E01 WEBRip 1080p AAC2.0 H265-d3g.mkv"), "show")
	if later.Series != "Blue Mountain State" {
		t.Errorf("series = %q, want Blue Mountain State", later.Series)
	}
	if later.Season != 2 || later.Episode != 1 {
		t.Errorf("got S%dE%d, want S2E1", later.Season, later.Episode)
	}

	if first.Series != later.Series {
		t.Errorf("the two conventions produced %q and %q — one show, two names",
			first.Series, later.Series)
	}
}

// A season marker trailing the *series* name is noise: the season is already
// known from the marker that followed it. `Spider-Noir.Season.1.S01E01…` left a
// series called "Spider Noir Season 1", which matches no show anywhere.
func TestATrailingSeasonMarkerLeavesTheSeriesName(t *testing.T) {
	root := filepath.Join("Y", "TV Shows")
	dir := "Spider-Noir Season 1 S01 1080p AMZN WEB-DL MULTi DDP5 1 H 264-MQ"
	file := "Spider-Noir.Season.1.S01E01.1080p.AMZN.WEB-DL.MULTi.DDP5.1.H.264-MQ.mkv"

	got := Parse(root, filepath.Join(root, dir, file), "show")
	if got.Series != "Spider Noir" {
		t.Errorf("series = %q, want Spider Noir", got.Series)
	}
	if got.Season != 1 || got.Episode != 1 {
		t.Errorf("got S%dE%d, want S1E1", got.Season, got.Episode)
	}
}

/*
 * `Show S01` sitting directly under the library root *is* the show folder.
 *
 * The near-miss when trailing markers were first recognised: the walk skipped
 * the folder as a season, found the library root above it, and returned no show
 * dir at all — so a layout ADR 0037 had already fixed produced zero shows
 * instead of twenty. There is nothing above it to name the series, so it names
 * itself, and the marker comes off the name rather than off the folder.
 */
func TestASeasonFolderAtTheRootIsTheShow(t *testing.T) {
	root := filepath.Join("Y", "TV Shows")

	first := Parse(root, filepath.Join(root, "It's Always Sunny in Philadelphia S01",
		"It's Always Sunny in Philadelphia.S01E01.mkv"), "show")
	second := Parse(root, filepath.Join(root, "It's Always Sunny in Philadelphia S02",
		"It's Always Sunny in Philadelphia.S02E01.mkv"), "show")

	if first.Series != "It's Always Sunny in Philadelphia" {
		t.Errorf("series = %q, want the show without its season marker", first.Series)
	}
	if first.Series != second.Series {
		t.Errorf("seasons resolved to %q and %q — one show, two names",
			first.Series, second.Series)
	}
	if ShowDir(root, filepath.Join(root, "It's Always Sunny in Philadelphia S01",
		"It's Always Sunny in Philadelphia.S01E01.mkv")) == "" {
		t.Error("no show directory — the folder was skipped as a season with nothing above it")
	}
}

/*
 * An episode marker behind a quality tag is still an episode marker.
 *
 * From a real television library:
 *
 *	Storm.Of.The.Century.[1999].DVDRip.XviD.EP2-BLiTZKRiEG.avi
 *
 * `stripNoise` cuts everything from the first quality marker onward, which is
 * right for a title and wrong for a marker that sits after one — `EP2` went
 * with `DVDRip`. A three-part miniseries became three *films* in a television
 * library, each searched against TMDB's movie data and each landing in the
 * review queue with nothing a person could fix.
 */
func TestAnEpisodeMarkerAfterAQualityTagIsFound(t *testing.T) {
	root := filepath.Join("Y", "TV Shows")
	dir := filepath.Join(root, "Storm Of the Century (1999)")

	for _, tt := range []struct {
		file string
		want int
	}{
		{"Storm.Of.The.Century.[1999].DVDRip.XviD.EP2-BLiTZKRiEG.avi", 2},
		{"Storm.Of.The.Century.[1999].DVDRip.XviD.EP3-BLiTZKRiEG.avi", 3},
	} {
		t.Run(tt.file, func(t *testing.T) {
			got := Parse(root, filepath.Join(dir, tt.file), "show")
			if got.Kind != KindEpisode {
				t.Fatalf("kind = %q, want episode — a miniseries part became a film", got.Kind)
			}
			if got.Episode != tt.want {
				t.Errorf("episode = %d, want %d", got.Episode, tt.want)
			}
			if got.Series != "Storm Of The Century" {
				t.Errorf("series = %q, want the work title without the release tags", got.Series)
			}
		})
	}
}

/*
 * The tidied name is still searched first, so nothing that resolves today
 * resolves differently.
 *
 * The raw name is only consulted when the tidied one yields nothing, which is
 * what keeps a release tag that happens to look like an ordinal from changing
 * an answer that already works.
 */
func TestTheMarkerInTheTitleStillWins(t *testing.T) {
	root := filepath.Join("Y", "TV Shows")

	// The marker before the noise is found in the tidied name, as before.
	got := Parse(root, filepath.Join(root, "Some Show", "Some Show Part 2 1080p BluRay x264.mkv"), "show")
	if got.Kind != KindEpisode || got.Episode != 2 {
		t.Errorf("got %+v, want episode 2", got)
	}
	if got.Series != "Some Show" {
		t.Errorf("series = %q, want Some Show", got.Series)
	}
}

// A show whose name merely ends in a number keeps it. "Terminator 2" is a name;
// "S3rvant" is a name. Only a marker behind a separator is a season.
func TestANumberInAShowNameIsNotASeason(t *testing.T) {
	for _, name := range []string{"Terminator 2", "S3rvant", "Season", "Blake's 7"} {
		if reSeasonSuffix.MatchString(name) {
			t.Errorf("%q was read as ending in a season marker", name)
		}
	}
	for _, name := range []string{"BMS S01", "Spider-Noir Season 1", "Some Show - Series 2"} {
		if !reSeasonSuffix.MatchString(name) {
			t.Errorf("%q was not read as ending in a season marker", name)
		}
	}
}

// The second half of a double episode belongs to neither the number nor the
// title. `S01E01-E02 - Emissary` was titled "E02 Emissary".
func TestADoubleEpisodeRangeDoesNotLeakIntoTheTitle(t *testing.T) {
	root := filepath.Join("Y", "TV Shows")
	show := filepath.Join(root, "Star Trek Deep Space Nine")

	for _, tt := range []struct{ file, want string }{
		{"Star Trek Deep Space Nine - S01E01-E02 - Emissary.mkv", "Emissary"},
		{"s01e001-002.emissary.mkv", "emissary"},
	} {
		t.Run(tt.file, func(t *testing.T) {
			got := Parse(root, filepath.Join(show, "Season 01", tt.file), "show")
			if got.Episode != 1 {
				t.Errorf("episode = %d, want 1 (the first of the pair)", got.Episode)
			}
			if got.Title != tt.want {
				t.Errorf("title = %q, want %q", got.Title, tt.want)
			}
		})
	}
}

// "Title (Year)/Title.ext" states the year once, on the folder. Reading only the
// filename loses it, and a missing year is not a weak signal but a cap: it scores
// half credit, which holds the weighted total strictly under the auto-accept
// threshold no matter how exact the title. Every film in a library shaped this way
// then sits in review forever, which is what a real library looked like.
func TestMovieYearComesFromTheFolderWhenTheFilenameOmitsIt(t *testing.T) {
	root := filepath.Join("W", "Movies")

	for _, tt := range []struct {
		name string
		path string
		want int
	}{
		{
			name: "bracketed year on the parent",
			path: filepath.Join("Spiderman (2002)", "Spiderman.mp4"),
			want: 2002,
		},
		{
			name: "parent year survives an edition suffix",
			path: filepath.Join("Aliens SE (1986)", "Aliens SE.mp4"),
			want: 1986,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(root, filepath.Join(root, tt.path), "movie")
			if got.Year != tt.want {
				t.Errorf("year = %d, want %d (info: %+v)", got.Year, tt.want, got)
			}
		})
	}
}

// The filename is the better evidence and keeps precedence: it names this file,
// where the folder may name a set. A disagreement resolves to the filename.
func TestFilenameYearBeatsTheFolderYear(t *testing.T) {
	root := filepath.Join("W", "Movies")
	path := filepath.Join(root, "Halloween (1978)", "Halloween II 1981.mkv")

	if got := Parse(root, path, "movie"); got.Year != 1981 {
		t.Errorf("year = %d, want 1981 — the filename states this film's year", got.Year)
	}
}

// A collection folder's year belongs to the set, not to any one film in it, and
// stamping it onto every child would manufacture a wrong year for all of them.
// Only the immediate parent is read, and a range is not a release year.
func TestCollectionFolderYearsAreNotInherited(t *testing.T) {
	root := filepath.Join("X", "Index")

	// A year range names a collection. Nothing in it is a 1986 film by virtue of
	// sitting there.
	loose := filepath.Join(root, "Alien(1986-2024)", "Alien.mkv")
	if got := Parse(root, loose, "movie"); got.Year != 0 {
		t.Errorf("year = %d, want 0 — a range is a collection, not a release year", got.Year)
	}

	// Two levels up is a collection even when it does parse as a year. Only the
	// immediate parent counts.
	nested := filepath.Join(root, "Marvel (2008)", "Cinematic", "Iron Man.mkv")
	if got := Parse(root, nested, "movie"); got.Year != 0 {
		t.Errorf("year = %d, want 0 — only the immediate parent may supply a year", got.Year)
	}
}

// The library root names the library. A film sitting loose in "Movies (2024)" is
// not a 2024 film.
func TestLibraryRootNeverSuppliesAYear(t *testing.T) {
	root := filepath.Join("D", "Movies (2024)")

	if got := Parse(root, filepath.Join(root, "Dredd.mp4"), "movie"); got.Year != 0 {
		t.Errorf("year = %d, want 0 — the root names the library, not the film", got.Year)
	}
}

// In a show library a bare episode marker ("Storm of the Century E2") is a TV
// episode of season 1, so it matches against TMDB TV rather than being taken for
// a same-named film. In a movie library the identical name stays a film — the
// gate that keeps an oddly-named movie from being torn into a fake series.
func TestParseLibraryKindBiasesEpisodes(t *testing.T) {
	root := filepath.Join("R", "TV")
	file := filepath.Join(root, "Storm of the Century", "Storm of the Century E2.mkv")

	inShow := Parse(root, file, "show")
	if inShow.Kind != KindEpisode || inShow.Series != "Storm of the Century" ||
		inShow.Season != 1 || inShow.Episode != 2 {
		t.Errorf("show library: got %+v, want episode S1E2 of Storm of the Century", inShow)
	}

	inMovie := Parse(root, file, "movie")
	if inMovie.Kind != KindMovie {
		t.Errorf("movie library: got kind %q, want movie (E2 must not become an episode)", inMovie.Kind)
	}

	// In a show library, Part and Chapter markers are episodes too — everything
	// there is television. In a movie library Part stays a film work.
	partFile := filepath.Join(root, "Storm of the Century", "Storm of the Century Part 2.mkv")
	if p := Parse(root, partFile, "show"); p.Kind != KindEpisode || p.Episode != 2 {
		t.Errorf("show library Part 2: got %+v, want episode 2", p)
	}
	if p := Parse(root, partFile, "movie"); p.Kind != KindMovie {
		t.Errorf("movie library Part 2: got kind %q, want movie work", p.Kind)
	}

	// The guard still holds inside a show library: a real film with an "e"+digit
	// in its name is not shredded into an episode.
	se7en := Parse(root, filepath.Join(root, "Se7en (1995).mkv"), "show")
	if se7en.Kind != KindMovie {
		t.Errorf("Se7en in a show library became %q, want movie", se7en.Kind)
	}
}

func TestIsVideo(t *testing.T) {
	yes := []string{"a.mkv", "b.MP4", "c.avi", "d.m2ts"}
	no := []string{"a.srt", "b.nfo", "c.jpg", "d", "e.mkv.part"}
	for _, p := range yes {
		if !IsVideo(p) {
			t.Errorf("IsVideo(%q) = false, want true", p)
		}
	}
	for _, p := range no {
		if IsVideo(p) {
			t.Errorf("IsVideo(%q) = true, want false", p)
		}
	}
}

// The gate asks what the library is for, not what the file is. Both mistakes
// are real: a movie library absorbing the MP3s in a soundtrack folder, and a
// music library indexing the MKV a band shipped with an album (ADR 0024).
func TestIsScannableFollowsTheLibraryKind(t *testing.T) {
	cases := []struct {
		path    string
		libKind string
		want    bool
	}{
		{"Aladdin (1992).mkv", LibraryMovie, true},
		{"Aladdin (1992).mkv", LibraryShow, true},
		{"soundtrack/01 A Whole New World.mp3", LibraryMovie, false},
		{"soundtrack/01 A Whole New World.mp3", LibraryShow, false},

		{"Artist/Album/01 Track.mp3", LibraryMusic, true},
		{"Artist/Album/01 Track.flac", LibraryMusic, true},
		{"Artist/Album/live at wembley.mkv", LibraryMusic, false},

		// An unrecognised kind behaves as every library did before music.
		{"Aladdin (1992).mkv", LibraryOther, true},
		{"01 Track.mp3", LibraryOther, false},
		{"Aladdin (1992).mkv", "", true},
	}
	for _, tc := range cases {
		if got := IsScannable(tc.path, tc.libKind); got != tc.want {
			t.Errorf("IsScannable(%q, %q) = %v, want %v", tc.path, tc.libKind, got, tc.want)
		}
	}
}

func TestIsAudio(t *testing.T) {
	for _, p := range []string{
		"a.mp3", "a.flac", "a.m4a", "a.aac", "a.ogg", "a.oga", "a.opus",
		"a.wav", "a.aiff", "a.aif", "a.wma", "a.alac",
		"A.MP3", "Track 01.FLAC",
	} {
		if !IsAudio(p) {
			t.Errorf("IsAudio(%q) = false, want true", p)
		}
	}
	for _, p := range []string{
		"a.mkv", "a.mp4", "a.jpg", "a.nfo", "a.txt", "a", "cover.jpg",
		// .ogv is video-in-Ogg and belongs to the video set, not this one.
		"a.ogv",
	} {
		if IsAudio(p) {
			t.Errorf("IsAudio(%q) = true, want false", p)
		}
	}
}

// The two sets must not overlap, or a file's classification depends on which
// question is asked first.
func TestVideoAndAudioExtensionsAreDisjoint(t *testing.T) {
	for ext := range audioExts {
		if videoExts[ext] {
			t.Errorf("%s is in both the video and audio extension sets", ext)
		}
	}
}

// ParseTrack is the *fallback* for music — tags outrank it (ADR 0024) — but it
// has to produce something sane when a rip carries none.
func TestParseTrack(t *testing.T) {
	const root = `/music`
	cases := []struct {
		path  string
		title string
		disc  int
		track int
		album string
	}{
		{`/music/Nine Inch Nails/The Downward Spiral/01 Mr Self Destruct.mp3`,
			"Mr Self Destruct", 0, 1, "The Downward Spiral"},
		{`/music/Portishead/Dummy/03. Strangers.flac`,
			"Strangers", 0, 3, "Dummy"},
		{`/music/Portishead/Dummy/04 - Roads.flac`,
			"Roads", 0, 4, "Dummy"},
		// Multi-disc: "1-02" is disc 1, track 2 — not a title beginning "1-".
		{`/music/The Beatles/White Album/2-04 Blackbird.flac`,
			"Blackbird", 2, 4, "White Album"},
		// Three-digit track numbers appear on long compilations.
		{`/music/Various/Big Box/112 Something.mp3`,
			"Something", 0, 112, "Big Box"},
		// No number at all: the whole stem is the title.
		{`/music/Aphex Twin/Selected Ambient Works/Xtal.flac`,
			"Xtal", 0, 0, "Selected Ambient Works"},
		// A track loose at the library root has no album to name.
		{`/music/Stray Song.mp3`, "Stray Song", 0, 0, ""},
	}

	for _, tc := range cases {
		got := ParseTrack(root, filepath.FromSlash(tc.path))
		if got.Kind != KindTrack {
			t.Errorf("%s: Kind = %q, want track", tc.path, got.Kind)
		}
		if got.Title != tc.title {
			t.Errorf("%s: Title = %q, want %q", tc.path, got.Title, tc.title)
		}
		if got.Episode != tc.track {
			t.Errorf("%s: track = %d, want %d", tc.path, got.Episode, tc.track)
		}
		if got.Season != tc.disc {
			t.Errorf("%s: disc = %d, want %d", tc.path, got.Season, tc.disc)
		}
		if got.Series != tc.album {
			t.Errorf("%s: album = %q, want %q", tc.path, got.Series, tc.album)
		}
	}
}

// A file named only by its number still has to show something.
func TestParseTrackNumberOnlyFilename(t *testing.T) {
	got := ParseTrack(`/music`, filepath.FromSlash(`/music/Artist/Album/07.mp3`))
	if got.Title != "Track 7" {
		t.Errorf("Title = %q, want %q", got.Title, "Track 7")
	}
}

// Parse routes to ParseTrack for a music library and must not apply any of the
// video heuristics — a year in an album name is not a release year to strip,
// and "S01E02" in a song title is not an episode.
func TestParseInMusicLibraryUsesTrackRules(t *testing.T) {
	got := Parse(`/music`, filepath.FromSlash(`/music/Artist/1999/02 Party.mp3`), LibraryMusic)
	if got.Kind != KindTrack {
		t.Fatalf("Kind = %q, want track", got.Kind)
	}
	if got.Series != "1999" {
		t.Errorf("album = %q, want %q — a numeric album name is not a year", got.Series, "1999")
	}
	if got.Year != 0 {
		t.Errorf("Year = %d, want 0 — music does not carry a release year from the path", got.Year)
	}
	if got.Episode != 2 {
		t.Errorf("track = %d, want 2", got.Episode)
	}
}

// A 4-digit stem is a title, not a track number — "1984" is an album or a
// song, and the 1-to-3 digit cap is what keeps it one.
func TestParseTrackLeavesFourDigitTitlesAlone(t *testing.T) {
	got := ParseTrack(`/music`, filepath.FromSlash(`/music/Artist/Album/1984.mp3`))
	if got.Title != "1984" {
		t.Errorf("Title = %q, want %q", got.Title, "1984")
	}
	if got.Episode != 0 {
		t.Errorf("track = %d, want 0 — a four-digit stem is not a track number", got.Episode)
	}
}

// A scene release group is a trailing marker on the folder and a leading one on
// the file. stripNoise only knew about trailing ones, so the group survived
// into the title — found on a real library as "veto beavis and butthead do
// america".
func TestLeadingReleaseGroupIsStrippedWhenTheFolderAgrees(t *testing.T) {
	path := filepath.FromSlash(
		"/lib/Beavis.And.Butthead.Do.America.1996.1080p.BluRay.x264-VETO/" +
			"veto-beavis.and.butthead.do.america.1996.1080p.bluray.x264.mkv")
	got := Parse(filepath.FromSlash("/lib"), path, "movie")
	// Lower case because the filename is: this parser does not title-case, and
	// the group is the only thing being removed here. A provider match supplies
	// the real casing later.
	if got.Title != "beavis and butthead do america" {
		t.Errorf("Title = %q, want the group gone", got.Title)
	}
	if got.Year != 1996 {
		t.Errorf("Year = %v, want 1996", got.Year)
	}
}

// The reason the folder has to agree. Nothing in the filename alone separates a
// group prefix from a hyphenated title, and getting this wrong renames the film
// to "Man".
func TestHyphenatedTitleIsNotMistakenForAReleaseGroup(t *testing.T) {
	path := filepath.FromSlash("/lib/Spider-Man (2002)/Spider-Man.2002.1080p.BluRay.x264.mkv")
	got := Parse(filepath.FromSlash("/lib"), path, "movie")
	if got.Title != "Spider Man" {
		t.Errorf("Title = %q, want %q", got.Title, "Spider Man")
	}
}

// A leading word that looks like a group but is not confirmed by the folder is
// left alone.
func TestLeadingWordSurvivesWhenTheFolderDoesNotAgree(t *testing.T) {
	path := filepath.FromSlash("/lib/Some Film (2010)/nonsense-some.film.2010.1080p.mkv")
	got := Parse(filepath.FromSlash("/lib"), path, "movie")
	if got.Title != "nonsense some film" {
		t.Errorf("Title = %q, want the name untouched", got.Title)
	}
}

/*
 * Quotes around a whole title are not part of it.
 *
 * A real library showed a film called `"Wuthering Heights"` — quotes included —
 * sorted to the very front of the grid, because a quote character orders before
 * every letter.
 */
func TestQuotedTitleLosesItsQuotes(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`"Wuthering Heights" (2026).mkv`, "Wuthering Heights"},
		{`'The Thing' (1982).mkv`, "The Thing"},
		{`“Nosferatu” (2024).mkv`, "Nosferatu"},
	} {
		got := Parse(editionRoot, filepath.Join(editionRoot, c.in), "movie")
		if got.Title != c.want {
			t.Errorf("Parse(%q).Title = %q, want %q", c.in, got.Title, c.want)
		}
	}
}

/*
 * A leading apostrophe that is part of the name stays.
 *
 * `'71` is a 2014 film. Trimming quote characters off both ends rather than
 * removing a matched pair would rename it to "71", which is the reason this is
 * a pair rule and not a trim.
 */
func TestUnpairedQuoteIsPartOfTheTitle(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"'71 (2014).mkv", "'71"},
		{"Rock 'n' Roll High School (1979).mkv", "Rock 'n' Roll High School"},
	} {
		got := Parse(editionRoot, filepath.Join(editionRoot, c.in), "movie")
		if got.Title != c.want {
			t.Errorf("Parse(%q).Title = %q, want %q", c.in, got.Title, c.want)
		}
	}
}

// An edition marker at the end of a title is an edition, not a name, so the
// file matches the work it is an edition of instead of matching nothing.
func TestEditionSuffixIsStripped(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Alien DC.mkv", "Alien"},
		{"Alien Resurrection SE.mkv", "Alien Resurrection"},
		{"Blade Runner - Final Cut.mkv", "Blade Runner"},
		{"Apocalypse Now Uncut.mkv", "Apocalypse Now"},
		{"Aliens Special Edition.mkv", "Aliens"},
		{"Watchmen Director's Cut.mkv", "Watchmen"},
	} {
		got := Parse(editionRoot, filepath.Join(editionRoot, c.in), "movie")
		if got.Title != c.want {
			t.Errorf("Parse(%q).Title = %q, want %q", c.in, got.Title, c.want)
		}
	}
}

/*
 * The same two letters at the *front* of a title are a name.
 *
 * This is why the rule is anchored to the end rather than added to reNoise,
 * which strips from a marker onward wherever it appears: "DC League of
 * Super-Pets" would have become an empty title.
 */
func TestEditionMarkerAtTheFrontIsATitle(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"DC League of Super-Pets (2022).mkv", "DC League of Super Pets"},
		{"SE7EN (1995).mkv", "SE7EN"},
		// A film actually called "Uncut" keeps its name rather than emptying.
		{"Uncut.mkv", "Uncut"},
	} {
		got := Parse(editionRoot, filepath.Join(editionRoot, c.in), "movie")
		if got.Title != c.want {
			t.Errorf("Parse(%q).Title = %q, want %q", c.in, got.Title, c.want)
		}
	}
}

/*
 * A parenthetical before the year keeps both of its brackets.
 *
 * The year is stripped first, leaving `Film (Alternate Cut) `, and clean's Trim
 * — whose cutset carries `)` for the `[2018]`-style leftovers it was written for
 * — then took the *inner* group's closing bracket along with the trailing space.
 * The surviving `Film (Alternate Cut` is not merely wrong on a tile: the title is
 * what goes to the provider as a search query, and TMDB returned zero results
 * for it where the same query without the fragment returned the right film first.
 *
 * General rather than an edition-marker quirk — the cause is the group's
 * position, not its contents.
 */
func TestParentheticalBeforeTheYearStaysBalanced(t *testing.T) {
	// A group that is *not* an edition marker survives intact — the edition
	// vocabulary removes those, and this rule is about the bracket, not the
	// contents.
	for _, c := range []struct{ in, want string }{
		{"Some Film (Japanese) (2000).mkv", "Some Film (Japanese)"},
		{"Some Film (Part 1) (2001).mkv", "Some Film (Part 1)"},
		{"Some Film (Criterion) (1998).mkv", "Some Film (Criterion)"},
	} {
		got := Parse(editionRoot, filepath.Join(editionRoot, c.in), "movie")
		if got.Title != c.want {
			t.Errorf("Parse(%q).Title = %q, want %q", c.in, got.Title, c.want)
		}
	}
}

/*
 * The bracket is restored, not the group discarded.
 *
 * Discarding looks tempting because such a group is usually a qualifier, but
 * `Birdman or (The Unexpected Virtue of Ignorance)` is a real title whose
 * brackets are part of its name and which arrives in exactly the same shape. A
 * rule that dropped the group would fix the file above by corrupting this one —
 * which an earlier attempt at this fix did, turning it into "Birdman or".
 */
func TestBracketsThatAreTheTitleSurvive(t *testing.T) {
	const in = "Birdman or (The Unexpected Virtue of Ignorance) (2014).mkv"
	const want = "Birdman or (The Unexpected Virtue of Ignorance)"

	got := Parse(editionRoot, filepath.Join(editionRoot, in), "movie")
	if got.Title != want {
		t.Errorf("Parse(%q).Title = %q, want %q", in, got.Title, want)
	}
	if got.Year != 2014 {
		t.Errorf("Year = %d, want 2014", got.Year)
	}
}

/*
 * An edition marker in brackets is still an edition.
 *
 * The suffix rule was anchored to a bare marker at the end, so
 * `(Alternate Cut)` slipped past it twice over: the vocabulary did not carry the
 * phrase, and the closing bracket meant the marker was not last even when it did.
 *
 * This is the one that cost a real library a match. From its provider cache:
 *
 *	query=Spider+Man+Into+the+Spider+Verse+%28Alternate+Cut   0 results
 *	query=Spider+Man+Into+the+Spider+Verse                    the right film, first
 */
func TestBracketedEditionMarkerIsStripped(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Spider-Man Into the Spider-Verse (Alternate Cut) (2018).mkv",
			"Spider Man Into the Spider Verse"},
		{"Some Film (Uncut) (1999).mkv", "Some Film"},
		{"Some Film [Director's Cut] (1999).mkv", "Some Film"},
	} {
		got := Parse(editionRoot, filepath.Join(editionRoot, c.in), "movie")
		if got.Title != c.want {
			t.Errorf("Parse(%q).Title = %q, want %q", c.in, got.Title, c.want)
		}
	}
}

/*
 * A title that *ends* with a vocabulary word keeps it.
 *
 * "The Final Cut" is a 2004 film and `final cut` is in the vocabulary, so the
 * rule reduced it to "The" — a title matching nothing, sorted into the Ts. The
 * empty-string guard could not catch it, because "The" is not empty.
 *
 * The guard is on what survives rather than on the vocabulary: an article with
 * no noun behind it is not a title anybody has, so its appearance is proof the
 * marker was part of the name.
 */
func TestEditionStripNeverLeavesOnlyAnArticle(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"The Final Cut (2004).mkv", "The Final Cut"},
		{"Uncut Gems (2019).mkv", "Uncut Gems"},
		{"DC League of Super-Pets (2022).mkv", "DC League of Super Pets"},
	} {
		got := Parse(editionRoot, filepath.Join(editionRoot, c.in), "movie")
		if got.Title != c.want {
			t.Errorf("Parse(%q).Title = %q, want %q", c.in, got.Title, c.want)
		}
	}
}

// editionRoot is a library root for the title-parsing cases above; the files
// sit directly in it, so nothing above them influences the parse.
var editionRoot = filepath.Join("R", "Movies")

/*
 * The edition marker, kept rather than discarded (ADR 0042).
 *
 * The strip has always happened; what is new is that the finding survives. Both
 * halves are asserted together because they have to agree: a strip that
 * happened with no marker recorded leaves two identical rows, and a marker
 * recorded with no strip labels a film with part of its own name.
 */
func TestSplitEdition(t *testing.T) {
	cases := []struct {
		in      string
		title   string
		edition string
	}{
		// The motivating file. It was a byte-for-byte copy of the theatrical
		// cut, which is exactly why the marker is a label and not a key.
		{"Spider-Man Into the Spider-Verse (Alternate Cut)", "Spider-Man Into the Spider-Verse", "Alternate Cut"},
		{"Alien DC", "Alien", "DC"},
		{"Blade Runner (Director's Cut)", "Blade Runner", "Director's Cut"},
		{"Dune [Extended Edition]", "Dune", "Extended Edition"},

		// No marker: the title is the title and the edition is empty.
		{"Fight Club", "Fight Club", ""},

		/*
		 * The refusals, which must yield no marker either. Refusing to strip
		 * means the words were part of the title -- and a title is not an
		 * edition of itself. "The Final Cut" is a 2004 film; the older strip
		 * reduced it to "The".
		 */
		{"The Final Cut", "The Final Cut", ""},
		{"Uncut", "Uncut", ""},
	}
	for _, c := range cases {
		title, edition := splitEdition(c.in)
		if title != c.title || edition != c.edition {
			t.Errorf("splitEdition(%q) = (%q, %q), want (%q, %q)",
				c.in, title, edition, c.title, c.edition)
		}
	}
}

// The marker is shown to a person, so the file's own spelling has to survive.
// The vocabulary is matched case-insensitively for exactly this reason.
func TestEditionKeepsTheFilesOwnSpelling(t *testing.T) {
	for _, in := range []string{"Alien (Director's Cut)", "Alien (DIRECTOR'S CUT)"} {
		_, edition := splitEdition(in)
		if edition == "" {
			t.Fatalf("splitEdition(%q) found no edition", in)
		}
		if edition == "directors cut" {
			t.Errorf("splitEdition(%q) normalised the marker to %q", in, edition)
		}
	}
}

// stripEditionSuffix is now splitEdition's first return, and every existing
// caller depends on it behaving exactly as it did.
func TestStripEditionSuffixIsUnchanged(t *testing.T) {
	for in, want := range map[string]string{
		"Alien DC":                "Alien",
		"The Final Cut":           "The Final Cut",
		"Fight Club":              "Fight Club",
		"Dune [Extended Edition]": "Dune",
	} {
		if got := stripEditionSuffix(in); got != want {
			t.Errorf("stripEditionSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}
