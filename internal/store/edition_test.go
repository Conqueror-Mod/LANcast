package store

import (
	"context"
	"testing"
)

/*
 * The edition backfill (ADR 0049).
 *
 * `edition` arrived at revision 29 and is written by UpsertItem, and the
 * scanner only upserts a file whose bytes moved — so every row older than the
 * column reads NULL for ever. On the reporting library that was all 1,229
 * movies, and no rule can group on a field that is null everywhere.
 *
 * These tests are about the two properties that make the backfill safe to run
 * inside a scan: it reaches rows re-parse deliberately refuses, and it writes
 * nothing for a film that has no marker — NULL is already how "no marker" is
 * spelt (ADR 0042), and a second spelling would be one the API disagrees with
 * itself over.
 */

// legacy clears the column, which is both what a pre-revision-29 row looks like
// and what an ordinary unmarked film looks like. The two are indistinguishable
// by design, and that is the trade this backfill accepts.
func legacy(t *testing.T, st *Store, id int64) {
	t.Helper()
	if _, err := st.db.Exec(`UPDATE media_item SET edition = NULL WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
}

func editionOf(t *testing.T, st *Store, id int64) (string, bool) {
	t.Helper()
	var ed *string
	if err := st.db.QueryRow(`SELECT edition FROM media_item WHERE id = ?`, id).Scan(&ed); err != nil {
		t.Fatal(err)
	}
	if ed == nil {
		return "", false
	}
	return *ed, true
}

func hasTarget(targets []EditionTarget, id int64) bool {
	for _, t := range targets {
		if t.ItemID == id {
			return true
		}
	}
	return false
}

/*
 * A row that already carries a marker is left alone.
 *
 * The backfill is driven by the state of the column rather than by a stamp, so
 * this is the only thing keeping it from rewriting settled rows on every scan.
 */
func TestARowThatAlreadyHasAMarkerIsNotOffered(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	unread := seedItem(t, st, lib, `X:\Aliens SE (1986)\Aliens SE.mkv`)
	marked := seedItem(t, st, lib, `X:\Alien DC (1979)\Alien DC.mkv`)
	if _, err := st.SetEdition(ctx, marked, "DC"); err != nil {
		t.Fatal(err)
	}

	targets, err := st.EditionBackfillTargets(ctx, lib.ID)
	if err != nil {
		t.Fatalf("EditionBackfillTargets: %v", err)
	}
	if !hasTarget(targets, unread) {
		t.Error("a row with no edition was not offered — the backfill can never reach it")
	}
	if hasTarget(targets, marked) {
		t.Error("a row that already has its marker was offered again")
	}
}

/*
 * The exclusion ReparseTargets has, deliberately absent here.
 *
 * Re-parse refuses 'matched' and 'locked' rows because a provider title is
 * better evidence than a filename. An edition marker is not a title: no
 * provider knows which cut a particular file holds, and only the filename
 * records it. The motivating case in the reporting library is itself 'locked',
 * so a backfill that inherited re-parse's scope would miss the one row it
 * exists for.
 */
func TestTheBackfillReachesMatchedAndLockedRows(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)

	matched := seedItem(t, st, lib, `X:\Alien 3 SE (1992)\Alien 3 SE.mkv`)
	locked := seedItem(t, st, lib,
		`X:\Spider-Man Into the Spider-Verse (Alternate Cut) (2018)\film.mkv`)
	legacy(t, st, matched)
	legacy(t, st, locked)

	if err := st.SetMatch(ctx, matched, "tmdb", "8077", "matched", 0.95); err != nil {
		t.Fatal(err)
	}
	if err := st.SetMatch(ctx, locked, "tmdb", "324857", "locked", 1.0); err != nil {
		t.Fatal(err)
	}

	targets, err := st.EditionBackfillTargets(ctx, lib.ID)
	if err != nil {
		t.Fatalf("EditionBackfillTargets: %v", err)
	}
	if !hasTarget(targets, matched) {
		t.Error("a matched row was not offered")
	}
	if !hasTarget(targets, locked) {
		t.Error("a locked row was not offered — this is the state the one real case is in")
	}
}

/*
 * A film with no marker is not written at all.
 *
 * NULL is how "no marker" is spelt, and `Edition *string` exists to say so
 * (ADR 0042) — the scanner's own test asserts an unmarked film comes back nil.
 * Writing an empty string here to record "we looked" would put a second
 * spelling of the same fact in the column, which the API would then have to
 * report as an edition named "".
 */
func TestAFilmWithNoMarkerIsLeftAlone(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `X:\Alien (1979)\Alien.mkv`)
	legacy(t, st, id)

	marked, err := st.SetEdition(ctx, id, "")
	if err != nil {
		t.Fatalf("SetEdition: %v", err)
	}
	if marked {
		t.Error("an absent marker was reported as one that was found")
	}
	if _, present := editionOf(t, st, id); present {
		t.Error("an empty marker was written — NULL is how 'no marker' is spelt")
	}
}

func TestAMarkerThatWasFoundIsReported(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `X:\Aliens SE (1986)\Aliens SE.mkv`)
	legacy(t, st, id)

	marked, err := st.SetEdition(ctx, id, "SE")
	if err != nil {
		t.Fatalf("SetEdition: %v", err)
	}
	if !marked {
		t.Error("marked = false, want true")
	}
	if ed, _ := editionOf(t, st, id); ed != "SE" {
		t.Errorf("edition = %q, want SE", ed)
	}
}

/*
 * Locked fields are never overwritten (CLAUDE.md).
 *
 * A person who edited this field owns it, and a filename guess is exactly what
 * the lock exists to keep out.
 */
func TestALockedEditionIsNotWrittenAtAll(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `X:\Aliens SE (1986)\Aliens SE.mkv`)
	legacy(t, st, id)
	if err := st.LockField(ctx, id, "edition"); err != nil {
		t.Fatal(err)
	}

	marked, err := st.SetEdition(ctx, id, "SE")
	if err != nil {
		t.Fatalf("SetEdition: %v", err)
	}
	if marked {
		t.Error("a locked edition was reported as written")
	}
	if _, present := editionOf(t, st, id); present {
		t.Error("a locked edition was written over")
	}
}

// A lock on some other field says "I own the title", not "stop looking at this
// file" — the same per-field rule ApplyGuess follows.
func TestALockOnAnotherFieldDoesNotBlockTheEdition(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `X:\Aliens SE (1986)\Aliens SE.mkv`)
	legacy(t, st, id)
	if err := st.LockField(ctx, id, "title"); err != nil {
		t.Fatal(err)
	}

	if _, err := st.SetEdition(ctx, id, "SE"); err != nil {
		t.Fatalf("SetEdition: %v", err)
	}
	if ed, _ := editionOf(t, st, id); ed != "SE" {
		t.Errorf("edition = %q, want SE — a locked title must not stop it", ed)
	}
}

/*
 * The backfill must not requeue enrichment.
 *
 * ApplyGuess clears metadata_updated_at on purpose: a corrected guess is a
 * better question, and writing one without asking it fixes nothing. This is the
 * opposite case. An edition marker is an answer no provider has, so requeueing
 * the library to ask again would turn a repair into a provider-quota event —
 * on every row in the library at once, since every row needs backfilling.
 */
func TestTheBackfillDoesNotRequeueEnrichment(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	lib := mustLibrary(t, st)
	id := seedItem(t, st, lib, `X:\Aliens SE (1986)\Aliens SE.mkv`)
	legacy(t, st, id)

	title := "Aliens"
	if err := st.UpdateItemMetadata(ctx, id, ItemMetadata{Title: &title}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetMatch(ctx, id, "tmdb", "679", "matched", 0.95); err != nil {
		t.Fatal(err)
	}

	if _, err := st.SetEdition(ctx, id, "SE"); err != nil {
		t.Fatalf("SetEdition: %v", err)
	}

	pending, err := st.PendingEnrichment(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pending {
		if p.ID == id {
			t.Fatal("reading an edition marker put the row back in the enrichment queue")
		}
	}
}
