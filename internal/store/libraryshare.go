package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"lancast/internal/rating"
)

/*
 * Library shares: which of this server's libraries each paired server may see.
 *
 * Schema revision 49. The modelling notes are there; what these methods exist
 * to keep is [ADR 0071](../../docs/adr/0071-a-shared-library-is-a-standing-grant.md)
 * §1 and §6.
 *
 * **The absence of a row is the answer.** Nothing here has a "shared: false"
 * state to get wrong — un-sharing deletes, unpairing cascades, and a peer this
 * server has never heard of resolves to the empty set by the same path as one
 * that was shared nothing. That is deliberate: the failure mode of a boolean
 * column is a row that says false and a code path that forgets to read it.
 *
 * **CeilingFor is the half that must not fail open**, and it is separate from
 * SharedLibraries for that reason. See its own comment: a friend whose ceiling
 * cannot be resolved is refused, which is the opposite of what `MayPlay` does
 * for an account and is the whole point of ADR 0071 §6.
 */

// ErrNotShared reports that a peer has no grant on a library.
//
// Its own error because callers must be able to tell "shared with no ceiling"
// from "not shared at all" — the two differ by everything, and an empty string
// cannot say which it is.
var ErrNotShared = errors.New("library is not shared with this peer")

// LibraryShare is one standing grant from this server to one paired server.
type LibraryShare struct {
	Fingerprint string `json:"fingerprint"`
	LibraryID   int64  `json:"library_id"`
	// Ceiling is the age limit for this share, empty for none. A kind that
	// carries no certificate ignores it entirely (ADR 0071 §6).
	Ceiling  string `json:"ceiling"`
	SharedAt int64  `json:"shared_at"`
}

/*
 * ShareLibrary grants a peer standing access to one library.
 *
 * The foreign keys do the work that matters: the peer must be known and the
 * library must exist, so a grant cannot outlive either, and unpairing revokes
 * without anything here being called.
 *
 * Re-sharing a library already shared updates the ceiling and leaves shared_at
 * alone. Somebody adjusting a limit has not re-made the decision to share, and
 * moving the date would misreport when they did.
 *
 * An unknown ceiling is refused rather than stored. `rating.Known` is the only
 * arbiter of what a rung is, and a value it does not recognise would resolve to
 * "no ceiling" at read time — a limit that silently is not one, which is the
 * shape of fault ADR 0071 §6 exists to prevent.
 */
func (s *Store) ShareLibrary(ctx context.Context, fingerprint string, libraryID int64, ceiling string, at time.Time) error {
	if ceiling != "" && !rating.Known(ceiling) {
		return fmt.Errorf("share library: %q is not a rating this server knows", ceiling)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO library_share (fingerprint, library_id, ceiling, shared_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(fingerprint, library_id)
		DO UPDATE SET ceiling = excluded.ceiling`,
		fingerprint, libraryID, ceiling, at.Unix())
	if err != nil {
		return fmt.Errorf("share library %d with %s: %w", libraryID, fingerprint, err)
	}
	return nil
}

// UnshareLibrary takes a grant away. Removing one that is not there is not an
// error: the caller asked for a state and that state now holds.
func (s *Store) UnshareLibrary(ctx context.Context, fingerprint string, libraryID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM library_share WHERE fingerprint = ? AND library_id = ?`,
		fingerprint, libraryID)
	if err != nil {
		return fmt.Errorf("unshare library %d from %s: %w", libraryID, fingerprint, err)
	}
	return nil
}

// SharesTo lists what one peer has been granted, for the host's own screen.
func (s *Store) SharesTo(ctx context.Context, fingerprint string) ([]LibraryShare, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT fingerprint, library_id, ceiling, shared_at
		FROM library_share WHERE fingerprint = ?
		ORDER BY library_id`, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("list shares to %s: %w", fingerprint, err)
	}
	defer rows.Close()

	var out []LibraryShare
	for rows.Next() {
		var sh LibraryShare
		if err := rows.Scan(&sh.Fingerprint, &sh.LibraryID, &sh.Ceiling, &sh.SharedAt); err != nil {
			return nil, fmt.Errorf("scan share: %w", err)
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

/*
 * SharedLibraries is the set a friend session carries.
 *
 * Ids only, and ordered, because this is what every scoped listing filters on
 * and nothing downstream needs the ceiling or the date. An empty result is the
 * ordinary answer for a peer that has been granted nothing, and callers must
 * treat it as "sees nothing" rather than "sees everything" — which is why this
 * returns a slice rather than an optional filter.
 */
func (s *Store) SharedLibraries(ctx context.Context, fingerprint string) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT library_id FROM library_share WHERE fingerprint = ? ORDER BY library_id`,
		fingerprint)
	if err != nil {
		return nil, fmt.Errorf("shared libraries for %s: %w", fingerprint, err)
	}
	defer rows.Close()

	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan shared library: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

/*
 * CeilingFor resolves the ceiling that applies to one peer on one library, and
 * **fails closed**.
 *
 * This is the half of ADR 0071 §6 that matters. `Store.MayPlay` resolves an
 * account's ceiling and returns *permitted* when there is no account row, for a
 * reason that is right for every caller it has: an unsecured loopback server
 * has no accounts at all, and refusing the unknown there empties the library
 * for the one configuration meant to work out of the box.
 *
 * A friend has no account row **by design**, so that default would admit them
 * past every ceiling a host set — silently, and looking like it worked. So this
 * is a different function with the opposite default rather than a parameter on
 * that one: a grant that cannot be found is ErrNotShared, and a caller that
 * ignores the error gets the zero value of a string it must then treat as
 * unusable.
 *
 * Not shared and shared-without-a-ceiling are different answers and both are
 * returned faithfully. Collapsing them would make "no limit" indistinguishable
 * from "no access", and the safe reading of that ambiguity is the one that
 * breaks sharing entirely.
 */
func (s *Store) CeilingFor(ctx context.Context, fingerprint string, libraryID int64) (string, error) {
	var ceiling string
	err := s.db.QueryRowContext(ctx,
		`SELECT ceiling FROM library_share WHERE fingerprint = ? AND library_id = ?`,
		fingerprint, libraryID).Scan(&ceiling)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotShared
	}
	if err != nil {
		return "", fmt.Errorf("ceiling for %s on library %d: %w", fingerprint, libraryID, err)
	}
	return ceiling, nil
}

/*
 * UnratedInShare counts what a ceiling would hide for being unrated, so the
 * host can be told before they choose (ADR 0071 §6).
 *
 * An unrated item is blocked, and the ADR's reasoning is that home video is
 * exactly where the gap sits — the material most likely to be personal is the
 * material a ceiling would otherwise leak. The cost is that those items vanish
 * with no explanation, and the mitigation is this number, shown to the host
 * rather than to the friend.
 *
 * Kinds that carry no certificate are excluded from both halves: a music
 * library is not "all unrated", it is a library a ceiling does not apply to,
 * and reporting 9,894 unrated tracks would read as a warning about nothing.
 */
func (s *Store) UnratedInShare(ctx context.Context, libraryID int64) (unrated, total int, err error) {
	exempt := make([]any, 0, len(unratedKinds)+1)
	exempt = append(exempt, libraryID)
	for _, k := range unratedKinds {
		exempt = append(exempt, k)
	}
	q := `SELECT
			COUNT(*),
			SUM(CASE WHEN ` + effectiveRating + ` IS NULL THEN 1 ELSE 0 END)
		  FROM media_item
		  WHERE media_item.library_id = ?
		    AND media_item.missing = 0
		    AND media_item.kind NOT IN (` + placeholders(len(unratedKinds)) + `)`

	var u sql.NullInt64
	if err := s.db.QueryRowContext(ctx, q, exempt...).Scan(&total, &u); err != nil {
		return 0, 0, fmt.Errorf("count unrated in library %d: %w", libraryID, err)
	}
	return int(u.Int64), total, nil
}
