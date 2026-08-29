package store

import (
	"context"
	"fmt"
)

/*
 * Which items a metadata refresh re-asks about.
 *
 * "Refresh metadata" clears the stamp that keeps an item out of the enrichment
 * queue, and it did so for a whole library at once. On a real library that is
 * about 1,480 provider lookups for the films alone, and at five a second it is
 * five minutes of work to fix the handful of rows somebody actually meant.
 *
 * The scopes exist because "re-ask about everything" and "re-ask about the ones
 * that never got an answer" are different requests, and only the first was
 * possible. Somebody who has just corrected a filename, or added an API key, or
 * fixed a provider outage wants the second — and paying for the first to get it
 * is what made the button something to avoid.
 *
 * Modelled on HistoryScope deliberately, including the shared clause: a preview
 * that prices one set and an action that touches another is the worst shape this
 * kind of feature can take, so the count and the clear read the same SQL.
 */

// RefreshScope is which rows a metadata refresh will requeue.
type RefreshScope string

const (
	// RefreshAll re-asks about every item a provider could answer for. What the
	// button has always done, and still the default, so a caller that names no
	// scope gets the behaviour it got before scopes existed.
	RefreshAll RefreshScope = "all"
	/*
	 * RefreshUnmatched re-asks only about items no provider identified.
	 *
	 * The scope that makes the feature worth having. A library where everything
	 * matched has nothing to do here, and that is the honest answer rather than
	 * a disappointing one — the number is shown before the work is done, so
	 * "0 items" is information and not a wasted five minutes.
	 */
	RefreshUnmatched RefreshScope = "unmatched"
)

/*
 * refreshWhere is the row set one scope names, shared by the count and the
 * clear.
 *
 * `enrichableKinds` is part of every scope rather than a caller's job. A track
 * or a photo can never be matched by any provider LANcast ships (ADR 0024), so
 * counting them would price work that will never happen — which on a music
 * library means quoting twelve thousand and doing none of it.
 *
 * `locked` is excluded for the reason the whole project excludes it: a rescan
 * reconciles files and does not re-litigate identity. A refresh that requeued
 * locked rows would be a button that undoes a person's decision, and no amount
 * of scoping makes that acceptable.
 */
func refreshWhere(libraryID int64, scope RefreshScope) (string, []any, error) {
	where := ` WHERE library_id = ? AND missing = 0 AND match_state != 'locked' AND ` + enrichableKinds
	args := []any{libraryID}

	switch scope {
	case RefreshAll:
	case RefreshUnmatched:
		where += ` AND match_state = 'unmatched'`
	default:
		return "", nil, fmt.Errorf("unknown refresh scope %q", scope)
	}
	return where, args, nil
}

/*
 * RefreshCount prices a refresh without performing it.
 *
 * The same reason HistoryCount exists: a number is what makes an expensive
 * action reviewable before it is taken. Somebody who expected to re-ask about a
 * dozen unmatched films and is told fourteen hundred has learned something while
 * it is still free.
 */
func (s *Store) RefreshCount(ctx context.Context, libraryID int64, scope RefreshScope) (int64, error) {
	where, args, err := refreshWhere(libraryID, scope)
	if err != nil {
		return 0, fmt.Errorf("refresh count: %w", err)
	}
	var n int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM media_item`+where, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("refresh count: %w", err)
	}
	return n, nil
}

/*
 * RefreshScoped requeues one scope's items and reports how many it moved.
 *
 * The count comes from the write rather than from a second query, so what is
 * reported is what happened — a number gathered separately can differ from the
 * work by whatever changed in between.
 */
func (s *Store) RefreshScoped(ctx context.Context, libraryID int64, scope RefreshScope) (int64, error) {
	where, args, err := refreshWhere(libraryID, scope)
	if err != nil {
		return 0, fmt.Errorf("refresh library: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE media_item SET metadata_updated_at = NULL`+where, args...)
	if err != nil {
		return 0, fmt.Errorf("refresh library: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("refresh library: %w", err)
	}
	return n, nil
}
