package store

import (
	"context"
	"testing"
)

// review puts a seeded item into the uncertain band, which is the only state a
// re-parse is allowed to touch.
func review(t *testing.T, st *Store, id int64, title string, year int) {
	t.Helper()
	ctx := context.Background()
	if err := st.UpdateItemMetadata(ctx, id, ItemMetadata{Title: &title, Year: &year}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetMatch(ctx, id, "tmdb", "1", "review", 0.68); err != nil {
		t.Fatal(err)
	}
}

/*
 * The case this exists for: a film whose year lived only in its folder name was
 * searched with no year, matched something 34 years off, and the wrong answer
 * was applied. Re-parsing must correct the stored guess *and* put the row back
 * in the enrichment queue — writing a better question and never asking it fixes
 * nothing.
 */
func TestApplyGuessCorrectsIdentityAndRequeues(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `X:\Aliens SE (1986)\Aliens SE.mp4`)
	review(t, st, id, "Alien Sexting", 2020)

	changed, err := st.ApplyGuess(ctx, id, Guess{Title: "Aliens", SortTitle: "aliens", Year: 1986})
	if err != nil {
		t.Fatalf("ApplyGuess: %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}

	it, err := st.GetItem(ctx, id, "local")
	if err != nil {
		t.Fatal(err)
	}
	if it.Title != "Aliens" {
		t.Errorf("title = %q, want Aliens", it.Title)
	}
	if it.Year == nil || *it.Year != 1986 {
		t.Errorf("year = %v, want 1986", it.Year)
	}

	pending, err := st.PendingEnrichment(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range pending {
		if p.ID == id {
			found = true
		}
	}
	if !found {
		t.Error("item was not requeued for enrichment — the corrected guess is never searched")
	}
}

// Locked fields are never overwritten (CLAUDE.md), and the lock is per field:
// owning the title does not stop the year being re-parsed.
func TestApplyGuessSkipsLockedFieldsIndividually(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `X:\Blade Runner (1982)\Blade Runner.mkv`)
	review(t, st, id, "My Own Title", 0)

	if err := st.LockField(ctx, id, "title"); err != nil {
		t.Fatal(err)
	}

	if _, err := st.ApplyGuess(ctx, id, Guess{Title: "Blade Runner", SortTitle: "blade runner", Year: 1982}); err != nil {
		t.Fatalf("ApplyGuess: %v", err)
	}

	it, _ := st.GetItem(ctx, id, "local")
	if it.Title != "My Own Title" {
		t.Errorf("title = %q — a locked field was overwritten", it.Title)
	}
	if it.Year == nil || *it.Year != 1982 {
		t.Errorf("year = %v, want 1982 — an unrelated lock stopped an unlocked field", it.Year)
	}
}

// An empty guess is not an answer. The parser having no opinion about a field
// must never erase what is already there.
func TestApplyGuessNeverClearsAFieldWithNothing(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `X:\Movie.mkv`)
	review(t, st, id, "Something Known", 1999)

	if _, err := st.ApplyGuess(ctx, id, Guess{}); err != nil {
		t.Fatalf("ApplyGuess: %v", err)
	}

	it, _ := st.GetItem(ctx, id, "local")
	if it.Title != "Something Known" {
		t.Errorf("title = %q, want it untouched by an empty guess", it.Title)
	}
	if it.Year == nil || *it.Year != 1999 {
		t.Errorf("year = %v, want 1999 untouched", it.Year)
	}
}

// Running it twice must be free. A version that requeued every row on every run
// would turn a repair into a provider-quota event.
func TestApplyGuessIsIdempotent(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `X:\Dredd (2012)\Dredd.mkv`)
	review(t, st, id, "Dredd", 2012)

	changed, err := st.ApplyGuess(ctx, id, Guess{Title: "Dredd", SortTitle: "dredd", Year: 2012})
	if err != nil {
		t.Fatalf("ApplyGuess: %v", err)
	}
	if changed {
		t.Error("changed = true for a row that already agrees with its filename")
	}
}

/*
 * Scope is the safety of the whole operation. A matched row's title came from a
 * provider, which is better evidence than any filename — offering it to a
 * re-parse would trade a thousand correct titles for a chance at a hundred
 * uncertain ones. A locked identity is never re-litigated at all.
 */
func TestReparseTargetsOnlyOffersUncertainRows(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	uncertain := seedItem(t, st, lib, `X:\a.mkv`)
	review(t, st, uncertain, "Uncertain", 0)

	matched := seedItem(t, st, lib, `X:\b.mkv`)
	title := "Matched"
	st.UpdateItemMetadata(ctx, matched, ItemMetadata{Title: &title})
	if err := st.SetMatch(ctx, matched, "tmdb", "2", "matched", 0.94); err != nil {
		t.Fatal(err)
	}

	locked := seedItem(t, st, lib, `X:\c.mkv`)
	st.UpdateItemMetadata(ctx, locked, ItemMetadata{Title: &title})
	if err := st.SetMatch(ctx, locked, "tmdb", "3", "locked", 1); err != nil {
		t.Fatal(err)
	}

	targets, err := st.ReparseTargets(ctx, lib.ID)
	if err != nil {
		t.Fatalf("ReparseTargets: %v", err)
	}
	if len(targets) != 1 || targets[0].ItemID != uncertain {
		t.Fatalf("targets = %+v, want only the uncertain row (%d)", targets, uncertain)
	}
}
