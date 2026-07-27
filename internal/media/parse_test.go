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
			got := Parse(root, filepath.Join(root, tt.path))
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
