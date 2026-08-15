package store

import (
	"context"
	"fmt"
)

/*
 * Watch history, and the numbers you can honestly derive from it.
 *
 * No new table. `playback_state` has held the answers since v0.4 — what was
 * played, how far into it, and when it was last touched — and the only reason
 * none of it was ever shown is that nothing asked. A separate history table
 * would be a second record of the same fact, free to disagree with the first,
 * for a page that needs nothing the first does not already know.
 *
 * The cost of that choice is stated rather than hidden: one row per item per
 * user means the *last* time you played something, not every time. This is a
 * history of what you have watched, not a log of every sitting, and the page
 * says so. Recording each play would need the table this deliberately avoids,
 * and no feature on the roadmap has yet asked for one.
 */

// HistoryEntry is one item you have played, with where you left it.
type HistoryEntry struct {
	Item       Item  `json:"item"`
	PositionMS int64 `json:"position_ms"`
	Watched    bool  `json:"watched"`
	PlayedAt   int64 `json:"played_at"`
}

// ProfileStats are the totals behind the history.
type ProfileStats struct {
	Started  int `json:"started"`
	Finished int `json:"finished"`
	// WatchedMS is time actually spent, not runtime owned: a finished item
	// counts its duration, an unfinished one counts how far in you got. The
	// alternative — summing the duration of everything touched — would report
	// eleven hours for eleven films abandoned in their first minute.
	WatchedMS int64 `json:"watched_ms"`
	// FirstAt is the oldest playback this user has, so the client can say what
	// period the numbers cover rather than implying they cover all time.
	FirstAt *int64 `json:"first_at"`
}

/*
 * scanItemThen reads the item columns plus whatever the query selected after
 * them, in one Scan.
 *
 * scanItem knows the item column list and nothing else, which is what keeps
 * that list in one place; a joined query that wants three more values cannot
 * call it and cannot restate it either. This hands scanItem a Scan that quietly
 * appends the extra destinations, so the item columns stay described once.
 */
type appendScan struct {
	sc    interface{ Scan(...any) error }
	extra []any
}

func (a appendScan) Scan(dest ...any) error {
	return a.sc.Scan(append(dest, a.extra...)...)
}

func scanItemThen(sc interface{ Scan(...any) error }, extra ...any) (*Item, error) {
	return scanItem(appendScan{sc: sc, extra: extra})
}

// History returns the user's most recently played items, newest first.
//
// The secondary sort is not decoration. `updated_at` is stored in seconds, so
// several items played in the same second tie, and SQLite is free to return a
// tie in a different order each time — which under LIMIT/OFFSET means a row
// appearing on two pages while another appears on none. Breaking the tie on id
// makes the paging total rather than merely usually correct.
//
// Missing
// items are included: "what happened to the film I watched last week" is a
// question about history, and a library that lost a drive should not lose its
// answer to it.
func (s *Store) History(ctx context.Context, userID string, limit, offset int) ([]HistoryEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemColsMI+`,
		    ps.position_ms, ps.watched, ps.updated_at
		FROM media_item mi
		JOIN playback_state ps ON ps.item_id = mi.id AND ps.user_id = ?
		WHERE ps.position_ms > 0 OR ps.watched = 1
		ORDER BY ps.updated_at DESC, mi.id DESC
		LIMIT ? OFFSET ?`, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("history: %w", err)
	}
	defer rows.Close()

	out := []HistoryEntry{}
	for rows.Next() {
		// scanItem consumes exactly the item columns and leaves the rest, so
		// the three trailing values are read from the same row afterwards —
		// the same shape every other joined listing in this file uses.
		var e HistoryEntry
		var watched int
		it, err := scanItemThen(rows, &e.PositionMS, &watched, &e.PlayedAt)
		if err != nil {
			return nil, fmt.Errorf("history: %w", err)
		}
		e.Item = *it
		e.Watched = watched != 0
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ProfileStatistics totals the same rows the history lists.
func (s *Store) ProfileStatistics(ctx context.Context, userID string) (ProfileStats, error) {
	var st ProfileStats
	err := s.db.QueryRowContext(ctx, `
		SELECT
		  COUNT(*),
		  COALESCE(SUM(ps.watched), 0),
		  COALESCE(SUM(
		    CASE WHEN ps.watched = 1 THEN COALESCE(mi.duration_ms, ps.position_ms)
		         ELSE ps.position_ms END
		  ), 0),
		  MIN(ps.updated_at)
		FROM playback_state ps
		JOIN media_item mi ON mi.id = ps.item_id
		WHERE ps.user_id = ? AND (ps.position_ms > 0 OR ps.watched = 1)`,
		userID,
	).Scan(&st.Started, &st.Finished, &st.WatchedMS, &st.FirstAt)
	if err != nil {
		return st, fmt.Errorf("profile statistics: %w", err)
	}
	return st, nil
}
