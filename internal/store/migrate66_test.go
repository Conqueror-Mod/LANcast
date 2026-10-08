package store

import (
	"context"
	"testing"
)

/*
 * Revision 66 sends back through identification the ROMs whose art the new
 * rules can change, and only those: unmatched ones, and matched ones with no
 * box art. A matched ROM with its box and a locked one stay as they are.
 */
func TestRevision66RequeuesOnlyROMsMissingArt(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	lib, err := s.CreateLibrary(ctx, "Games", "retro", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	add := func(name string) int64 {
		id, err := s.UpsertItem(ctx, ScanFile{LibraryID: lib.ID, Path: lib.Path + "/" + name, Kind: "rom",
			Title: name, SortTitle: name, Container: "zip", SizeBytes: 1, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.MarkROMChecked(ctx, id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	withBox := add("with box.zip")
	noBox := add("no box.zip")
	unmatched := add("unmatched.zip")
	locked := add("locked.zip")
	for id, state := range map[int64]string{withBox: "matched", noBox: "matched", unmatched: "unmatched", locked: "locked"} {
		if err := s.SetMatch(ctx, id, "libretro-db", "x", state, 1); err != nil {
			t.Fatal(err)
		}
	}
	const h = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := s.PutArtwork(ctx, withBox, h, "poster", "u", 1, 1, 1); err != nil {
		t.Fatal(err)
	}

	if _, err := s.db.Exec(`UPDATE meta SET value = '65' WHERE key = 'schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(s.db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pending, err := s.PendingROMs(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]bool{}
	for _, it := range pending {
		got[it.ID] = true
	}
	if !got[noBox] || !got[unmatched] {
		t.Errorf("not re-queued: no box %v, unmatched %v", got[noBox], got[unmatched])
	}
	if got[withBox] {
		t.Error("a matched ROM with its box was re-queued, and would fetch its art again")
	}
	if got[locked] {
		t.Error("a locked ROM was re-queued")
	}
}
