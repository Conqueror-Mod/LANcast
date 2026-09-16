package enrich

import (
	"context"
	"testing"

	"lancast/internal/meta"
	"lancast/internal/store"
)

/*
 * Re-asking about a title whose identity is settled.
 *
 * This is the only path in the project that touches a locked row, so the tests
 * that matter are the ones proving what it does *not* do. "Includes locked
 * rows" reads like the bug every other scope exists to prevent, and the
 * difference is not visible from the call site — it is visible here, in whether
 * a search was issued and whether the match state moved.
 *
 * The motivating case, for anybody reading this later: certificates were not
 * fetched until v0.9.23, so titles enriched before it carry none. A locked one
 * could never be told, because every refresh works by requeueing for the
 * search-and-score pass. Eighteen titles on the library this was built for,
 * three of them shows, and 88 episodes inheriting nothing beneath them.
 */

// settled marks a row as a locked identity with a provider id to fetch by.
func settled(t *testing.T, st *store.Store, id int64, state, provider, external string, score float64) {
	t.Helper()
	if err := st.UpdateItemMetadata(context.Background(), id, store.ItemMetadata{
		MatchState: &state, Provider: &provider, ExternalID: &external, MatchScore: &score,
	}); err != nil {
		t.Fatal(err)
	}
}

func settledWorker(t *testing.T, st *store.Store, rec *meta.Record) (*Worker, *fakeProvider) {
	t.Helper()
	p := &fakeProvider{id: "fake", record: rec}
	reg := meta.NewRegistry()
	reg.AddProvider(p)
	return New(st, reg, &fakeArt{}, quietLog()), p
}

func TestARefetchNeverSearches(t *testing.T) {
	/*
	 * The assertion the whole design rests on.
	 *
	 * A lock forbids re-scoring an identity, and re-scoring begins with a
	 * search. If this ever issued one, the pass would be the thing every other
	 * scope excludes locked rows to avoid — and nothing about the result would
	 * show it, because the fake would return the same record either way.
	 */
	ctx := context.Background()
	st, lib := harness(t)
	id := addItem(t, st, lib, `C:\m\raiders.mkv`, "Raiders of the Lost Ark", 1981)
	settled(t, st, id, "locked", "fake", "85", 1.0)

	w, p := settledWorker(t, st, arrivalRecord())
	item, err := st.GetItem(ctx, id, "local")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := w.RefetchSettled(ctx, *item)
	if err != nil || !ok {
		t.Fatalf("refetch = %v, %v", ok, err)
	}

	if p.searchN != 0 {
		t.Errorf("issued %d searches; a settled identity is not re-scored", p.searchN)
	}
	if p.fetchN != 1 {
		t.Errorf("fetched %d times, want exactly one", p.fetchN)
	}
	// And by the row's own recorded id, not by anything rediscovered.
	if p.lastFetch.ExternalID != "85" {
		t.Errorf("fetched %q, want the row's own id 85", p.lastFetch.ExternalID)
	}
}

func TestALockedRowStaysLockedAtTheScoreItHad(t *testing.T) {
	/*
	 * ApplyMatch decides a state because a person just chose an identity. Here
	 * nobody chose anything, so writing any state at all would be this pass
	 * forming an opinion about an identity it was told not to question.
	 */
	ctx := context.Background()
	st, lib := harness(t)
	id := addItem(t, st, lib, `C:\m\raiders.mkv`, "Raiders of the Lost Ark", 1981)
	settled(t, st, id, "locked", "fake", "85", 0.42)

	w, _ := settledWorker(t, st, arrivalRecord())
	item, _ := st.GetItem(ctx, id, "local")
	if _, err := w.RefetchSettled(ctx, *item); err != nil {
		t.Fatal(err)
	}

	after, err := st.GetItem(ctx, id, "local")
	if err != nil {
		t.Fatal(err)
	}
	if after.MatchState != "locked" {
		t.Errorf("match state = %q, want it untouched at locked", after.MatchState)
	}
	if after.MatchScore == nil || *after.MatchScore != 0.42 {
		t.Errorf("match score = %v, want the 0.42 it had", after.MatchScore)
	}
}

func TestTheProviderStillFillsInWhatWasMissing(t *testing.T) {
	// The point of the exercise: a locked row learns something it could not
	// have learned before. Overview stands in for content_rating here, since
	// the fake record carries one and the mechanism is identical.
	ctx := context.Background()
	st, lib := harness(t)
	id := addItem(t, st, lib, `C:\m\raiders.mkv`, "Raiders", 1981)
	settled(t, st, id, "locked", "fake", "85", 1.0)

	before, _ := st.GetItem(ctx, id, "local")
	if before.Overview != nil && *before.Overview != "" {
		t.Fatal("the row already had an overview, so this proves nothing")
	}

	w, _ := settledWorker(t, st, arrivalRecord())
	if _, err := w.RefetchSettled(ctx, *before); err != nil {
		t.Fatal(err)
	}

	after, _ := st.GetItem(ctx, id, "local")
	if after.Overview == nil || *after.Overview == "" {
		t.Error("the locked row learned nothing; the whole pass exists to fill this in")
	}
}

func TestALockedFieldIsStillNotOverwritten(t *testing.T) {
	/*
	 * The rule this pass is most likely to be accused of breaking, so it is
	 * asserted rather than argued. Locking a *field* is a separate act from
	 * locking a *match*, and this honours the first exactly as the background
	 * pass does.
	 */
	ctx := context.Background()
	st, lib := harness(t)
	id := addItem(t, st, lib, `C:\m\raiders.mkv`, "Raiders of the Lost Ark", 1981)
	settled(t, st, id, "locked", "fake", "85", 1.0)
	if err := st.LockField(ctx, id, meta.FieldTitle); err != nil {
		t.Fatal(err)
	}

	w, _ := settledWorker(t, st, arrivalRecord()) // the record is titled "Arrival"
	item, _ := st.GetItem(ctx, id, "local")
	if _, err := w.RefetchSettled(ctx, *item); err != nil {
		t.Fatal(err)
	}

	after, _ := st.GetItem(ctx, id, "local")
	if after.Title != "Raiders of the Lost Ark" {
		t.Errorf("title = %q, want the locked one kept", after.Title)
	}
}

func TestARowWithNothingToAskAboutIsSkippedNotFailed(t *testing.T) {
	// A locked row somebody set by hand carries no provider id. Skipping it is
	// an ordinary outcome; failing would abandon the rest of the pass.
	ctx := context.Background()
	st, lib := harness(t)
	id := addItem(t, st, lib, `C:\m\home-video.mkv`, "A Home Video", 0)
	settled(t, st, id, "locked", "", "", 0)

	w, p := settledWorker(t, st, arrivalRecord())
	item, _ := st.GetItem(ctx, id, "local")
	ok, err := w.RefetchSettled(ctx, *item)
	if err != nil {
		t.Errorf("err = %v, want a skip rather than a failure", err)
	}
	if ok {
		t.Error("reported an update for a row it could not ask about")
	}
	if p.fetchN != 0 {
		t.Error("fetched despite having no id to fetch by")
	}
}

func TestAProviderThatIsNoLongerConfiguredIsSkipped(t *testing.T) {
	// A library enriched by a provider whose key has since been removed. An
	// ordinary state, and a pass over hundreds of rows must not stop for it.
	ctx := context.Background()
	st, lib := harness(t)
	id := addItem(t, st, lib, `C:\m\raiders.mkv`, "Raiders", 1981)
	settled(t, st, id, "locked", "some-provider-that-left", "85", 1.0)

	w, _ := settledWorker(t, st, arrivalRecord())
	item, _ := st.GetItem(ctx, id, "local")
	ok, err := w.RefetchSettled(ctx, *item)
	if err != nil || ok {
		t.Errorf("refetch = %v, %v; want a quiet skip", ok, err)
	}
}

func TestOneBadRowDoesNotEndTheRun(t *testing.T) {
	/*
	 * A pass that abandoned eighteen titles because the fourth had been
	 * withdrawn from TMDB would be a button nobody could rely on. The failing
	 * row is counted and the rest are still attempted.
	 */
	ctx := context.Background()
	st, lib := harness(t)
	good := addItem(t, st, lib, `C:\m\a.mkv`, "A", 2001)
	bad := addItem(t, st, lib, `C:\m\b.mkv`, "B", 2002)
	settled(t, st, good, "locked", "fake", "85", 1.0)
	settled(t, st, bad, "locked", "gone", "99", 1.0)

	w, _ := settledWorker(t, st, arrivalRecord())
	a, _ := st.GetItem(ctx, good, "local")
	b, _ := st.GetItem(ctx, bad, "local")

	updated, failed := w.RefetchSettledAll(ctx, []store.Item{*b, *a}, quietLog())
	if updated != 1 {
		t.Errorf("updated = %d, want the one good row", updated)
	}
	// The unconfigured provider is a skip, not a failure — so nothing failed,
	// and the good row was still reached despite being second.
	if failed != 0 {
		t.Errorf("failed = %d, want 0", failed)
	}
}

func TestACancelledRunStopsRatherThanBurningLookups(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	st, lib := harness(t)
	id := addItem(t, st, lib, `C:\m\a.mkv`, "A", 2001)
	settled(t, st, id, "locked", "fake", "85", 1.0)

	w, p := settledWorker(t, st, arrivalRecord())
	item, _ := st.GetItem(context.Background(), id, "local")
	cancel()

	updated, failed := w.RefetchSettledAll(ctx, []store.Item{*item}, quietLog())
	if updated != 0 || failed != 0 {
		t.Errorf("updated %d failed %d, want nothing attempted", updated, failed)
	}
	if p.fetchN != 0 {
		t.Error("spent a lookup after the run was cancelled")
	}
}
