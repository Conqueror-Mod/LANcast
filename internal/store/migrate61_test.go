package store

import (
	"context"
	"path/filepath"
	"testing"
)

/*
 * A season added after its show was matched gets its own art.
 *
 * EnsureSeason stamped every new season resolved, so nothing ever looked one up
 * and it wore its show's poster: Futurama seasons 6 to 8, scanned in later than
 * 1 to 5, all showed the same image.
 */

func seasonsFixture(t *testing.T, s *Store, n int) []int64 {
	t.Helper()
	ctx := context.Background()
	lib, err := s.CreateLibrary(ctx, "TV", "show", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	show, _, err := s.EnsureShow(ctx, lib.ID, filepath.Join(lib.Path, "Show"), "Show", "show")
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for sn := 1; sn <= n; sn++ {
		id, _, err := s.EnsureSeason(ctx, lib.ID, show, sn,
			filepath.Join(lib.Path, "Show", "S"+string(rune('0'+sn))), "Season", "season")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func pendingIDs(t *testing.T, s *Store) map[int64]bool {
	t.Helper()
	items, err := s.PendingEnrichment(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	out := map[int64]bool{}
	for _, it := range items {
		out[it.ID] = true
	}
	return out
}

func TestANewSeasonWaitsToBeResolvedFromItsShow(t *testing.T) {
	s := openTestStore(t)
	season := seasonsFixture(t, s, 1)[0]
	if !pendingIDs(t, s)[season] {
		t.Error("a new season was stamped resolved at birth, so nothing will ever fetch its own poster")
	}
}

// Revision 61 queues the seasons nothing resolved, and leaves alone one that
// has its provider identity and one a person has locked.
func TestRevision61QueuesOnlyUnresolvedSeasons(t *testing.T) {
	s := openTestStore(t)
	ids := seasonsFixture(t, s, 3)
	stranded, resolved, locked := ids[0], ids[1], ids[2]

	// The shape every season made before this change is in: stamped matched.
	steps := []struct {
		q    string
		args []any
	}{
		{`UPDATE media_item SET match_state = 'matched', match_score = 1, metadata_updated_at = 1 WHERE kind = 'season'`, nil},
		{`UPDATE media_item SET provider = 'tmdb', external_id = '615' WHERE id = ?`, []any{resolved}},
		{`INSERT INTO item_lock (item_id, field) VALUES (?, 'title')`, []any{locked}},
		{`UPDATE meta SET value = '60' WHERE key = 'schema_version'`, nil},
	}
	for _, st := range steps {
		if _, err := s.db.Exec(st.q, st.args...); err != nil {
			t.Fatal(err)
		}
	}
	// The show is pending too — nothing has enriched it — so only the seasons
	// are asked about.
	before := pendingIDs(t, s)
	for _, id := range ids {
		if before[id] {
			t.Fatal("fixture: a season was already pending before the migration")
		}
	}

	if err := migrate(s.db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	got := pendingIDs(t, s)
	if !got[stranded] {
		t.Error("the season with no provider identity was not queued")
	}
	if got[resolved] {
		t.Error("a season already resolved from its show was queued again")
	}
	if got[locked] {
		t.Error("a season a person has edited was queued; a cleanup does not re-litigate decisions")
	}
}
