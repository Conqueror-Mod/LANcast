package store

import (
	"context"
	"path/filepath"
	"testing"
)

// seedIntroSeason builds a show with one season of n probed episodes.
func seedIntroSeason(t *testing.T, st *Store, show string, season, n int) (int64, []int64) {
	t.Helper()
	ctx := context.Background()
	lib, err := st.CreateLibrary(ctx, "Shows "+show, "show", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	showID, err := st.UpsertItem(ctx, ScanFile{
		LibraryID: lib.ID, Path: filepath.Join(lib.Path, show), Kind: "show",
		Title: show, SortTitle: show, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	var eps []int64
	for i := 1; i <= n; i++ {
		se, ep := season, i
		id, err := st.UpsertItem(ctx, ScanFile{
			LibraryID: lib.ID,
			Path:      filepath.Join(lib.Path, show, "e.mkv"),
			Kind:      "episode", Title: "Ep", SortTitle: "ep",
			Season: &se, Episode: &ep, Container: "mkv", SizeBytes: 1, MTime: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.db.ExecContext(ctx,
			`UPDATE media_item SET parent_id = ?, probed_at = 1, duration_ms = 1200000,
			 path = ? WHERE id = ?`,
			showID, filepath.Join(lib.Path, show, "s", "e", string(rune('a'+i))+".mkv"), id); err != nil {
			t.Fatal(err)
		}
		eps = append(eps, id)
	}
	return showID, eps
}

func TestPendingIntroSeasonsGroupsBySeason(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	seedIntroSeason(t, st, "Show", 1, 4)
	seedIntroSeason(t, st, "Show", 2, 3)

	got, err := st.PendingIntroSeasons(ctx, 2, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d seasons, want 2 — intros change between seasons", len(got))
	}
	total := 0
	for _, s := range got {
		total += len(s.Episodes)
	}
	if total != 7 {
		t.Errorf("got %d episodes across the seasons, want 7", total)
	}
}

// One episode has nothing to compare against, so its season is never offered.
func TestPendingIntroSeasonsSkipsASeasonTooSmallToCompare(t *testing.T) {
	st := openTestStore(t)
	seedIntroSeason(t, st, "Lonely", 1, 1)
	got, err := st.PendingIntroSeasons(context.Background(), 2, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %d seasons, want none", len(got))
	}
}

// A season is offered whole even when one episode is pending, because the
// comparison needs the others.
func TestPendingIntroSeasonsOffersTheWholeSeason(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	_, eps := seedIntroSeason(t, st, "Show", 1, 4)
	if err := st.MarkIntrosExamined(ctx, eps[:3], 100); err != nil {
		t.Fatal(err)
	}

	got, err := st.PendingIntroSeasons(ctx, 2, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d seasons, want 1 — one episode is still pending", len(got))
	}
	if len(got[0].Episodes) != 4 {
		t.Errorf("got %d episodes, want all 4 — the comparison needs the others",
			len(got[0].Episodes))
	}
}

func TestASeasonFullyExaminedIsNotOffered(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	_, eps := seedIntroSeason(t, st, "Show", 1, 4)
	if err := st.MarkIntrosExamined(ctx, eps, 100); err != nil {
		t.Fatal(err)
	}
	got, err := st.PendingIntroSeasons(ctx, 2, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %d seasons, want none — every episode was examined", len(got))
	}
}

func TestClearIntrosBringsASeasonBack(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	_, eps := seedIntroSeason(t, st, "Show", 1, 4)
	if err := st.MarkIntrosExamined(ctx, eps, 100); err != nil {
		t.Fatal(err)
	}
	n, err := st.ClearIntros(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Errorf("cleared %d, want 4", n)
	}
	got, _ := st.PendingIntroSeasons(ctx, 2, 20)
	if len(got) != 1 {
		t.Error("the season did not come back onto the queue")
	}
}
