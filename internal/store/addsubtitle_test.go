package store

import (
	"context"
	"path/filepath"
	"testing"
)

// Recording the same subtitle twice returns the first row's id (R07-4).
//
// The second call takes the upsert's UPDATE branch, where SQLite leaves
// last_insert_rowid() as it was. Another item's subtitle is inserted in
// between, so an id read that way names that one instead. One connection
// makes the stale value certain rather than a matter of which pooled
// connection the call happens to get.
func TestAddSubtitleTwiceReturnsTheSameRow(t *testing.T) {
	st := openTestStore(t)
	st.db.SetMaxOpenConns(1)
	ctx := context.Background()

	lib, err := st.CreateLibrary(ctx, "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	film := func(name string) int64 {
		id, err := st.UpsertItem(ctx, ScanFile{
			LibraryID: lib.ID, Path: filepath.Join(lib.Path, name), Kind: "movie",
			Title: name, SortTitle: name, Container: "mkv", SizeBytes: 1, MTime: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	a, b := film("a.mkv"), film("b.mkv")
	add := func(item int64, path, title string) int64 {
		id, err := st.AddSubtitle(ctx, ExternalSubtitle{
			ItemID: item, Path: path, Language: "en", Title: title, Format: "srt", Source: "downloaded",
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}

	first := add(a, "/subs/a/7.en.srt", "first")
	other := add(b, "/subs/b/8.en.srt", "other")
	again := add(a, "/subs/a/7.en.srt", "again")

	if again != first {
		t.Errorf("first=%d other=%d again=%d; the repeat must name the first row", first, other, again)
	}
	got, err := st.ExternalSubtitle(ctx, a, again)
	if err != nil {
		t.Fatalf("the returned id is not this item's subtitle: %v", err)
	}
	if got.Title != "again" {
		t.Errorf("title = %q, want the update applied to the same row", got.Title)
	}
}
