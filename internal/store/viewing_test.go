package store

import (
	"context"
	"path/filepath"
	"testing"
)

/*
 * The watch history (ADR 0074, revision 53).
 *
 * playback_state keeps one row per item, so "when did I watch this" had an
 * answer only for the last viewing. The log keeps one row per finished viewing
 * of a film or an episode.
 */

func viewingFixture(t *testing.T) (*Store, string, int64) {
	t.Helper()
	st := openTestStore(t)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "", "chris", "hash", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	lib, err := st.CreateLibrary(ctx, "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	year := 1995
	film, err := st.UpsertItem(ctx, ScanFile{
		LibraryID: lib.ID, Path: filepath.Join(lib.Path, "Heat.mkv"), Kind: "movie",
		Title: "Heat", SortTitle: "heat", Year: &year, Container: "mkv", SizeBytes: 1, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return st, u.ID, film
}

func viewingTitles(t *testing.T, st *Store, user string) []string {
	t.Helper()
	vs, _, err := st.Viewings(context.Background(), user, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, v := range vs {
		out = append(out, v.Title)
	}
	return out
}

// Heartbeats arrive every five seconds with watched=true; a viewing is the
// edge into finished, and a rewatch is a second edge.
func TestFinishingAFilmLogsOneViewingAndARewatchLogsAnother(t *testing.T) {
	ctx := context.Background()
	st, me, film := viewingFixture(t)

	for _, p := range []struct {
		pos     int64
		watched bool
	}{{60_000, false}, {7_000_000, true}, {7_005_000, true}, {7_010_000, true}} {
		if err := st.SaveProgress(ctx, film, me, p.pos, p.watched); err != nil {
			t.Fatal(err)
		}
	}
	if got := viewingTitles(t, st, me); len(got) != 1 {
		t.Fatalf("one finished viewing with heartbeats logged %v, want one row", got)
	}

	// Started again from the top, then finished.
	if err := st.SaveProgress(ctx, film, me, 30_000, false); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProgress(ctx, film, me, 7_000_000, true); err != nil {
		t.Fatal(err)
	}
	vs, total, err := st.Viewings(ctx, me, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(vs) != 2 {
		t.Fatalf("a rewatch left %d rows, want 2", total)
	}
	if vs[0].Estimated || vs[0].Year == nil || *vs[0].Year != 1995 || vs[0].Kind != "movie" {
		t.Errorf("row = %+v, want a dated, non-estimated movie row carrying its year", vs[0])
	}
}

// Marking something watched that was never played is a viewing nobody
// recorded at the time, the same rule watch_count follows.
func TestMarkingWatchedLogsAViewing(t *testing.T) {
	ctx := context.Background()
	st, me, film := viewingFixture(t)
	if err := st.SaveProgress(ctx, film, me, 0, true); err != nil {
		t.Fatal(err)
	}
	if got := viewingTitles(t, st, me); len(got) != 1 {
		t.Errorf("marking watched logged %v, want one row", got)
	}
}

// Films and episodes only (ADR 0074): music would dominate the log.
func TestAnEpisodeIsLoggedWithItsShowAndATrackIsNot(t *testing.T) {
	ctx := context.Background()
	st, me, _ := viewingFixture(t)
	tv, err := st.CreateLibrary(ctx, "TV", "show", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	show, _, err := st.EnsureShow(ctx, tv.ID, filepath.Join(tv.Path, "Futurama"), "Futurama", "futurama")
	if err != nil {
		t.Fatal(err)
	}
	showYear, showIMDb := 1999, "tt0149460"
	if _, err := st.db.ExecContext(ctx, `UPDATE media_item SET year = ?, imdb_id = ? WHERE id = ?`,
		showYear, showIMDb, show); err != nil {
		t.Fatal(err)
	}
	series, s, e := "Futurama", 1, 1
	ep, err := st.UpsertItem(ctx, ScanFile{
		LibraryID: tv.ID, Path: filepath.Join(tv.Path, "Futurama", "s01e01.mkv"), Kind: "episode",
		Title: "Space Pilot 3000", SortTitle: "futurama", Series: &series, Season: &s, Episode: &e,
		Container: "mkv", SizeBytes: 1, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetParent(ctx, ep, &show); err != nil {
		t.Fatal(err)
	}
	_, _, _, track := seedBand(t, st)

	if err := st.SaveProgress(ctx, ep, me, 0, true); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProgress(ctx, track, me, 0, true); err != nil {
		t.Fatal(err)
	}
	vs, _, err := st.Viewings(ctx, me, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 {
		t.Fatalf("logged %d rows, want only the episode", len(vs))
	}
	v := vs[0]
	if v.Kind != "episode" || v.Series == nil || *v.Series != "Futurama" ||
		v.Season == nil || *v.Season != 1 || v.Episode == nil || *v.Episode != 1 {
		t.Errorf("episode row = %+v", v)
	}
	if v.ShowYear == nil || *v.ShowYear != showYear || v.ShowIMDbID == nil || *v.ShowIMDbID != showIMDb {
		t.Errorf("the row lost its show's year or imdb id: %+v", v)
	}
}

// A history that forgot the film the moment its file was deleted would fail
// the question it exists to answer.
func TestAViewingOutlivesItsItem(t *testing.T) {
	ctx := context.Background()
	st, me, film := viewingFixture(t)
	if err := st.SaveProgress(ctx, film, me, 0, true); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteItems(ctx, []int64{film}); err != nil {
		t.Fatal(err)
	}
	vs, _, err := st.Viewings(ctx, me, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 || vs[0].ItemID != nil || vs[0].Title != "Heat" {
		t.Errorf("after deleting the item, history = %+v, want Heat with no item id", vs)
	}
}

func TestResettingFinishedHistoryClearsTheLogAndUnfinishedDoesNot(t *testing.T) {
	ctx := context.Background()
	st, me, film := viewingFixture(t)
	if err := st.SaveProgress(ctx, film, me, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ResetHistory(ctx, me, HistoryUnfinished, 0); err != nil {
		t.Fatal(err)
	}
	if got := viewingTitles(t, st, me); len(got) != 1 {
		t.Errorf("forgetting unfinished positions removed the log: %v", got)
	}
	if _, err := st.ResetHistory(ctx, me, HistoryFinished, 0); err != nil {
		t.Fatal(err)
	}
	if got := viewingTitles(t, st, me); len(got) != 0 {
		t.Errorf("forgetting what was finished left the log: %v", got)
	}
}

// Somebody else's history is not mine, and a reset reaches only its caller.
func TestTheLogIsPerAccount(t *testing.T) {
	ctx := context.Background()
	st, me, film := viewingFixture(t)
	other, err := st.CreateUser(ctx, "", "other", "hash", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProgress(ctx, film, me, 0, true); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProgress(ctx, film, other.ID, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ResetHistory(ctx, other.ID, HistoryAll, 0); err != nil {
		t.Fatal(err)
	}
	if got := viewingTitles(t, st, me); len(got) != 1 {
		t.Errorf("another account's reset reached my history: %v", got)
	}
	if got := viewingTitles(t, st, other.ID); len(got) != 0 {
		t.Errorf("the reset left the other account's history: %v", got)
	}
}

// Revision 53 seeds one estimated row per finished film or episode, and no
// more: earlier rewatches were never recorded and are not invented.
func TestRevision53SeedsOneEstimatedRowPerFinishedVideo(t *testing.T) {
	ctx := context.Background()
	st, me, film := viewingFixture(t)
	if err := st.SaveProgress(ctx, film, me, 0, true); err != nil {
		t.Fatal(err)
	}
	// A second viewing in the tally, which the old table cannot date.
	if _, err := st.db.ExecContext(ctx,
		`UPDATE playback_state SET watch_count = 2, updated_at = 1600000000 WHERE item_id = ?`, film); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`DROP TABLE viewing`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE meta SET value = '52' WHERE key = 'schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(st.db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	vs, _, err := st.Viewings(ctx, me, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 || !vs[0].Estimated || vs[0].FinishedAt != 1_600_000_000 {
		t.Errorf("seeded = %+v, want one estimated row dated from the state row", vs)
	}
}
