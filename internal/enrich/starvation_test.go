package enrich

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"lancast/internal/meta"
	"lancast/internal/store"
)

// The enrichment queue is `ORDER BY added_at LIMIT BatchSize`, and the worker
// stops the moment a batch stamps nothing. That guard exists for a good reason
// — the queue is a query rather than a cursor, so an unproductive batch would
// otherwise be re-read forever at full tilt — but it stops the whole run, not
// just the batch.
//
// So a block of items no provider can enrich, sitting at the front of the queue,
// strands everything behind it permanently. Music is exactly that block: tracks,
// albums and artists have no provider, so their metadata stamp is never set and
// they never leave the queue. On a server with a music library, every film added
// afterwards is behind a wall that never comes down.
//
// Observed on 2026-08-08: 2,192 pending music items, a newly scanned film that
// stayed `unmatched` through repeated passes, and enrichment reporting
// `enriched 0, remaining 2202` on every run.
func TestNewItemIsEnrichedBehindAnUnenrichableBacklog(t *testing.T) {
	st, lib := harness(t)
	ctx := context.Background()

	// The backlog: more than one batch of items no provider handles.
	for i := 0; i < 60; i++ {
		addKind(t, st, lib, fmt.Sprintf("/music/track-%02d.flac", i), "track")
	}

	// added_at has one-second resolution, and the queue orders by it. The sleep
	// is what puts the film genuinely behind the backlog rather than relying on
	// how SQLite happens to break ties.
	time.Sleep(1100 * time.Millisecond)

	id := addItem(t, st, lib, "/films/arrival.mkv", "Arrival", 2016)

	p := &fakeProvider{
		id:     "fake",
		cands:  []meta.Candidate{{Provider: "fake", ExternalID: "329865", Kind: meta.KindMovie, Title: "Arrival", Year: 2016, Popularity: 40}},
		record: arrivalRecord(),
	}
	reg := meta.NewRegistry()
	reg.AddProvider(p)

	w := New(st, reg, &fakeArt{}, quietLog())
	if err := w.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	it, err := st.GetItem(ctx, id, "local")
	if err != nil {
		t.Fatal(err)
	}
	if it.MatchState == "unmatched" {
		t.Fatalf("the film was never enriched: match_state = %q, provider searched %d times\n\n"+
			"This is the bug: %d un-enrichable items sit ahead of it in the queue, "+
			"the first batch stamps nothing, and the worker stops the entire run "+
			"instead of looking past that batch. Anything added after an "+
			"un-enrichable backlog is never reached.",
			it.MatchState, p.searchN, 60)
	}
}

// The guard the fix must not break: when the whole queue is un-enrichable there
// is genuinely no progress to make, and the worker must stop rather than spin
// re-reading the same rows.
func TestWorkerStopsWhenNothingCanBeEnriched(t *testing.T) {
	st, lib := harness(t)
	ctx := context.Background()

	for i := 0; i < 60; i++ {
		addKind(t, st, lib, fmt.Sprintf("/music/track-%02d.flac", i), "track")
	}

	p := &fakeProvider{id: "fake"}
	reg := meta.NewRegistry()
	reg.AddProvider(p)

	w := New(st, reg, &fakeArt{}, quietLog())

	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not stop on an entirely un-enrichable queue; it is spinning")
	}
}

func addKind(t *testing.T, st *store.Store, lib *store.Library, path, kind string) int64 {
	t.Helper()
	id, err := st.UpsertItem(context.Background(), store.ScanFile{
		LibraryID: lib.ID, Path: filepath.FromSlash(path), Kind: kind,
		Title: filepath.Base(path), SortTitle: filepath.Base(path),
		Container: "flac", SizeBytes: 1, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}
