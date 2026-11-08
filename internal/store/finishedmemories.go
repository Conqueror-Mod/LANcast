package store

import (
	"context"
	"fmt"
)

/*
 * On this day, from the watch history (ADR 0119 §3a).
 *
 * What you finished on today's date in an earlier year: films, episodes,
 * audiobooks and comics, the kinds `viewing` records. The day comes from the
 * same clock as PhotoMemories, SQLite's 'now' with 'localtime', for the same
 * reason: a client working out a date gets it wrong every US evening.
 *
 * Only rows with a real date. The rows seeded when the log began carry the day
 * the old watched flag was last written, not the day of the viewing, so one of
 * them could put a June film under "a year ago today" in October. Real history
 * began 2026-09-30, so this answers nothing until 2027-09-30 and then starts
 * on its own.
 *
 * One entry per item, at its latest such finish: a film you watch every
 * Christmas Eve is one memory, dated the most recent year.
 *
 * A deleted item has no row to show and is left out; a missing file too,
 * because a tile here is for opening. Hidden libraries and the ceiling are the
 * caller's to apply (PermittedItems), as for the photographs.
 */

// FinishedMemory is one item finished on today's date in an earlier year.
type FinishedMemory struct {
	Item       Item  `json:"item"`
	FinishedAt int64 `json:"finished_at"`
}

// FinishedOnThisDay returns what the account finished on today's date in an
// earlier year, most recent first.
func (s *Store) FinishedOnThisDay(ctx context.Context, userID string, limit int) ([]FinishedMemory, error) {
	if limit <= 0 || limit > 200 {
		limit = 40
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+qualifyCols(itemCols, "mi")+`, v.at
		  FROM (SELECT item_id, MAX(finished_at) AS at
		          FROM viewing
		         WHERE user_id = ? AND estimated = 0 AND item_id IS NOT NULL
		           AND strftime('%m-%d', finished_at, 'unixepoch', 'localtime')
		               = strftime('%m-%d', 'now', 'localtime')
		           AND strftime('%Y', finished_at, 'unixepoch', 'localtime')
		               < strftime('%Y', 'now', 'localtime')
		         GROUP BY item_id) v
		  JOIN media_item mi ON mi.id = v.item_id
		 WHERE mi.missing = 0
		 ORDER BY v.at DESC
		 LIMIT ?`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("finished on this day: %w", err)
	}
	defer rows.Close()
	out := []FinishedMemory{}
	for rows.Next() {
		var at int64
		it, err := scanItem(scanTail{rows, &at})
		if err != nil {
			return nil, fmt.Errorf("finished on this day: %w", err)
		}
		out = append(out, FinishedMemory{Item: *it, FinishedAt: at})
	}
	return out, rows.Err()
}

// scanTail lets scanItem read a row that carries extra columns after the
// item's own: it appends their destinations to every Scan.
type scanTail struct {
	sc   interface{ Scan(...any) error }
	tail any
}

func (t scanTail) Scan(dest ...any) error { return t.sc.Scan(append(dest, t.tail)...) }
