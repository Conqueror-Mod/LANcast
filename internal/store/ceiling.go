package store

import (
	"context"
	"fmt"

	"lancast/internal/rating"
)

/*
 * A content-rating ceiling, enforced where it has to be (ADR 0015).
 *
 * Server-side, in the queries and in playback authorisation, because a
 * client-side hide is a suggestion and this API serves files. An account that
 * can be talked into a stream by a hand-written request has no ceiling at all,
 * and the difference between hiding a tile and refusing a byte is the whole
 * feature.
 *
 * Two rules, stated once here and enforced from one place, because a rule
 * applied in four places with three interpretations is not a rule:
 *
 * **An item with no rating of its own inherits one.** An episode almost never
 * carries a certificate; its show does. Without inheritance a ceiling would
 * hide every episode in the library while leaving films visible, which is not a
 * limit anybody asked for and is how a feature gets switched off and called
 * broken. This is the same shape as the resolved `sensitive` flag (ADR 0051):
 * the server answers what the item *effectively* is, rather than making each
 * caller walk the tree.
 *
 * **What is still unrated after inheriting is blocked.** internal/rating
 * carries the argument; the short version is that a limit stopping at the
 * catalogued and waving the rest past is a filter that looks like a limit.
 */

/*
 * Kinds no certificate system describes.
 *
 * A ceiling is a statement about film and television, which is what the
 * national systems on the ladder classify. A track and a photograph carry no
 * certificate and never will, so judging them by one means "unrated, therefore
 * blocked" — and that is not a limit, it is a lockout: a child account with a
 * PG ceiling could not see a single song or a single photograph.
 *
 * Found by asking what the rule does to a music library rather than by reading
 * it, which is the only way this sort of thing gets found. The blocked-when-
 * unrated rule stands exactly where it is meant to: things somebody could have
 * rated and did not.
 *
 * Spelled literally because store owns its SQL and does not import the client's
 * notion of kind, the same reason 'unmatched' is spelled out in ListItems.
 */
var unratedKinds = []string{
	"artist", "album", "track", "playlist",
	"gallery", "photo",
}

// exemptKindsSQL is unratedKinds as a predicate fragment plus its arguments.
func exemptKindsSQL() (string, []any) {
	args := make([]any, 0, len(unratedKinds))
	for _, k := range unratedKinds {
		args = append(args, k)
	}
	return `media_item.kind IN (` + placeholders(len(unratedKinds)) + `)`, args
}

/*
 * effectiveRating is the rating an item is judged by: its own, else its
 * parent's, else its grandparent's.
 *
 * Two levels because that is how deep the hierarchy goes for the case that
 * matters — episode, season, show — and a recursive walk would be a great deal
 * of machinery for a third level that does not exist. A part of a two-part film
 * reaches its film in one.
 *
 * Correlated subqueries rather than joins, and that is not a style preference.
 * Every listing in this file selects unqualified column names; joining
 * media_item to itself makes every one of them ambiguous, and the fix would be
 * qualifying a column list that a dozen other queries share. This composes into
 * a WHERE clause and changes nothing about the FROM it lands in, which is also
 * what lets the count and the page agree by construction.
 */
const effectiveRating = `COALESCE(
	NULLIF(media_item.content_rating, ''),
	NULLIF((SELECT p.content_rating FROM media_item p
	         WHERE p.id = media_item.parent_id), ''),
	NULLIF((SELECT g.content_rating FROM media_item g
	          JOIN media_item p2 ON p2.parent_id = g.id
	         WHERE p2.id = media_item.parent_id), '')
)`

/*
 * ceilingPredicate restricts a listing to what a ceiling permits.
 *
 * Returns an empty string when there is no ceiling, so the ordinary case adds
 * no SQL at all — an unrestricted account pays nothing for this feature
 * existing, including the joins.
 */
func ceilingPredicate(ceiling string) (string, []any) {
	labels := rating.AllowedLabels(ceiling)
	if len(labels) == 0 {
		return "", nil
	}
	args := make([]any, 0, len(labels)+len(unratedKinds))
	exempt, exemptArgs := exemptKindsSQL()
	args = append(args, exemptArgs...)
	for _, l := range labels {
		args = append(args, l)
	}
	/*
	 * Either the kind carries no certificate at all, or its effective rating is
	 * one the ceiling permits.
	 *
	 * NULL is not IN anything, so an item left unrated after inheritance fails
	 * the second half automatically. That is the intended reading and not an
	 * accident of three-valued logic — it is asserted in the tests.
	 */
	return ` AND (` + exempt + ` OR ` + effectiveRating +
		` IN (` + placeholders(len(labels)) + `))`, args
}

/*
 * MayPlay reports whether an account's ceiling permits one item.
 *
 * This is the half that matters. A listing that hides a tile is a convenience;
 * this is the check that stands between a hand-written request and a file. It
 * takes an item id rather than an Item so that a caller cannot pass a struct it
 * assembled from something the client sent.
 *
 * An account with no ceiling, an item that does not exist, and a caller with no
 * account at all all take the fast path: there is no limit to apply. A missing
 * item is somebody else's 404 to report, and answering "forbidden" here would
 * turn a wrong id into a claim about what this library holds.
 */
func (s *Store) MayPlay(ctx context.Context, userID string, itemID int64) (bool, error) {
	if userID == "" {
		return true, nil
	}
	var ceiling string
	err := s.db.QueryRowContext(ctx,
		`SELECT max_content_rating FROM user WHERE id = ?`, userID).Scan(&ceiling)
	if err != nil {
		/*
		 * No account row means no ceiling, not a refusal.
		 *
		 * This started out the other way — an id naming no account looked like
		 * a stale or forged session, so it was refused — and the store's own
		 * tests caught what that actually does. An unsecured loopback server
		 * has no accounts at all and reads everything as store.LocalUserID, so
		 * refusing the unknown emptied the entire library for the one
		 * configuration that is meant to work out of the box.
		 *
		 * The deeper error was making this function do two jobs. Deciding
		 * whether a session is real belongs to the session layer, which does it
		 * on every request; this one answers what a ceiling permits, and an
		 * account that has none is not restricted.
		 */
		return true, nil
	}
	if !rating.Known(ceiling) {
		return true, nil
	}

	var effective *string
	var kind string
	err = s.db.QueryRowContext(ctx,
		`SELECT `+effectiveRating+`, media_item.kind FROM media_item WHERE media_item.id = ?`,
		itemID).Scan(&effective, &kind)
	if err != nil {
		return true, nil
	}
	// The same exemption the listing makes, and it has to be the same or the
	// grid and the player disagree — which is the failure this whole feature
	// exists to prevent.
	for _, k := range unratedKinds {
		if kind == k {
			return true, nil
		}
	}
	if effective == nil {
		return false, nil
	}
	return rating.Allowed(*effective, ceiling), nil
}

/*
 * SetMaxContentRating records a ceiling for one account.
 *
 * Deliberately admin-facing, and deliberately without a self-service twin. The
 * account it limits cannot clear it, because a limit you can lift is not a
 * limit — the mirror image of SetShareActivity next door, which has no
 * administrator route for the opposite reason.
 *
 * An unrecognised label is refused rather than stored. A ceiling nothing can
 * place would silently mean "no limit", and a household would believe a limit
 * was in force that was not — which is the only failure here that is worse than
 * being too strict.
 */
func (s *Store) SetMaxContentRating(ctx context.Context, userID, label string) error {
	if label != "" && !rating.Known(label) {
		return fmt.Errorf("set content rating ceiling: %q is not a rating this server can place", label)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE user SET max_content_rating = ? WHERE id = ?`, label, userID)
	if err != nil {
		return fmt.Errorf("set content rating ceiling: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// MaxContentRating reads one account's ceiling, or empty for none.
func (s *Store) MaxContentRating(ctx context.Context, userID string) (string, error) {
	if userID == "" {
		return "", nil
	}
	var label string
	err := s.db.QueryRowContext(ctx,
		`SELECT max_content_rating FROM user WHERE id = ?`, userID).Scan(&label)
	if err != nil {
		return "", ErrNotFound
	}
	return label, nil
}
