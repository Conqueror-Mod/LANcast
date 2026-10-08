package store

import (
	"context"
	"path/filepath"
	"testing"
)

func romLibrary(t *testing.T, s *Store) Library {
	t.Helper()
	lib, err := s.CreateLibrary(context.Background(), "Games", "retro", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return *lib
}

func putROM(t *testing.T, s *Store, lib Library, name, platform string, size int64) int64 {
	t.Helper()
	var p *string
	if platform != "" {
		p = &platform
	}
	id, err := s.UpsertItem(context.Background(), ScanFile{
		LibraryID: lib.ID, Path: filepath.Join(lib.Path, name), Kind: "rom",
		Title: name, SortTitle: name, Platform: p,
		Container: filepath.Ext(name)[1:], SizeBytes: size, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// Revision 64 replays cleanly over a database that already has it, which is
// what the columns list exists to make true.
func TestRevision64Replays(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.db.Exec(`UPDATE meta SET value = '63' WHERE key = 'schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(s.db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	lib := romLibrary(t, s)
	id := putROM(t, s, lib, "a.z64", "n64", 1)
	if err := s.PutROMHash(context.Background(), id, ROMHash{CRC32: "ABCD1234"}); err != nil {
		t.Fatal(err)
	}
}

func TestPlatformRoundTripsAndFilters(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	lib := romLibrary(t, s)
	putROM(t, s, lib, "a.z64", "n64", 1)
	putROM(t, s, lib, "b.sfc", "snes", 1)
	putROM(t, s, lib, "c.gba", "gba", 1)
	putROM(t, s, lib, "d.zip", "", 1)

	items, total, err := s.ListItems(ctx, ItemFilter{LibraryID: lib.ID, Platforms: []string{"n64", "gba"}})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("filter returned %d (total %d), want 2", len(items), total)
	}
	for _, it := range items {
		if it.Platform == nil || (*it.Platform != "n64" && *it.Platform != "gba") {
			t.Errorf("unexpected item %s platform %v", it.Path, it.Platform)
		}
	}

	all, _, _ := s.ListItems(ctx, ItemFilter{LibraryID: lib.ID})
	for _, it := range all {
		if filepath.Base(it.Path) == "d.zip" && it.Platform != nil {
			t.Errorf("an unplaced zip has platform %q", *it.Platform)
		}
	}
}

// ROMs are never in the enrichment queue: no provider can answer for one, and
// a row that can never leave a queue is a count that never falls.
func TestROMsAreNotPendingEnrichment(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	lib := romLibrary(t, s)
	putROM(t, s, lib, "a.z64", "n64", 1)
	items, err := s.PendingEnrichment(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("%d ROMs pending enrichment, want 0", len(items))
	}
	if n, _ := s.PendingROMCount(ctx); n != 1 {
		t.Errorf("pending rom count = %d, want 1", n)
	}
}

// A changed file is read again: its old hash describes bytes that are gone.
func TestChangedROMIsRehashed(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	lib := romLibrary(t, s)
	id := putROM(t, s, lib, "a.z64", "n64", 1)
	if err := s.PutROMHash(ctx, id, ROMHash{CRC32: "ABCD1234"}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkROMChecked(ctx, id); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.PendingROMCount(ctx); n != 0 {
		t.Fatalf("checked rom still pending")
	}

	putROM(t, s, lib, "a.z64", "n64", 2)
	if h, _ := s.GetROMHash(ctx, id); h != nil {
		t.Errorf("stale hash survived a change of bytes: %+v", h)
	}
	if n, _ := s.PendingROMCount(ctx); n != 1 {
		t.Errorf("changed rom not re-queued")
	}
}

// Installing the DATs re-asks about every ROM except a locked one.
func TestRequeueROMsSkipsLocked(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	lib := romLibrary(t, s)
	plain := putROM(t, s, lib, "a.z64", "n64", 1)
	locked := putROM(t, s, lib, "b.z64", "n64", 1)
	for _, id := range []int64{plain, locked} {
		if err := s.MarkROMChecked(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetMatch(ctx, locked, "libretro-db", "B (USA)", "locked", 1); err != nil {
		t.Fatal(err)
	}
	n, err := s.RequeueROMs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("requeued %d, want 1", n)
	}
	pending, _ := s.PendingROMs(ctx, 10)
	if len(pending) != 1 || pending[0].ID != plain {
		t.Errorf("pending = %+v, want only the unlocked rom", pending)
	}
}

// The Console filter offers only consoles the library holds, and a missing
// game's console is not one of them.
func TestFacetsListPlatformsPresent(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	lib := romLibrary(t, s)
	putROM(t, s, lib, "a.z64", "n64", 1)
	putROM(t, s, lib, "b.z64", "n64", 1)
	gone := putROM(t, s, lib, "c.gba", "gba", 1)
	putROM(t, s, lib, "d.zip", "", 1)
	if err := s.MarkMissing(ctx, []int64{gone}); err != nil {
		t.Fatal(err)
	}
	f, err := s.LibraryFacets(ctx, lib.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Platforms) != 1 || f.Platforms[0] != "n64" {
		t.Errorf("platforms = %v, want [n64]", f.Platforms)
	}
}
