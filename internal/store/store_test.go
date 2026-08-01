package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func mustLibrary(t *testing.T, st *Store) *Library {
	t.Helper()
	lib, err := st.CreateLibrary(context.Background(), "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	return lib
}

func file(libID int64, path, title string) ScanFile {
	return ScanFile{
		LibraryID: libID, Path: path, Kind: "movie",
		Title: title, SortTitle: title,
		Container: "mkv", SizeBytes: 1000, MTime: 500,
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	for i := 0; i < 3; i++ {
		st, err := Open(path)
		if err != nil {
			t.Fatalf("Open #%d: %v", i, err)
		}
		st.Close()
	}
}

func TestCreateAndListLibraries(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)

	lib := mustLibrary(t, st)
	if lib.ID == 0 {
		t.Fatal("CreateLibrary returned zero id")
	}

	libs, err := st.ListLibraries(ctx)
	if err != nil {
		t.Fatalf("ListLibraries: %v", err)
	}
	if len(libs) != 1 || libs[0].Name != "Films" {
		t.Fatalf("ListLibraries = %+v, want one library named Films", libs)
	}
	if libs[0].ItemCount != 0 {
		t.Errorf("ItemCount = %d, want 0", libs[0].ItemCount)
	}

	if _, err := st.GetLibrary(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetLibrary(9999) error = %v, want ErrNotFound", err)
	}
}

func TestDuplicateLibraryPathRejected(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	dir := t.TempDir()

	if _, err := st.CreateLibrary(ctx, "A", "movie", dir); err != nil {
		t.Fatalf("first CreateLibrary: %v", err)
	}
	if _, err := st.CreateLibrary(ctx, "B", "movie", dir); err == nil {
		t.Fatal("second CreateLibrary on same path succeeded, want unique violation")
	}
}

func TestUpsertItemIsIdempotent(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	f := file(lib.ID, `C:\m\a.mkv`, "A")
	for i := 0; i < 3; i++ {
		if _, err := st.UpsertItem(ctx, f); err != nil {
			t.Fatalf("UpsertItem #%d: %v", i, err)
		}
	}

	items, total, err := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID})
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	// Upserting the same path repeatedly must not create duplicate rows.
	if total != 1 || len(items) != 1 {
		t.Fatalf("total=%d len=%d, want 1 row after repeated upsert", total, len(items))
	}
}

func TestKnownFilesRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	if _, err := st.UpsertItem(ctx, file(lib.ID, `C:\m\a.mkv`, "A")); err != nil {
		t.Fatal(err)
	}

	known, err := st.KnownFiles(ctx, lib.ID)
	if err != nil {
		t.Fatalf("KnownFiles: %v", err)
	}
	got, ok := known[`C:\m\a.mkv`]
	if !ok {
		t.Fatalf("KnownFiles missing path; got %+v", known)
	}
	if got.SizeBytes == nil || *got.SizeBytes != 1000 {
		t.Errorf("SizeBytes = %v, want 1000", got.SizeBytes)
	}
	if got.MTime == nil || *got.MTime != 500 {
		t.Errorf("MTime = %v, want 500", got.MTime)
	}
}

// Rows are marked missing, never deleted — an unmounted drive must not destroy
// library data. This is the behavior that protects watch history.
func TestMarkMissingPreservesRowAndProgress(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	if _, err := st.UpsertItem(ctx, file(lib.ID, `C:\m\a.mkv`, "A")); err != nil {
		t.Fatal(err)
	}
	known, _ := st.KnownFiles(ctx, lib.ID)
	id := known[`C:\m\a.mkv`].ID

	if err := st.SaveProgress(ctx, id, "local", 4242, false); err != nil {
		t.Fatalf("SaveProgress: %v", err)
	}
	if err := st.MarkMissing(ctx, []int64{id}); err != nil {
		t.Fatalf("MarkMissing: %v", err)
	}

	it, err := st.GetItem(ctx, id, "local")
	if err != nil {
		t.Fatalf("GetItem after MarkMissing: %v", err)
	}
	if !it.Missing {
		t.Error("Missing = false, want true")
	}
	if it.Progress == nil || it.Progress.PositionMS != 4242 {
		t.Errorf("Progress = %+v, want position 4242 preserved", it.Progress)
	}
}

func TestContinueWatching(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	id := func(path, title string) int64 {
		if _, err := st.UpsertItem(ctx, file(lib.ID, path, title)); err != nil {
			t.Fatal(err)
		}
		known, _ := st.KnownFiles(ctx, lib.ID)
		return known[path].ID
	}

	a := id(`C:\m\a.mkv`, "A") // in progress
	b := id(`C:\m\b.mkv`, "B") // in progress
	c := id(`C:\m\c.mkv`, "C") // finished — excluded
	d := id(`C:\m\d.mkv`, "D") // never started — excluded
	e := id(`C:\m\e.mkv`, "E") // in progress but missing — excluded

	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(st.SaveProgress(ctx, a, "local", 1000, false))
	must(st.SaveProgress(ctx, b, "local", 2000, false))
	must(st.SaveProgress(ctx, c, "local", 3000, true)) // watched
	must(st.SaveProgress(ctx, d, "local", 0, false))   // zero position
	must(st.SaveProgress(ctx, e, "local", 1500, false))
	must(st.MarkMissing(ctx, []int64{e}))

	got, err := st.ContinueWatching(ctx, "local", 20)
	if err != nil {
		t.Fatalf("ContinueWatching: %v", err)
	}

	titles := map[string]bool{}
	for _, it := range got {
		titles[it.Title] = true
		if it.Progress == nil || it.Progress.PositionMS == 0 {
			t.Errorf("%s returned without progress attached: %+v", it.Title, it.Progress)
		}
	}
	if len(got) != 2 || !titles["A"] || !titles["B"] {
		t.Fatalf("ContinueWatching returned %v, want exactly {A, B}", titles)
	}

	// A different user shares no progress.
	other, err := st.ContinueWatching(ctx, "someone-else", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Errorf("another user saw %d in-progress items, want 0", len(other))
	}
}

func TestDeleteLibraryCascadesAndSpares(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)

	keep := mustLibrary(t, st)
	gone, err := st.CreateLibrary(ctx, "Doomed", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	st.UpsertItem(ctx, file(keep.ID, `C:\k\a.mkv`, "A"))
	st.UpsertItem(ctx, file(gone.ID, `C:\g\b.mkv`, "B"))
	knownGone, _ := st.KnownFiles(ctx, gone.ID)
	goneItem := knownGone[`C:\g\b.mkv`].ID
	if err := st.SaveProgress(ctx, goneItem, "local", 500, false); err != nil {
		t.Fatal(err)
	}

	if err := st.DeleteLibrary(ctx, gone.ID); err != nil {
		t.Fatalf("DeleteLibrary: %v", err)
	}

	// The library is gone, and its item cascaded away with it.
	if _, err := st.GetLibrary(ctx, gone.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetLibrary(deleted) = %v, want ErrNotFound", err)
	}
	if items, _, _ := st.ListItems(ctx, ItemFilter{LibraryID: gone.ID}); len(items) != 0 {
		t.Errorf("deleted library still has %d items", len(items))
	}
	// The other library is untouched.
	if _, err := st.GetLibrary(ctx, keep.ID); err != nil {
		t.Errorf("kept library was affected: %v", err)
	}
	if items, _, _ := st.ListItems(ctx, ItemFilter{LibraryID: keep.ID}); len(items) != 1 {
		t.Errorf("kept library items = %d, want 1", len(items))
	}
	// Deleting a nonexistent library is a clear not-found.
	if err := st.DeleteLibrary(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteLibrary(9999) = %v, want ErrNotFound", err)
	}
}

func TestMarkMissingEmptyIsNoOp(t *testing.T) {
	if err := newStore(t).MarkMissing(context.Background(), nil); err != nil {
		t.Fatalf("MarkMissing(nil) = %v, want nil", err)
	}
}

// A file reappearing after being marked missing must clear the flag.
func TestUpsertClearsMissing(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	f := file(lib.ID, `C:\m\a.mkv`, "A")
	st.UpsertItem(ctx, f)
	known, _ := st.KnownFiles(ctx, lib.ID)
	id := known[`C:\m\a.mkv`].ID
	st.MarkMissing(ctx, []int64{id})

	if _, err := st.UpsertItem(ctx, f); err != nil {
		t.Fatal(err)
	}
	it, _ := st.GetItem(ctx, id, "local")
	if it.Missing {
		t.Error("Missing = true after re-upsert, want false")
	}
}

func TestListItemsFilterAndSort(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	y1999, y2016 := 1999, 2016
	series := "Andor"
	season, episode := 1, 7

	st.UpsertItem(ctx, ScanFile{LibraryID: lib.ID, Path: `C:\m\matrix.mkv`, Kind: "movie",
		Title: "The Matrix", SortTitle: "matrix", Year: &y1999, Container: "mkv"})
	st.UpsertItem(ctx, ScanFile{LibraryID: lib.ID, Path: `C:\m\arrival.mkv`, Kind: "movie",
		Title: "Arrival", SortTitle: "arrival", Year: &y2016, Container: "mkv"})
	st.UpsertItem(ctx, ScanFile{LibraryID: lib.ID, Path: `C:\m\andor.mkv`, Kind: "episode",
		Title: "Announcement", SortTitle: "andor", Series: &series, Season: &season,
		Episode: &episode, Container: "mkv"})

	t.Run("kind filter", func(t *testing.T) {
		items, total, err := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID, Kind: "episode"})
		if err != nil {
			t.Fatal(err)
		}
		if total != 1 || items[0].Title != "Announcement" {
			t.Errorf("got total=%d items=%+v, want the single episode", total, items)
		}
	})

	t.Run("query matches title", func(t *testing.T) {
		_, total, err := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID, Query: "Matrix"})
		if err != nil {
			t.Fatal(err)
		}
		if total != 1 {
			t.Errorf("total = %d, want 1", total)
		}
	})

	t.Run("query matches series", func(t *testing.T) {
		_, total, err := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID, Query: "Andor"})
		if err != nil {
			t.Fatal(err)
		}
		if total != 1 {
			t.Errorf("total = %d, want 1 (series match)", total)
		}
	})

	t.Run("sort by title uses sort_title", func(t *testing.T) {
		items, _, err := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID})
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"andor", "arrival", "matrix"}
		for i, w := range want {
			if items[i].SortTitle != w {
				t.Errorf("position %d = %q, want %q (order: %+v)", i, items[i].SortTitle, w, items)
				break
			}
		}
	})

	t.Run("sort by year descending", func(t *testing.T) {
		items, _, err := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID, Kind: "movie", Sort: "year"})
		if err != nil {
			t.Fatal(err)
		}
		if items[0].Title != "Arrival" {
			t.Errorf("first by year = %q, want Arrival (2016)", items[0].Title)
		}
	})

	t.Run("sort by rating, unrated last", func(t *testing.T) {
		// Rate Arrival above The Matrix; the episode stays unrated.
		movies, _, err := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID, Kind: "movie"})
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range movies {
			var r float64
			switch m.Title {
			case "Arrival":
				r = 8.0
			case "The Matrix":
				r = 7.0
			}
			if r > 0 {
				if err := st.UpdateItemMetadata(ctx, m.ID, ItemMetadata{Rating: &r}); err != nil {
					t.Fatal(err)
				}
			}
		}
		items, _, err := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID, Sort: "rating"})
		if err != nil {
			t.Fatal(err)
		}
		if items[0].Title != "Arrival" || items[1].Title != "The Matrix" {
			t.Errorf("rating order = %q, %q; want Arrival then The Matrix", items[0].Title, items[1].Title)
		}
		// The unrated episode sinks to the bottom rather than sorting as zero.
		if last := items[len(items)-1]; last.Title != "Announcement" {
			t.Errorf("last by rating = %q, want the unrated episode", last.Title)
		}
	})

	t.Run("pagination", func(t *testing.T) {
		items, total, err := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID, Limit: 2, Offset: 2})
		if err != nil {
			t.Fatal(err)
		}
		if total != 3 {
			t.Errorf("total = %d, want 3 (total ignores paging)", total)
		}
		if len(items) != 1 {
			t.Errorf("len = %d, want 1 on the last page", len(items))
		}
	})
}

func TestProgressRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	st.UpsertItem(ctx, file(lib.ID, `C:\m\a.mkv`, "A"))
	known, _ := st.KnownFiles(ctx, lib.ID)
	id := known[`C:\m\a.mkv`].ID

	it, _ := st.GetItem(ctx, id, "local")
	if it.Progress != nil {
		t.Errorf("Progress = %+v before any save, want nil", it.Progress)
	}

	if err := st.SaveProgress(ctx, id, "local", 1000, false); err != nil {
		t.Fatal(err)
	}
	// Saving again must update in place rather than conflict.
	if err := st.SaveProgress(ctx, id, "local", 2000, true); err != nil {
		t.Fatalf("second SaveProgress: %v", err)
	}

	it, _ = st.GetItem(ctx, id, "local")
	if it.Progress == nil || it.Progress.PositionMS != 2000 || !it.Progress.Watched {
		t.Errorf("Progress = %+v, want position 2000 watched true", it.Progress)
	}
}

// The schema is keyed by user from revision 1 (ADR 0006) so multi-user can
// arrive without a migration. Prove the column actually isolates state.
func TestProgressIsPerUser(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	st.UpsertItem(ctx, file(lib.ID, `C:\m\a.mkv`, "A"))
	known, _ := st.KnownFiles(ctx, lib.ID)
	id := known[`C:\m\a.mkv`].ID

	st.SaveProgress(ctx, id, "alice", 1000, false)
	st.SaveProgress(ctx, id, "bob", 9000, true)

	alice, _ := st.GetItem(ctx, id, "alice")
	bob, _ := st.GetItem(ctx, id, "bob")
	if alice.Progress.PositionMS != 1000 {
		t.Errorf("alice position = %d, want 1000", alice.Progress.PositionMS)
	}
	if bob.Progress.PositionMS != 9000 {
		t.Errorf("bob position = %d, want 9000", bob.Progress.PositionMS)
	}
}

func TestAttachProgress(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	st.UpsertItem(ctx, file(lib.ID, `C:\m\a.mkv`, "A"))
	st.UpsertItem(ctx, file(lib.ID, `C:\m\b.mkv`, "B"))
	known, _ := st.KnownFiles(ctx, lib.ID)
	st.SaveProgress(ctx, known[`C:\m\a.mkv`].ID, "local", 777, false)

	items, _, _ := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID})
	if err := st.AttachProgress(ctx, items, "local"); err != nil {
		t.Fatalf("AttachProgress: %v", err)
	}

	var withProgress int
	for _, it := range items {
		if it.Progress != nil {
			withProgress++
			if it.Progress.PositionMS != 777 {
				t.Errorf("position = %d, want 777", it.Progress.PositionMS)
			}
		}
	}
	if withProgress != 1 {
		t.Errorf("%d items carry progress, want exactly 1", withProgress)
	}

	if err := st.AttachProgress(ctx, nil, "local"); err != nil {
		t.Errorf("AttachProgress(nil) = %v, want nil", err)
	}
}

func TestGetItemNotFound(t *testing.T) {
	if _, err := newStore(t).GetItem(context.Background(), 9999, "local"); !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}
