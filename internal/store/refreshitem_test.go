package store

import (
	"context"
	"testing"
)

/*
 * Refreshing one title, and what "one title" means for a show.
 *
 * A show's own row carries a title, an overview and a poster, so clearing that
 * alone does something — and leaves every episode exactly as it was, which is
 * not what "refresh this show" means to anybody. Before this, reaching the
 * episodes meant refreshing the whole library: about 1,480 provider lookups on
 * a real film library to correct one series.
 *
 * The exclusions are shared with the library scopes deliberately, so the two
 * cannot drift into disagreeing about what a refresh is allowed to touch.
 */

// enriched marks a row as already matched and stamped, which is the state a
// refresh has to clear. A row that was never enriched is already queued and
// proves nothing.
func enriched(t *testing.T, st *Store, id int64, state string) {
	t.Helper()
	if err := st.UpdateItemMetadata(context.Background(), id, ItemMetadata{
		MatchState: &state,
	}); err != nil {
		t.Fatal(err)
	}
	// The stamp is what a refresh clears, and UpdateItemMetadata does not set
	// it — enrichment does. Written directly so the test starts from the state
	// a refresh is actually about.
	if _, err := st.db.ExecContext(context.Background(),
		`UPDATE media_item SET metadata_updated_at = ? WHERE id = ?`,
		1_700_000_000, id); err != nil {
		t.Fatal(err)
	}
}

func stamped(t *testing.T, st *Store, id int64) bool {
	t.Helper()
	var n int
	if err := st.db.QueryRowContext(context.Background(),
		`SELECT metadata_updated_at IS NOT NULL FROM media_item WHERE id = ?`, id).
		Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func TestRefreshingAShowCarriesItsEpisodes(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	_, show, ep, _ := seedLibrary(t, st)

	if err := st.SetParent(ctx, ep, &show); err != nil {
		t.Fatal(err)
	}
	enriched(t, st, show, "matched")
	enriched(t, st, ep, "matched")

	n, err := st.RefreshItemTree(ctx, show)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("requeued %d, want 2 — the show and its episode", n)
	}
	if stamped(t, st, ep) {
		t.Error("the episode kept its stamp, so the refresh stopped at the show")
	}
}

func TestRefreshingOneTitleLeavesTheRestAlone(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	_, a, b, _ := seedLibrary(t, st)
	enriched(t, st, a, "matched")
	enriched(t, st, b, "matched")

	if _, err := st.RefreshItemTree(ctx, a); err != nil {
		t.Fatal(err)
	}
	if stamped(t, st, a) {
		t.Error("the named title was not requeued")
	}
	if !stamped(t, st, b) {
		t.Error("an unrelated title was requeued; this is the all-or-nothing fault")
	}
}

/*
 * A locked row is a decision somebody made, and reaching it through a narrower
 * door does not make undoing it more acceptable.
 */
func TestRefreshingATitleDoesNotUnlockItsChildren(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	_, show, ep, _ := seedLibrary(t, st)

	if err := st.SetParent(ctx, ep, &show); err != nil {
		t.Fatal(err)
	}
	enriched(t, st, show, "matched")
	enriched(t, st, ep, "locked")

	n, err := st.RefreshItemTree(ctx, show)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("requeued %d, want 1 — the locked episode must not be among them", n)
	}
	if !stamped(t, st, ep) {
		t.Error("a locked episode was requeued")
	}
}
