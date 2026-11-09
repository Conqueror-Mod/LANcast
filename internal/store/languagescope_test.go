package store

import (
	"context"
	"path/filepath"
	"testing"
)

/*
 * Which rows a language refresh fetches again (ADR 0121): every TMDB-identified
 * title, matched or locked, and nothing in review, nothing from another
 * provider, nothing with no id to fetch by.
 */

func TestTheLanguageScopeIsEveryIdentifiedTMDBTitle(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	lib, a, b, _ := seedLibrary(t, st)
	libRow, err := st.GetLibrary(ctx, lib)
	if err != nil {
		t.Fatal(err)
	}
	c, err := st.UpsertItem(ctx, ScanFile{
		LibraryID: lib, Path: filepath.Join(libRow.Path, "third.mkv"), Kind: "movie",
		Title: "Third", SortTitle: "Third", Container: "mkv", SizeBytes: 1, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	settledRow(t, st, a, "locked", "tmdb", "85")
	settledRow(t, st, b, "matched", "tmdb", "603")
	// A suggestion nobody confirmed: its id is a candidate's, not the title's.
	settledRow(t, st, c, "review", "tmdb", "11")

	n, err := st.LanguageCount(ctx, lib)
	if err != nil {
		t.Fatal(err)
	}
	items, err := st.LanguageItems(ctx, lib)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || len(items) != 2 {
		t.Fatalf("count %d, items %d; want the locked and the matched rows", n, len(items))
	}
	for _, it := range items {
		if it.ID == c {
			t.Error("a row in review was included; it would be filled with an unconfirmed candidate's text")
		}
	}
}

func TestTheLanguageScopeLeavesOtherProvidersAndRowsWithNoID(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	lib, a, b, _ := seedLibrary(t, st)
	settledRow(t, st, a, "matched", "musicbrainz", "abc")
	settledRow(t, st, b, "locked", "tmdb", "")

	if n, err := st.LanguageCount(ctx, lib); err != nil || n != 0 {
		t.Errorf("count = %d (%v), want 0: one row is another provider's, the other has no id", n, err)
	}
}
