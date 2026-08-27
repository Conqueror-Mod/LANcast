package store

import (
	"context"
	"testing"
)

// seedProgress gives the user one finished item and one part-watched one.
func seedProgress(t *testing.T, st *Store, finished, partial int64) {
	t.Helper()
	ctx := context.Background()
	if err := st.SaveProgress(ctx, finished, "u1", 0, true); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProgress(ctx, partial, "u1", 90_000, false); err != nil {
		t.Fatal(err)
	}
	// Somebody else's record, which nothing here may ever touch.
	if err := st.SaveProgress(ctx, finished, "u2", 0, true); err != nil {
		t.Fatal(err)
	}
}

/*
 * The scopes are three different questions, and the split is the feature.
 *
 * `playback_state` is one table carrying two meanings. Forgetting a finished
 * show must not cost the position in the one being watched, which is what a
 * single "clear history" button would have done.
 */
func TestResetHistoryScopes(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		scope     HistoryScope
		want      int64
		leftForU1 int
	}{
		{HistoryFinished, 1, 1},
		{HistoryUnfinished, 1, 1},
		{HistoryAll, 2, 0},
	} {
		st := openTestStore(t)
		_, uhd, sdr, _ := seedLibrary(t, st)
		seedProgress(t, st, uhd, sdr)

		n, err := st.ResetHistory(ctx, "u1", tc.scope, 0)
		if err != nil {
			t.Fatalf("%s: %v", tc.scope, err)
		}
		if n != tc.want {
			t.Errorf("%s removed %d, want %d", tc.scope, n, tc.want)
		}
		left, err := st.HistoryCount(ctx, "u1", HistoryAll, 0)
		if err != nil {
			t.Fatal(err)
		}
		if int(left) != tc.leftForU1 {
			t.Errorf("%s left %d rows, want %d", tc.scope, left, tc.leftForU1)
		}
		// The other account is untouched in every case. This is the property
		// ADR 0006 exists for, and the one worth asserting in every branch
		// rather than once.
		other, err := st.HistoryCount(ctx, "u2", HistoryAll, 0)
		if err != nil {
			t.Fatal(err)
		}
		if other != 1 {
			t.Errorf("%s touched another account: %d rows left", tc.scope, other)
		}
	}
}

// The preview has to price exactly what the delete removes, or an irreversible
// action was confirmed against a number that was never true.
func TestHistoryCountMatchesWhatResetRemoves(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	_, uhd, sdr, _ := seedLibrary(t, st)
	seedProgress(t, st, uhd, sdr)

	for _, scope := range []HistoryScope{HistoryAll, HistoryFinished, HistoryUnfinished} {
		fresh := openTestStore(t)
		_, a, b, _ := seedLibrary(t, fresh)
		seedProgress(t, fresh, a, b)

		want, err := fresh.HistoryCount(ctx, "u1", scope, 0)
		if err != nil {
			t.Fatal(err)
		}
		got, err := fresh.ResetHistory(ctx, "u1", scope, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s: previewed %d, removed %d", scope, want, got)
		}
	}
}

func TestResetHistoryRefusesAnUnknownScope(t *testing.T) {
	st := openTestStore(t)
	if _, err := st.ResetHistory(context.Background(), "u1", "everything-ever", 0); err == nil {
		t.Error("an unknown scope was accepted")
	}
	if _, err := st.HistoryCount(context.Background(), "u1", "everything-ever", 0); err == nil {
		t.Error("an unknown scope was priced")
	}
}
