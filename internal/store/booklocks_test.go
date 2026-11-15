package store

import (
	"context"
	"path/filepath"
	"testing"
)

// A book part's place is its `episode`, and an edit locks it like any other
// field. The book pass must leave a locked place alone, as it leaves a locked
// part title alone; otherwise a part somebody moved goes back on the next
// pass, and the book plays in the folder's order again.
func TestSetBookPartLeavesALockedPlaceAlone(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib, err := st.CreateLibrary(ctx, "Books", "audiobooks", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	book, err := st.EnsureDerivedContainer(ctx, lib.ID, "book", "k1", "Q-Squared", "Q-Squared", nil)
	if err != nil {
		t.Fatal(err)
	}
	part, err := st.UpsertItem(ctx, ScanFile{
		LibraryID: lib.ID, Path: filepath.Join(lib.Path, "Q-Squared", "01.m4b"), Kind: "book_part",
		Title: "Part One", SortTitle: "part one", Container: "m4b", SizeBytes: 1, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	place := func() int {
		t.Helper()
		it, err := st.GetItem(ctx, part, "")
		if err != nil {
			t.Fatal(err)
		}
		if it.Episode == nil {
			return 0
		}
		return *it.Episode
	}

	// Control: unlocked, the pass sets the place.
	if err := st.SetBookPart(ctx, part, book, 1, "Part One", "part one"); err != nil {
		t.Fatal(err)
	}
	if got := place(); got != 1 {
		t.Fatalf("control: place %d, want 1", got)
	}

	// Somebody moves it to third and the edit locks it.
	three := 3
	if err := st.UpdateItemMetadata(ctx, part, ItemMetadata{Episode: &three}); err != nil {
		t.Fatal(err)
	}
	if err := st.LockField(ctx, part, "episode"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetBookPart(ctx, part, book, 1, "Part One", "part one"); err != nil {
		t.Fatal(err)
	}
	if got := place(); got != 3 {
		t.Errorf("a locked place was overwritten by the book pass: %d, want 3", got)
	}
	// The part still hangs under its book: only the place is held.
	if it, err := st.GetItem(ctx, part, ""); err != nil || it.ParentID == nil || *it.ParentID != book {
		t.Errorf("part's parent = %v (%v), want %d", it.ParentID, err, book)
	}
}
