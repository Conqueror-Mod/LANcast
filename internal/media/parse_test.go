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
