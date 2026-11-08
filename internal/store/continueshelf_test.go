package store

import (
	"context"
	"fmt"
	"testing"
)

/*
 * A shelf has a limit of its own, so a run of half-played songs cannot push the
 * films off Continue Watching.
 *
 * The shape is the owner's library on 2026-11-03: films paused earlier, then an
 * evening of music skipped part-way through. In one mixed list of 20 the
 * songs took every slot. The first assertion pins that, so the test shows the
 * fault it guards against rather than only the absence of it.
 */
func TestContinueShelvesHaveTheirOwnLimits(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	at := int64(1_800_000_000)
	add := func(path, title, kind string) int64 {
		t.Helper()
		f := file(lib.ID, path, title)
		f.Kind = kind
		if _, err := st.UpsertItem(ctx, f); err != nil {
			t.Fatal(err)
		}
		known, _ := st.KnownFiles(ctx, lib.ID)
		id := known[path].ID
		if err := st.SaveProgress(ctx, id, "local", 60_000, false); err != nil {
			t.Fatal(err)
		}
		// Each one played after the last, so the order is certain.
		at++
		if _, err := st.db.ExecContext(ctx,
			`UPDATE playback_state SET updated_at = ? WHERE item_id = ?`, at, id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	add(`C:\m\film-1.mkv`, "Film 1", "movie")
	add(`C:\m\film-2.mkv`, "Film 2", "movie")
	add(`C:\b\book.m4b`, "A Book", "book")
	for i := 0; i < 25; i++ {
		add(fmt.Sprintf(`C:\s\%02d.mp3`, i), fmt.Sprintf("Song %02d", i), "track")
	}

	kinds := func(items []Item) map[string]int {
		n := map[string]int{}
		for _, it := range items {
			n[it.Kind]++
		}
		return n
	}

	all, err := st.ContinueWatching(ctx, "local", 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := kinds(all); got["movie"] != 0 || got["track"] != 20 {
		t.Fatalf("mixed list = %v, want the songs filling all 20 (the fault)", got)
	}

	cases := []struct {
		shelf ContinueShelf
		want  map[string]int
	}{
		{ShelfWatching, map[string]int{"movie": 2}},
		{ShelfMusic, map[string]int{"track": 20}},
		{ShelfBooks, map[string]int{"book": 1}},
	}
	for _, c := range cases {
		got, err := st.ContinueOn(ctx, "local", c.shelf, 20, 0)
		if err != nil {
			t.Fatalf("%s: %v", c.shelf, err)
		}
		k := kinds(got)
		if len(k) != len(c.want) {
			t.Errorf("%s shelf = %v, want %v", c.shelf, k, c.want)
			continue
		}
		for kind, n := range c.want {
			if k[kind] != n {
				t.Errorf("%s shelf = %v, want %v", c.shelf, k, c.want)
			}
		}
	}

	if _, err := st.ContinueOn(ctx, "local", ContinueShelf("films"), 20, 0); err == nil {
		t.Error("an unknown shelf was accepted")
	}
}

// Shows and collections are watched, never heard: the music and books shelves
// leave them out, and the watching shelf keeps them.
func TestContinueShelvesPlaceShowsOnWatching(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	_, eps := seedShow(t, st)
	markWatched(t, st, eps[0], "local")

	watching, err := st.ContinueOn(ctx, "local", ShelfWatching, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(watching) != 1 || watching[0].Kind != "show" {
		t.Fatalf("watching shelf = %+v, want the show", watching)
	}
	for _, shelf := range []ContinueShelf{ShelfMusic, ShelfBooks} {
		got, err := st.ContinueOn(ctx, "local", shelf, 20, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Errorf("%s shelf holds %d rows, want none", shelf, len(got))
		}
	}
}
