package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

/*
 * Peers: the other LANcast servers this one has been introduced to.
 *
 * Schema revision 27. The modelling notes are there; what matters here is the
 * one rule these methods exist to keep — **the fingerprint is the identity**
 * (ADR 0044 §5). Every method takes or returns a fingerprint, never an address,
 * and nothing in this file can express "the peer at 10.0.0.1". An address is a
 * hint attached to a peer; a peer that moves is the same peer.
 */

// PeerState is how far a pairing has got.
//
// Deliberately two values. `added` means this side has accepted an invite;
// `paired` means the other side has been confirmed to hold us too. They are
// separate because ADR 0044 §3 makes pairing mutual, and a relationship one
// party can create alone is one that can be created *at* you — so a row this
// server created on its own must not be able to claim it is a pairing.
//
// Only the transport can move a peer to `paired`, because only the transport
// can ask.
const (
	PeerAdded  = "added"
	PeerPaired = "paired"
)

// Peer is another server, as this one knows it.
type Peer struct {
	Fingerprint string   `json:"fingerprint"`
	Name        string   `json:"name"`
	State       string   `json:"state"`
	Addrs       []string `json:"addrs"`
	AddedAt     int64    `json:"added_at"`
	// LastSeen is when this peer last answered, or 0 for never. Never, and
	// "not for three days", are different sentences on a peer list.
	LastSeen int64 `json:"last_seen,omitempty"`
}

// RemotePerson is somebody with an account on a peer, as that peer describes
// them. The id is assigned by the owning server and is meaningful only there;
// its one job here is to be stable, so a grant naming this person survives them
// being renamed.
type RemotePerson struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	UpdatedAt int64  `json:"updated_at"`
}

/*
 * AddPeer records an introduction, or refreshes one already recorded.
 *
 * An upsert rather than an insert, because pasting an invite twice is not an
 * error — it is what somebody does when a peer has moved and sent a new one.
 * The name and addresses are replaced; `added_at`, `state` and `last_seen`
 * survive, because none of them is something a fresh invite knows better.
 * Re-pasting an invite must not silently demote an established pairing back to
 * `added`, which would make the peer look like a stranger again.
 */
func (s *Store) AddPeer(ctx context.Context, p Peer) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("add peer: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO peer (fingerprint, name, state, added_at, last_seen)
		VALUES (?, ?, ?, ?, NULL)
		ON CONFLICT(fingerprint) DO UPDATE SET name = excluded.name`,
		p.Fingerprint, p.Name, PeerAdded, now); err != nil {
		return fmt.Errorf("add peer: %w", err)
	}

	// Addresses are replaced wholesale. A merge would accumulate every address
	// a peer has ever had, and the stale ones are exactly what makes a
	// reachability check slow and its answer ambiguous.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM peer_address WHERE fingerprint = ?`, p.Fingerprint); err != nil {
		return fmt.Errorf("add peer: clear addresses: %w", err)
	}
	for i, a := range p.Addrs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO peer_address (fingerprint, ord, addr) VALUES (?, ?, ?)`,
			p.Fingerprint, i, a); err != nil {
			return fmt.Errorf("add peer: address %d: %w", i, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("add peer: commit: %w", err)
	}
	return nil
}

// Peers lists every peer, with addresses, newest introduction first.
func (s *Store) Peers(ctx context.Context) ([]Peer, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT fingerprint, name, state, added_at, COALESCE(last_seen, 0)
		FROM peer ORDER BY added_at DESC, fingerprint`)
	if err != nil {
		return nil, fmt.Errorf("list peers: %w", err)
	}
	defer rows.Close()

	out := []Peer{}
	for rows.Next() {
		var p Peer
		if err := rows.Scan(&p.Fingerprint, &p.Name, &p.State, &p.AddedAt, &p.LastSeen); err != nil {
			return nil, fmt.Errorf("list peers: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list peers: %w", err)
	}

	// Addresses in one pass rather than a query per peer. A household has a
	// handful of peers, so this is not about speed — it is that a per-row query
	// inside a row loop holds two statements open on one connection, which is
	// the shape that deadlocks under the busy timeout.
	addrs, err := s.peerAddresses(ctx, "")
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Addrs = addrs[out[i].Fingerprint]
	}
	return out, nil
}

// PeerByFingerprint returns one peer, or ErrNotFound.
func (s *Store) PeerByFingerprint(ctx context.Context, fingerprint string) (Peer, error) {
	var p Peer
	err := s.db.QueryRowContext(ctx, `
		SELECT fingerprint, name, state, added_at, COALESCE(last_seen, 0)
		FROM peer WHERE fingerprint = ?`, fingerprint).
		Scan(&p.Fingerprint, &p.Name, &p.State, &p.AddedAt, &p.LastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return Peer{}, ErrNotFound
	}
	if err != nil {
		return Peer{}, fmt.Errorf("get peer: %w", err)
	}
	addrs, err := s.peerAddresses(ctx, fingerprint)
	if err != nil {
		return Peer{}, err
	}
	p.Addrs = addrs[fingerprint]
	return p, nil
}

// peerAddresses reads address lists in fingerprint order. An empty fingerprint
// means every peer.
func (s *Store) peerAddresses(ctx context.Context, fingerprint string) (map[string][]string, error) {
	q := `SELECT fingerprint, addr FROM peer_address`
	args := []any{}
	if fingerprint != "" {
		q += ` WHERE fingerprint = ?`
		args = append(args, fingerprint)
	}
	q += ` ORDER BY fingerprint, ord`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("read peer addresses: %w", err)
	}
	defer rows.Close()

	out := map[string][]string{}
	for rows.Next() {
		var fp, addr string
		if err := rows.Scan(&fp, &addr); err != nil {
			return nil, fmt.Errorf("read peer addresses: %w", err)
		}
		out[fp] = append(out[fp], addr)
	}
	return out, rows.Err()
}

/*
 * RemovePeer un-pairs, and takes everything that hung off the pairing with it.
 *
 * This is the revocation mechanism ADR 0046 relies on: "unpairing revokes
 * everything, immediately, with no per-person cleanup". The cascade in revision
 * 27 is what makes that a property of the schema rather than a promise about
 * this function — addresses and remote people go automatically, and anything
 * added later that references a peer must cascade too.
 */
func (s *Store) RemovePeer(ctx context.Context, fingerprint string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM peer WHERE fingerprint = ?`, fingerprint)
	if err != nil {
		return fmt.Errorf("remove peer: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetPeerState records how far the pairing has got. Only the transport calls
// this, because only the transport can find out.
func (s *Store) SetPeerState(ctx context.Context, fingerprint, state string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE peer SET state = ? WHERE fingerprint = ?`, state, fingerprint)
	if err != nil {
		return fmt.Errorf("set peer state: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkPeerSeen records that a peer answered just now.
func (s *Store) MarkPeerSeen(ctx context.Context, fingerprint string, at time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE peer SET last_seen = ? WHERE fingerprint = ?`, at.Unix(), fingerprint)
	if err != nil {
		return fmt.Errorf("mark peer seen: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

/*
 * ReplaceRemotePeople stores the roster a peer sent.
 *
 * Wholesale in effect, because that is what a roster is: the complete set of
 * accounts on that server willing to be seen, as of now. Somebody who turned
 * `visible_to_peers` off is absent from it and is removed here, which is the
 * one thing a roster refresh has to be able to do — an opt-out that cannot take
 * back what it gave is not an opt-out (ADR 0035).
 *
 * **It is written as upsert-then-delete-the-absent rather than delete-then-
 * reinsert, and the difference is not stylistic.** `presence_grant` cascades
 * from this table (revision 28), so clearing the roster first destroys every
 * grant naming anybody on this peer — including the people the very next
 * statement puts back. A refresh is routine and unattended, so the effect was
 * that consent quietly evaporated on a schedule: the person is still listed,
 * the switch reads off, and nobody did anything. Rows that survive a refresh
 * are now never deleted, so nothing cascades from them.
 *
 * Scoped to one peer. A peer can only ever describe its own people, and this
 * signature is why: there is no call that lets one peer's roster touch
 * another's.
 */
func (s *Store) ReplaceRemotePeople(ctx context.Context, fingerprint string, people []RemotePerson) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("replace remote people: %w", err)
	}
	defer tx.Rollback()

	// The peer must exist. Without this the insert would fail on the foreign
	// key anyway, but with a message about a constraint rather than about an
	// unknown peer.
	var exists int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM peer WHERE fingerprint = ?`, fingerprint).Scan(&exists); err != nil {
		return fmt.Errorf("replace remote people: %w", err)
	}
	if exists == 0 {
		return ErrNotFound
	}

	now := time.Now().Unix()
	keep := make([]any, 0, len(people)+1)
	keep = append(keep, fingerprint)
	for _, p := range people {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO remote_person (fingerprint, id, name, updated_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(fingerprint, id) DO UPDATE SET name = excluded.name, updated_at = excluded.updated_at`,
			fingerprint, p.ID, p.Name, now); err != nil {
			return fmt.Errorf("replace remote people: %w", err)
		}
		keep = append(keep, p.ID)
	}

	// Everybody this peer no longer vouches for. Their grants cascade away with
	// them, which is the point: they withdrew.
	del := `DELETE FROM remote_person WHERE fingerprint = ?`
	if len(people) > 0 {
		del += ` AND id NOT IN (?` + strings.Repeat(`, ?`, len(people)-1) + `)`
	}
	if _, err := tx.ExecContext(ctx, del, keep...); err != nil {
		return fmt.Errorf("replace remote people: prune: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("replace remote people: commit: %w", err)
	}
	return nil
}

// RemotePeople lists the accounts a peer has told us about.
func (s *Store) RemotePeople(ctx context.Context, fingerprint string) ([]RemotePerson, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, updated_at FROM remote_person
		WHERE fingerprint = ? ORDER BY name, id`, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("list remote people: %w", err)
	}
	defer rows.Close()

	out := []RemotePerson{}
	for rows.Next() {
		var p RemotePerson
		if err := rows.Scan(&p.ID, &p.Name, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("list remote people: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

/*
 * SetVisibleToPeers records one account's own decision to appear in the roster
 * this server hands its peers.
 *
 * Its own decision, like share_activity: there is deliberately no admin-facing
 * version, because a switch somebody else can flip is not consent (ADR 0035).
 * Being listed to another server is a disclosure in its own right — it says
 * this person exists here — and it is the precondition for anybody granting
 * them anything, since an account nobody can name cannot be named in a grant.
 */
func (s *Store) SetVisibleToPeers(ctx context.Context, userID string, on bool) error {
	v := 0
	if on {
		v = 1
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE user SET visible_to_peers = ? WHERE id = ?`, v, userID)
	if err != nil {
		return fmt.Errorf("set visible to peers: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// VisibleToPeers reports one account's own setting.
func (s *Store) VisibleToPeers(ctx context.Context, userID string) (bool, error) {
	var v int
	err := s.db.QueryRowContext(ctx,
		`SELECT visible_to_peers FROM user WHERE id = ?`, userID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("get visible to peers: %w", err)
	}
	return v != 0, nil
}

/*
 * RosterForPeers is what this server would tell a peer about the people on it.
 *
 * The filter is in the query rather than applied afterwards, the same shape
 * SharedActivity uses and for the same reason: there is no arrangement of calls
 * that returns somebody who did not opt in. A caller that forgets to check gets
 * a shorter list, which is the correct answer.
 */
func (s *Store) RosterForPeers(ctx context.Context) ([]RemotePerson, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name FROM user
		WHERE visible_to_peers = 1 ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("build roster: %w", err)
	}
	defer rows.Close()

	out := []RemotePerson{}
	for rows.Next() {
		var p RemotePerson
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			return nil, fmt.Errorf("build roster: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
