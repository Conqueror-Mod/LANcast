package store

import (
	"context"
	"path/filepath"
	"testing"
)

/*
 * A re-upsert never overwrites a locked field (R04-1).
 *
 * The scanner calls UpsertItem for a file that comes back after being marked
 * missing and for a file whose size or mtime moved. Both used to rewrite every
 * identity column from the filename guess, so an unplugged drive or a re-muxed
 * file quietly undid a person's correction — and, because metadata_updated_at
 * was left stamped, enrichment never came back to repair even an unlocked one.
 *
 * A rescan reconciles files; it does not re-litigate identity.
 */

type upsertFixture struct {
	st   *Store
	lib  *Library
	root string
}

func newUpsertFixture(t *testing.T, kind string) upsertFixture {
	t.Helper()
	st := openTestStore(t)
	root := t.TempDir()
	lib, err := st.CreateLibrary(context.Background(), "Lib", kind, root)
	if err != nil {
		t.Fatal(err)
	}
	return upsertFixture{st: st, lib: lib, root: root}
}

func ip(v int) *int       { return &v }
func sp(v string) *string { return &v }

type identity struct {
	Kind      string
	Title     string
	SortTitle string
	Year      *int
	Series    *string
	Season    *int
	Episode   *int
	Size      int64
	MTime     int64
	Missing   bool
	Probed    bool
}

func readIdentity(t *testing.T, st *Store, id int64) identity {
	t.Helper()
	var r identity
	var probed *int64
	if err := st.db.QueryRow(`
		SELECT kind, title, sort_title, year, series, season, episode,
		       size_bytes, mtime, missing, probed_at
		  FROM media_item WHERE id = ?`, id).Scan(
		&r.Kind, &r.Title, &r.SortTitle, &r.Year, &r.Series, &r.Season, &r.Episode,
		&r.Size, &r.MTime, &r.Missing, &probed); err != nil {
		t.Fatal(err)
	}
	r.Probed = probed != nil
	return r
}

func intEq(p *int, v int) bool { return p != nil && *p == v }

func show[T any](p *T) any {
	if p == nil {
		return "NULL"
	}
	return *p
}

// The two ways a scan reaches the update branch for a file it already knows.
var reupsertTriggers = []struct {
	name  string
	apply func(t *testing.T, st *Store, id int64, f *ScanFile)
}{
	{"missing then back", func(t *testing.T, st *Store, id int64, f *ScanFile) {
		// The drive is unplugged for one scan; the file returns byte-identical.
		if err := st.MarkMissing(context.Background(), []int64{id}); err != nil {
			t.Fatal(err)
		}
	}},
	{"size and mtime changed", func(t *testing.T, st *Store, id int64, f *ScanFile) {
		// Re-muxed, tags edited, or replaced with a better copy at the same path.
		f.SizeBytes += 900
		f.MTime += 60
	}},
}

func TestAReUpsertNeverOverwritesALockedMovieField(t *testing.T) {
	for _, tr := range reupsertTriggers {
		t.Run(tr.name, func(t *testing.T) {
			ctx := context.Background()
			fx := newUpsertFixture(t, "movie")
			f := ScanFile{LibraryID: fx.lib.ID, Kind: "movie",
				Path:  filepath.Join(fx.root, "alien.1979.mkv"),
				Title: "alien", SortTitle: "alien", Year: ip(1979),
				Container: "mkv", SizeBytes: 100, MTime: 1}
			id, err := fx.st.UpsertItem(ctx, f)
			if err != nil {
				t.Fatal(err)
			}

			// A person corrects title, sort title and year; each edit locks.
			if err := fx.st.UpdateItemMetadata(ctx, id, ItemMetadata{
				Title: sp("Alien (Director's Cut, my edit)"), SortTitle: sp("Alien 1"), Year: ip(2003),
			}); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"title", "sort_title", "year"} {
				if err := fx.st.LockField(ctx, id, field); err != nil {
					t.Fatal(err)
				}
			}

			tr.apply(t, fx.st, id, &f)
			if _, err := fx.st.UpsertItem(ctx, f); err != nil {
				t.Fatal(err)
			}

			got := readIdentity(t, fx.st, id)
			if got.Title != "Alien (Director's Cut, my edit)" {
				t.Errorf("locked title overwritten by the re-upsert: now %q", got.Title)
			}
			if got.SortTitle != "Alien 1" {
				t.Errorf("locked sort_title overwritten: now %q", got.SortTitle)
			}
			if !intEq(got.Year, 2003) {
				t.Errorf("locked year overwritten: now %v", show(got.Year))
			}
			// The file facts are still reconciled.
			if got.Missing {
				t.Error("missing = 1 after the upsert, want 0")
			}
			if got.Size != f.SizeBytes || got.MTime != f.MTime {
				t.Errorf("size/mtime = %d/%d, want %d/%d", got.Size, got.MTime, f.SizeBytes, f.MTime)
			}
		})
	}
}

func TestAReUpsertNeverOverwritesALockedEpisodeField(t *testing.T) {
	for _, tr := range reupsertTriggers {
		t.Run(tr.name, func(t *testing.T) {
			ctx := context.Background()
			fx := newUpsertFixture(t, "show")
			f := ScanFile{LibraryID: fx.lib.ID, Kind: "episode",
				Path:  filepath.Join(fx.root, "Show", "Show.S01E02.mkv"),
				Title: "Show", SortTitle: "show", Series: sp("Show"), Season: ip(1), Episode: ip(2),
				Container: "mkv", SizeBytes: 100, MTime: 1}
			id, err := fx.st.UpsertItem(ctx, f)
			if err != nil {
				t.Fatal(err)
			}

			// The file is misnumbered; somebody fixes series, season and episode.
			if err := fx.st.UpdateItemMetadata(ctx, id, ItemMetadata{
				Title: sp("The Real Pilot"), Series: sp("The Show (2004)"), Season: ip(0), Episode: ip(1),
			}); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"title", "series", "season", "episode"} {
				if err := fx.st.LockField(ctx, id, field); err != nil {
					t.Fatal(err)
				}
			}

			tr.apply(t, fx.st, id, &f)
			if _, err := fx.st.UpsertItem(ctx, f); err != nil {
				t.Fatal(err)
			}

			got := readIdentity(t, fx.st, id)
			if got.Title != "The Real Pilot" {
				t.Errorf("locked title overwritten: now %q", got.Title)
			}
			if got.Series == nil || *got.Series != "The Show (2004)" {
				t.Errorf("locked series overwritten: now %v", show(got.Series))
			}
			if !intEq(got.Season, 0) || !intEq(got.Episode, 1) {
				t.Errorf("locked season/episode overwritten: now S%v E%v", show(got.Season), show(got.Episode))
			}
		})
	}
}

// Locking one field must not freeze the rest of an unmatched row: its other
// identity columns are still only a filename guess, and a better guess (a newer
// parser, say) is allowed to replace them.
func TestALockOnOneFieldLeavesTheOthersToTheGuess(t *testing.T) {
	ctx := context.Background()
	fx := newUpsertFixture(t, "movie")
	f := ScanFile{LibraryID: fx.lib.ID, Kind: "movie",
		Path:  filepath.Join(fx.root, "alien.1979.mkv"),
		Title: "alien", SortTitle: "alien", Year: ip(1979),
		Container: "mkv", SizeBytes: 100, MTime: 1}
	id, err := fx.st.UpsertItem(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if err := fx.st.UpdateItemMetadata(ctx, id, ItemMetadata{Title: sp("Mine")}); err != nil {
		t.Fatal(err)
	}
	if err := fx.st.LockField(ctx, id, "title"); err != nil {
		t.Fatal(err)
	}

	f.Year = ip(1980)
	f.SizeBytes, f.MTime = 200, 2
	if _, err := fx.st.UpsertItem(ctx, f); err != nil {
		t.Fatal(err)
	}

	got := readIdentity(t, fx.st, id)
	if got.Title != "Mine" {
		t.Errorf("locked title = %q, want Mine", got.Title)
	}
	if !intEq(got.Year, 1980) {
		t.Errorf("unlocked year on an unmatched row = %v, want the new guess 1980", show(got.Year))
	}
}

/*
 * A matched row keeps its identity when a file only reappears or changes bytes.
 *
 * The provider's title is unlocked, so the lock check alone would hand it back
 * to the filename guess — and enrichment would never repair it, because the
 * row is still stamped. Matching is what settled who this file is; a rescan
 * that sees the same path again has learned nothing that unsettles it.
 */
func TestAReUpsertKeepsAMatchedRowsIdentity(t *testing.T) {
	for _, state := range []string{"matched", "locked"} {
		for _, tr := range reupsertTriggers {
			t.Run(state+"/"+tr.name, func(t *testing.T) {
				ctx := context.Background()
				fx := newUpsertFixture(t, "movie")
				f := ScanFile{LibraryID: fx.lib.ID, Kind: "movie",
					Path:  filepath.Join(fx.root, "the.matrx.mkv"),
					Title: "the matrx", SortTitle: "matrx",
					Container: "mkv", SizeBytes: 100, MTime: 1}
				id, err := fx.st.UpsertItem(ctx, f)
				if err != nil {
					t.Fatal(err)
				}
				if err := fx.st.UpdateItemMetadata(ctx, id, ItemMetadata{
					Title: sp("The Matrix"), SortTitle: sp("Matrix"), Year: ip(1999),
					Provider: sp("tmdb"), ExternalID: sp("603"), MatchState: sp(state),
				}); err != nil {
					t.Fatal(err)
				}

				tr.apply(t, fx.st, id, &f)
				if _, err := fx.st.UpsertItem(ctx, f); err != nil {
					t.Fatal(err)
				}

				got := readIdentity(t, fx.st, id)
				if got.Title != "The Matrix" || got.SortTitle != "Matrix" || !intEq(got.Year, 1999) {
					t.Errorf("matched identity re-guessed from the filename: %q / %q / %v",
						got.Title, got.SortTitle, show(got.Year))
				}
				if got.Missing {
					t.Error("missing = 1 after the upsert, want 0")
				}
				if got.Probed {
					t.Error("probed_at kept, want NULL so the new bytes are probed")
				}
			})
		}
	}
}

// A file whose classification moved families (re-typing a library, a parser
// fix) is the one re-upsert that is meant to re-derive identity: the scanner
// clears the metadata stamp so it is matched again. The guess wins for every
// unlocked field — and a locked one still does not move.
func TestAReinterpretedFileTakesTheNewGuessExceptWhereLocked(t *testing.T) {
	ctx := context.Background()
	fx := newUpsertFixture(t, "show")
	f := ScanFile{LibraryID: fx.lib.ID, Kind: "movie",
		Path:  filepath.Join(fx.root, "Show", "Show Part 2.mkv"),
		Title: "Show Part 2", SortTitle: "show part 2",
		Container: "mkv", SizeBytes: 100, MTime: 1}
	id, err := fx.st.UpsertItem(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if err := fx.st.UpdateItemMetadata(ctx, id, ItemMetadata{
		Title: sp("Some Film"), Year: ip(2001), MatchState: sp("matched"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := fx.st.LockField(ctx, id, "year"); err != nil {
		t.Fatal(err)
	}

	f.Kind = "episode"
	f.Title, f.SortTitle = "Show", "show"
	f.Series, f.Season, f.Episode = sp("Show"), ip(1), ip(2)
	if _, err := fx.st.UpsertItem(ctx, f); err != nil {
		t.Fatal(err)
	}

	got := readIdentity(t, fx.st, id)
	if got.Kind != "episode" {
		t.Fatalf("kind = %q, want episode", got.Kind)
	}
	if got.Title != "Show" || got.Series == nil || *got.Series != "Show" || !intEq(got.Episode, 2) {
		t.Errorf("reinterpreted row kept its old identity: %q series=%v ep=%v",
			got.Title, show(got.Series), show(got.Episode))
	}
	if !intEq(got.Year, 2001) {
		t.Errorf("locked year = %v, want 2001 even across a reinterpretation", show(got.Year))
	}
}
