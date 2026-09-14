package store

import (
	"context"
	"fmt"
)

/*
 * A year, out of the history that is already there.
 *
 * No new table, for the reason history.go gives: `playback_state` has held
 * these answers since v0.4, and a second record of the same fact is free to
 * disagree with the first.
 *
 * What that costs has to be said plainly, because the whole appeal of this page
 * is that it is not a marketing artefact. **One row per item per user means the
 * last time you played something.** So a year here is "the things whose last
 * play landed in it" — a film watched in January and again in December belongs
 * to December and is absent from January, and nothing can recover the January
 * sitting because the row was overwritten.
 *
 * Two consequences follow, and both are deliberate:
 *
 * **Time spent counts one viewing, not `watch_count` viewings.** The tally is
 * real and the *dates* of those viewings are not: multiplying would attribute
 * every rewatch to the year of the most recent one, which is how a total grows
 * on its own. Under-reporting is the safer direction — a number that is missing
 * is easier to disbelieve than one that is inflated.
 *
 * **The boundaries are local.** A year is a calendar the household keeps, not a
 * UTC range: something played at eight on New Year's Eve belongs to the year
 * they were in when they watched it. SQLite does the conversion, the same way
 * the photo timeline groups its buckets.
 */

// YearMonth is one month's activity — the shape of a year, which is the part
// that actually reads as a year rather than as a number.
type YearMonth struct {
	// Month is 1–12 and every month is present, including the empty ones. A
	// chart with the quiet months missing is a chart that lies about its shape.
	Month int `json:"month"`
	// Titles is distinct items whose last play fell in this month.
	Titles int `json:"titles"`
}

// YearKind is how much of one kind of thing — the breadth half.
type YearKind struct {
	Kind   string `json:"kind"`
	Titles int    `json:"titles"`
}

// YearInReview is one calendar year of an account's own history.
type YearInReview struct {
	Year int `json:"year"`
	// Titles is distinct items whose last play landed in this year.
	Titles int `json:"titles"`
	// Finished and Abandoned split those: the difference between what you saw
	// through and what you put down, which is the honest version of a "top
	// list" this data can support.
	Finished  int `json:"finished"`
	Abandoned int `json:"abandoned"`
	/*
	 * WatchedMS is time spent, counting one viewing of each title.
	 *
	 * A finished title counts its runtime, an unfinished one counts how far in
	 * you got, and a title with no known runtime counts its position — the same
	 * three rules the lifetime figure uses, minus the rewatch multiplier that
	 * this view has no dates for.
	 */
	WatchedMS int64       `json:"watched_ms"`
	Months    []YearMonth `json:"months"`
	Kinds     []YearKind  `json:"kinds"`
	// Libraries is how many distinct libraries were touched. Breadth, in the
	// one unit this server can state without an opinion about genre.
	Libraries int `json:"libraries"`
	// First and Last are the first and last things played in the year. They are
	// the two the page can name without implying a ranking it cannot compute.
	First *Item `json:"first,omitempty"`
	Last  *Item `json:"last,omitempty"`
}

/*
 * localYear is the SQL that puts a playback in a calendar year.
 *
 * `strftime` with 'localtime' rather than arithmetic on the timestamp, because
 * a year boundary is a fact about a timezone and there is no offset to add that
 * is right in both halves of a year with daylight saving in it.
 */
const localYear = `CAST(strftime('%Y', ps.updated_at, 'unixepoch', 'localtime') AS INTEGER)`

const localMonth = `CAST(strftime('%m', ps.updated_at, 'unixepoch', 'localtime') AS INTEGER)`

/*
 * oneViewing is time spent on a title, counting a single viewing.
 *
 * Deliberately not `watch_count * duration`: see the package comment. The
 * lifetime total in history.go does multiply, and correctly — it is not
 * claiming those viewings happened in any particular year.
 */
const oneViewing = `CASE
	WHEN mi.duration_ms IS NULL THEN ps.position_ms
	WHEN ps.watched = 1 THEN mi.duration_ms
	ELSE ps.position_ms
END`

// playedPredicate is what counts as having played something at all, matching
// the lifetime statistics exactly so the two cannot disagree about whether a
// title is in somebody's history.
const playedPredicate = `ps.user_id = ? AND (ps.position_ms > 0 OR ps.watched = 1)`

/*
 * WatchYears lists the years this account has any history in, newest first.
 *
 * So the page can offer the years that exist rather than a spinner for one that
 * does not. An account with no history at all answers with an empty list, which
 * is a fact rather than an error.
 */
func (s *Store) WatchYears(ctx context.Context, userID string) ([]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT `+localYear+` AS y
		FROM playback_state ps
		WHERE `+playedPredicate+`
		ORDER BY y DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("watch years: %w", err)
	}
	defer rows.Close()

	out := []int{}
	for rows.Next() {
		var y int
		if err := rows.Scan(&y); err != nil {
			return nil, fmt.Errorf("watch years: %w", err)
		}
		out = append(out, y)
	}
	return out, rows.Err()
}

// YearInReview computes one account's year. It answers about the caller and
// nobody else: viewing is private by default (ADR 0035), and there is
// deliberately no variant of this that takes somebody else's id.
func (s *Store) YearInReview(ctx context.Context, userID string, year int) (YearInReview, error) {
	out := YearInReview{Year: year, Months: emptyMonths(), Kinds: []YearKind{}}

	err := s.db.QueryRowContext(ctx, `
		SELECT
		  COUNT(*),
		  COALESCE(SUM(ps.watched), 0),
		  COALESCE(SUM(`+oneViewing+`), 0),
		  COUNT(DISTINCT mi.library_id)
		FROM playback_state ps
		JOIN media_item mi ON mi.id = ps.item_id
		WHERE `+playedPredicate+` AND `+localYear+` = ?`,
		userID, year,
	).Scan(&out.Titles, &out.Finished, &out.WatchedMS, &out.Libraries)
	if err != nil {
		return out, fmt.Errorf("year in review: %w", err)
	}
	// Abandoned is derived rather than counted separately: every title is one
	// or the other, and two queries that could disagree about a total is the
	// shape of bug this file exists to avoid.
	out.Abandoned = out.Titles - out.Finished

	if err := s.yearMonths(ctx, userID, year, &out); err != nil {
		return out, err
	}
	if err := s.yearKinds(ctx, userID, year, &out); err != nil {
		return out, err
	}
	if err := s.yearEnds(ctx, userID, year, &out); err != nil {
		return out, err
	}
	return out, nil
}

// emptyMonths is twelve months with nothing in them. The quiet months are part
// of the shape and are never omitted.
func emptyMonths() []YearMonth {
	months := make([]YearMonth, 12)
	for i := range months {
		months[i] = YearMonth{Month: i + 1}
	}
	return months
}

func (s *Store) yearMonths(ctx context.Context, userID string, year int, out *YearInReview) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+localMonth+` AS m, COUNT(*)
		FROM playback_state ps
		JOIN media_item mi ON mi.id = ps.item_id
		WHERE `+playedPredicate+` AND `+localYear+` = ?
		GROUP BY m`, userID, year)
	if err != nil {
		return fmt.Errorf("year in review: months: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var m, n int
		if err := rows.Scan(&m, &n); err != nil {
			return fmt.Errorf("year in review: months: %w", err)
		}
		if m >= 1 && m <= 12 {
			out.Months[m-1].Titles = n
		}
	}
	return rows.Err()
}

func (s *Store) yearKinds(ctx context.Context, userID string, year int, out *YearInReview) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT mi.kind, COUNT(*) AS n
		FROM playback_state ps
		JOIN media_item mi ON mi.id = ps.item_id
		WHERE `+playedPredicate+` AND `+localYear+` = ?
		GROUP BY mi.kind
		ORDER BY n DESC, mi.kind`, userID, year)
	if err != nil {
		return fmt.Errorf("year in review: kinds: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var k YearKind
		if err := rows.Scan(&k.Kind, &k.Titles); err != nil {
			return fmt.Errorf("year in review: kinds: %w", err)
		}
		out.Kinds = append(out.Kinds, k)
	}
	return rows.Err()
}

/*
 * yearEnds finds the first and last thing played in the year.
 *
 * Two queries rather than one ordered pair, because the first and the last of a
 * one-title year are the same row and a single query returning it twice would
 * have to be untangled by the caller.
 *
 * Missing items are included, as everywhere else in the history: "what was that
 * film I started the year with" is a question about the year, and a lost drive
 * should not lose the answer.
 */
func (s *Store) yearEnds(ctx context.Context, userID string, year int, out *YearInReview) error {
	pick := func(order string) (*Item, error) {
		row := s.db.QueryRowContext(ctx, `
			SELECT `+itemColsMI+`
			FROM playback_state ps
			JOIN media_item mi ON mi.id = ps.item_id
			WHERE `+playedPredicate+` AND `+localYear+` = ?
			ORDER BY ps.updated_at `+order+`, mi.id
			LIMIT 1`, userID, year)
		it, err := scanItem(row)
		if err != nil {
			return nil, err
		}
		return it, nil
	}

	first, err := pick("ASC")
	if err != nil {
		// No history in this year at all, which is an ordinary answer for a
		// year somebody did not use the server.
		return nil
	}
	out.First = first

	last, err := pick("DESC")
	if err != nil {
		return nil
	}
	out.Last = last
	return nil
}
