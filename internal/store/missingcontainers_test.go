package store

import (
	"context"
	"path/filepath"
	"testing"
)

/*
 * Containers follow their children offline, and back.
 *
 * From a real library: a show reorganised on disk left a season row whose eight
 * episodes were all marked missing and whose folder no longer existed. Nothing
 * ever marked a container missing — the flag was only ever set on files — so the
 * show page showed two "Season 1" tiles, one of them describing a directory that
 * had been deleted. It was reported as a duplicated season, and the duplicate
 * was real: it was the empty one that should not have been there.
 */

// seedSeason builds show → season → two episodes and returns their ids.
func seedSeason(t *testing.T, st *Store) (lib, show, season, ep1, ep2 int64) {
	t.Helper()
	ctx := context.Background()
	l, err := st.CreateLibrary(ctx, "Shows", "show", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mkContainer := func(kind, title, path string, parent *int64) int64 {
		id, err := st.UpsertItem(ctx, ScanFile{
			LibraryID: l.ID, Path: path, Kind: kind, Title: title,
			SortTitle: title, MTime: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		// A container has no file of its own, which is what identifies it here.
		if _, err := st.db.ExecContext(ctx,
			`UPDATE media_item SET container = NULL WHERE id = ?`, id); err != nil {
			t.Fatal(err)
		}
		if parent != nil {
			if err := st.SetParent(ctx, id, parent); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}

	showID := mkContainer("show", "A Show", "lancast:show:a show", nil)
	seasonID := mkContainer("season", "Season 1", "lancast:show:a show::season=1", &showID)

	mkEpisode := func(name string) int64 {
		id, err := st.UpsertItem(ctx, ScanFile{
			LibraryID: l.ID, Path: filepath.Join(l.Path, name), Kind: "episode",
			Title: name, SortTitle: name, Container: "mkv", SizeBytes: 1, MTime: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetParent(ctx, id, &seasonID); err != nil {
			t.Fatal(err)
		}
		return id
	}
	return l.ID, showID, seasonID, mkEpisode("e1.mkv"), mkEpisode("e2.mkv")
}

func missingOf(t *testing.T, st *Store, id int64) bool {
	t.Helper()
	var m int
	if err := st.db.QueryRowContext(context.Background(),
		`SELECT missing FROM media_item WHERE id = ?`, id).Scan(&m); err != nil {
		t.Fatal(err)
	}
	return m == 1
}

func TestAFullSeasonStaysPresent(t *testing.T) {
	st := openTestStore(t)
	lib, show, season, _, _ := seedSeason(t, st)

	marked, restored, err := st.ReconcileMissingContainers(context.Background(), lib)
	if err != nil {
		t.Fatal(err)
	}
	if marked != 0 || restored != 0 {
		t.Errorf("marked %d, restored %d on an untouched library; want none", marked, restored)
	}
	if missingOf(t, st, season) || missingOf(t, st, show) {
		t.Error("a season with live episodes was marked missing")
	}
}

// One live episode is enough. A season half offline is still a season.
func TestASeasonWithOneLiveEpisodeStaysPresent(t *testing.T) {
	st := openTestStore(t)
	lib, show, season, ep1, _ := seedSeason(t, st)

	if err := st.MarkMissing(context.Background(), []int64{ep1}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.ReconcileMissingContainers(context.Background(), lib); err != nil {
		t.Fatal(err)
	}
	if missingOf(t, st, season) || missingOf(t, st, show) {
		t.Error("a season with one live episode was marked missing")
	}
}

/*
 * The reported case, and the cascade with it: every episode gone takes the
 * season, and the season taking the show. Two levels in one sweep is why the
 * pass iterates.
 */
func TestAnEmptiedSeasonAndItsShowGoMissing(t *testing.T) {
	st := openTestStore(t)
	lib, show, season, ep1, ep2 := seedSeason(t, st)

	if err := st.MarkMissing(context.Background(), []int64{ep1, ep2}); err != nil {
		t.Fatal(err)
	}
	marked, _, err := st.ReconcileMissingContainers(context.Background(), lib)
	if err != nil {
		t.Fatal(err)
	}
	if marked != 2 {
		t.Errorf("marked %d containers, want the season and the show", marked)
	}
	if !missingOf(t, st, season) {
		t.Error("the emptied season is still present")
	}
	if !missingOf(t, st, show) {
		t.Error("the show above an emptied season is still present")
	}
}

/*
 * Reversible, which is the whole reason this marks rather than deletes. An
 * unmounted drive must cost nothing: remount, rescan, and the season is a season
 * again with no rebuilding.
 */
func TestAReturnedEpisodeBringsItsSeasonBack(t *testing.T) {
	st := openTestStore(t)
	lib, show, season, ep1, ep2 := seedSeason(t, st)
	ctx := context.Background()

	if err := st.MarkMissing(ctx, []int64{ep1, ep2}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.ReconcileMissingContainers(ctx, lib); err != nil {
		t.Fatal(err)
	}

	// The drive comes back: one file is seen again.
	if _, err := st.db.ExecContext(ctx,
		`UPDATE media_item SET missing = 0 WHERE id = ?`, ep1); err != nil {
		t.Fatal(err)
	}
	_, restored, err := st.ReconcileMissingContainers(ctx, lib)
	if err != nil {
		t.Fatal(err)
	}
	if restored != 2 {
		t.Errorf("restored %d containers, want the season and the show", restored)
	}
	if missingOf(t, st, season) || missingOf(t, st, show) {
		t.Error("a container whose child came back is still marked missing")
	}
}

/*
 * A container with no children at all is left entirely alone.
 *
 * That is PruneEmptyContainers' job, and it deletes rather than marks. More to
 * the point, collections and playlists group through their own tables and have
 * no parent_id children — so the rule here must not touch them, and it does not,
 * by construction rather than by an exclusion list somebody has to maintain.
 */
func TestAChildlessContainerIsUntouched(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	l, err := st.CreateLibrary(ctx, "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.UpsertItem(ctx, ScanFile{
		LibraryID: l.ID, Path: "lancast:collection:franchise", Kind: "collection",
		Title: "A Franchise", SortTitle: "a franchise", MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx,
		`UPDATE media_item SET container = NULL WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}

	marked, _, err := st.ReconcileMissingContainers(ctx, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if marked != 0 {
		t.Errorf("marked %d childless containers; want none", marked)
	}
	if missingOf(t, st, id) {
		t.Error("a collection with no parent_id children was marked missing")
	}
}

// A file is never touched by this pass, whatever state it is in: it owns its own
// missing flag, set by the walk that did or did not find it.
func TestFilesAreNotTouched(t *testing.T) {
	st := openTestStore(t)
	lib, _, _, ep1, ep2 := seedSeason(t, st)
	ctx := context.Background()

	if err := st.MarkMissing(ctx, []int64{ep1}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.ReconcileMissingContainers(ctx, lib); err != nil {
		t.Fatal(err)
	}
	if !missingOf(t, st, ep1) {
		t.Error("a missing episode was restored by the container sweep")
	}
	if missingOf(t, st, ep2) {
		t.Error("a live episode was marked missing by the container sweep")
	}
}
