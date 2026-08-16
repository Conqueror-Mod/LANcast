package store

import (
	"context"
	"strings"
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
	f, err := st.LibraryFacets(ctx, lib.ID, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Genres; len(got) != 3 || got[0] != "Comedy" || got[2] != "Science Fiction" {
		t.Errorf("genres = %v, want [Comedy Drama Science Fiction]", got)
	}
	if got := f.Decades; len(got) != 2 || got[0] != 2010 || got[1] != 1990 {
		t.Errorf("decades = %v, want [2010 1990]", got)
	}
	if f.HasWatched {
		t.Errorf("HasWatched = true with nothing watched, want false")
	}

	// Genre filter.
	drama, _, _ := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID, TopLevel: true, Genres: []string{"Drama"}})
	if g := ids(drama); len(drama) != 2 || !g[a] || !g[c] {
		t.Errorf("Drama = %v, want Arrival + Contact", g)
	}

	// Two genres widen (OR within the facet): Comedy or Science Fiction.
	widen, _, _ := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID, TopLevel: true, Genres: []string{"Comedy", "Science Fiction"}})
	if len(widen) != 2 {
		t.Errorf("Comedy|SciFi = %v, want Arrival + Booksmart", ids(widen))
	}

	// Decade filter, combined with genre (AND across facets).
	both, total, _ := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID, TopLevel: true, Genres: []string{"Drama"}, Decades: []int{2010}})
	if len(both) != 1 || both[0].ID != a || total != 1 {
		t.Errorf("Drama+2010s = %v (total %d), want just Arrival", ids(both), total)
	}

	// Two decades widen: 1990s or 2010s (both Drama films).
	twoDec, _, _ := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID, TopLevel: true, Genres: []string{"Drama"}, Decades: []int{1990, 2010}})
	if len(twoDec) != 2 {
		t.Errorf("Drama+{1990s,2010s} = %v, want Arrival + Contact", ids(twoDec))
	}

	// Unwatched filter: mark Arrival watched, it drops out.
	if err := st.SaveProgress(ctx, a, "u1", 1000, true); err != nil {
		t.Fatal(err)
	}
	unwatched, _, _ := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID, TopLevel: true, Unwatched: true, UserID: "u1"})
	if g := ids(unwatched); g[a] || len(unwatched) != 2 {
		t.Errorf("unwatched = %v, want Booksmart + Contact (not Arrival)", g)
	}
	// A different user has watched nothing, so sees all three.
	other, _, _ := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID, TopLevel: true, Unwatched: true, UserID: "u2"})
	if len(other) != 3 {
		t.Errorf("unwatched for u2 = %v, want all three", ids(other))
	}
	// And the facet now reports a watched item exists for u1.
	f2, _ := st.LibraryFacets(ctx, lib.ID, "u1")
	if !f2.HasWatched {
		t.Errorf("HasWatched = false after watching Arrival, want true")
	}
	if len(f2.ContentRatings) != 0 {
		t.Errorf("ContentRatings = %v, want empty (none set)", f2.ContentRatings)
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

/*
 * The Collections page asks for kind=collection, which is not a top-level
 * query — and that listing showed every singleton the grid refuses.
 *
 * Found by piloting a real library: a "Hitman Collection" containing Hitman, an
 * "Aquaman Collection" containing Aquaman, a "Deadpool Collection" containing
 * Deadpool, and a hundred more, on a page whose own empty state promises "a
 * collection appears once at least two of its films are in this library".
 *
 * The rule is a fact about collections, not about the grid, so it now applies
 * to any listing rather than to the ones somebody remembered.
 */
func TestSingletonCollectionsHiddenFromKindListing(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	toy1 := mkItem(t, st, lib.ID, "movie", "/m/Toy1.mkv", "Toy Story")
	toy2 := mkItem(t, st, lib.ID, "movie", "/m/Toy2.mkv", "Toy Story 2")
	toyColl := mkItem(t, st, lib.ID, "collection", "/c/toy", "Toy Story Collection")
	hitman := mkItem(t, st, lib.ID, "movie", "/m/Hitman.mkv", "Hitman")
	hitmanColl := mkItem(t, st, lib.ID, "collection", "/c/hitman", "Hitman Collection")

	for _, l := range []struct {
		item, coll int64
		ord        int
	}{{toy1, toyColl, 0}, {toy2, toyColl, 1}, {hitman, hitmanColl, 0}} {
		if err := st.AddToCollection(ctx, l.item, l.coll, l.ord); err != nil {
			t.Fatal(err)
		}
	}

	items, total, err := st.ListItems(ctx, ItemFilter{LibraryID: lib.ID, Kind: "collection"})
	if err != nil {
		t.Fatal(err)
	}
	got := ids(items)
	if !got[toyColl] {
		t.Error("a real collection is missing from the collections listing")
	}
	if got[hitmanColl] {
		t.Error("a one-film collection is listed — it is a duplicate tile of that film")
	}
	// The total has to agree with the rows, or the page reports a number it
	// cannot show.
	if total != len(items) {
		t.Errorf("total = %d, rows = %d", total, len(items))
	}
}

// A collection's own members are still listed in full. The rule hides a
// collection from listings of collections; it must not hide what is inside one.
func TestCollectionMembersAreStillListed(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	a := mkItem(t, st, lib.ID, "movie", "/m/A.mkv", "A")
	b := mkItem(t, st, lib.ID, "movie", "/m/B.mkv", "B")
	coll := mkItem(t, st, lib.ID, "collection", "/c/ab", "AB Collection")
	for i, id := range []int64{a, b} {
		if err := st.AddToCollection(ctx, id, coll, i); err != nil {
			t.Fatal(err)
		}
	}

	members, err := st.CollectionMembers(ctx, coll)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 {
		t.Errorf("members = %d, want 2", len(members))
	}
}

/*
 * A playlist is not an artist, and the music grid was full of them.
 *
 * Found by piloting a real library: tiles named `00-health-rat_wars-16bit-web-
 * flac-2023` sat among the artists. They were the `.m3u` files scene releases
 * ship beside the audio — imported correctly, listed in the wrong place, one
 * tile per release. `exclude_kind` took a single kind, so the second one had
 * nowhere to go.
 */
func TestExcludeKindsDropsSeveralKinds(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	artist := mkItem(t, st, lib.ID, "artist", "/a/health", "HEALTH")
	playlist := mkItem(t, st, lib.ID, "playlist", "/p/rat_wars.m3u", "00-health-rat_wars")
	film := mkItem(t, st, lib.ID, "movie", "/m/A.mkv", "A Film")
	b := mkItem(t, st, lib.ID, "movie", "/m/B.mkv", "B Film")
	coll := mkItem(t, st, lib.ID, "collection", "/c/ab", "AB Collection")
	for i, id := range []int64{film, b} {
		if err := st.AddToCollection(ctx, id, coll, i); err != nil {
			t.Fatal(err)
		}
	}

	items, _, err := st.ListItems(ctx, ItemFilter{
		LibraryID:    lib.ID,
		TopLevel:     true,
		ExcludeKinds: []string{"collection", "playlist"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := ids(items)
	if got[playlist] {
		t.Error("a playlist is in the grid — it groups tracks rather than being one")
	}
	if got[coll] {
		t.Error("a collection is in the grid")
	}
	if !got[artist] || !got[film] {
		t.Error("excluding container kinds took real items with it")
	}
}

// One kind still works: the parameter grew a list without changing what a
// single value means, so a client written against the old contract is fine.
func TestExcludeKindsAcceptsOne(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	artist := mkItem(t, st, lib.ID, "artist", "/a/health", "HEALTH")
	playlist := mkItem(t, st, lib.ID, "playlist", "/p/x.m3u", "X")

	items, _, err := st.ListItems(ctx, ItemFilter{
		LibraryID: lib.ID, TopLevel: true, ExcludeKinds: []string{"playlist"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := ids(items)
	if got[playlist] || !got[artist] {
		t.Errorf("single-kind exclusion changed meaning: %+v", items)
	}
}

/*
 * The sidebar count and the grid have to answer the same question.
 *
 * They did not, and this was the original report: a real library's sidebar read
 * 1,381 beside a grid that said 1,211 — the difference exactly its 170
 * collections — and the music sidebar read 1,177 beside a grid of 1,171, the
 * difference exactly its 6 imported `.m3u` playlists.
 *
 * It survived three separate fixes to the grid, because each one changed what
 * the grid *showed* and none changed what the count *counted*.
 */
func TestLibraryCountMatchesTheGrid(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	a := mkItem(t, st, lib.ID, "movie", "/m/A.mkv", "A Film")
	b := mkItem(t, st, lib.ID, "movie", "/m/B.mkv", "B Film")
	coll := mkItem(t, st, lib.ID, "collection", "/c/ab", "AB Collection")
	for i, id := range []int64{a, b} {
		if err := st.AddToCollection(ctx, id, coll, i); err != nil {
			t.Fatal(err)
		}
	}
	mkItem(t, st, lib.ID, "playlist", "/p/mix.m3u", "A Mix")

	_, gridTotal, err := st.ListItems(ctx, ItemFilter{
		LibraryID: lib.ID, TopLevel: true, ExcludeKinds: GroupingKinds,
	})
	if err != nil {
		t.Fatal(err)
	}

	libs, err := st.ListLibraries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	for _, l := range libs {
		if l.ID == lib.ID {
			count = l.ItemCount
		}
	}

	if count != gridTotal {
		t.Errorf("library count = %d, grid total = %d — the sidebar and the grid disagree",
			count, gridTotal)
	}
	if gridTotal != 2 {
		t.Errorf("grid total = %d, want 2 films", gridTotal)
	}
}

// The SQL predicate is hand-written for the same reason its neighbours are, so
// this is what stops it drifting from the list the API hands to clients.
func TestGroupingKindsMatchTheCountPredicate(t *testing.T) {
	for _, k := range GroupingKinds {
		if !strings.Contains(notAGroupingKind, "'"+k+"'") {
			t.Errorf("GroupingKinds has %q but the count predicate does not exclude it", k)
		}
	}
	// And nothing extra hides in the predicate that the API would not exclude.
	if got := strings.Count(notAGroupingKind, "'"); got != len(GroupingKinds)*2 {
		t.Errorf("the predicate names %d kinds, GroupingKinds has %d",
			got/2, len(GroupingKinds))
	}
}
