package store

import (
	"context"
	"testing"
)

func mkPlaylist(t *testing.T, st *Store, libID int64, path, title string) int64 {
	t.Helper()
	id, err := st.EnsurePlaylist(context.Background(), libID, path, title, title)
	if err != nil {
		t.Fatalf("EnsurePlaylist(%q): %v", path, err)
	}
	return id
}

func entryTitles(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The whole reason playlist_entry exists instead of item_collection. A
// collection's primary key is (item_id, collection_id) and physically cannot
// hold a repeat; a playlist must. If this ever fails, the table has been
// "tidied" back into a set and every playlist with a reprise has silently lost
// a track.
func TestAPlaylistMayHoldTheSameTrackTwice(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	a := mkItem(t, st, lib.ID, "track", "/m/a.mp3", "Opening")
	b := mkItem(t, st, lib.ID, "track", "/m/b.mp3", "Middle")
	pl := mkPlaylist(t, st, lib.ID, "/m/set.m3u", "The Set")

	if err := st.SetPlaylistEntries(ctx, pl, []int64{a, b, a}); err != nil {
		t.Fatalf("SetPlaylistEntries: %v", err)
	}

	got, err := st.PlaylistEntries(ctx, pl)
	if err != nil {
		t.Fatalf("PlaylistEntries: %v", err)
	}
	want := []string{"Opening", "Middle", "Opening"}
	if !eq(entryTitles(got), want) {
		t.Errorf("entries = %v, want %v", entryTitles(got), want)
	}

	n, err := st.PlaylistCount(ctx, pl)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("count = %d, want 3 — repeats are entries, not duplicates", n)
	}
}

// Order is the playlist. Storing it as a set with an ord column is only useful
// if the order comes back the way it went in, including when it is not
// alphabetical and not the order the tracks were created in.
func TestPlaylistKeepsItsOrder(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	z := mkItem(t, st, lib.ID, "track", "/m/z.mp3", "Zulu")
	a := mkItem(t, st, lib.ID, "track", "/m/a.mp3", "Alpha")
	m := mkItem(t, st, lib.ID, "track", "/m/m.mp3", "Mike")
	pl := mkPlaylist(t, st, lib.ID, "/m/order.m3u", "Order")

	if err := st.SetPlaylistEntries(ctx, pl, []int64{z, a, m}); err != nil {
		t.Fatal(err)
	}
	got, _ := st.PlaylistEntries(ctx, pl)
	if want := []string{"Zulu", "Alpha", "Mike"}; !eq(entryTitles(got), want) {
		t.Errorf("entries = %v, want %v — sorted, not stored", entryTitles(got), want)
	}
}

// Replace, not merge. Setting the entries again must leave exactly the new
// list — including when the new list reorders rows that already exist, which is
// the case that would collide with the (playlist_id, ord) key if the writes
// were not one transaction.
func TestSetPlaylistEntriesReplacesAndReorders(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	a := mkItem(t, st, lib.ID, "track", "/m/a.mp3", "A")
	b := mkItem(t, st, lib.ID, "track", "/m/b.mp3", "B")
	c := mkItem(t, st, lib.ID, "track", "/m/c.mp3", "C")
	pl := mkPlaylist(t, st, lib.ID, "/m/p.m3u", "P")

	if err := st.SetPlaylistEntries(ctx, pl, []int64{a, b, c}); err != nil {
		t.Fatal(err)
	}
	// Reversed, and one dropped: every ord collides with an existing row.
	if err := st.SetPlaylistEntries(ctx, pl, []int64{c, a}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	got, _ := st.PlaylistEntries(ctx, pl)
	if want := []string{"C", "A"}; !eq(entryTitles(got), want) {
		t.Errorf("entries = %v, want %v", entryTitles(got), want)
	}
}

func TestEmptyingAPlaylistIsAllowed(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	a := mkItem(t, st, lib.ID, "track", "/m/a.mp3", "A")
	pl := mkPlaylist(t, st, lib.ID, "/m/p.m3u", "P")
	if err := st.SetPlaylistEntries(ctx, pl, []int64{a}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetPlaylistEntries(ctx, pl, nil); err != nil {
		t.Fatalf("emptying: %v", err)
	}
	got, _ := st.PlaylistEntries(ctx, pl)
	if len(got) != 0 {
		t.Errorf("entries = %v, want none", entryTitles(got))
	}
}

// ON DELETE CASCADE on item_id. Deleting a track must remove it from every
// playlist holding it — a playlist that silently plays eleven of its twelve
// entries is worse than one that visibly got shorter. This also covers the
// repeat case: both copies go.
func TestDeletingATrackRemovesItFromPlaylists(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	a := mkItem(t, st, lib.ID, "track", "/m/a.mp3", "A")
	b := mkItem(t, st, lib.ID, "track", "/m/b.mp3", "B")
	pl := mkPlaylist(t, st, lib.ID, "/m/p.m3u", "P")
	if err := st.SetPlaylistEntries(ctx, pl, []int64{a, b, a}); err != nil {
		t.Fatal(err)
	}

	if err := st.DeleteItems(ctx, []int64{a}); err != nil {
		t.Fatalf("DeleteItems: %v", err)
	}

	got, _ := st.PlaylistEntries(ctx, pl)
	if want := []string{"B"}; !eq(entryTitles(got), want) {
		t.Errorf("entries = %v, want %v — both copies of the deleted track should go",
			entryTitles(got), want)
	}
}

// Re-importing the same .m3u must update one playlist rather than accumulate a
// second one per scan. The path is the key, exactly as it is for an album.
func TestEnsurePlaylistIsIdempotentOnPath(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	first := mkPlaylist(t, st, lib.ID, "/m/road trip.m3u", "Road Trip")
	second, err := st.EnsurePlaylist(ctx, lib.ID, "/m/road trip.m3u", "Road Trip", "Road Trip")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("ids %d and %d — a re-import made a second playlist", first, second)
	}
}
