package store

import (
	"context"
	"testing"
)

/*
 * Sorting by running time.
 *
 * The ordering is the easy half. The half worth a test is where a row with no
 * running time goes, because "shortest" is exactly the sort that would put it
 * first — and a film of no minutes is not the shortest film, it is a film
 * nobody measured.
 */
func durationFixture(t *testing.T) (*Store, int64) {
	t.Helper()
	st := openTestStore(t)
	ctx := context.Background()
	lib, err := st.CreateLibrary(ctx, "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	/*
	 * Durations are set with an UPDATE rather than through UpsertItem, because
	 * a scan does not know one: duration_ms is written by the probe worker
	 * afterwards. Which is also why "no running time" is an ordinary state
	 * rather than a corrupt one — every unprobed film is in it.
	 */
	add := func(title string, ms *int64) {
		id, err := st.UpsertItem(ctx, ScanFile{
			LibraryID: lib.ID, Path: title + ".mkv", Kind: "movie",
			Title: title, SortTitle: title, Container: "mkv", SizeBytes: 1, MTime: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if ms != nil {
			if _, err := st.db.ExecContext(ctx,
				`UPDATE media_item SET duration_ms = ? WHERE id = ?`, *ms, id); err != nil {
				t.Fatal(err)
			}
		}
	}

	// Deliberately not in title order, so a sort that silently fell through to
	// the default would be visible rather than coincidentally right.
	mins := func(m int64) *int64 { v := m * 60000; return &v }
	add("Bravo", mins(90))
	add("Alpha", mins(200))
	add("Delta", mins(30))
	// One never probed, and one a probe measured as zero.
	add("Unmeasured", nil)
	zero := int64(0)
	add("Zero", &zero)

	return st, lib.ID
}

// titles() is trackorder_test.go's, reused rather than written twice.

func TestLongestPutsTheLongestFirst(t *testing.T) {
	st, lib := durationFixture(t)
	items, _, err := st.ListItems(context.Background(), ItemFilter{
		LibraryID: lib, Sort: "longest", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := titles(items)
	/*
	 * The measured three in order, then the two unknowns in either order.
	 *
	 * Asserting a fixed order for the unknowns would be asserting an
	 * accident — NULL and 0 have no meaningful ordering between themselves,
	 * and pinning today's happens to be a test that fails on a change that
	 * matters to nobody.
	 */
	want := []string{"Alpha", "Bravo", "Delta"}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("longest = %v, want %v then the unmeasured ones", got, want)
		}
	}
	tail := map[string]bool{got[len(got)-1]: true, got[len(got)-2]: true}
	if !tail["Unmeasured"] || !tail["Zero"] {
		t.Errorf("longest = %v — a film nobody measured is not the longest film", got)
	}
}

/*
 * And "shortest" does not put the unmeasured ones first, which is the whole
 * point of the test.
 */
func TestShortestSinksTheUnmeasuredRatherThanLeadingWithThem(t *testing.T) {
	st, lib := durationFixture(t)
	items, _, err := st.ListItems(context.Background(), ItemFilter{
		LibraryID: lib, Sort: "shortest", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := titles(items)
	if len(got) < 3 || got[0] != "Delta" {
		t.Fatalf("shortest = %v, want the 30-minute film first", got)
	}
	// Both kinds of unknown are at the end, in either order between themselves.
	tail := map[string]bool{got[len(got)-1]: true, got[len(got)-2]: true}
	if !tail["Unmeasured"] || !tail["Zero"] {
		t.Errorf("shortest = %v — a film nobody measured is not the shortest film", got)
	}
}
