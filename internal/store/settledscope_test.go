package store

import (
	"context"
	"testing"
)

/*
 * Which rows the settled-identity pass will re-ask about.
 *
 * Every other refresh in this package excludes `locked`, and rightly: they
 * work by requeueing a row for a pass that searches and scores, which is what a
 * lock forbids. This scope is the inverse and the assertions have to say so
 * clearly, because "includes locked rows" reads like the bug those exclusions
 * exist to prevent.
 *
 * The distinction is that this scope names rows to be fetched **by their own
 * recorded provider id**. Nothing is searched, so nothing can be re-picked.
 * What the tests below pin is that the scope cannot name a row for which that
 * is untrue — one with no id to fetch by.
 */

func settledRow(t *testing.T, st *Store, id int64, state, provider, external string) {
	t.Helper()
	if _, err := st.db.ExecContext(context.Background(),
		`UPDATE media_item SET match_state = ?, provider = ?, external_id = ? WHERE id = ?`,
		state, provider, external, id); err != nil {
		t.Fatal(err)
	}
}

func TestTheSettledScopeIsTheRowsNothingElseCanReach(t *testing.T) {
	/*
	 * The whole point of the scope, stated as the comparison that motivates it:
	 * an ordinary refresh cannot see a locked row, and this one sees only those.
	 */
	ctx := context.Background()
	st := openTestStore(t)
	lib, a, b, _ := seedLibrary(t, st)
	settledRow(t, st, a, "locked", "tmdb", "85")
	settledRow(t, st, b, "matched", "tmdb", "603")

	all, err := st.RefreshCount(ctx, lib, RefreshAll)
	if err != nil {
		t.Fatal(err)
	}
	settled, err := st.SettledCount(ctx, lib)
	if err != nil {
		t.Fatal(err)
	}
	if settled != 1 {
		t.Errorf("settled = %d, want the one locked row", settled)
	}
	// And the two scopes must not overlap: a row reachable by an ordinary
	// refresh has no business being paid for twice.
	items, err := st.SettledItems(ctx, lib)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != a {
		t.Fatalf("settled items = %v, want only the locked row", items)
	}
	if all == 0 {
		t.Error("the ordinary refresh counted nothing, so this comparison proves nothing")
	}
}

func TestALockedRowWithNothingToAskAboutIsNotCounted(t *testing.T) {
	/*
	 * A locked row with no provider id is a decision somebody made by hand.
	 * There is no id to fetch by, so counting it would price a lookup that
	 * cannot happen — the same fault the enrichable-kinds exclusion exists to
	 * prevent, arriving by a different door.
	 */
	ctx := context.Background()
	st := openTestStore(t)
	lib, a, b, _ := seedLibrary(t, st)
	settledRow(t, st, a, "locked", "", "")
	settledRow(t, st, b, "locked", "tmdb", "603")

	n, err := st.SettledCount(ctx, lib)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("settled = %d, want only the row that carries an id", n)
	}
}

func TestTheCountAndTheListAgree(t *testing.T) {
	/*
	 * A preview that prices one set and an action that touches another is the
	 * worst shape this kind of feature can take — the reason refreshWhere is
	 * shared between its count and its clear. The same guarantee, asserted
	 * rather than assumed, because here they are two separate queries.
	 */
	ctx := context.Background()
	st := openTestStore(t)
	lib, a, b, _ := seedLibrary(t, st)
	settledRow(t, st, a, "locked", "tmdb", "85")
	settledRow(t, st, b, "locked", "tmdb", "603")

	n, err := st.SettledCount(ctx, lib)
	if err != nil {
		t.Fatal(err)
	}
	items, err := st.SettledItems(ctx, lib)
	if err != nil {
		t.Fatal(err)
	}
	if int(n) != len(items) {
		t.Errorf("priced %d and listed %d", n, len(items))
	}
}

func TestAMissingFileIsNotReAskedAbout(t *testing.T) {
	// Scanning marks missing rather than deleting, so these rows persist. A
	// library-wide sweep that included them would spend lookups on titles
	// nobody can play — the same exclusion the other library scopes make.
	ctx := context.Background()
	st := openTestStore(t)
	lib, a, b, _ := seedLibrary(t, st)
	settledRow(t, st, a, "locked", "tmdb", "85")
	settledRow(t, st, b, "locked", "tmdb", "603")
	if _, err := st.db.ExecContext(ctx,
		`UPDATE media_item SET missing = 1 WHERE id = ?`, a); err != nil {
		t.Fatal(err)
	}

	n, err := st.SettledCount(ctx, lib)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("settled = %d, want the one row still on disk", n)
	}
}

func TestOneLibraryIsNotAnother(t *testing.T) {
	// The scope is per library, like every other refresh. A pass launched from
	// one library's settings page that swept the whole server would be a
	// surprise measured in provider quota.
	ctx := context.Background()
	st := openTestStore(t)
	libA, a, _, _ := seedLibrary(t, st)
	libB, c, _, _ := seedLibrary(t, st)
	settledRow(t, st, a, "locked", "tmdb", "85")
	settledRow(t, st, c, "locked", "tmdb", "603")

	if n, err := st.SettledCount(ctx, libA); err != nil || n != 1 {
		t.Fatalf("library A = %d (%v), want 1", n, err)
	}
	if n, err := st.SettledCount(ctx, libB); err != nil || n != 1 {
		t.Fatalf("library B = %d (%v), want 1", n, err)
	}
}

func TestTheListCarriesWhatAFetchNeeds(t *testing.T) {
	/*
	 * The rows come back whole because the caller fetches by provider id, and
	 * for an episode or a season by the numbers that select within a show.
	 * Returning ids alone would mean a query per lookup on top of the network
	 * call each already costs.
	 */
	ctx := context.Background()
	st := openTestStore(t)
	lib, a, _, _ := seedLibrary(t, st)
	settledRow(t, st, a, "locked", "tmdb", "85")

	items, err := st.SettledItems(ctx, lib)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	got := items[0]
	if got.Provider == nil || *got.Provider != "tmdb" {
		t.Errorf("provider = %v, want tmdb", got.Provider)
	}
	if got.ExternalID == nil || *got.ExternalID != "85" {
		t.Errorf("external id = %v, want 85", got.ExternalID)
	}
	if got.Kind == "" || got.Path == "" {
		t.Error("the row came back without the kind or path a fetch and a local read need")
	}
}
