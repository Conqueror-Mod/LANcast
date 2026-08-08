package store

import (
	"context"
	"path/filepath"
	"testing"
)

// The bug this pins, in one sentence: a music library starved video metadata.
//
// The queue is ordered oldest-first and music rows can never be matched — no
// music provider exists (ADR 0024) — so on the real library 1,592 tracks, 394
// albums and 206 artists sat ahead of everything else, and nothing behind them
// was reached. The films were never looked at, not slow.
//
// The worker also walks past unproductive batches now, which covers the general
// case. This covers the known-permanent one, and it is what makes the remaining
// count honest.
func TestMusicIsNotQueuedForEnrichment(t *testing.T) {
	st := queueStore(t)
	ctx := context.Background()

	lib, err := st.CreateLibrary(ctx, "Music", "music", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"artist", "album", "track"} {
		seedPending(t, st, lib.ID, k, k+".flac", k)
	}
	film := seedPending(t, st, lib.ID, "movie", "A Film (1999).mkv", "A Film")

	pending, err := st.PendingEnrichment(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}

	var sawFilm bool
	for _, it := range pending {
		switch it.Kind {
		case "track", "album", "artist":
			t.Errorf("%s %q is queued; no provider can ever match it", it.Kind, it.Title)
		case "movie":
			if it.ID == film {
				sawFilm = true
			}
		}
	}
	if !sawFilm {
		t.Error("the film was not in the queue")
	}
}

// The number the UI shows. With music counted it read 2,198 on the real library
// and never fell — indistinguishable from a stuck backlog, for work that was
// never going to happen.
func TestPendingCountExcludesWhatCannotBeEnriched(t *testing.T) {
	st := queueStore(t)
	ctx := context.Background()

	lib, err := st.CreateLibrary(ctx, "Mixed", "music", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i, k := range []string{"artist", "album", "track", "track", "track"} {
		seedPending(t, st, lib.ID, k, filepath.Join("m", k+string(rune('a'+i))+".flac"), k)
	}
	seedPending(t, st, lib.ID, "movie", "A Film (1999).mkv", "A Film")

	n, err := st.PendingCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("PendingCount = %d, want 1 — only the film can be enriched", n)
	}
}

// Containers that are not music stay in the queue. A collection has no provider
// today either, but that is a gap rather than a permanent fact, and excluding it
// here would hide it — the filter names what can never be matched, not what
// merely is not matched yet.
func TestNonMusicContainersStayQueued(t *testing.T) {
	st := queueStore(t)
	ctx := context.Background()

	lib, err := st.CreateLibrary(ctx, "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seedPending(t, st, lib.ID, "show", "A Show", "A Show")

	pending, err := st.PendingEnrichment(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Kind != "show" {
		t.Errorf("pending = %+v, want the show to still be queued", pending)
	}
}

func queueStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// seedPending inserts a row that is awaiting enrichment.
func seedPending(t *testing.T, st *Store, libraryID int64, kind, path, title string) int64 {
	t.Helper()
	id, err := st.UpsertItem(context.Background(), ScanFile{
		LibraryID: libraryID, Path: path, Kind: kind,
		Title: title, SortTitle: title, Container: "mkv", SizeBytes: 1, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}
