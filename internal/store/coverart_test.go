package store

import (
	"context"
	"testing"
)

// musicLibrary and seedAlbum build the shape the cover-art worker queries: an
// artist containing an album containing tracks.
func musicLibrary(t *testing.T, st *Store) *Library {
	t.Helper()
	lib, err := st.CreateLibrary(context.Background(), "Music", "music", t.TempDir())
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	return lib
}

// seedTrack adds a track to an album. Disc and track numbers go into season
// and episode, which is where music puts them on the wide table (ADR 0002).
func seedTrack(t *testing.T, st *Store, lib *Library, albumID int64, path, title string, disc, track int) int64 {
	t.Helper()
	ctx := context.Background()
	if _, err := st.UpsertItem(ctx, ScanFile{
		LibraryID: lib.ID, Path: path, Kind: "track",
		Title: title, SortTitle: title,
		Season: &disc, Episode: &track,
		Container: "flac", SizeBytes: 1000, MTime: 500,
	}); err != nil {
		t.Fatalf("UpsertItem track: %v", err)
	}
	known, _ := st.KnownFiles(ctx, lib.ID)
	id := known[path].ID
	if err := st.SetParent(ctx, id, &albumID); err != nil {
		t.Fatalf("SetParent: %v", err)
	}
	return id
}

func seedAlbum(t *testing.T, st *Store, lib *Library, artist, album string) int64 {
	t.Helper()
	ctx := context.Background()
	// Containers are keyed by a synthetic path, the way the scanner keys them:
	// it is what makes them idempotent across rescans, and `path` is UNIQUE, so
	// two containers sharing one would silently become the same row.
	artistKey := lib.Path + "::artist=" + artist
	albumKey := artistKey + "::album=" + album

	artistID, err := st.EnsureMusicContainer(ctx, lib.ID, "artist", artistKey, artist, artist, nil)
	if err != nil {
		t.Fatalf("EnsureMusicContainer artist: %v", err)
	}
	albumID, err := st.EnsureMusicContainer(ctx, lib.ID, "album", albumKey, album, album, &artistID)
	if err != nil {
		t.Fatalf("EnsureMusicContainer album: %v", err)
	}
	if albumID == artistID {
		t.Fatalf("album and artist collapsed into one row (id %d)", albumID)
	}
	return albumID
}

func TestPendingCoverArtReturnsUncheckedAlbums(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := musicLibrary(t, st)
	albumID := seedAlbum(t, st, lib, "An Artist", "A Record")

	pending, err := st.PendingCoverArt(ctx, 10)
	if err != nil {
		t.Fatalf("PendingCoverArt: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != albumID {
		t.Fatalf("pending = %v, want just the album", pending)
	}

	n, err := st.PendingCoverArtCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("PendingCoverArtCount = %d, want 1", n)
	}
}

// The property the whole queue design rests on: once looked at, an album does
// not come back. Without this the worker re-reads every artless album on every
// pass and the queue never drains.
func TestCheckedAlbumsLeaveTheQueue(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := musicLibrary(t, st)
	albumID := seedAlbum(t, st, lib, "An Artist", "A Record")

	if err := st.MarkCoverArtChecked(ctx, albumID); err != nil {
		t.Fatalf("MarkCoverArtChecked: %v", err)
	}

	pending, err := st.PendingCoverArt(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending = %v, want empty after being checked", pending)
	}
	if n, _ := st.PendingCoverArtCount(ctx); n != 0 {
		t.Errorf("PendingCoverArtCount = %d, want 0", n)
	}
}

// Only albums. An artist row has no directory of its own to search and no file
// to extract from, and a track would be looked at once per song.
func TestOnlyAlbumsAreQueuedForCoverArt(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := musicLibrary(t, st)
	albumID := seedAlbum(t, st, lib, "An Artist", "A Record")

	// A film in another library must not appear either.
	films := mustLibrary(t, st)
	seedItem(t, st, films, `C:\m\a.mkv`)

	pending, err := st.PendingCoverArt(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %d items, want only the album", len(pending))
	}
	if pending[0].ID != albumID {
		t.Errorf("queued item %d, want album %d", pending[0].ID, albumID)
	}
	if pending[0].Kind != "album" {
		t.Errorf("queued kind = %q, want album", pending[0].Kind)
	}
}

// Re-queueing is the counterpart to re-probing. A user who has just added
// cover.jpg files has no other way to ask LANcast to look again, because the
// pending query cannot see an album it already stamped.
func TestClearCoverArtChecksRequeues(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := musicLibrary(t, st)
	albumID := seedAlbum(t, st, lib, "An Artist", "A Record")

	if err := st.MarkCoverArtChecked(ctx, albumID); err != nil {
		t.Fatal(err)
	}
	queued, err := st.ClearCoverArtChecks(ctx, 0)
	if err != nil {
		t.Fatalf("ClearCoverArtChecks: %v", err)
	}
	if queued != 1 {
		t.Errorf("queued = %d, want 1", queued)
	}
	if n, _ := st.PendingCoverArtCount(ctx); n != 1 {
		t.Errorf("PendingCoverArtCount = %d, want 1 after a refresh", n)
	}
}

func TestClearCoverArtChecksCanScopeToOneLibrary(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	one := musicLibrary(t, st)
	two := musicLibrary(t, st)
	a := seedAlbum(t, st, one, "Artist One", "Record One")
	b := seedAlbum(t, st, two, "Artist Two", "Record Two")

	for _, id := range []int64{a, b} {
		if err := st.MarkCoverArtChecked(ctx, id); err != nil {
			t.Fatal(err)
		}
	}

	queued, err := st.ClearCoverArtChecks(ctx, one.ID)
	if err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Errorf("queued = %d, want 1 — the other library must be untouched", queued)
	}

	pending, err := st.PendingCoverArt(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != a {
		t.Errorf("pending = %v, want only the album in the refreshed library", pending)
	}
}

// The worker tries an album's first tracks for embedded art, so "first" has to
// mean the first track of the record — disc then track number, which for music
// live in season and episode (ADR 0002's wide table, reused not extended).
func TestAlbumTrackPathsAreInDiscAndTrackOrder(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := musicLibrary(t, st)
	albumID := seedAlbum(t, st, lib, "An Artist", "A Record")

	// Inserted deliberately out of order.
	seedTrack(t, st, lib, albumID, `C:\m\d2t1.flac`, "D2T1", 2, 1)
	seedTrack(t, st, lib, albumID, `C:\m\d1t2.flac`, "D1T2", 1, 2)
	seedTrack(t, st, lib, albumID, `C:\m\d1t1.flac`, "D1T1", 1, 1)

	paths, err := st.AlbumTrackPaths(ctx, albumID)
	if err != nil {
		t.Fatalf("AlbumTrackPaths: %v", err)
	}
	want := []string{`C:\m\d1t1.flac`, `C:\m\d1t2.flac`, `C:\m\d2t1.flac`}
	if len(paths) != len(want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Errorf("paths[%d] = %q, want %q (full: %v)", i, paths[i], want[i], paths)
		}
	}
}

func TestAlbumTrackPathsSkipsMissingFiles(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := musicLibrary(t, st)
	albumID := seedAlbum(t, st, lib, "An Artist", "A Record")
	id := seedTrack(t, st, lib, albumID, `C:\m\gone.flac`, "Gone", 1, 1)
	seedTrack(t, st, lib, albumID, `C:\m\here.flac`, "Here", 1, 2)

	if err := st.MarkMissing(ctx, []int64{id}); err != nil {
		t.Fatalf("MarkMissing: %v", err)
	}

	paths, err := st.AlbumTrackPaths(ctx, albumID)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != `C:\m\here.flac` {
		t.Errorf("paths = %v, want only the present file — an unmounted drive is not a source of art", paths)
	}
}
