package store

import (
	"context"
	"path/filepath"
	"testing"
)

/*
 * An artist's tracks, in the order the artist page draws them.
 *
 * The shape that matters is the mixed one: two records and a loose single
 * parented straight to the artist, which is what made an artist's Play all do
 * nothing once (see fetchArtistQueue's history in the client). The single's
 * title sorts between the two records, so it must land between them.
 */
func TestTracksOfAnArtistFollowThePage(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	lib, err := st.CreateLibrary(ctx, "Music", "music", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	add := func(kind, title string, parent *int64, disc, track int) int64 {
		t.Helper()
		n++
		f := ScanFile{
			LibraryID: lib.ID, Path: filepath.Join(lib.Path, kind+string(rune('a'+n))),
			Kind: kind, Title: title, SortTitle: title, MTime: 1,
		}
		if kind == "track" {
			f.Season, f.Episode = &disc, &track
			f.Container, f.SizeBytes = "flac", 1
		}
		id, err := st.UpsertItem(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		if parent != nil {
			if err := st.SetParent(ctx, id, parent); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}

	artist := add("artist", "band", nil, 0, 0)
	other := add("artist", "other band", nil, 0, 0)
	// Inserted out of order on purpose: row ids must not be what orders this.
	zebra := add("album", "zebra", &artist, 0, 0)
	alpha := add("album", "alpha", &artist, 0, 0)
	z2 := add("track", "z two", &zebra, 1, 2)
	z1 := add("track", "z one", &zebra, 1, 1)
	a21 := add("track", "a disc two", &alpha, 2, 1)
	a11 := add("track", "a disc one", &alpha, 1, 1)
	single := add("track", "middle single", &artist, 0, 0)
	gone := add("track", "gone", &alpha, 1, 2)
	theirAlbum := add("album", "theirs", &other, 0, 0)
	add("track", "not ours", &theirAlbum, 1, 1)
	if _, err := st.db.ExecContext(ctx, `UPDATE media_item SET missing = 1 WHERE id = ?`, gone); err != nil {
		t.Fatal(err)
	}

	got, err := st.TracksOf(ctx, artist)
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{a11, a21, single, z1, z2}
	if len(got) != len(want) {
		t.Fatalf("got %d tracks, want %d: %v", len(got), len(want), trackIDs(got))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("order = %v, want %v (alpha by disc, the loose single, then zebra)", trackIDs(got), want)
		}
	}
}

func trackIDs(items []Item) []int64 {
	out := make([]int64, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}
