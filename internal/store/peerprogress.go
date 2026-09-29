package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

/*
 * Where we are in somebody else's films.
 *
 * Schema revision 50, and
 * [ADR 0071](../../docs/adr/0071-a-shared-library-is-a-standing-grant.md) §4 —
 * the one place that ADR departs from ADR 0046's "a guest writes nothing".
 *
 * **This is the watcher's server, not the host's.** §4 argues the host is the
 * wrong place: a row there keyed to a remote principal is an account by another
 * name — it outlives the evening, it has to be listed and deleted, and
 * unpairing would no longer be complete. So the position is kept by whoever is
 * watching, keyed by the peer's fingerprint and *their* item id.
 *
 * Two consequences worth stating, because both are properties somebody could
 * remove without noticing:
 *
 * **A friend's viewing is private to their own household.** The host writes
 * nothing and knows nothing about where anybody is in a film — which is the
 * answer [ADR 0035](../../docs/adr/0035-who-may-see-whose-viewing.md) would
 * give if asked, arrived at by where the row lives rather than by a rule
 * somebody has to enforce.
 *
 * **Nothing here joins `media_item`.** A peer's film must never appear in
 * Continue Watching, Recently Added, a count or a search (§5). A separate table
 * is a stronger version of that than a filter every query has to remember, and
 * it is why these methods return positions rather than items.
 */

// PeerProgress is where one account got to in one of a peer's items.
//
// Deliberately not an Item: this server has no row for a peer's film and must
// not grow one. What it holds is a number and when it was written.
type PeerProgress struct {
	ItemID     int64 `json:"item_id"`
	PositionMS int64 `json:"position_ms"`
	UpdatedAt  int64 `json:"updated_at"`
}

/*
 * SetPeerProgress records where we are in one of their films.
 *
 * Last write wins, per (peer, item). There is no per-account key and that is
 * the shape §4 asks for: the row is about this *server's* relationship with
 * that peer, and a household that shares a server already shares a position in
 * its own library.
 *
 * A position of zero deletes rather than storing a zero. "Back at the start" is
 * the same fact as "never started", and keeping a row saying nought would put
 * an entry in a table whose whole purpose is to answer *where were we*.
 */
func (s *Store) SetPeerProgress(ctx context.Context, fingerprint string, itemID, positionMS int64, now time.Time) error {
	if fingerprint == "" || itemID <= 0 {
		return errors.New("store: peer progress needs a peer and an item")
	}
	if positionMS <= 0 {
		return s.ClearPeerProgress(ctx, fingerprint, itemID)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO peer_progress (fingerprint, item_id, position_ms, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(fingerprint, item_id) DO UPDATE SET
    position_ms = excluded.position_ms,
    updated_at  = excluded.updated_at`,
		fingerprint, itemID, positionMS, now.Unix())
	if err != nil {
		return fmt.Errorf("set peer progress: %w", err)
	}
	return nil
}

// ClearPeerProgress forgets one position. Finishing a film and dragging back to
// the start are the same instruction here: there is nowhere to resume to.
func (s *Store) ClearPeerProgress(ctx context.Context, fingerprint string, itemID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM peer_progress WHERE fingerprint = ? AND item_id = ?`,
		fingerprint, itemID)
	if err != nil {
		return fmt.Errorf("clear peer progress: %w", err)
	}
	return nil
}

/*
 * PeerProgressFor answers where we got to, or zero.
 *
 * No row is not an error: it is the answer for a film nobody has started, which
 * is most of them. A caller that treated absence as a failure would make
 * starting a film look like something going wrong.
 */
func (s *Store) PeerProgressFor(ctx context.Context, fingerprint string, itemID int64) (int64, error) {
	var pos int64
	err := s.db.QueryRowContext(ctx,
		`SELECT position_ms FROM peer_progress WHERE fingerprint = ? AND item_id = ?`,
		fingerprint, itemID).Scan(&pos)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("peer progress: %w", err)
	}
	return pos, nil
}
