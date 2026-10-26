package store

import (
	"context"
	"testing"
)

/*
 * A collection says how far through it this account is.
 *
 * Reported: the Collections page showed nothing about whether a box set had
 * been watched or begun. A film carries its own answer and a show had
 * unwatched_episodes; a collection had neither.
 */
func TestACollectionReportsHowFarThroughItThisAccountIs(t *testing.T) {
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
	empty, _, err := st.EnsureCollection(ctx, lib.ID, "tmdb", "2", "Empty", "Empty")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []int64{one, two, three} {
		if err := st.AddToCollection(ctx, f, coll, 0); err != nil {
			t.Fatal(err)
		}
	}

	standing := func(user string) (*CollectionProgress, *CollectionProgress, *CollectionProgress) {
		items := []Item{{ID: coll, Kind: "collection"}, {ID: empty, Kind: "collection"}, {ID: one, Kind: "movie"}}
		if err := st.AttachCollectionProgress(ctx, items, user); err != nil {
			t.Fatal(err)
		}
		return items[0].CollectionProgress, items[1].CollectionProgress, items[2].CollectionProgress
	}

	p, e, film := standing("local")
	if p == nil || *p != (CollectionProgress{Total: 3}) {
		t.Fatalf("untouched collection = %+v, want 3 films, none begun", p)
	}
	// Absent, not zero: an empty box set is not finished, and a film is not a collection.
	if e != nil {
		t.Errorf("a collection with no films reported %+v; want it absent", e)
	}
	if film != nil {
		t.Errorf("a film was given collection progress: %+v", film)
	}

	// One finished, one part way: begun, not finished.
	if err := st.SaveProgress(ctx, one, "local", 0, true); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProgress(ctx, two, "local", 60_000, false); err != nil {
		t.Fatal(err)
	}
	p, _, _ = standing("local")
	if *p != (CollectionProgress{Total: 3, Watched: 1, InProgress: 1}) {
		t.Fatalf("after one finished and one started = %+v", *p)
	}

	// Per account: somebody else's viewing is not this account's.
	p, _, _ = standing("u_other")
	if *p != (CollectionProgress{Total: 3}) {
		t.Errorf("another account sees %+v; want untouched", *p)
	}

	// All three finished.
	for _, f := range []int64{two, three} {
		if err := st.SaveProgress(ctx, f, "local", 0, true); err != nil {
			t.Fatal(err)
		}
	}
	p, _, _ = standing("local")
	if *p != (CollectionProgress{Total: 3, Watched: 3}) {
		t.Errorf("finished collection = %+v", *p)
	}
}
