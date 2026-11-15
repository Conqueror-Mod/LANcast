package store

import (
	"context"
	"errors"
	"testing"
)

/*
 * The permanent rule, applied to membership: once a person has edited a
 * playlist, the .m3u it was seeded from never overwrites it (ADR 0030,
 * CLAUDE.md). In the manner of the locked-fields test. If this fails, a
 * rescan can undo somebody's edit, and LANcast has become the thing it was
 * built to replace.
 */
func TestAnImportNeverOverwritesALockedPlaylist(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	a := mkItem(t, st, lib.ID, "track", "/m/a.mp3", "Alpha")
	b := mkItem(t, st, lib.ID, "track", "/m/b.mp3", "Beta")
	pl := mkPlaylist(t, st, lib.ID, "/m/set.m3u", "The Set")

	// A person edits it down to one track and the edit locks it.
	if err := st.SetPlaylistEntries(ctx, pl, []int64{b}); err != nil {
		t.Fatal(err)
	}
	if err := st.LockField(ctx, pl, PlaylistMembersLock); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		err := st.ImportPlaylistEntries(ctx, pl, []int64{a, b})
		if !errors.Is(err, ErrPlaylistLocked) {
			t.Fatalf("import %d over a locked playlist: err = %v, want ErrPlaylistLocked", i, err)
		}
	}
	got, err := st.PlaylistEntries(ctx, pl)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Beta"}; !eq(entryTitles(got), want) {
		t.Errorf("entries = %v, want %v — the import overwrote an edit", entryTitles(got), want)
	}
}

// The lock is the exception: an unedited playlist still follows its file, or
// "never overwrite an edit" quietly becomes "import once and never again".
func TestAnImportReplacesAnUnlockedPlaylist(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	a := mkItem(t, st, lib.ID, "track", "/m/a.mp3", "Alpha")
	b := mkItem(t, st, lib.ID, "track", "/m/b.mp3", "Beta")
	pl := mkPlaylist(t, st, lib.ID, "/m/set.m3u", "The Set")

	if err := st.ImportPlaylistEntries(ctx, pl, []int64{a}); err != nil {
		t.Fatal(err)
	}
	if err := st.ImportPlaylistEntries(ctx, pl, []int64{a, b, a}); err != nil {
		t.Fatal(err)
	}
	got, err := st.PlaylistEntries(ctx, pl)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Alpha", "Beta", "Alpha"}; !eq(entryTitles(got), want) {
		t.Errorf("entries = %v, want %v", entryTitles(got), want)
	}
}

// A lock that cannot be read is not a lock that is absent (R08-4). The old
// check treated a failed lookup as "not locked" and imported over the edit;
// here any failure leaves the playlist as it was.
func TestAnImportThatCannotReadTheLockWritesNothing(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	a := mkItem(t, st, lib.ID, "track", "/m/a.mp3", "Alpha")
	b := mkItem(t, st, lib.ID, "track", "/m/b.mp3", "Beta")
	pl := mkPlaylist(t, st, lib.ID, "/m/set.m3u", "The Set")
	if err := st.SetPlaylistEntries(ctx, pl, []int64{b}); err != nil {
		t.Fatal(err)
	}

	// Make the lock read fail, the way a busy or damaged database would.
	if _, err := st.db.ExecContext(ctx, `ALTER TABLE item_lock RENAME TO item_lock_gone`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = st.db.ExecContext(context.Background(), `ALTER TABLE item_lock_gone RENAME TO item_lock`)
	})

	if err := st.ImportPlaylistEntries(ctx, pl, []int64{a, b}); err == nil {
		t.Fatal("import succeeded although the lock could not be read")
	}
	got, err := st.PlaylistEntries(ctx, pl)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Beta"}; !eq(entryTitles(got), want) {
		t.Errorf("entries = %v, want %v — a failed lock read let the import through", entryTitles(got), want)
	}
}
