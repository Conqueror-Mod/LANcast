package store

import (
	"context"
	"testing"
)

/*
 * ADR 0035, asserted at the layer that enforces it.
 *
 * These are the tests that matter most in this package, because the failure is
 * silent and cannot be taken back: a listing that forgets the filter publishes
 * somebody's viewing, and nothing about the response looks wrong.
 */

func seedWatcher(t *testing.T, st *Store, name string, share bool) (*User, int64) {
	t.Helper()
	ctx := context.Background()

	u, err := st.CreateUser(ctx, "", name, "hash", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := st.CreateLibrary(ctx, "Films "+name, "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.UpsertItem(ctx, ScanFile{
		LibraryID: lib.ID, Path: t.TempDir() + "/" + name + ".mkv", Kind: "movie",
		Title: "Arrival", SortTitle: "arrival", Container: "mkv", SizeBytes: 1, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProgress(ctx, id, u.ID, 5000, true); err != nil {
		t.Fatal(err)
	}
	if share {
		if err := st.SetShareActivity(ctx, u.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	return u, id
}

// The default is the one that fails safe: a server upgraded into this ADR
// shares nothing until somebody deliberately turns it on.
func TestSharingIsOffByDefault(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	u, _ := seedWatcher(t, st, "quiet", false)
	sharing, err := st.SharesActivity(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sharing {
		t.Error("a new account shares by default; an upgrade would publish every existing history")
	}
}

// The check lives in the query, so a caller that forgets to check gets the
// correct answer rather than somebody's evening.
func TestActivityIsNotReadableWithoutConsent(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	u, _ := seedWatcher(t, st, "quiet", false)
	got, err := st.SharedActivity(ctx, u.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("returned %d entries for an account that never opted in", len(got))
	}
}

func TestActivityIsReadableAfterOptingIn(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	u, _ := seedWatcher(t, st, "sharer", true)
	got, err := st.SharedActivity(ctx, u.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("entries = %d, want 1 after opting in", len(got))
	}
	// Resume positions are excluded: where somebody stopped is a different and
	// more intrusive fact than what they watched.
	if got[0].PositionMS != 0 {
		t.Errorf("position_ms = %d, want 0 — ADR 0035 excludes resume positions",
			got[0].PositionMS)
	}
}

/*
 * Opting out is retroactive.
 *
 * A switch that cannot take back what it gave is not a switch, and this is the
 * property somebody will actually rely on — they share, they regret it, they
 * turn it off, and past activity has to go too.
 */
func TestOptingOutHidesPastActivity(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	u, _ := seedWatcher(t, st, "changed-mind", true)
	if got, _ := st.SharedActivity(ctx, u.ID, 20); len(got) != 1 {
		t.Fatalf("setup: expected shared activity, got %d entries", len(got))
	}

	if err := st.SetShareActivity(ctx, u.ID, false); err != nil {
		t.Fatal(err)
	}
	got, err := st.SharedActivity(ctx, u.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("entries = %d after opting out; the switch cannot take back what it gave", len(got))
	}
}

/*
 * A people list names everybody, and counts only for those who share.
 *
 * "Has not shared" and "watches nothing" are different statements, and a page
 * that cannot tell them apart accuses the private of being inactive.
 */
func TestPeopleReportsSharingWithoutLeakingCounts(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	me, err := st.CreateUser(ctx, "", "me", "hash", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	sharer, _ := seedWatcher(t, st, "sharer", true)
	quiet, _ := seedWatcher(t, st, "quiet", false)

	people, err := st.People(ctx, me.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 2 {
		t.Fatalf("people = %d, want 2 — the caller is excluded from their own list", len(people))
	}

	byID := map[string]Person{}
	for _, p := range people {
		byID[p.ID] = p
	}
	if !byID[sharer.ID].Sharing || byID[sharer.ID].Watched != 1 {
		t.Errorf("sharer = %+v, want sharing with a count", byID[sharer.ID])
	}
	if byID[quiet.ID].Sharing {
		t.Errorf("quiet account reported as sharing")
	}
	if byID[quiet.ID].Watched != 0 {
		t.Errorf("watched = %d for an account that did not opt in; a count is still a fact about a person",
			byID[quiet.ID].Watched)
	}
}

// The caller does not appear in their own people list.
func TestPeopleExcludesTheCaller(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	me, err := st.CreateUser(ctx, "", "me", "hash", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	people, err := st.People(ctx, me.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 0 {
		t.Errorf("people = %+v, want empty on a server with one account", people)
	}
}
