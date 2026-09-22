package store

import (
	"context"
	"errors"
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
 * Principal is who a permission question is about.
 *
 * It exists because the two kinds are both strings. An account id and a peer
 * fingerprint are indistinguishable to a compiler, and ADR 0071 §6 names the
 * confusion between them as a real defect waiting in this code: a friend has no
 * `user` row by design, and the account path answers *permitted* when there is
 * no row. Pass a fingerprint where a user id was expected and every ceiling a
 * host set is bypassed — silently, and looking like it worked.
 *
 * So the two are constructed differently and cannot be mixed by accident. The
 * fields are unexported: there is no way to build one of these except by
 * saying which kind it is.
 */
type Principal struct {
	account string
	peer    string
}

// Account names one of this server's own people. Empty is the unsecured
// loopback case, which has no accounts at all and no ceiling to apply.
func Account(userID string) Principal { return Principal{account: userID} }

// Friend names a paired server. Its permission comes from what that server was
// granted, never from a user row (ADR 0071 §2).
func Friend(fingerprint string) Principal { return Principal{peer: fingerprint} }

/*
 * resolveCeiling answers which ceiling applies to a principal, and the two
 * kinds fail in opposite directions **on purpose**.
 *
 * An account with no row resolves to no ceiling, for the reason that default
 * was written: an unsecured loopback server has no accounts at all and reads
 * everything as store.LocalUserID, so refusing the unknown emptied the entire
 * library for the one configuration meant to work out of the box.
 *
 * A friend whose share cannot be found is refused. There is no equivalent
 * configuration to protect — a friend is admitted by a ticket from a server
 * this one paired with, and a grant that is missing means it was never made or
 * has been taken away. Both of those are "no".
 */
func (s *Store) resolveCeiling(ctx context.Context, who Principal, itemID int64) (string, error) {
	if who.peer != "" {
		var libraryID int64
		err := s.db.QueryRowContext(ctx,
			`SELECT library_id FROM media_item WHERE id = ?`, itemID).Scan(&libraryID)
		if err != nil {
			// An item that cannot be placed in a library cannot be shown to
			// belong to a share, so it is not shown at all.
			return "", ErrNotShared
		}
		return s.CeilingFor(ctx, who.peer, libraryID)
	}

	if who.account == "" {
		return "", nil
	}
	var ceiling string
	if err := s.db.QueryRowContext(ctx,
		`SELECT max_content_rating FROM user WHERE id = ?`, who.account).Scan(&ceiling); err != nil {
		return "", nil
	}
	return ceiling, nil
}

/*
 * ceilingPermits applies a ceiling to one item. The half that needed no
 * change: it was already about a label and an item rather than about who is
 * asking, which is why every listing, filter and search generalised untouched.
 */
func (s *Store) ceilingPermits(ctx context.Context, ceiling string, itemID int64) (bool, error) {
	var effective *string
	var kind string
	err := s.db.QueryRowContext(ctx,
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
 * MayPlay reports whether a principal's ceiling permits one item.
 *
 * This is the half that matters. A listing that hides a tile is a convenience;
 * this is the check that stands between a hand-written request and a file. It
 * takes an item id rather than an Item so that a caller cannot pass a struct it
 * assembled from something the client sent.
 *
 * Refusal is (false, nil). An error means the question could not be answered,
 * which callers must also treat as no — see GetItem, which does.
 *
 * An item that does not exist is somebody else's 404 to report for an account:
 * answering "forbidden" here would turn a wrong id into a claim about what this
 * library holds. For a friend it is a refusal, because a friend has no business
 * learning the difference either way.
 */
func (s *Store) MayPlay(ctx context.Context, who Principal, itemID int64) (bool, error) {
	ceiling, err := s.resolveCeiling(ctx, who, itemID)
	if errors.Is(err, ErrNotShared) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if ceiling == "" || !rating.Known(ceiling) {
		return true, nil
	}
	return s.ceilingPermits(ctx, ceiling, itemID)
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

/*
 * PermittedItems removes what an account's ceiling blocks from a list it did
 * not build.
 *
 * ListItems applies the predicate in its own WHERE, and GetItem is the
 * chokepoint for anything that turns an id into bytes. This is for the third
 * shape: a listing that builds its own SQL — a shelf, a collection's members,
 * a season's episodes — and then hands back rows. Without it those surfaces
 * show a restricted account a title it cannot open, which is worse than not
 * showing it: it names precisely what the household is keeping from them.
 *
 * One query for the whole slice rather than a check per row, and the *same*
 * predicate the listing uses, so the two cannot drift apart. Order is
 * preserved, because these lists are ordered for a reason — a shelf is ranked
 * and a season is in episode order.
 *
 * The unrestricted case costs one indexed lookup and returns the slice it was
 * given, untouched.
 */
func (s *Store) PermittedItems(ctx context.Context, userID string, items []Item) ([]Item, error) {
	if userID == "" || len(items) == 0 {
		return items, nil
	}
	var ceiling string
	if err := s.db.QueryRowContext(ctx,
		`SELECT max_content_rating FROM user WHERE id = ?`, userID).Scan(&ceiling); err != nil {
		// No account row means no ceiling, for the reason MayPlay records.
		return items, nil
	}
	pred, ceilArgs := ceilingPredicate(ceiling)
	if pred == "" {
		return items, nil
	}

	args := make([]any, 0, len(items)+len(ceilArgs))
	for _, it := range items {
		args = append(args, it.ID)
	}
	args = append(args, ceilArgs...)

	rows, err := s.db.QueryContext(ctx,
		`SELECT media_item.id FROM media_item WHERE media_item.id IN (`+
			placeholders(len(items))+`)`+pred, args...)
	if err != nil {
		return nil, fmt.Errorf("filter by ceiling: %w", err)
	}
	defer rows.Close()

	allowed := make(map[int64]bool, len(items))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("filter by ceiling: %w", err)
		}
		allowed[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]Item, 0, len(items))
	for _, it := range items {
		if allowed[it.ID] {
			out = append(out, it)
		}
	}
	return out, nil
}
