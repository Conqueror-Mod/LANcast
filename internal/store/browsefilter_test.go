package store

/*
 * The browse filters added for the filter-shell work: resolution, year, cast
 * and status.
 *
 * The resolution cases are the ones worth having. A tier is a bucket over a
 * width, and the boundaries are chosen against how real files are actually
 * encoded rather than against the nominal number — so the tests that matter are
 * the off-nominal ones, because those are the files a library is full of.
 */

import (
	"context"
	"path/filepath"
	"testing"
)

func TestResolutionBucketsCoverRealWidths(t *testing.T) {
	cases := []struct {
		width int
		want  string
		why   string
	}{
		{3840, "uhd", "nominal 4K"},
		{4096, "uhd", "DCI 4K is wider than UHD and is still 4K"},
		{1920, "hd1080", "nominal 1080p"},
		{1912, "hd1080", "1080p cropped by a few pixels is still 1080p"},
		{1280, "hd720", "nominal 720p"},
		{720, "sd", "PAL DVD"},
		{640, "sd", "an old rip"},
	}
	for _, c := range cases {
		b, ok := resolutionBucket(c.width)
		if !ok {
			t.Errorf("width %d (%s): no bucket", c.width, c.why)
			continue
		}
		if b.Key != c.want {
			t.Errorf("width %d (%s) = %s, want %s", c.width, c.why, b.Key, c.want)
		}
	}
}

/*
 * A scope film at 4K is 3840x1608 rather than 3840x2160 — same format, height
 * 550px lower. Bucketing on height would file it as 1080p, which is the whole
 * reason the rule reads width.
 */
func TestScopeFilmIsNotDemotedATier(t *testing.T) {
	b, ok := resolutionBucket(3840)
	if !ok || b.Key != "uhd" {
		t.Fatalf("3840 wide = %v (%v), want uhd", b.Key, ok)
	}
}

// An unprobed file has no width, and "no width" is not SD — it is unknown. A
// file filed under a resolution it never claimed is worse than one left out.
func TestUnprobedWidthHasNoBucket(t *testing.T) {
	if b, ok := resolutionBucket(0); ok {
		t.Errorf("width 0 = %s, want no bucket", b.Key)
	}
}

func TestBucketKeysAreUniqueAndOrdered(t *testing.T) {
	seen := map[string]bool{}
	last := 1 << 30
	for _, b := range ResolutionBuckets {
		if seen[b.Key] {
			t.Errorf("duplicate bucket key %q", b.Key)
		}
		seen[b.Key] = true
		// Widest first, so a facet list emitted in table order reads the way a
		// person expects without the caller sorting it.
		if b.MinWidth > last {
			t.Errorf("bucket %q is wider than the one before it", b.Key)
		}
		last = b.MinWidth
	}
}

// seedLibrary makes a library with two films: one 4K from 1994 and one SD from
// 2003, the second credited to a person the first is not.
func seedLibrary(t *testing.T, st *Store) (libID, uhdID, sdID, personFilm int64) {
	t.Helper()
	ctx := context.Background()
	lib, err := st.CreateLibrary(ctx, "Movies", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mk := func(name, title string, year, width int) int64 {
		y := year
		id, err := st.UpsertItem(ctx, ScanFile{
			LibraryID: lib.ID, Path: filepath.Join(lib.Path, name), Kind: "movie",
			Title: title, SortTitle: title, Year: &y,
			Container: "mkv", SizeBytes: 1, MTime: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.ExecContext(ctx,
			`UPDATE media_item SET width = ? WHERE id = ?`, width, id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	uhd := mk("big.mkv", "Big", 1994, 3840)
	sd := mk("small.mkv", "Small", 2003, 720)

	if err := st.ReplaceCredits(ctx, sd, "tmdb", []Credit{
		{Name: "Ada Vance", Role: "actor", Order: 0},
	}); err != nil {
		t.Fatal(err)
	}
	return lib.ID, uhd, sd, sd
}

func TestResolutionFilterSelectsTheTier(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	lib, uhd, _, _ := seedLibrary(t, st)

	items, total, err := st.ListItems(ctx, ItemFilter{
		LibraryID: lib, Resolutions: []string{"uhd"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != uhd {
		t.Fatalf("uhd filter returned %d items (total %d), want just the 4K one", len(items), total)
	}
}

/*
 * A resolution key that no longer exists widens rather than errors. These
 * arrive from bookmarked query strings, and a renamed tier should show the
 * library again rather than break the page.
 */
func TestUnknownResolutionKeyDoesNotNarrow(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	lib, _, _, _ := seedLibrary(t, st)

	_, total, err := st.ListItems(ctx, ItemFilter{
		LibraryID: lib, Resolutions: []string{"betamax"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Errorf("unknown resolution key returned %d, want the whole library (2)", total)
	}
}

func TestYearFilterIsExact(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	lib, uhd, _, _ := seedLibrary(t, st)

	_, total, err := st.ListItems(ctx, ItemFilter{LibraryID: lib, Years: []int{1994}})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("year 1994 = %d items, want 1", total)
	}
	// And the decade filter still means the decade, not the year.
	_, dtotal, err := st.ListItems(ctx, ItemFilter{LibraryID: lib, Decades: []int{1990}})
	if err != nil {
		t.Fatal(err)
	}
	if dtotal != 1 {
		t.Errorf("decade 1990 = %d items, want 1", dtotal)
	}
	_ = uhd
}

func TestCastFilterAndSearch(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	lib, _, sd, _ := seedLibrary(t, st)

	people, err := st.SearchCast(ctx, lib, "ada", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 1 || people[0].Name != "Ada Vance" {
		t.Fatalf("cast search = %+v, want Ada Vance", people)
	}
	if people[0].Items != 1 {
		t.Errorf("Ada is in %d items, want 1", people[0].Items)
	}

	items, total, err := st.ListItems(ctx, ItemFilter{
		LibraryID: lib, PersonIDs: []int64{people[0].ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != sd {
		t.Fatalf("person filter returned %d items, want the one they are in", total)
	}
}

// Surname search: somebody typing "vance" means the person, and a prefix-only
// match on the full name would find nobody.
func TestCastSearchMatchesASurname(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	lib, _, _, _ := seedLibrary(t, st)

	people, err := st.SearchCast(ctx, lib, "vance", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 1 {
		t.Fatalf("surname search = %d people, want 1", len(people))
	}
}

func TestFacetsOfferOnlyWhatIsPresent(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	lib, _, _, _ := seedLibrary(t, st)

	f, err := st.LibraryFacets(ctx, lib, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Years) != 2 || f.Years[0] != 2003 {
		t.Errorf("years = %v, want [2003 1994] newest first", f.Years)
	}
	// Two tiers present, widest first, and nothing between them offered.
	if len(f.Resolutions) != 2 || f.Resolutions[0].Key != "uhd" || f.Resolutions[1].Key != "sd" {
		t.Errorf("resolutions = %+v, want uhd then sd", f.Resolutions)
	}
	// Nothing has been played, so neither status filter has anything to remove.
	if f.HasInProgress {
		t.Error("HasInProgress on a library nobody has watched")
	}
	// Both films were scanned, never matched, so the tidy-up filter is worth
	// offering.
	if !f.HasUnmatched {
		t.Error("HasUnmatched false with two unmatched films")
	}
}

/*
 * Acting and directing are different questions.
 *
 * The case that decides the split: one person who acts in one film and directs
 * another. An any-role filter answers "both", which is not what either question
 * asked, and there is no way to tell from the result which was meant.
 */
func TestActorAndDirectorAreSeparateQuestions(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	lib, uhd, sd, _ := seedLibrary(t, st)

	// Ada acts in the SD film (seeded) and directs the 4K one.
	if err := st.ReplaceCredits(ctx, uhd, "tmdb", []Credit{
		{Name: "Ada Vance", Role: "director", Order: 0},
	}); err != nil {
		t.Fatal(err)
	}
	people, err := st.SearchCast(ctx, lib, "ada", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 1 {
		t.Fatalf("one person credited twice = %d rows, want 1", len(people))
	}
	ada := people[0].ID

	acted, _, err := st.ListItems(ctx, ItemFilter{LibraryID: lib, ActorIDs: []int64{ada}})
	if err != nil {
		t.Fatal(err)
	}
	if len(acted) != 1 || acted[0].ID != sd {
		t.Errorf("acted in = %d items, want just the one she is in", len(acted))
	}

	directed, _, err := st.ListItems(ctx, ItemFilter{LibraryID: lib, DirectorIDs: []int64{ada}})
	if err != nil {
		t.Fatal(err)
	}
	if len(directed) != 1 || directed[0].ID != uhd {
		t.Errorf("directed = %d items, want just the one she directed", len(directed))
	}

	// And the any-role form still answers both, which is why it is kept.
	both, _, err := st.ListItems(ctx, ItemFilter{LibraryID: lib, PersonIDs: []int64{ada}})
	if err != nil {
		t.Fatal(err)
	}
	if len(both) != 2 {
		t.Errorf("any-role = %d items, want both", len(both))
	}
}

// A role-scoped search lists only that side of the camera.
func TestCastSearchScopesToARole(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	lib, uhd, _, _ := seedLibrary(t, st)
	if err := st.ReplaceCredits(ctx, uhd, "tmdb", []Credit{
		{Name: "Bo Reyes", Role: "director", Order: 0},
	}); err != nil {
		t.Fatal(err)
	}

	directors, err := st.SearchCast(ctx, lib, "", "director", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(directors) != 1 || directors[0].Name != "Bo Reyes" {
		t.Fatalf("directors = %+v, want only Bo Reyes", directors)
	}

	actors, err := st.SearchCast(ctx, lib, "", "actor", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range actors {
		if a.Name == "Bo Reyes" {
			t.Error("a director appeared in the actor list")
		}
	}
}

/*
 * Collection membership is not parenthood.
 *
 * A film belongs to a franchise without being inside it — that is why ADR 0017
 * gave collections their own table — so the filter reads item_collection and
 * not parent_id. A test because the two are one keystroke apart in SQL and the
 * wrong one silently returns nothing.
 */
func TestCollectionFilterReadsMembership(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	lib, uhd, sd, _ := seedLibrary(t, st)

	col, err := st.UpsertItem(ctx, ScanFile{
		LibraryID: lib, Path: "/collections/franchise", Kind: "collection",
		Title: "A Franchise", SortTitle: "a franchise", Container: "", SizeBytes: 0, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx,
		`INSERT INTO item_collection (item_id, collection_id, ord) VALUES (?, ?, 0)`,
		uhd, col); err != nil {
		t.Fatal(err)
	}

	items, _, err := st.ListItems(ctx, ItemFilter{
		LibraryID: lib, CollectionIDs: []int64{col},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != uhd {
		t.Fatalf("collection filter = %d items, want the one member", len(items))
	}

	/*
	 * The *filter* still works on a one-film collection -- membership is
	 * membership, and a caller naming a collection id gets its members.
	 *
	 * The *facet* does not offer it. A collection of one film is not a
	 * collection: TMDB publishes a franchise for almost everything, so a
	 * library owning one Anchorman gets an "Anchorman Collection" tile that
	 * opens onto the film you could already see. On a real library that was
	 * 102 of 277. Offering a chip for one the Collections page will not show
	 * would be a filter leading to a grid you cannot get back to.
	 */
	f, err := st.LibraryFacets(ctx, lib, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Collections) != 0 {
		t.Errorf("collection facet = %+v, want a one-film collection hidden", f.Collections)
	}

	// A second film, and it earns its place -- by count, so it reappears on
	// its own with no flag to maintain.
	if _, err := st.db.ExecContext(ctx,
		`INSERT INTO item_collection (item_id, collection_id, ord) VALUES (?, ?, 0)`,
		sd, col); err != nil {
		t.Fatal(err)
	}
	f, err = st.LibraryFacets(ctx, lib, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Collections) != 1 || f.Collections[0].Members != 2 {
		t.Errorf("collection facet = %+v, want one collection with two members", f.Collections)
	}
}

/*
 * An unrated film is not a film rated zero.
 *
 * The case that matters: sweeping unrated items into the bottom of every rating
 * filter would quietly hide the unmatched half of a library behind a control
 * that says nothing about matching.
 */
func TestRatingFloorExcludesUnratedRatherThanSinkingThem(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	lib, uhd, sd, _ := seedLibrary(t, st)

	if _, err := st.db.ExecContext(ctx,
		`UPDATE media_item SET rating = 8.4 WHERE id = ?`, uhd); err != nil {
		t.Fatal(err)
	}
	// sd is left unrated on purpose.

	rated, _, err := st.ListItems(ctx, ItemFilter{LibraryID: lib, MinRating: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(rated) != 1 || rated[0].ID != uhd {
		t.Fatalf("8+ = %d items, want the 8.4 one", len(rated))
	}

	// Above what anything scores: empty, not "everything unrated".
	none, _, err := st.ListItems(ctx, ItemFilter{LibraryID: lib, MinRating: 9})
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Errorf("9+ = %d items, want none", len(none))
	}

	// And a threshold of zero is no filter at all rather than "unrated only".
	all, _, err := st.ListItems(ctx, ItemFilter{LibraryID: lib, MinRating: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("no threshold = %d items, want the whole library", len(all))
	}

	f, err := st.LibraryFacets(ctx, lib, "u1")
	if err != nil {
		t.Fatal(err)
	}
	// The ceiling is what stops the client offering a 9+ that cannot match.
	if f.MaxRating != 8.4 {
		t.Errorf("max rating = %v, want 8.4", f.MaxRating)
	}
	_ = sd
}
