package store

import (
	"context"
	"testing"
	"time"
)

/*
 * Resolving anything to the show it belongs to.
 *
 * The home page's Resume button played one episode of a half-watched series and
 * stopped, with nothing to distinguish that from a show that had ended. The
 * cause was here: the episodes endpoint took a show id, and every client was
 * expected to have walked the hierarchy already.
 */

func showTree(t *testing.T, s *Store) (lib, show, season, ep int64) {
	t.Helper()
	ctx := context.Background()
	l, err := s.CreateLibrary(ctx, "Shows", "show", `C:\tv`)
	if err != nil {
		t.Fatal(err)
	}
	row := func(kind, path, title string, parent *int64) int64 {
		t.Helper()
		res, err := s.db.Exec(`
			INSERT INTO media_item (library_id, kind, path, title, sort_title,
				parent_id, added_at, updated_at, missing)
			VALUES (?, ?, ?, ?, ?, ?, 1, 1, 0)`,
			l.ID, kind, path, title, title, parent)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	show = row("show", `C:\tv\Show`, "Show", nil)
	season = row("season", `C:\tv\Show\S01`, "Season 1", &show)
	ep = row("episode", `C:\tv\Show\S01\E01.mkv`, "E01", &season)
	return l.ID, show, season, ep
}

// The reported case: an episode two levels down finds its show.
func TestAnEpisodeResolvesToItsShow(t *testing.T) {
	s := openTestStore(t)
	_, show, _, ep := showTree(t, s)

	got, err := s.ShowOf(context.Background(), ep)
	if err != nil {
		t.Fatal(err)
	}
	if got != show {
		t.Errorf("ShowOf(episode) = %d, want the show %d", got, show)
	}
}

// A season resolves too, since a client may hold one.
func TestASeasonResolvesToItsShow(t *testing.T) {
	s := openTestStore(t)
	_, show, season, _ := showTree(t, s)

	if got, _ := s.ShowOf(context.Background(), season); got != show {
		t.Errorf("ShowOf(season) = %d, want %d", got, show)
	}
}

// A show is already the answer, and must not walk past itself into whatever
// happens to be above it.
func TestAShowResolvesToItself(t *testing.T) {
	s := openTestStore(t)
	_, show, _, _ := showTree(t, s)

	if got, _ := s.ShowOf(context.Background(), show); got != show {
		t.Errorf("ShowOf(show) = %d, want %d", got, show)
	}
}

/*
 * Some shows keep their episodes directly under the show row, which is why the
 * walk is a loop rather than two hard-coded hops.
 */
func TestAnEpisodeDirectlyUnderItsShowResolves(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	l, err := s.CreateLibrary(ctx, "Shows", "show", `C:\tv2`)
	if err != nil {
		t.Fatal(err)
	}
	res, _ := s.db.Exec(`INSERT INTO media_item (library_id, kind, path, title,
		sort_title, added_at, updated_at, missing)
		VALUES (?, 'show', ?, 'Flat', 'flat', 1, 1, 0)`, l.ID, `C:\tv2\Flat`)
	show, _ := res.LastInsertId()
	res, _ = s.db.Exec(`INSERT INTO media_item (library_id, kind, path, title,
		sort_title, parent_id, added_at, updated_at, missing)
		VALUES (?, 'episode', ?, 'E01', 'e01', ?, 1, 1, 0)`,
		l.ID, `C:\tv2\Flat\E01.mkv`, show)
	ep, _ := res.LastInsertId()

	if got, _ := s.ShowOf(ctx, ep); got != show {
		t.Errorf("ShowOf = %d, want the show %d", got, show)
	}
}

/*
 * An ungrouped episode reports itself.
 *
 * The scanner has not parented it yet. Returning it is the honest answer — it
 * is the only thing anybody can play — and beats an error about a show that
 * does not exist.
 */
func TestAnUnparentedEpisodeReportsItself(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	l, _ := s.CreateLibrary(ctx, "Shows", "show", `C:\tv3`)
	res, _ := s.db.Exec(`INSERT INTO media_item (library_id, kind, path, title,
		sort_title, added_at, updated_at, missing)
		VALUES (?, 'episode', ?, 'Loose', 'loose', 1, 1, 0)`, l.ID, `C:\tv3\loose.mkv`)
	ep, _ := res.LastInsertId()

	if got, err := s.ShowOf(ctx, ep); err != nil || got != ep {
		t.Errorf("ShowOf = %d, %v; want the episode itself", got, err)
	}
}

// A missing row is not found rather than a zero that a caller might use.
func TestShowOfAMissingItem(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.ShowOf(context.Background(), 987654); err == nil {
		t.Error("resolving a nonexistent item reported success")
	}
}

/*
 * A parent cycle terminates.
 *
 * No scanner writes one, and a bad row could — and an unbounded walk would hang
 * the request rather than answer it wrongly, which is the worse of the two.
 */
func TestAParentCycleDoesNotHang(t *testing.T) {
	s := openTestStore(t)
	_, show, season, ep := showTree(t, s)
	if _, err := s.db.Exec(`UPDATE media_item SET parent_id = ? WHERE id = ?`, ep, show); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_, _ = s.ShowOf(context.Background(), season)
		close(done)
	}()
	select {
	case <-done:
	case <-timeoutAfterSeconds(5):
		t.Fatal("ShowOf did not return on a cyclic hierarchy")
	}
}

func timeoutAfterSeconds(n int) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		time.Sleep(time.Duration(n) * time.Second)
		close(ch)
	}()
	return ch
}
