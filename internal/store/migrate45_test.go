package store

import (
	"context"
	"testing"
)

/*
 * Revision 45 — seasons examined by the old intro rule are examined again.
 *
 * The detector changed underneath a stamp. Without this, a season the old rule
 * examined and marked nothing in keeps intros_at for ever, and the fix reaches
 * only episodes added after it — which on a real library is almost none of the
 * nineteen seasons it was written for.
 */

func rewindTo44(t *testing.T, st *Store) {
	t.Helper()
	if _, err := st.db.Exec(`UPDATE meta SET value = '44' WHERE key = 'schema_version'`); err != nil {
		t.Fatal(err)
	}
}

func TestRevision45PutsExaminedSeasonsBackOnTheQueue(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	_, eps := seedIntroSeason(t, st, "Blue Mountain State", 1, 4)
	if err := st.MarkIntrosExamined(ctx, eps, 100); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.PendingIntroSeasons(ctx, 2, 20); len(got) != 0 {
		t.Fatalf("setup: %d seasons pending before the migration, want none", len(got))
	}

	rewindTo44(t, st)
	if err := migrate(st.db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	got, err := st.PendingIntroSeasons(ctx, 2, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Episodes) != 4 {
		t.Errorf("after revision 45: %d seasons pending, want the one season with its 4 episodes", len(got))
	}
}

// The reset clears a stamp and nothing else: markers already found stay until
// the pass replaces them, so a library is never briefly without the intros it
// had.
func TestRevision45KeepsTheMarkersAlreadyFound(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	_, eps := seedIntroSeason(t, st, "Sunny", 3, 3)
	end := int64(85_500)
	if err := st.SaveMarkers(ctx, eps[0], []string{MarkerIntro}, []Marker{{
		Kind: MarkerIntro, StartMS: 55_300, EndMS: &end, Source: "fingerprint", Confidence: 1,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkIntrosExamined(ctx, eps, 100); err != nil {
		t.Fatal(err)
	}

	rewindTo44(t, st)
	if err := migrate(st.db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	ms, err := st.MarkersFor(ctx, eps[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].StartMS != 55_300 {
		t.Errorf("markers after revision 45 = %+v, want the intro that was already there", ms)
	}
}

func TestRevision45IsIdempotent(t *testing.T) {
	st := openTestStore(t)
	rewindTo44(t, st)
	for i := 0; i < 2; i++ {
		if err := migrate(st.db); err != nil {
			t.Fatalf("migrate %d: %v", i+1, err)
		}
	}
	var v int
	if err := st.db.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	// The constant, not a literal: this test failed the moment revision 46 was
	// added, on a migration that was working perfectly.
	if v != CurrentSchemaVersion {
		t.Errorf("schema_version = %d, want %d", v, CurrentSchemaVersion)
	}
}
