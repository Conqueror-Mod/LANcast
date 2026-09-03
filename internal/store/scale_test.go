package store

import (
	"context"
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
		items, total, err := st.ListItems(ctx, ItemFilter{
			LibraryID: lib, Kind: "movie", Sort: "title", Limit: 60,
		})
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
		if _, _, err := st.ListItems(ctx, ItemFilter{
			LibraryID: lib, Kind: "movie", Sort: "title", Limit: 60, Offset: 39_000,
		}); err != nil {
			b.Fatal(err)
		}
	}
}

// Two facets at once — the Plex semantics of widening within and narrowing
// across, which is the query a person builds by clicking.
func BenchmarkScaleBrowseFiltered(b *testing.B) {
	st, lib := scaleStore(b, scaleItems)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := st.ListItems(ctx, ItemFilter{
			LibraryID: lib, Kind: "movie", Sort: "title", Limit: 60,
			Genres:      []string{"Horror", "Sci-Fi"},
			Resolutions: []string{"hd1080"},
		}); err != nil {
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

// Typing in the search box, which happens per keystroke.
func BenchmarkScaleSearch(b *testing.B) {
	st, lib := scaleStore(b, scaleItems)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := st.ListItems(ctx, ItemFilter{
			LibraryID: lib, Query: "Title 391", Limit: 40,
		}); err != nil {
			b.Fatal(err)
		}
	}
}

// The home page's first shelf, which everyone sees before anything else.
func BenchmarkScaleContinueWatching(b *testing.B) {
	st, lib := scaleStore(b, scaleItems)
	ctx := context.Background()

	// A hundred part-watched films, which is far more than anyone has.
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

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := st.ContinueWatching(ctx, "local", 20, 0); err != nil {
			b.Fatal(err)
		}
	}
}

/*
 * A guard rather than a benchmark: the browse page must stay flat.
 *
 * Budgets rot unless something checks them, and a benchmark nobody runs is a
 * budget nobody checks. So this runs in the ordinary suite — which means it
 * has to be cheap, and it builds its own smaller library rather than the
 * benchmarks' 40,000. Building that one takes over two minutes, and a guard
 * that adds two minutes to every `go test ./...` is a guard somebody will
 * eventually delete.
 *
 * A lost index shows up just as clearly at 6,000 rows: the failure it exists
 * to catch is a page becoming a scan of the whole table, and the ratio between
 * a first page and a deep one says that at any size.
 *
 * It asserts a **shape, not a millisecond figure**. An absolute threshold
 * would fail on whichever machine CI happened to allocate, and a flaky
 * performance test is deleted rather than fixed — at which point the budget is
 * gone and nobody notices.
 */
func TestBrowseStaysFlatAcrossTheLibrary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a library")
	}
	const guardItems = 6_000
	st, lib, err := buildScaleStore(guardItems)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	timed := func(offset int) time.Duration {
		start := time.Now()
		for i := 0; i < 5; i++ {
			if _, _, err := st.ListItems(ctx, ItemFilter{
				LibraryID: lib, Kind: "movie", Sort: "title", Limit: 60, Offset: offset,
			}); err != nil {
				t.Fatal(err)
			}
		}
		return time.Since(start) / 5
	}

	first := timed(0)
	deep := timed(guardItems - 100)
	t.Logf("first page %v, deep page %v (%d items)", first, deep, guardItems)

	// Ten times is a deliberately loose ceiling. It catches a scan of the whole
	// table per page without failing on the ordinary difference between an
	// offset of 0 and one near the end.
	if deep > first*10 && deep > 50*time.Millisecond {
		t.Errorf("a deep page costs %v against %v for the first — paging is not flat",
			deep, first)
	}
}
