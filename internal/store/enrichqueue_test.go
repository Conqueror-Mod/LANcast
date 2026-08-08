package store

import (
	"context"
	"testing"
)

// The bug this pins, in one sentence: a music library starved video metadata.
//
// The queue is ordered oldest-first and music rows can never be matched — no
// music provider exists (ADR 0024) — so on the real library 1,592 tracks, 394
// albums and 206 artists sat ahead of 21 films, the worker made no progress on
// the first batch and stopped, and no poster ever appeared. The films were
// never reached, not slow.
func TestMusicDoesNotBlockTheEnrichmentQueue(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := musicLibrary(t, st)

	// Music first, so it is oldest and would head the queue.
	albumID := seedAlbum(t, st, lib, "Some Band", "A Record")
	for i, name := range []string{"one.flac", "two.flac", "three.flac"} {
		seedTrack(t, st, lib, albumID, lib.Path+"/"+name, name, 1, i+1)
	}

	film := mkItem(t, st, lib.ID, "movie", lib.Path+"/A Film (1999).mkv", "A Film")

	pending, err := st.PendingEnrichment(ctx, 50)
	if err != nil {
		t.Fatalf("PendingEnrichment: %v", err)
	}

	var sawFilm bool
	for _, it := range pending {
		switch it.Kind {
		case "track", "album", "artist":
			t.Errorf("%s %q is queued for enrichment; no provider can match it",
				it.Kind, it.Title)
		case "movie":
			if it.ID == film {
				sawFilm = true
			}
		}
	}
	if !sawFilm {
		t.Error("the film is not in the queue — it is the only thing that can be enriched")
	}
}

// The count drives the Settings display and the worker's own idea of how much
// is left. Counting work that can never be done makes "2,213 remaining" a
// number that never falls, which is worse than no number.
func TestPendingCountIgnoresMusic(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := musicLibrary(t, st)

	albumID := seedAlbum(t, st, lib, "Some Band", "A Record")
	seedTrack(t, st, lib, albumID, lib.Path+"/one.flac", "One", 1, 1)
	mkItem(t, st, lib.ID, "movie", lib.Path+"/A Film (1999).mkv", "A Film")

	n, err := st.PendingCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("pending count = %d, want 1 — only the film can be enriched", n)
	}
}

// Episodes, shows and seasons still queue: they have a provider, and excluding
// them would be the same bug pointed at television.
func TestVideoContainersStillQueue(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := musicLibrary(t, st)

	show := mkItem(t, st, lib.ID, "show", lib.Path+"/Show", "A Show")
	ep := mkItem(t, st, lib.ID, "episode", lib.Path+"/Show/S01E01.mkv", "Pilot")

	pending, err := st.PendingEnrichment(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int64]bool{}
	for _, it := range pending {
		seen[it.ID] = true
	}
	if !seen[show] || !seen[ep] {
		t.Errorf("show or episode missing from the queue: %v", seen)
	}
}
