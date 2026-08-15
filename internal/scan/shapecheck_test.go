package scan

import (
	"context"
	"testing"

	"lancast/internal/store"
)

/*
 * The rules, one named case each — the model docs/adr and decide_test.go set.
 *
 * These assert the *verdict and its code*, not just that something was said.
 * A check that fires with the wrong explanation is worse than one that stays
 * quiet: the whole point is that kind is immutable, so the sentence somebody
 * reads here decides whether they remove and re-add a library or shrug.
 */
func TestShapeCheck(t *testing.T) {
	cases := []struct {
		name     string
		kind     string
		shape    store.LibraryShape
		progress Progress
		want     string // empty means no warning
	}{
		{
			name:  "a healthy shows library says nothing",
			kind:  "show",
			shape: store.LibraryShape{Shows: 3, Seasons: 3, Episodes: 15, Total: 21},
			want:  "",
		},
		{
			// The measured case from the roadmap: the same folder scanned as
			// `movie` produced 12 episodes-as-films plus a miniseries read as
			// one film in three parts.
			name:     "a shows library scanned as movies is caught",
			kind:     "movie",
			shape:    store.LibraryShape{Movies: 13, Parts: 3, Total: 16},
			progress: Progress{EpisodesInMovieLibrary: 12},
			want:     "episodes_in_movie_library",
		},
		{
			name:  "a shows library that produced no shows is caught",
			kind:  "show",
			shape: store.LibraryShape{Movies: 15, Total: 15},
			want:  "no_shows_in_show_library",
		},
		{
			// A shows library legitimately holds a few loose files — an extras
			// folder, a documentary beside a series. One show is enough to say
			// the kind was chosen correctly.
			name:  "one show is enough to be a shows library",
			kind:  "show",
			shape: store.LibraryShape{Shows: 1, Seasons: 2, Episodes: 20, Movies: 4, Total: 27},
			want:  "",
		},
		{
			// Films named like episodes happen — a series of numbered entries,
			// a box set. A handful is not a wrong library, and a check that
			// cries wolf is a check that gets ignored.
			name:     "a few episode-shaped names in a film library are tolerated",
			kind:     "movie",
			shape:    store.LibraryShape{Movies: 40, Total: 40},
			progress: Progress{EpisodesInMovieLibrary: 3},
			want:     "",
		},
		{
			// Proportions mean nothing at this size: two files, both episodes,
			// is a folder somebody is still filling.
			name:     "a nearly empty library is not judged",
			kind:     "movie",
			shape:    store.LibraryShape{Movies: 2, Total: 2},
			progress: Progress{EpisodesInMovieLibrary: 2},
			want:     "",
		},
		{
			name:  "an empty shows library is not judged either",
			kind:  "show",
			shape: store.LibraryShape{},
			want:  "",
		},
		{
			// The audio-versus-video case: nothing imported, plenty
			// discarded. "0 items · scanned" reads as success, so it is said
			// out loud.
			name:     "a library that imported nothing while discarding much is caught",
			kind:     "movie",
			shape:    store.LibraryShape{},
			progress: Progress{SkippedKind: 1592},
			want:     "everything_skipped_for_kind",
		},
		{
			// A music library is not judged on shows or episodes — those rules
			// do not apply to it, and inventing a third rule to have one would
			// be a rule with no failure behind it.
			name:  "a music library is left alone",
			kind:  "music",
			shape: store.LibraryShape{Tracks: 1592, Total: 1592},
			want:  "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckShape(tc.kind, tc.shape, tc.progress)
			if got.Code != tc.want {
				t.Errorf("code = %q, want %q (message: %q)", got.Code, tc.want, got.Message)
			}
			if tc.want == "" {
				return
			}
			if got.Message == "" {
				t.Error("a warning with no message is a warning nobody can act on")
			}
			// Kind cannot be changed, so every warning has to say what the
			// actual remedy is. Implying a settings toggle exists would send
			// somebody hunting for one.
			if got.Remedy == "" {
				t.Error("a warning about an immutable property must say what can be done instead")
			}
		})
	}
}

// The counts come from the database, so the rule must not accidentally depend
// on Total being the sum of the fields it was given — a library with kinds this
// struct does not enumerate still has a Total.
func TestShapeCheckUsesTotalNotTheSumOfParts(t *testing.T) {
	w := CheckShape("show", store.LibraryShape{Total: 30}, Progress{})
	if w.Code != "no_shows_in_show_library" {
		t.Errorf("code = %q; a shows library with 30 items and no shows is the case this exists for", w.Code)
	}
}

/*
 * The check against a real scan, not a struct literal.
 *
 * The unit table above proves the rules; this proves they are reached — that a
 * finished scan carries the verdict, and that the counts feeding it come from
 * what the scanner actually produced rather than from what the walk thought it
 * saw. Those are different numbers, and the gap between them is where this
 * feature would silently do nothing.
 */
func TestScanReportsAWrongLookingLibrary(t *testing.T) {
	sc, st := newScanner(t)
	dir := t.TempDir()

	// A season of television, filed as a film library.
	for _, name := range []string{
		"Some Show/Season 01/Some.Show.S01E01.mkv",
		"Some Show/Season 01/Some.Show.S01E02.mkv",
		"Some Show/Season 01/Some.Show.S01E03.mkv",
		"Some Show/Season 01/Some.Show.S01E04.mkv",
		"Some Show/Season 01/Some.Show.S01E05.mkv",
		"Some Show/Season 01/Some.Show.S01E06.mkv",
	} {
		writeFile(t, dir, name, 1024)
	}

	lib, err := st.CreateLibrary(context.Background(), "Films", "movie", dir)
	if err != nil {
		t.Fatal(err)
	}

	p := scanAndWait(t, sc, *lib)
	if p.ShapeWarning == nil {
		t.Fatalf("no shape warning; a season of episodes scanned as films is the case this exists for (episodes seen: %d)",
			p.EpisodesInMovieLibrary)
	}
	if p.ShapeWarning.Code != "episodes_in_movie_library" {
		t.Errorf("code = %q, want episodes_in_movie_library", p.ShapeWarning.Code)
	}
}

// The other side of the same guarantee: an ordinary library finishes with
// nothing to say. A check that fires on a healthy scan would be turned off
// within a day, and then it would be there for nothing.
func TestScanIsQuietAboutARightLookingLibrary(t *testing.T) {
	sc, st := newScanner(t)
	dir := t.TempDir()

	for _, name := range []string{
		"Arrival (2016)/Arrival.2016.mkv",
		"Antz (1998)/Antz.1998.mkv",
		"Ted (2012)/Ted.2012.mkv",
		"Tank Girl (1995)/Tank.Girl.1995.mkv",
		"Toy Story (1995)/Toy.Story.1995.mkv",
		"Aladdin (1992)/Aladdin.1992.mkv",
	} {
		writeFile(t, dir, name, 1024)
	}

	lib, err := st.CreateLibrary(context.Background(), "Films", "movie", dir)
	if err != nil {
		t.Fatal(err)
	}

	if p := scanAndWait(t, sc, *lib); p.ShapeWarning != nil {
		t.Errorf("warned about a healthy film library: %s", p.ShapeWarning.Message)
	}
}
