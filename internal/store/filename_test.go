package store

import (
	"context"
	"strings"
	"testing"
)

// A container's `path` is a synthetic key, not a path. Exposing its base as a
// file name put `TEST MUSIC LIBRARY::artist=ABBA` on the artist page — and a
// bare `DC` for AC/DC, because Base splits on the separator in the name.
func TestContainersHaveNoFileName(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := musicLibrary(t, st)
	albumID := seedAlbum(t, st, lib, "AC/DC", "Back in Black")

	var artistID int64
	if err := st.db.QueryRowContext(ctx,
		`SELECT parent_id FROM media_item WHERE id = ?`, albumID).Scan(&artistID); err != nil {
		t.Fatal(err)
	}

	for _, id := range []int64{artistID, albumID} {
		it, err := st.GetItem(ctx, id, "")
		if err != nil {
			t.Fatalf("GetItem(%d): %v", id, err)
		}
		if err := st.LoadDetail(ctx, it); err != nil {
			t.Fatalf("LoadDetail(%d): %v", id, err)
		}
		if it.FileName != "" {
			t.Errorf("%s %q exposes file_name %q — it has no file",
				it.Kind, it.Title, it.FileName)
		}
	}
}

// The privacy rule this quietly defeated: `path` is never serialized because it
// discloses the server's filesystem layout. A synthetic key contains the
// library path, so leaking its base leaked part of that.
func TestContainerFileNameDoesNotLeakTheLibraryPath(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := musicLibrary(t, st)
	albumID := seedAlbum(t, st, lib, "ABBA", "Arrival")

	it, err := st.GetItem(ctx, albumID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.LoadDetail(ctx, it); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(it.FileName, "::") || strings.Contains(it.FileName, "artist=") {
		t.Errorf("file_name %q carries the synthetic key", it.FileName)
	}
}

// The reason FileName exists at all still works: a real file says which file it
// is, so a wrongly matched title can be told apart from its siblings.
func TestARealFileStillReportsItsName(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := musicLibrary(t, st)
	albumID := seedAlbum(t, st, lib, "ABBA", "Arrival")
	trackID := seedTrack(t, st, lib, albumID, lib.Path+"/01 Dancing Queen.mp3",
		"Dancing Queen", 1, 1)

	it, err := st.GetItem(ctx, trackID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.LoadDetail(ctx, it); err != nil {
		t.Fatal(err)
	}
	if it.FileName != "01 Dancing Queen.mp3" {
		t.Errorf("file_name = %q, want the file's own name", it.FileName)
	}
}
