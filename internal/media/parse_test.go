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
