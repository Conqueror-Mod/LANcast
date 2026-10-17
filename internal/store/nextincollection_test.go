package store

import (
	"context"
	"testing"
)

func seq(states ...string) ([]Item, []int64) {
	items := make([]Item, len(states))
	touched := make([]int64, len(states))
	for i, s := range states {
		items[i] = Item{ID: int64(i + 1)}
		switch s {
		case "watched":
			items[i].Progress = &Progress{Watched: true}
		case "part":
			items[i].Progress = &Progress{PositionMS: 60_000}
		}
		touched[i] = int64(100 + i)
	}
	return items, touched
}

// The same four rules a show follows, over a franchise in release order.
func TestNextInSequence(t *testing.T) {
	cases := []struct {
		name      string
		states    []string
		want      int64 // 0 for none
		resume    bool
		exhausted bool
		started   bool
	}{
		{"nothing watched starts at the first", []string{"", "", ""}, 1, false, false, false},
		{"after the one finished", []string{"watched", "", ""}, 2, false, false, true},
		{"forward only: a skipped film is not gone back to", []string{"", "watched", ""}, 3, false, false, true},
		{"in progress wins", []string{"watched", "", "part"}, 3, true, false, true},
		{"everything after the one finished most recently", []string{"", "", "watched"}, 0, false, true, true},
		{"all watched", []string{"watched", "watched"}, 0, false, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			items, touched := seq(c.states...)
			got, _ := nextInSequence(items, touched)
			var id int64
			if got.Item != nil {
				id = got.Item.ID
			}
			if id != c.want || got.Resume != c.resume || got.Exhausted != c.exhausted || got.Started != c.started {
				t.Fatalf("got item %d resume %v exhausted %v started %v; want %d %v %v %v",
					id, got.Resume, got.Exhausted, got.Started, c.want, c.resume, c.exhausted, c.started)
			}
		})
	}
}

/*
 * The report this rule was changed for, in its own shape: Saw, Saw II and
 * Saw III watched in order, then Saw 3D (seventh by release) out of order,
 * then Saw IV and Saw V. The next film is Saw VI, counted from Saw V, the one
 * just finished -- not Jigsaw, counted from Saw 3D, the furthest.
 */
func TestTheNextFilmFollowsTheOneJustFinished(t *testing.T) {
	// Release order: Saw, II, III, IV, V, VI, 3D, Jigsaw.
	items, touched := seq("watched", "watched", "watched", "watched", "watched", "", "watched", "")
	// When each was finished: 3D (index 6) before IV and V.
	order := map[int]int64{0: 1, 1: 2, 2: 3, 6: 4, 3: 5, 4: 6}
	for i := range touched {
		touched[i] = order[i]
	}
	got, from := nextInSequence(items, touched)
	if got.Item == nil || got.Item.ID != 6 {
		t.Fatalf("next = %+v, want Saw VI (id 6)", got.Item)
	}
	if from != 4 {
		t.Errorf("counted from index %d, want 4 (Saw V, the one just finished)", from)
	}
}

/*
 * Finishing a film in a collection puts the next one on Continue Watching,
 * the way finishing an episode leaves the show there (settled 2026-10-12),
 * and Remove from Continue Watching takes it off without anything new to
 * store.
 */
func TestContinueWatchingOffersTheNextFilmInACollection(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	id := func(path, title string) int64 {
		if _, err := st.UpsertItem(ctx, file(lib.ID, path, title)); err != nil {
			t.Fatal(err)
		}
		known, _ := st.KnownFiles(ctx, lib.ID)
		return known[path].ID
	}
	one := id(`C:\m\f1.mkv`, "Franchise 1")
	two := id(`C:\m\f2.mkv`, "Franchise 2")
	three := id(`C:\m\f3.mkv`, "Franchise 3")

	coll, _, err := st.EnsureCollection(ctx, lib.ID, "tmdb", "1", "Franchise", "Franchise")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []int64{one, two, three} {
		if err := st.AddToCollection(ctx, f, coll, 0); err != nil {
			t.Fatal(err)
		}
	}

	shelf := func() []int64 {
		got, err := st.ContinueWatching(ctx, "local", 20, 0)
		if err != nil {
			t.Fatal(err)
		}
		var ids []int64
		for _, it := range got {
			ids = append(ids, it.ID)
		}
		return ids
	}

	if got := shelf(); len(got) != 0 {
		t.Fatalf("an untouched collection is on the shelf: %v", got)
	}

	// Film 1 finished: film 2 is next, and it is the film, not the collection.
	if err := st.SaveProgress(ctx, one, "local", 0, true); err != nil {
		t.Fatal(err)
	}
	if got := shelf(); len(got) != 1 || got[0] != two {
		t.Fatalf("after finishing film 1 the shelf is %v, want [film 2 = %d]", got, two)
	}
	// And the row says what it follows, for the spotlight's "Because you watched".
	rows, _ := st.ContinueWatching(ctx, "local", 20, 0)
	if c := rows[0].ContinuesFrom; c == nil || c.CollectionID != coll || c.AfterID != one || c.AfterTitle != "Franchise 1" {
		t.Fatalf("continues_from = %+v; want collection %d after film 1", c, coll)
	}

	// The button asks the same question and gets the same answer.
	next, err := st.NextInCollectionFor(ctx, coll, "local")
	if err != nil || next.Item == nil || next.Item.ID != two || !next.Started {
		t.Fatalf("NextInCollectionFor = %+v, %v; want film 2, started", next, err)
	}

	// Removed from the shelf: the Home menu writes the film unwatched at zero.
	if err := st.SaveProgress(ctx, two, "local", 0, false); err != nil {
		t.Fatal(err)
	}
	if got := shelf(); len(got) != 0 {
		t.Fatalf("Remove from Continue Watching left %v on the shelf", got)
	}

	// Film 2 started: it is on the shelf on its own account, once.
	if err := st.SaveProgress(ctx, two, "local", 600_000, false); err != nil {
		t.Fatal(err)
	}
	if got := shelf(); len(got) != 1 || got[0] != two {
		t.Fatalf("film 2 in progress gives a shelf of %v, want it once", got)
	}

	// Watched to the end of the franchise: nothing to continue.
	for _, f := range []int64{two, three} {
		if err := st.SaveProgress(ctx, f, "local", 0, true); err != nil {
			t.Fatal(err)
		}
	}
	if got := shelf(); len(got) != 0 {
		t.Fatalf("a finished franchise is still on the shelf: %v", got)
	}
}
