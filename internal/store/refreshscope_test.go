package store

import (
	"context"
	"testing"
)

/*
 * What a scoped refresh will and will not re-ask about.
 *
 * "Refresh metadata" cleared the stamp for a whole library, which on a real film
 * library is about 1,480 provider lookups at five a second — five minutes of
 * work to fix the handful of rows somebody actually meant. The scopes exist so
 * that "re-ask about the ones that never got an answer" stops costing the same
 * as "re-ask about everything".
 *
 * The assertions that matter are the exclusions. A scope that quietly includes
 * rows no provider can answer for prices work that will never happen, and one
 * that includes locked rows is a button that undoes a person's decision.
 */

func matched(t *testing.T, st *Store, id int64, state string) {
	t.Helper()
	if err := st.UpdateItemMetadata(context.Background(), id, ItemMetadata{
		MatchState: &state,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshAllCountsEveryEnrichableRow(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	lib, a, b, _ := seedLibrary(t, st)
	matched(t, st, a, "matched")
	matched(t, st, b, "unmatched")

	n, err := st.RefreshCount(ctx, lib, RefreshAll)
	if err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Errorf("all = %d, want at least the two seeded films", n)
	}
}

func TestRefreshUnmatchedLeavesMatchedRowsAlone(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	lib, a, b, _ := seedLibrary(t, st)
	matched(t, st, a, "matched")
	matched(t, st, b, "unmatched")

	all, err := st.RefreshCount(ctx, lib, RefreshAll)
	if err != nil {
		t.Fatal(err)
	}
	only, err := st.RefreshCount(ctx, lib, RefreshUnmatched)
	if err != nil {
		t.Fatal(err)
	}
	if only >= all {
		t.Errorf("unmatched = %d and all = %d; the scope narrowed nothing", only, all)
	}
	if only != 1 {
		t.Errorf("unmatched = %d, want 1 — only the row that never got an answer", only)
	}
}

/*
 * A locked row is a decision somebody made, and no scope may undo it.
 *
 * This is the locked-fields rule reaching a new caller. A refresh that requeued
 * locked items would re-litigate identity, which is the thing a rescan is
 * forbidden from doing — and the permanent integration test exists because
 * failing it means LANcast has become the thing it was built to replace.
 */
func TestNoScopeTouchesALockedRow(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	lib, a, b, _ := seedLibrary(t, st)
	matched(t, st, a, "locked")
	matched(t, st, b, "unmatched")

	for _, scope := range []RefreshScope{RefreshAll, RefreshUnmatched} {
		n, err := st.RefreshScoped(ctx, lib, scope)
		if err != nil {
			t.Fatal(err)
		}
		if scope == RefreshAll && n != 1 {
			t.Errorf("all requeued %d, want 1 — the locked row must not be among them", n)
		}
	}

	var state string
	if err := st.db.QueryRowContext(ctx,
		`SELECT match_state FROM media_item WHERE id = ?`, a).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "locked" {
		t.Errorf("the locked row is now %q", state)
	}
}

// The count and the clear must never disagree about what a scope means. A
// preview that prices one set and an action that touches another is the worst
// shape this kind of feature can take.
func TestThePreviewMatchesTheWork(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	lib, a, b, _ := seedLibrary(t, st)
	matched(t, st, a, "matched")
	matched(t, st, b, "unmatched")

	for _, scope := range []RefreshScope{RefreshAll, RefreshUnmatched} {
		priced, err := st.RefreshCount(ctx, lib, scope)
		if err != nil {
			t.Fatal(err)
		}
		done, err := st.RefreshScoped(ctx, lib, scope)
		if err != nil {
			t.Fatal(err)
		}
		if priced != done {
			t.Errorf("%s: priced %d and requeued %d", scope, priced, done)
		}
	}
}

func TestAnUnknownScopeIsRefusedRatherThanWidened(t *testing.T) {
	// Silently doing every lookup because somebody mistyped is the expensive
	// failure this feature exists to prevent.
	if _, err := (&Store{}).RefreshCount(context.Background(), 1, "unmatced"); err == nil {
		t.Error("an unknown scope was priced")
	}
	if _, err := (&Store{}).RefreshScoped(context.Background(), 1, "everything"); err == nil {
		t.Error("an unknown scope was performed")
	}
}
