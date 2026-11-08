package store

import (
	"context"
	"path/filepath"
	"testing"
)

/*
 * On this day from the watch history (ADR 0119 §3a). Like the photograph
 * tests beside it, most cases are about what must not appear.
 */

func logAt(t *testing.T, st *Store, user string, item, at int64, estimated bool) {
	t.Helper()
	est := 0
	if estimated {
		est = 1
	}
	if _, err := st.db.Exec(`INSERT INTO viewing (user_id, item_id, finished_at, estimated, kind, title)
		SELECT ?, id, ?, ?, kind, title FROM media_item WHERE id = ?`, user, at, est, item); err != nil {
		t.Fatal(err)
	}
}

func finishedIDs(t *testing.T, st *Store, user string) []int64 {
	t.Helper()
	got, err := st.FinishedOnThisDay(context.Background(), user, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := []int64{}
	for _, m := range got {
		out = append(out, m.Item.ID)
	}
	return out
}

func TestFinishedOnThisDayIsTodayInAnEarlierYear(t *testing.T) {
	ctx := context.Background()
	st, me, heat := viewingFixture(t)
	lib, err := st.CreateLibrary(ctx, "More films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	film := func(name string) int64 {
		id, err := st.UpsertItem(ctx, ScanFile{LibraryID: lib.ID, Path: filepath.Join(lib.Path, name+".mkv"),
			Kind: "movie", Title: name, SortTitle: name, Container: "mkv", SizeBytes: 1, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	ronin, thisYear, tomorrow, guessed := film("Ronin"), film("Collateral"), film("Thief"), film("Manhunter")

	logAt(t, st, me, heat, takenAt(2), false)
	logAt(t, st, me, ronin, takenAt(1), false)
	logAt(t, st, me, thisYear, takenAt(0), false)      // today is not a memory
	logAt(t, st, me, tomorrow, takenAt(1)+86400, false) // a different day
	logAt(t, st, me, guessed, takenAt(1), true)         // a backfilled date is not a real one

	got := finishedIDs(t, st, me)
	if len(got) != 2 || got[0] != ronin || got[1] != heat {
		t.Errorf("got %v, want [%d %d] (Ronin then Heat)", got, ronin, heat)
	}
}

func TestAFilmWatchedEveryYearIsOneMemory(t *testing.T) {
	st, me, heat := viewingFixture(t)
	logAt(t, st, me, heat, takenAt(3), false)
	logAt(t, st, me, heat, takenAt(1), false)
	got, err := st.FinishedOnThisDay(context.Background(), me, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].FinishedAt != takenAt(1) {
		t.Errorf("got %+v, want one entry at the latest year", got)
	}
}

func TestFinishedOnThisDayIsYourOwn(t *testing.T) {
	ctx := context.Background()
	st, me, heat := viewingFixture(t)
	other, err := st.CreateUser(ctx, "", "eli", "hash", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	logAt(t, st, other.ID, heat, takenAt(1), false)
	if got := finishedIDs(t, st, me); len(got) != 0 {
		t.Errorf("another account's history showed up: %v", got)
	}
}

func TestFinishedOnThisDaySkipsMissingAndDeleted(t *testing.T) {
	st, me, heat := viewingFixture(t)
	logAt(t, st, me, heat, takenAt(1), false)
	if _, err := st.db.Exec(`UPDATE media_item SET missing = 1 WHERE id = ?`, heat); err != nil {
		t.Fatal(err)
	}
	if got := finishedIDs(t, st, me); len(got) != 0 {
		t.Errorf("a missing file showed up: %v", got)
	}
	if _, err := st.db.Exec(`UPDATE viewing SET item_id = NULL`); err != nil {
		t.Fatal(err)
	}
	if got := finishedIDs(t, st, me); len(got) != 0 {
		t.Errorf("a deleted item showed up: %v", got)
	}
}
