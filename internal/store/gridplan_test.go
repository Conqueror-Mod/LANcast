package store

import (
	"context"
	"strings"
	"testing"
)

/*
 * How the grid is read, asserted on the plan rather than the clock (ADR 0057).
 *
 * The browse grid's budget rests on two facts about the query plan: a plain
 * grid page is a walk along one of the idx_item_grid_* indexes with no sort,
 * and anything those indexes cannot answer alone keeps away from them, because
 * through them the rows come in random order on disk. Either can be lost
 * without any result changing — a dropped index, a renamed column, one more
 * index that the planner prefers — and a timing guard cannot see it on a fast
 * machine. TestBrowseStaysFlatAcrossTheLibrary could not: on the code these
 * indexes replaced, a page 39,000 deep cost three times the first one, inside
 * its ten-times ceiling, because the first page was a full sort as well.
 *
 * SQLite plans without statistics here (nothing runs ANALYZE), so the plan
 * does not depend on how many rows the fixture has, and three items are enough.
 */

func gridPlan(t *testing.T, st *Store, f ItemFilter) string {
	t.Helper()
	var query string
	var args []any
	listQueryHook = func(q string, a []any) { query, args = q, a }
	defer func() { listQueryHook = nil }()
	if _, _, err := st.ListItems(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if query == "" {
		t.Fatal("ListItems ran no page query")
	}
	rows, err := st.db.Query(`EXPLAIN QUERY PLAN `+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	return strings.Join(plan, " | ")
}

func gridPlanStore(t *testing.T) (*Store, int64) {
	st := openTestStore(t)
	ctx := context.Background()
	lib, err := st.CreateLibrary(ctx, "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Alien", "Brazil", "Heat"} {
		if _, err := st.UpsertItem(ctx, ScanFile{
			LibraryID: lib.ID, Path: title + ".mkv", Kind: "movie",
			Title: title, SortTitle: title, Container: "mkv", SizeBytes: 1, MTime: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return st, lib.ID
}

// The grid as the client asks for it: a library's present top level, without
// the containers that group items.
func plainGrid(lib int64, sort string) ItemFilter {
	return ItemFilter{LibraryID: lib, TopLevel: true, ExcludeMissing: true,
		ExcludeKinds: GroupingKinds, Sort: sort, Limit: 60, Offset: 600}
}

func TestAPlainGridPageIsAWalkAlongAnIndex(t *testing.T) {
	st, lib := gridPlanStore(t)
	for _, c := range []struct {
		name  string
		f     ItemFilter
		index string
	}{
		{"by title", plainGrid(lib, "title"), "idx_item_grid_title"},
		{"by the default", plainGrid(lib, ""), "idx_item_grid_title"},
		{"by year", plainGrid(lib, "year"), "idx_item_grid_year"},
		{"by date added", plainGrid(lib, "added"), "idx_item_grid_added"},
		// The home page's Unwatched shelf: "not begun" is checked against a
		// set built from the person's plays, so the walk needs only ids.
		{"not begun, shuffled", func() ItemFilter {
			f := plainGrid(lib, "random")
			f.Unstarted, f.UserID, f.Seed = true, "u", 3
			return f
		}(), "idx_item_grid_"},
		// The home page's Recently Added shelf, across every library.
		{"recently added everywhere", ItemFilter{TopLevel: true, ExcludeMissing: true,
			ExcludeKinds: []string{"artist", "album", "track", "gallery", "photo"},
			Sort:         "added", Limit: 20}, "idx_item_grid_added"},
	} {
		t.Run(c.name, func(t *testing.T) {
			plan := gridPlan(t, st, c.f)
			if !strings.Contains(plan, "COVERING INDEX "+c.index) {
				t.Errorf("not read from %s alone: %s", c.index, plan)
			}
			// A shuffle sorts by its nature; what it must not do is visit rows.
			if c.f.Sort != "random" && strings.Contains(plan, "TEMP B-TREE FOR ORDER BY") {
				t.Errorf("sorts the whole grid to return one page: %s", plan)
			}
		})
	}
}

func TestAGridTheIndexesCannotAnswerReadsRowsInOrder(t *testing.T) {
	st, lib := gridPlanStore(t)
	filtered := plainGrid(lib, "title")
	filtered.Genres = []string{"Horror"}
	ceiling := plainGrid(lib, "title")
	ceiling.MaxContentRating = "PG-13"
	for _, c := range []struct {
		name  string
		f     ItemFilter
		index string
	}{
		{"filtered by genre", filtered, "idx_item_library"},
		{"under a rating ceiling", ceiling, "idx_item_library"},
		{"sorted by rating", plainGrid(lib, "rating"), "idx_item_library"},
		{"sorted by running time", plainGrid(lib, "longest"), "idx_item_library"},
		{"searched within a library", ItemFilter{LibraryID: lib, TopLevel: true,
			ExcludeMissing: true, Query: "ali", Limit: 60}, "idx_item_library"},
		{"sorted by rating everywhere", ItemFilter{TopLevel: true, ExcludeMissing: true,
			Sort: "rating", Limit: 20}, "idx_item_parent"},
	} {
		t.Run(c.name, func(t *testing.T) {
			plan := gridPlan(t, st, c.f)
			if strings.Contains(plan, "idx_item_grid_") {
				t.Errorf("reads rows through a grid index, in random order: %s", plan)
			}
			if !strings.Contains(plan, c.index) {
				t.Errorf("not walked by %s: %s", c.index, plan)
			}
		})
	}

	t.Run("searched everywhere", func(t *testing.T) {
		plan := gridPlan(t, st, ItemFilter{TopLevel: true, ExcludeMissing: true, Query: "ali", Limit: 60})
		if !strings.Contains(plan, "SCAN media_item") || strings.Contains(plan, "idx_item_grid_") {
			t.Errorf("a substring search should read the table in order: %s", plan)
		}
	})
}

// A collection is real when two present members say so, and that is counted
// from its memberships — never by walking the table for present rows, which
// one extra index on media_item once made the planner choose.
func TestCollectionMembersAreCountedFromTheCollection(t *testing.T) {
	st, lib := gridPlanStore(t)
	plan := gridPlan(t, st, plainGrid(lib, "title"))
	if !strings.Contains(plan, "SEARCH m2 USING INTEGER PRIMARY KEY") {
		t.Errorf("collection members not looked up by id: %s", plan)
	}
}
