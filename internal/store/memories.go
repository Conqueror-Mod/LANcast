package store

import (
	"context"
	"fmt"
)

/*
 * On this day: photographs taken on today's date in an earlier year.
 *
 * WHY THE SERVER DECIDES WHAT DAY IT IS
 *
 * There is no `taken_on=MM-DD` parameter and this is not a shape /api/items
 * could take. A parameter would invite the client to work out today's date, and
 * a client computing a calendar date is the bug this project has already
 * written down twice: `toISOString().slice(0,10)` is UTC, so in a US evening it
 * resolves to *tomorrow*, and PhotoTimeline already carries the note that a
 * client must not re-derive months from timestamps or it will disagree about
 * where one begins.
 *
 * So the day comes from the same clock the photographs were filed under —
 * SQLite's `'now'` with `'localtime'`, exactly as the timeline computes its
 * months — and there is one answer rather than two that usually agree.
 *
 * WHAT IS EXCLUDED, AND WHY IT IS MORE THAN THE TIMELINE EXCLUDES
 *
 * Marked folders (ADR 0051), for a stronger reason than anywhere else. The
 * timeline is somewhere you navigated to; a memory is unsolicited, and it
 * appears on the home page where anybody in the room can see it. A shelf is the
 * last place a covered photograph should surface.
 *
 * The current year, because a photograph from this morning is not a memory. It
 * would also be the one thing on the shelf every time somebody imported a card,
 * pushing the actual memories off the end.
 *
 * Missing files, because a shelf is for looking at.
 */

// PhotoMemories returns photographs taken on today's date in an earlier year,
// newest first, along with the date the server resolved as today.
//
// The date is returned rather than assumed by the caller so a long-lived client
// can notice it has crossed midnight and the shelf it is showing is yesterday's.
func (s *Store) PhotoMemories(ctx context.Context, limit int) ([]Item, string, error) {
	if limit <= 0 || limit > 200 {
		limit = 40
	}

	var today string
	if err := s.db.QueryRowContext(ctx,
		`SELECT strftime('%m-%d', 'now', 'localtime')`).Scan(&today); err != nil {
		return nil, "", fmt.Errorf("photo memories: read today: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT `+itemCols+`
		  FROM media_item
		 WHERE kind = 'photo' AND missing = 0
		   AND sensitive_effective = 0
		   AND taken_at IS NOT NULL
		   AND strftime('%m-%d', taken_at, 'unixepoch', 'localtime') = ?
		   AND strftime('%Y', taken_at, 'unixepoch', 'localtime')
		       < strftime('%Y', 'now', 'localtime')
		 ORDER BY taken_at DESC
		 LIMIT ?`, today, limit)
	if err != nil {
		return nil, "", fmt.Errorf("photo memories: %w", err)
	}
	defer rows.Close()

	items, err := scanItems(rows)
	if err != nil {
		return nil, "", fmt.Errorf("photo memories: %w", err)
	}
	return items, today, nil
}
