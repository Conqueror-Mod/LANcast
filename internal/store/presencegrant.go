package store

import (
	"context"
	"fmt"
	"time"
)

/*
 * Presence grants: who each local account has agreed may see them watching.
 *
 * Schema revision 28. The modelling notes are there; what these methods exist
 * to keep is [ADR 0045](../../docs/adr/0045-live-presence-between-paired-servers.md)
 * §2 and §6, and both are about whose decision this is.
 *
 * **Every method takes the granting account's own id**, and there is
 * deliberately no variant that does not. An administrator has no privileged
 * position here (§6) — a switch somebody else can flip is not consent — so the
 * absence of an admin-facing call is the enforcement, not a check inside one.
 * There is nothing to call.
 *
 * The reverse lookup, ReadersOf, is the only query that runs on behalf of
 * somebody else, and it answers the narrowest possible question: given a remote
 * person asking, which local accounts have named *them*. It cannot be used to
 * enumerate anybody's grants.
 */

// PresenceGrant is one account's agreement that one remote person may see them
// watching.
type PresenceGrant struct {
	Fingerprint string `json:"fingerprint"`
	PersonID    string `json:"person_id"`
	GrantedAt   int64  `json:"granted_at"`
}

/*
 * GrantPresence records that userID agrees personID on peer fingerprint may see
 * them watching.
 *
 * The foreign key does the work that matters: the remote person must already
 * exist, which means the peer is paired and that person opted into its roster.
 * ADR 0045 §2 — an account that has not opted in cannot be named by anybody's
 * grant, in either direction — is therefore enforced by the schema rather than
 * by remembering to check.
 *
 * Re-granting is not an error and does not move granted_at. Somebody clicking a
 * switch that is already on has changed nothing, and rewriting the date would
 * misreport when they decided.
 */
func (s *Store) GrantPresence(ctx context.Context, userID, fingerprint, personID string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO presence_grant (user_id, fingerprint, person_id, granted_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id, fingerprint, person_id) DO NOTHING`,
		userID, fingerprint, personID, at.Unix())
	if err != nil {
		return fmt.Errorf("grant presence: %w", err)
	}
	return nil
}

/*
 * RevokePresence removes a grant.
 *
 * Deleting a row that is not there is success, not a failure: the caller asked
 * for this person not to be able to see them, and afterwards they cannot. ADR
 * 0045 §5 wants revocation to be immediate, and immediacy is a property of the
 * read path — nothing caches this, so the next presence request answers with
 * the row already gone, mid-film.
 */
func (s *Store) RevokePresence(ctx context.Context, userID, fingerprint, personID string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM presence_grant
		WHERE user_id = ? AND fingerprint = ? AND person_id = ?`,
		userID, fingerprint, personID)
	if err != nil {
		return fmt.Errorf("revoke presence: %w", err)
	}
	return nil
}

// PresenceGrants lists what one account has agreed to, so it can be shown back
// to them. Sorted, because a list of consents that reorders itself between
// views is one nobody can audit.
func (s *Store) PresenceGrants(ctx context.Context, userID string) ([]PresenceGrant, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT fingerprint, person_id, granted_at
		FROM presence_grant
		WHERE user_id = ?
		ORDER BY fingerprint, person_id`, userID)
	if err != nil {
		return nil, fmt.Errorf("presence grants: %w", err)
	}
	defer rows.Close()

	out := []PresenceGrant{}
	for rows.Next() {
		var g PresenceGrant
		if err := rows.Scan(&g.Fingerprint, &g.PersonID, &g.GrantedAt); err != nil {
			return nil, fmt.Errorf("presence grants: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

/*
 * ReadersOf answers the question the federation endpoint asks: given this
 * remote person, which local accounts have agreed that they may see them.
 *
 * This is the only place a grant is read on somebody else's behalf, and it is
 * shaped so that it cannot answer anything wider. It returns local account ids
 * and nothing else — no names, no titles, no grant dates — because the handler
 * that calls it is about to disclose only what ADR 0045 §3 permits, and a query
 * that returned more would be an invitation to send more.
 */
func (s *Store) ReadersOf(ctx context.Context, fingerprint, personID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT user_id FROM presence_grant
		WHERE fingerprint = ? AND person_id = ?
		ORDER BY user_id`, fingerprint, personID)
	if err != nil {
		return nil, fmt.Errorf("presence readers: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("presence readers: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
