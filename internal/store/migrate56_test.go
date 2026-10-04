package store

import (
	"context"
	"testing"
)

/*
 * Revision 56 — every season is compared again for its ending; and a file
 * whose length changes has its season compared again.
 */

func introsAt(t *testing.T, st *Store, id int64) *int64 {
	t.Helper()
	var v *int64
	if err := st.db.QueryRow(`SELECT intros_at FROM media_item WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestRevision56RequeuesEverySeason(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	lib, err := st.CreateLibrary(ctx, "TV", "show", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ep := seedProbed(t, st, lib, "S01E01.mkv", probedFile{kind: "episode", sizeBytes: 1, durationMS: 1_300_000})
	film := seedProbed(t, st, lib, "Film.mkv", probedFile{kind: "movie", sizeBytes: 1, durationMS: 6_000_000})
	if err := st.MarkIntrosExamined(ctx, []int64{ep, film}, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE meta SET value = '55' WHERE key = 'schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(st.db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if introsAt(t, st, ep) != nil {
		t.Error("an examined episode kept its stamp; its season will never be compared for an ending")
	}
	// Only episodes have seasons. A stray stamp elsewhere is not this
	// revision's business.
	if introsAt(t, st, film) == nil {
		t.Error("a film's intros_at was cleared")
	}
}

func TestANewLengthComparesTheSeasonAgain(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	lib, err := st.CreateLibrary(ctx, "TV", "show", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := seedProbed(t, st, lib, "S01E01.mkv", probedFile{kind: "episode", sizeBytes: 1, durationMS: 1_300_000})
	if err := st.MarkIntrosExamined(ctx, []int64{id}, 100); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProbe(ctx, id, ProbeResult{DurationMS: 1_300_040}); err != nil {
		t.Fatal(err)
	}
	if introsAt(t, st, id) == nil {
		t.Fatal("a re-probe that agreed to within a frame re-queued the season")
	}
	if err := st.SaveProbe(ctx, id, ProbeResult{DurationMS: 1_420_000}); err != nil {
		t.Fatal(err)
	}
	if introsAt(t, st, id) != nil {
		t.Error("the episode's length changed and its season was not compared again")
	}
}

func TestRevision57RequeuesEverySeasonAgain(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	lib, err := st.CreateLibrary(ctx, "TV", "show", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ep := seedProbed(t, st, lib, "S01E01.mkv", probedFile{kind: "episode", sizeBytes: 1, durationMS: 1_300_000})
	if err := st.MarkIntrosExamined(ctx, []int64{ep}, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE meta SET value = '56' WHERE key = 'schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(st.db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if introsAt(t, st, ep) != nil {
		t.Error("an examined episode kept its stamp; the wider window and the ident rule never reach it")
	}
}
