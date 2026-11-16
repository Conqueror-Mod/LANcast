package store

import (
	"context"
	"testing"
)

/*
 * Revision 97 sends back to the probe the films whose length is a provider's
 * whole-minute runtime (T-6), and nothing else: a length a probe measured, a
 * film no provider matched, and a track that happens to be a whole minute
 * long, stay probed.
 */
func TestRevision97RequeuesProviderRuntimes(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	lib, err := s.CreateLibrary(ctx, "Films", "movies", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	add := func(name, kind string, durationMS int64, provider bool) int64 {
		id, err := s.UpsertItem(ctx, ScanFile{LibraryID: lib.ID, Path: lib.Path + "/" + name, Kind: kind,
			Title: name, SortTitle: name, Container: "mkv", SizeBytes: 1, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SaveProbe(ctx, id, ProbeResult{DurationMS: 7_145_000}); err != nil {
			t.Fatal(err)
		}
		var p any
		if provider {
			p = "tmdb"
		}
		if _, err := s.db.Exec(`UPDATE media_item SET duration_ms = ?, provider = ? WHERE id = ?`, durationMS, p, id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	runtime := add("halloween ii.mkv", "movie", 960_000, true)
	measured := add("measured.mkv", "movie", 7_145_312, true)
	unmatched := add("home video.mkv", "movie", 6_000_000, false)
	track := add("song.flac", "track", 180_000, true)

	if _, err := s.db.Exec(`UPDATE meta SET value = '96' WHERE key = 'schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(s.db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pending, err := s.PendingProbe(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]bool{}
	for _, it := range pending {
		got[it.ID] = true
	}
	if !got[runtime] {
		t.Error("a film holding a provider's runtime was not re-queued")
	}
	if got[unmatched] {
		t.Error("a whole-minute film no provider matched was queued; nothing could have raced its probe")
	}
	if got[measured] {
		t.Error("a length the probe measured was queued again")
	}
	if got[track] {
		t.Error("a whole-minute track was queued; only films had the race")
	}
}
