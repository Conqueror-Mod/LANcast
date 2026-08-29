package store

import (
	"context"
	"database/sql"
	"errors"
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
	Started int `json:"started"`
	// Finished is how many distinct titles have been finished, and Viewings is
	// how many times finishing happened. They are different questions and the
	// difference is the whole point of revision 31: somebody who has seen
	// twelve films, one of them nine times, has finished twelve things and sat
	// through twenty. Reporting only the first is what a boolean could say.
	Finished int `json:"finished"`
	Viewings int `json:"viewings"`
	// WatchedMS is time actually spent, not runtime owned: a finished item
	// counts its duration *per viewing*, an unfinished one counts how far in
	// you got. The alternative — summing the duration of everything touched —
	// would report eleven hours for eleven films abandoned in their first
	// minute; counting a rewatched film once, which is what this did before
	// revision 31 was read, under-reports the opposite way.
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
	/*
	 * Time watched counts every viewing, and the partial one on top.
	 *
	 * `watch_count` viewings of a title are `watch_count` durations. The
	 * current sitting is added only when the title is *not* finished, because a
	 * finished one has already had this viewing counted by the tally — adding
	 * its position as well would count the last showing twice.
	 *
	 * A title with no known runtime falls back to how far in you got, once,
	 * and is deliberately *not* multiplied by the tally. There is no measurement
	 * of how long one viewing of it was, so a product would be inventing time
	 * rather than reporting it — and inventing upward is the worse direction,
	 * because a total that grows on its own is harder to disbelieve than one
	 * that is missing.
	 */
	err := s.db.QueryRowContext(ctx, `
		SELECT
		  COUNT(*),
		  COALESCE(SUM(ps.watched), 0),
		  COALESCE(SUM(ps.watch_count), 0),
		  COALESCE(SUM(
		    CASE WHEN mi.duration_ms IS NULL THEN ps.position_ms
		         ELSE ps.watch_count * mi.duration_ms
		              + CASE WHEN ps.watched = 1 THEN 0 ELSE ps.position_ms END
		    END
		  ), 0),
		  MIN(ps.updated_at)
		FROM playback_state ps
		JOIN media_item mi ON mi.id = ps.item_id
		WHERE ps.user_id = ? AND (ps.position_ms > 0 OR ps.watched = 1)`,
		userID,
	).Scan(&st.Started, &st.Finished, &st.Viewings, &st.WatchedMS, &st.FirstAt)
	if err != nil {
		return st, fmt.Errorf("profile statistics: %w", err)
	}

	/*
	 * Plus anything banked by a history reset.
	 *
	 * Clearing the history used to zero these, because every total was derived
	 * from the rows being deleted — so a profile reported nothing started and
	 * no time watched for somebody who had watched hundreds of hours. That
	 * conflates two requests: "forget the list of what I watched" is about the
	 * record, and "I have never watched anything" is a claim about the person
	 * that nobody made.
	 *
	 * A missing row is the ordinary case — most accounts have never cleared
	 * anything — so this reads as zero rather than expecting a row to exist.
	 */
	var banked ProfileStats
	err = s.db.QueryRowContext(ctx, `
		SELECT started, finished, viewings, watched_ms, first_at
		FROM profile_totals WHERE user_id = ?`, userID,
	).Scan(&banked.Started, &banked.Finished, &banked.Viewings, &banked.WatchedMS, &banked.FirstAt)
	if errors.Is(err, sql.ErrNoRows) {
		return st, nil
	}
	if err != nil {
		return st, fmt.Errorf("profile statistics: banked totals: %w", err)
	}

	st.Started += banked.Started
	st.Finished += banked.Finished
	st.Viewings += banked.Viewings
	st.WatchedMS += banked.WatchedMS
	// The oldest playback either half knows about. A reset does not make an
	// account younger than it is.
	if st.FirstAt == nil || (banked.FirstAt != nil && *banked.FirstAt < *st.FirstAt) {
		st.FirstAt = banked.FirstAt
	}
	return st, nil
}
