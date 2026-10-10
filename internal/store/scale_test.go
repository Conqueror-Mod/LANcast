package store

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

/*
 * What a 40,000-item library costs to read.
 *
 * The roadmap has carried "performance targets — budgets for a 40k-item
 * library" as unplanned for a long time, and the reason it never moved is that
 * a budget is meaningless without a way to check it. This is the way to check
 * it; ADR 0057 is the budget.
 *
 * Deliberately a *read* benchmark against a synthetic database rather than a
 * scan of 40,000 real files. The scan path has been measured directly this
 * week and its cost is dominated by touching media — which is a fact about
 * disks, not about scale. What nobody has ever measured is whether the queries
 * behind a browse page still answer promptly when the library is twice the size
 * of the largest real one measured (18,777 items), and those are the ones a
 * person waits on while looking at a spinner.
 *
 * Run with:
 *
 *	go test ./internal/store/ -run XXX -bench Scale -benchtime 1x
 *
 * `-run XXX` matches no test, so only the benchmarks run.
 */

const scaleItems = 40_000

/*
 * scaleStore builds a library of n items with the shape a real one has:
 * genres, ratings, years and resolutions, because a query over uniform rows
 * measures the wrong thing — every facet would match everything and the
 * planner would never have to choose.
 *
 * Built once and shared. Every benchmark here is read-only, and building a
 * 40,000-item library per benchmark took longer than every other test in the
 * repository put together. It is never closed: the test binary exiting is the
 * cleanup, and a shared fixture with a Cleanup hook would be closed by
 * whichever benchmark finished first.
 */
var (
	scaleOnce  sync.Once
	scaleSt    *Store
	scaleLibID int64
	scaleErr   error
)

func scaleStore(tb testing.TB, n int) (*Store, int64) {
	tb.Helper()
	scaleOnce.Do(func() { scaleSt, scaleLibID, scaleErr = buildScaleStore(n) })
	if scaleErr != nil {
		tb.Fatal(scaleErr)
	}
	return scaleSt, scaleLibID
}

func buildScaleStore(n int) (*Store, int64, error) {
	dir, err := os.MkdirTemp("", "lancast-scale")
	if err != nil {
		return nil, 0, err
	}
	st, err := Open(filepath.Join(dir, "scale.db"))
	if err != nil {
		return nil, 0, err
	}
	ctx := context.Background()

	lib, err := st.CreateLibrary(ctx, "Scale", "movie", dir)
	if err != nil {
		return nil, 0, err
	}

	genres := []string{"Action", "Comedy", "Drama", "Horror", "Sci-Fi",
		"Thriller", "Documentary", "Animation"}
	ratings := []string{"G", "PG", "PG-13", "R", "NC-17"}
	// Four resolution buckets, weighted the way a real library is: mostly
	// 1080p, some 4K, a tail of older material.
	sizes := [][2]int{{3840, 2160}, {1920, 1080}, {1920, 1080}, {1920, 1080},
		{1280, 720}, {720, 480}}

	r := rand.New(rand.NewSource(1))
	for i := 0; i < n; i++ {
		title := fmt.Sprintf("Title %05d", i)
		year := 1960 + r.Intn(65)
		id, err := st.UpsertItem(ctx, ScanFile{
			LibraryID: lib.ID,
			Path:      filepath.Join(lib.Path, fmt.Sprintf("%05d.mkv", i)),
			Kind:      "movie", Title: title, SortTitle: title,
			Year: &year, Container: "mkv", SizeBytes: int64(i) + 1, MTime: 1,
		})
		if err != nil {
			return nil, 0, err
		}

		sz := sizes[r.Intn(len(sizes))]
		rating := ratings[r.Intn(len(ratings))]
		score := float64(r.Intn(100)) / 10
		if _, err := st.db.ExecContext(ctx, `
			UPDATE media_item SET width = ?, height = ?, content_rating = ?,
				rating = ?, duration_ms = ?, probed_at = 1,
				metadata_updated_at = 1, added_at = ?
			WHERE id = ?`,
			sz[0], sz[1], rating, score, 5_400_000, 1_700_000_000+int64(i), id); err != nil {
			return nil, 0, err
		}

		// One or two genres each, so a genre filter selects a real subset.
		if err := st.ReplaceGenres(ctx, id, []string{
			genres[r.Intn(len(genres))], genres[r.Intn(len(genres))],
		}); err != nil {
			return nil, 0, err
		}
	}
	return st, lib.ID, nil
}

/*
 * gridFilter is the browse grid as the client asks for it: a library's present
 * top level, without the containers that group items.
 *
 * The first version of these benchmarks asked for `kind=movie` instead, which
 * the grid never sends. It is a cheaper query — no top-level rule, no
 * collection rule — so the table ADR 0057 was written from was measuring
 * something faster than anything a person waits on.
 */
func gridFilter(lib int64, sort string, offset int) ItemFilter {
	return ItemFilter{
		LibraryID: lib, TopLevel: true, ExcludeMissing: true, ExcludeKinds: GroupingKinds,
		Sort: sort, Limit: 60, Offset: offset,
	}
}

/*
 * The first page of a browse grid, which is what a person waits on.
 *
 * Limit 60 because that is roughly a screenful at the sizes design.md uses;
 * the total count comes back with it, and that count is the half most likely
 * to scale badly since it cannot stop early.
 */
func BenchmarkScaleBrowseFirstPage(b *testing.B) {
	st, lib := scaleStore(b, scaleItems)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		items, total, err := st.ListItems(ctx, gridFilter(lib, "title", 0))
		if err != nil {
			b.Fatal(err)
		}
		if len(items) != 60 || total != scaleItems {
			b.Fatalf("got %d items of %d", len(items), total)
		}
	}
}

// Deep into the grid. An offset the planner cannot skip is where a naive
// paging query stops being flat.
func BenchmarkScaleBrowseDeepPage(b *testing.B) {
	st, lib := scaleStore(b, scaleItems)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := st.ListItems(ctx, gridFilter(lib, "title", 39_000)); err != nil {
			b.Fatal(err)
		}
	}
}

// Deep by year and by rating: one sort the grid indexes hold, and one they do
// not. Rating is the one ADR 0057 records as over budget at this depth.
func BenchmarkScaleBrowseDeepPageByYear(b *testing.B) {
	st, lib := scaleStore(b, scaleItems)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := st.ListItems(ctx, gridFilter(lib, "year", 39_000)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkScaleBrowseDeepPageByRating(b *testing.B) {
	st, lib := scaleStore(b, scaleItems)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := st.ListItems(ctx, gridFilter(lib, "rating", 39_000)); err != nil {
			b.Fatal(err)
		}
	}
}

// Two facets at once — the Plex semantics of widening within and narrowing
// across, which is the query a person builds by clicking.
func BenchmarkScaleBrowseFiltered(b *testing.B) {
	st, lib := scaleStore(b, scaleItems)
	ctx := context.Background()
	f := gridFilter(lib, "title", 0)
	f.Genres = []string{"Horror", "Sci-Fi"}
	f.Resolutions = []string{"hd1080"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := st.ListItems(ctx, f); err != nil {
			b.Fatal(err)
		}
	}
}

// The filter bar itself. Every facet count over the whole library, which is
// the one query that cannot be limited.
func BenchmarkScaleFacets(b *testing.B) {
	st, lib := scaleStore(b, scaleItems)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := st.LibraryFacets(ctx, lib, "local"); err != nil {
			b.Fatal(err)
		}
	}
}

// Typing in the search box, which happens per keystroke. The box searches
// every library at once, which is what this asks.
func BenchmarkScaleSearch(b *testing.B) {
	st, _ := scaleStore(b, scaleItems)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := st.ListItems(ctx, ItemFilter{
			TopLevel: true, ExcludeMissing: true, Query: "Title 391", Limit: 60,
		}); err != nil {
			b.Fatal(err)
		}
	}
}

// The home page's Recently Added shelf, which asks across every library.
func BenchmarkScaleRecentlyAdded(b *testing.B) {
	st, _ := scaleStore(b, scaleItems)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := st.ListItems(ctx, ItemFilter{
			TopLevel: true, ExcludeMissing: true, Sort: "added", Limit: 20,
			ExcludeKinds: []string{"artist", "album", "track", "gallery", "photo"},
		}); err != nil {
			b.Fatal(err)
		}
	}
}

// The home page's first shelf, which everyone sees before anything else.
func BenchmarkScaleContinueWatching(b *testing.B) {
	st, lib := scaleStore(b, scaleItems)
	ctx := context.Background()
	scaleProgress(b, st, lib)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := st.ContinueWatching(ctx, "local", 20, 0); err != nil {
			b.Fatal(err)
		}
	}
}

// The home page's Unwatched shelf: a library's top level that this person has
// not begun, shuffled. Not begun is a question about every row's children.
func BenchmarkScaleUnwatchedShelf(b *testing.B) {
	st, lib := scaleStore(b, scaleItems)
	ctx := context.Background()
	scaleProgress(b, st, lib)
	f := gridFilter(lib, "random", 0)
	f.Seed, f.Unstarted, f.UserID, f.Limit = 3, true, "local", 21
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := st.ListItems(ctx, f); err != nil {
			b.Fatal(err)
		}
	}
}

// scaleProgress gives the shared fixture a hundred part-watched films, which
// is far more than anyone has, once.
var scaleProgressOnce sync.Once

func scaleProgress(b *testing.B, st *Store, lib int64) {
	ctx := context.Background()
	scaleProgressOnce.Do(func() {
		for i := 0; i < 100; i++ {
			var id int64
			if err := st.db.QueryRowContext(ctx,
				`SELECT id FROM media_item WHERE library_id = ? LIMIT 1 OFFSET ?`,
				lib, i*37).Scan(&id); err != nil {
				b.Fatal(err)
			}
			if err := st.SaveProgress(ctx, id, "local", 600_000, false); err != nil {
				b.Fatal(err)
			}
		}
	})
}

/*
 * Guards rather than benchmarks.
 *
 * Budgets rot unless something checks them, and a benchmark nobody runs is a
 * budget nobody checks. So these run in the ordinary suite — which means they
 * have to be cheap, and they share one smaller library rather than the
 * benchmarks' 40,000. Building that one takes over two minutes, and a guard
 * that adds two minutes to every `go test ./...` is a guard somebody will
 * eventually delete.
 *
 * They assert a **shape, not a millisecond figure**. An absolute threshold
 * would fail on whichever machine CI happened to allocate, and a flaky
 * performance test is deleted rather than fixed — at which point the budget is
 * gone and nobody notices. How the grid is read is asserted on the query plan
 * instead, in gridplan_test.go, which needs no library at all.
 */
const guardItems = 6_000

var (
	guardOnce  sync.Once
	guardSt    *Store
	guardLibID int64
	guardErr   error
)

func guardStore(t *testing.T) (*Store, int64) {
	t.Helper()
	if testing.Short() {
		t.Skip("builds a library")
	}
	guardOnce.Do(func() { guardSt, guardLibID, guardErr = buildScaleStore(guardItems) })
	if guardErr != nil {
		t.Fatal(guardErr)
	}
	return guardSt, guardLibID
}

// fastest is the quickest of several runs, which is the one least disturbed by
// whatever else the machine was doing.
func fastest(t *testing.T, f func() error) time.Duration {
	t.Helper()
	best := time.Duration(1<<63 - 1)
	for i := 0; i < 5; i++ {
		start := time.Now()
		if err := f(); err != nil {
			t.Fatal(err)
		}
		best = min(best, time.Since(start))
	}
	return best
}

/*
 * The browse page must stay flat.
 *
 * A lost index shows up just as clearly at 6,000 rows: the failure it exists
 * to catch is a page becoming a scan of the whole table, and the ratio between
 * a first page and a deep one says that at any size.
 */
func TestBrowseStaysFlatAcrossTheLibrary(t *testing.T) {
	st, lib := guardStore(t)
	ctx := context.Background()

	for _, sort := range []string{"title", "year", "added"} {
		page := func(offset int) time.Duration {
			return fastest(t, func() error {
				_, _, err := st.ListItems(ctx, gridFilter(lib, sort, offset))
				return err
			})
		}
		first := page(0)
		deep := page(guardItems - 100)
		t.Logf("by %s: first page %v, deep page %v (%d items)", sort, first, deep, guardItems)

		// Ten times is a deliberately loose ceiling. It catches a scan of the
		// whole table per page without failing on the ordinary difference
		// between an offset of 0 and one near the end.
		if deep > first*10 && deep > 50*time.Millisecond {
			t.Errorf("by %s, a deep page costs %v against %v for the first — paging is not flat",
				sort, deep, first)
		}
	}
}

/*
 * The filter bar reads the library about once.
 *
 * Every facet is over the whole library, so it cannot be cheaper than one walk
 * of it — and it must not be many. It was eight: one per column, each walking
 * every row to read one value, and it cost twice its budget at 40,000 items
 * (ADR 0057). Measured against a single walk of the same library in the same
 * moment, so a slow machine slows both sides. At this size the old shape
 * measured about thirty walks and this one about eight — each query has a
 * fixed cost that a small library does not hide — so sixteen is the line.
 */
func TestTheFilterBarWalksTheLibraryAboutOnce(t *testing.T) {
	st, lib := guardStore(t)
	ctx := context.Background()

	// duration_ms is in no index, so this has to visit every row: one walk.
	walk := fastest(t, func() error {
		var d sql.NullInt64
		return st.db.QueryRowContext(ctx,
			`SELECT MAX(duration_ms) FROM media_item WHERE library_id = ? AND missing = 0`,
			lib).Scan(&d)
	})
	facets := fastest(t, func() error {
		_, err := st.LibraryFacets(ctx, lib, "local")
		return err
	})
	t.Logf("filter bar %v, one walk %v (%d items)", facets, walk, guardItems)

	// The floor is for the clock, not the budget: under a few milliseconds a
	// ratio is mostly timer resolution.
	if facets > walk*16 && facets > 5*time.Millisecond {
		t.Errorf("the filter bar costs %v, %.1f walks of the library — a facet has gone "+
			"back to reading the library on its own", facets, float64(facets)/float64(walk))
	}
}

/*
 * The home page's Unwatched shelf is linear in the library.
 *
 * "Not begun" is a question about each row's children, and it was asked that
 * way: a search of the whole table for every candidate's children, through an
 * OR no index can answer — quadratic in the library, for a shelf with a 50ms
 * budget. At 40,000 items a single call ran for minutes. It is now asked from
 * what this person has played, which is one set built once (ADR 0057), and at
 * any size that is within a few walks of the library; quadratic is thousands.
 */
func TestTheUnwatchedShelfIsLinearInTheLibrary(t *testing.T) {
	st, lib := guardStore(t)
	ctx := context.Background()
	guardProgressOnce.Do(func() {
		for i := 0; i < 50; i++ {
			var id int64
			if err := st.db.QueryRowContext(ctx,
				`SELECT id FROM media_item WHERE library_id = ? LIMIT 1 OFFSET ?`,
				lib, i*97).Scan(&id); err != nil {
				t.Fatal(err)
			}
			if err := st.SaveProgress(ctx, id, "local", 600_000, i%2 == 0); err != nil {
				t.Fatal(err)
			}
		}
	})

	walk := fastest(t, func() error {
		var d sql.NullInt64
		return st.db.QueryRowContext(ctx,
			`SELECT MAX(duration_ms) FROM media_item WHERE library_id = ? AND missing = 0`,
			lib).Scan(&d)
	})
	f := gridFilter(lib, "random", 0)
	f.Seed, f.Unstarted, f.UserID, f.Limit = 3, true, "local", 21
	var items []Item
	shelf := fastest(t, func() error {
		var err error
		items, _, err = st.ListItems(ctx, f)
		return err
	})
	t.Logf("unwatched shelf %v, one walk %v (%d items)", shelf, walk, guardItems)

	if len(items) != 21 {
		t.Fatalf("the shelf holds %d items, want 21", len(items))
	}
	if shelf > walk*20 && shelf > 5*time.Millisecond {
		t.Errorf("the unwatched shelf costs %v, %.0f walks of the library — "+
			"\"not begun\" is being asked per row again", shelf, float64(shelf)/float64(walk))
	}
}

var guardProgressOnce sync.Once
