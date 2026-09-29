package store

import (
	"context"
	"testing"
	"time"
)

/*
 * Where we are in somebody else's films (ADR 0071 §4, schema revision 50).
 *
 * The decision §4 makes is about *where the row lives*, so these assert the
 * properties that follow from it rather than the SQL: it is keyed by a peer and
 * their item id, unpairing forgets it completely, and nothing about it can
 * reach this server's own library.
 */

func peerProgressStore(t *testing.T) (*Store, string) {
	t.Helper()
	s := openTestStore(t)
	ctx := context.Background()
	fp := "AAAABBBBCCCCDDDD"
	if err := s.AddPeer(ctx, Peer{
		Fingerprint: fp, Name: "Utopia", State: PeerPaired,
		Addrs: []string{"127.0.0.1:1"},
	}); err != nil {
		t.Fatal(err)
	}
	return s, fp
}

func TestAPeersPositionIsKeptAndReturned(t *testing.T) {
	s, fp := peerProgressStore(t)
	ctx := context.Background()

	if got, err := s.PeerProgressFor(ctx, fp, 42); err != nil || got != 0 {
		t.Fatalf("unstarted film = %d, %v; want 0 and no error", got, err)
	}

	if err := s.SetPeerProgress(ctx, fp, 42, 120_000, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := s.PeerProgressFor(ctx, fp, 42)
	if err != nil || got != 120_000 {
		t.Fatalf("got %d, %v; want 120000", got, err)
	}

	// Last write wins.
	if err := s.SetPeerProgress(ctx, fp, 42, 600_000, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.PeerProgressFor(ctx, fp, 42); got != 600_000 {
		t.Errorf("after a second write got %d, want 600000", got)
	}
}

/*
 * The id is theirs, and two peers may use the same number for different films.
 *
 * This is the whole reason the key is a pair. A position stored against the
 * item id alone would have one household's film resume at another's position —
 * silently, and plausibly, because both numbers are real.
 */
func TestTwoPeersMayUseTheSameItemId(t *testing.T) {
	s, first := peerProgressStore(t)
	ctx := context.Background()
	second := "ZZZZYYYYXXXXWWWW"
	if err := s.AddPeer(ctx, Peer{
		Fingerprint: second, Name: "Elsewhere", State: PeerPaired,
		Addrs: []string{"127.0.0.1:1"},
	}); err != nil {
		t.Fatal(err)
	}

	if err := s.SetPeerProgress(ctx, first, 7, 111_000, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPeerProgress(ctx, second, 7, 222_000, time.Now()); err != nil {
		t.Fatal(err)
	}

	if got, _ := s.PeerProgressFor(ctx, first, 7); got != 111_000 {
		t.Errorf("first peer's item 7 = %d, want 111000", got)
	}
	if got, _ := s.PeerProgressFor(ctx, second, 7); got != 222_000 {
		t.Errorf("second peer's item 7 = %d, want 222000", got)
	}
}

/*
 * Unpairing forgets where we were, with nothing per-item to clean up.
 *
 * ADR 0071 §1: unpairing takes it all back at once. The cascade is what makes
 * that true of positions as well as shares — and a row that survived would be a
 * record of somebody's viewing outliving the relationship that justified it.
 */
func TestUnpairingForgetsWhereWeWere(t *testing.T) {
	s, fp := peerProgressStore(t)
	ctx := context.Background()

	if err := s.SetPeerProgress(ctx, fp, 42, 600_000, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.RemovePeer(ctx, fp); err != nil {
		t.Fatal(err)
	}

	if got, err := s.PeerProgressFor(ctx, fp, 42); err != nil || got != 0 {
		t.Errorf("after unpairing got %d, %v; want it forgotten", got, err)
	}
}

/*
 * Back at the start is the same fact as never started.
 *
 * Storing a zero would put a row in a table whose only question is *where were
 * we* — and the row would then have to be recognised and ignored by everything
 * that reads it, which is a rule rather than a shape.
 */
func TestReturningToTheStartForgetsTheFilm(t *testing.T) {
	s, fp := peerProgressStore(t)
	ctx := context.Background()

	if err := s.SetPeerProgress(ctx, fp, 42, 600_000, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPeerProgress(ctx, fp, 42, 0, time.Now()); err != nil {
		t.Fatal(err)
	}

	var rows int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM peer_progress WHERE fingerprint = ? AND item_id = ?`,
		fp, 42).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Errorf("%d row(s) left saying nought", rows)
	}
}

/*
 * A peer's film never reaches this server's own lists (ADR 0071 §5).
 *
 * Asserted at the boundary rather than by reading the queries: the table has no
 * relationship to media_item, so a position cannot appear in Continue Watching,
 * Recently Added, a count or a search however those are later written.
 */
func TestAPeersPositionCannotReachOurOwnLibrary(t *testing.T) {
	s, fp := peerProgressStore(t)
	ctx := context.Background()
	if err := s.SetPeerProgress(ctx, fp, 42, 600_000, time.Now()); err != nil {
		t.Fatal(err)
	}

	var refs int
	if err := s.db.QueryRow(`
SELECT COUNT(*) FROM pragma_foreign_key_list('peer_progress')
WHERE "table" = 'media_item'`).Scan(&refs); err != nil {
		t.Fatal(err)
	}
	if refs != 0 {
		t.Errorf("peer_progress references media_item %d time(s); a peer's film "+
			"must never be reachable from this server's own library", refs)
	}
}
