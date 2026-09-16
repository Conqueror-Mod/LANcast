package store

import (
	"context"
	"fmt"
)

/*
 * Re-asking about a title whose identity is already settled.
 *
 * Every other refresh in this project works by clearing `metadata_updated_at`
 * so the enrichment queue picks the row up again — and that queue *searches and
 * scores*. Which is exactly why every one of them excludes `locked`: requeueing
 * a locked row would re-run the search and re-pick the candidate a person
 * rejected, and no amount of scoping makes that acceptable.
 *
 * The consequence went unnoticed until somebody counted. A locked row can never
 * be told anything new. Not a corrected identity — it has one — but a *field*
 * the provider did not used to return: on this library, eighteen locked titles
 * had no `content_rating` because certificates were not fetched until v0.9.23,
 * and three of them were shows, so 88 episodes inherited nothing and an account
 * with a rating ceiling could not play them. The lock was not protecting any of
 * that. `content_rating` was locked on **zero** items library-wide.
 *
 * So this scope is the other door. It does not clear a stamp and it does not go
 * near the queue: the caller fetches each row from its *own* recorded provider
 * id and applies the result under the locks the row already carries. Nothing is
 * searched, nothing is scored, no match state moves. The locked-fields rule
 * says a locked field is never overwritten and a locked match is never
 * re-scored — this does neither, which is what makes it the one refresh that
 * may include them.
 *
 * Restricted to rows that actually carry a provider id, because a fetch needs
 * one: a locked row with no id is a decision somebody made by hand and there is
 * nothing to ask about it.
 */

// settledWhere is the row set this scope names, shared by the count and the
// listing so the price and the work can never disagree.
//
// `locked` only, rather than every settled row. A `matched` row is already
// reachable by an ordinary refresh, so including it here would double the cost
// of the button to do again what another button does — and the gap this exists
// to close is precisely the rows nothing else can reach.
const settledWhere = `
	WHERE library_id = ? AND missing = 0
	  AND match_state = 'locked'
	  AND provider IS NOT NULL AND TRIM(provider) <> ''
	  AND external_id IS NOT NULL AND TRIM(external_id) <> ''
	  AND ` + enrichableKinds

/*
 * SettledCount prices the pass without performing it.
 *
 * Same contract as RefreshCount, and for the same reason: this spends provider
 * lookups, and a cost that only reveals itself once committed is one people
 * learn to avoid. On this library it answers 18 where a full refresh answers
 * 1,069, which is the number that makes it worth pressing.
 */
func (s *Store) SettledCount(ctx context.Context, libraryID int64) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM media_item`+settledWhere, libraryID).Scan(&n); err != nil {
		return 0, fmt.Errorf("settled count: %w", err)
	}
	return n, nil
}

/*
 * SettledItems lists the rows the pass will re-ask about.
 *
 * Whole rows rather than ids, because the caller needs the provider, the
 * external id, and — for an episode or a season — the numbers that select
 * within a show. Fetching them again one at a time would be a query per lookup
 * on top of the network call each already costs.
 *
 * Ordered by id so a run is reproducible and a partial one is resumable by
 * inspection: told it stopped after forty, somebody can see which forty.
 */
func (s *Store) SettledItems(ctx context.Context, libraryID int64) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+itemCols+` FROM media_item`+settledWhere+` ORDER BY id`, libraryID)
	if err != nil {
		return nil, fmt.Errorf("settled items: %w", err)
	}
	defer rows.Close()

	var out []Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, fmt.Errorf("settled items: %w", err)
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}
