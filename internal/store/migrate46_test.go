package store

import (
	"context"
	"testing"
)

/*
 * Revision 46 — episodes written off as examined for credits are looked at.
 *
 * Every episode of a real library carried markers_at and none carried a credits
 * marker, because the intro pass stamped the credits pass's flag. Clearing the
 * stamp where there is no marker is the whole migration.
 */

func rewindTo45(t *testing.T, st *Store) {
	t.Helper()
	/*
	 * The version *and* what later revisions added.
	 *
	 * Winding the number back on its own is a fiction: the store was opened at
	 * the current revision, so its tables are already carrying every column
	 * added since. Replaying from 45 then re-runs those ALTERs against columns
	 * that exist, and the migration fails for a reason that has nothing to do
	 * with what is being tested. Revision 47's column is the first to make that
	 * bite, and anything added after it belongs on this list too.
	 */
	dropPost45Additions(t, st)
	if _, err := st.db.Exec(`UPDATE meta SET value = '45' WHERE key = 'schema_version'`); err != nil {
		t.Fatal(err)
	}
}

func TestRevision46RequeuesEpisodesStampedWithoutACreditsMarker(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	_, eps := seedIntroSeason(t, st, "Show", 1, 3)

	// What the intro pass left behind: stamped, with only an intro marker.
	end := int64(30_000)
	for _, id := range eps {
		if _, err := st.db.ExecContext(ctx,
			`UPDATE media_item SET markers_at = 100 WHERE id = ?`, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SaveMarkers(ctx, eps[0], []string{MarkerIntro}, []Marker{{
		Kind: MarkerIntro, StartMS: 500, EndMS: &end, Source: "fingerprint",
	}}); err != nil {
		t.Fatal(err)
	}
	// And one episode the credits pass really did examine.
	credEnd := int64(1_300_000)
	if err := st.SaveMarkers(ctx, eps[2], []string{MarkerCredits}, []Marker{{
		Kind: MarkerCredits, StartMS: 1_200_000, EndMS: &credEnd, Source: "blackdetect",
	}}); err != nil {
		t.Fatal(err)
	}

	rewindTo45(t, st)
	if err := migrate(st.db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	pending, err := st.PendingMarkers(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	queued := map[int64]bool{}
	for _, it := range pending {
		queued[it.ID] = true
	}
	if !queued[eps[0]] || !queued[eps[1]] {
		t.Errorf("episodes stamped without a credits marker were not re-queued: %v", queued)
	}
	if queued[eps[2]] {
		t.Error("an episode that really was examined for credits was re-queued")
	}
}

// A film's stamp is the credits pass's own work and must be left alone: no
// intro pass has ever touched one.
func TestRevision46LeavesFilmsAlone(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	lib, err := st.CreateLibrary(ctx, "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.UpsertItem(ctx, ScanFile{
		LibraryID: lib.ID, Path: lib.Path + "/f.mkv", Kind: "movie",
		Title: "A Film", SortTitle: "a film", Container: "mkv", SizeBytes: 1, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx,
		`UPDATE media_item SET probed_at = 1, duration_ms = 5400000, markers_at = 100 WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}

	rewindTo45(t, st)
	if err := migrate(st.db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	pending, _ := st.PendingMarkers(ctx, 50)
	for _, it := range pending {
		if it.ID == id {
			t.Fatal("a film that had been examined for credits was re-queued")
		}
	}
}

func TestRevision46IsIdempotent(t *testing.T) {
	st := openTestStore(t)
	rewindTo45(t, st)
	for i := 0; i < 2; i++ {
		if err := migrate(st.db); err != nil {
			t.Fatalf("migrate %d: %v", i+1, err)
		}
	}
	var v int
	if err := st.db.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	// Against the constant, not a literal: a hardcoded number here is a test
	// that fails on the next revision for no reason, which is exactly what 45's
	// did when this one was added.
	if v != CurrentSchemaVersion {
		t.Errorf("schema_version = %d, want %d", v, CurrentSchemaVersion)
	}
}

/*
 * Undo what revisions after 45 added, so a rewind is more than a number.
 *
 * Shared by both rewinds because both replay through the same later
 * migrations. Anything added after revision 47 belongs here too.
 */
func dropPost45Additions(t *testing.T, st *Store) {
	t.Helper()
	if _, err := st.db.Exec(`ALTER TABLE user DROP COLUMN max_content_rating`); err != nil {
		t.Fatal(err)
	}
}
