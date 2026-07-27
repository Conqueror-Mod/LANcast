package store

import (
	"context"
	"testing"
)

// mkItem upserts a bare item of the given kind and returns its id.
func mkItem(t *testing.T, st *Store, libID int64, kind, path, title string) int64 {
	t.Helper()
	id, err := st.UpsertItem(context.Background(), ScanFile{
		LibraryID: libID, Path: path, Kind: kind,
		Title: title, SortTitle: title,
	})
	if err != nil {
		t.Fatalf("UpsertItem(%s %q): %v", kind, path, err)
	}
	return id
}

func ids(items []Item) map[int64]bool {
	m := map[int64]bool{}
	for _, it := range items {
		m[it.ID] = true
	}
	return m
}

// A collection is worth showing only when it groups two or more present
// members. A provider hands us a franchise even for a single owned film, and a
// collection of one is just a duplicate tile of that film (the Aladdin-Collection
// clutter). Its member count is reported through ChildCount like any container.
func TestSingletonCollectionsHiddenFromGrid(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	// A real series: two owned films grouped.
	toy1 := mkItem(t, st, lib.ID, "movie", "/m/Toy1.mkv", "Toy Story")
	toy2 := mkItem(t, st, lib.ID, "movie", "/m/Toy2.mkv", "Toy Story 2")
	toyColl := mkItem(t, st, lib.ID, "collection", "/c/toy", "Toy Story Collection")
	// A singleton: one owned film that names a franchise.
	aladdin := mkItem(t, st, lib.ID, "movie", "/m/Aladdin.mkv", "Aladdin")
	aladdinColl := mkItem(t, st, lib.ID, "collection", "/c/aladdin", "Aladdin Collection")

	for _, l := range []struct {
		item, coll int64
		ord        int
	}{{toy1, toyColl, 0}, {toy2, toyColl, 1}, {aladdin, aladdinColl, 0}} {
		if err := st.AddToCollection(ctx, l.item, l.coll, l.ord); err != nil {
			t.Fatal(err)
		}
	}

	top, _, err := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID, TopLevel: true})
	if err != nil {
		t.Fatal(err)
	}
	got := ids(top)
	if !got[toyColl] {
		t.Error("Toy Story Collection (2 members) missing from the grid")
	}
	if got[aladdinColl] {
		t.Error("Aladdin Collection (1 member) leaked into the grid — singletons must hide")
	}
	// The films themselves always show.
	if !got[toy1] || !got[aladdin] {
		t.Error("member films must remain top-level")
	}

	// ChildCount reflects join-table membership for a collection.
	coll := []Item{{ID: toyColl}}
	if err := st.AttachChildCounts(ctx, coll); err != nil {
		t.Fatal(err)
	}
	if coll[0].ChildCount != 2 {
		t.Errorf("Toy collection ChildCount = %d, want 2", coll[0].ChildCount)
	}
}

// The browse view filters by genre and decade, and facets report only the
// values actually present so a filter never empties the grid.
func TestBrowseFiltersAndFacets(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	mk := func(path, title string, year int, genres []string) int64 {
		y := year
		id, err := st.UpsertItem(ctx, ScanFile{
			LibraryID: lib.ID, Path: path, Kind: "movie", Title: title,
			SortTitle: title, Year: &y, Container: "mkv", SizeBytes: 1, MTime: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.ReplaceGenres(ctx, id, genres); err != nil {
			t.Fatal(err)
		}
		return id
	}
	a := mk("/m/a.mkv", "Arrival", 2016, []string{"Drama", "Science Fiction"})
	mk("/m/b.mkv", "Booksmart", 2019, []string{"Comedy"})
	c := mk("/m/c.mkv", "Contact", 1997, []string{"Drama"})

	// Facets: sorted genres, decades newest-first.
	f, err := st.LibraryFacets(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Genres; len(got) != 3 || got[0] != "Comedy" || got[2] != "Science Fiction" {
		t.Errorf("genres = %v, want [Comedy Drama Science Fiction]", got)
	}
	if got := f.Decades; len(got) != 2 || got[0] != 2010 || got[1] != 1990 {
		t.Errorf("decades = %v, want [2010 1990]", got)
	}

	// Genre filter.
	drama, _, _ := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID, TopLevel: true, Genre: "Drama"})
	if g := ids(drama); len(drama) != 2 || !g[a] || !g[c] {
		t.Errorf("Drama = %v, want Arrival + Contact", g)
	}

	// Decade filter, combined with genre.
	both, total, _ := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID, TopLevel: true, Genre: "Drama", Decade: 2010})
	if len(both) != 1 || both[0].ID != a || total != 1 {
		t.Errorf("Drama+2010s = %v (total %d), want just Arrival", ids(both), total)
	}
}

// A parented child (season, episode, part, chapter) must never appear in the
// default top-level grid — it belongs under its parent. This is the guard ADR
// 0010 and ADR 0017 require; without it a container's pieces leak in as if they
// were features.
func TestTopLevelFilterExcludesChildren(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	movie := mkItem(t, st, lib.ID, "movie", "/m/Arrival.mkv", "Arrival")
	show := mkItem(t, st, lib.ID, "show", "/tv/Show", "Some Show")
	episode := mkItem(t, st, lib.ID, "episode", "/tv/Show/S01E01.mkv", "Pilot")

	if err := st.SetParent(ctx, episode, &show); err != nil {
		t.Fatalf("SetParent: %v", err)
	}

	top, total, err := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID, TopLevel: true})
	if err != nil {
		t.Fatalf("ListItems top-level: %v", err)
	}
	got := ids(top)
	if !got[movie] || !got[show] {
		t.Errorf("top-level = %v, want movie %d and show %d present", got, movie, show)
	}
	if got[episode] {
		t.Errorf("top-level includes parented episode %d — it should be hidden under its show", episode)
	}
	if total != 2 {
		t.Errorf("top-level total = %d, want 2 (movie + show)", total)
	}

	// Without the flag, everything is returned — the flag is opt-in so existing
	// callers are unaffected.
	all, _, err := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID})
	if err != nil {
		t.Fatalf("ListItems all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("unfiltered = %d items, want 3", len(all))
	}
}

// A ParentID filter returns exactly that item's children, and overrides
// TopLevel — it is how a detail page loads a show's episodes or a work's parts.
func TestChildrenReturnsContained(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	show := mkItem(t, st, lib.ID, "show", "/tv/Show", "Some Show")
	e1 := mkItem(t, st, lib.ID, "episode", "/tv/Show/S01E01.mkv", "Pilot")
	e2 := mkItem(t, st, lib.ID, "episode", "/tv/Show/S01E02.mkv", "Second")
	other := mkItem(t, st, lib.ID, "episode", "/tv/Other/S01E01.mkv", "Unrelated")
	for _, id := range []int64{e1, e2} {
		if err := st.SetParent(ctx, id, &show); err != nil {
			t.Fatalf("SetParent(%d): %v", id, err)
		}
	}

	kids, err := st.Children(ctx, show)
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	got := ids(kids)
	if len(kids) != 2 || !got[e1] || !got[e2] || got[other] {
		t.Errorf("Children(show) = %v, want exactly episodes %d and %d", got, e1, e2)
	}
}

// Collection membership is many-to-many and independent of parent_id: a member
// stays top-level and may belong to more than one collection (ADR 0017).
func TestCollectionMembership(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	anne1 := mkItem(t, st, lib.ID, "movie", "/m/Anne1.mkv", "Anne of Green Gables")
	anne2 := mkItem(t, st, lib.ID, "movie", "/m/Anne2.mkv", "Anne of Avonlea")
	series := mkItem(t, st, lib.ID, "collection", "/c/anne", "Anne Series")
	themed := mkItem(t, st, lib.ID, "collection", "/c/cozy", "Cozy Classics")

	if err := st.AddToCollection(ctx, anne1, series, 0); err != nil {
		t.Fatalf("AddToCollection: %v", err)
	}
	if err := st.AddToCollection(ctx, anne2, series, 1); err != nil {
		t.Fatalf("AddToCollection: %v", err)
	}
	// A member can belong to a second collection.
	if err := st.AddToCollection(ctx, anne1, themed, 0); err != nil {
		t.Fatalf("AddToCollection second: %v", err)
	}

	members, err := st.CollectionMembers(ctx, series)
	if err != nil {
		t.Fatalf("CollectionMembers: %v", err)
	}
	if len(members) != 2 || members[0].ID != anne1 || members[1].ID != anne2 {
		t.Errorf("members = %v, want [anne1 %d, anne2 %d] in order", ids(members), anne1, anne2)
	}

	cols, err := st.CollectionsOf(ctx, anne1)
	if err != nil {
		t.Fatalf("CollectionsOf: %v", err)
	}
	if len(cols) != 2 {
		t.Errorf("CollectionsOf(anne1) = %d collections, want 2", len(cols))
	}

	// Members remain top-level items: membership is not containment, so it must
	// not hide them from the grid.
	top, _, err := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID, Kind: "movie", TopLevel: true})
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	if g := ids(top); !g[anne1] || !g[anne2] {
		t.Errorf("collection members missing from top-level movies: %v", g)
	}

	// Re-adding updates order rather than erroring — ingestion is idempotent.
	if err := st.AddToCollection(ctx, anne1, series, 5); err != nil {
		t.Fatalf("re-add: %v", err)
	}
	if err := st.RemoveFromCollection(ctx, anne2, series); err != nil {
		t.Fatalf("RemoveFromCollection: %v", err)
	}
	members, _ = st.CollectionMembers(ctx, series)
	if len(members) != 1 || members[0].ID != anne1 {
		t.Errorf("after remove, members = %v, want just anne1 %d", ids(members), anne1)
	}
}
