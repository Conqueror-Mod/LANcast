package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

/*
 * What you finished lately and have not rated: Home's "Rate what you've
 * watched" shelf (Chris, 2026-10-17).
 *
 * A film counts when you finished it. A show counts when you finished one of
 * its episodes, and it is the show that is offered: an episode is rated
 * through its show here, because a shelf of eight episodes of one series to
 * score one by one is a chore nobody asked for. Either leaves the shelf the
 * moment it has a rating, which is the only way off: the shelf is a question,
 * and answering it is how it goes away.
 *
 * Newest finish first, within RateWindow. Every candidate goes through
 * GetItem, which applies the account's rating ceiling and hidden libraries,
 * so the shelf never offers something the account cannot open.
 */

// RateWindow is how far back the shelf looks.
const RateWindow = 30 * 24 * time.Hour

// ToRate lists up to limit films and shows this user finished within the
// window and has not rated, most recently finished first.
func (s *Store) ToRate(ctx context.Context, userID string, limit int) ([]Item, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	since := time.Now().Add(-RateWindow).Unix()
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, MAX(finished) AS at FROM (
			-- Films finished.
			SELECT mi.id AS id, ps.updated_at AS finished
			FROM playback_state ps
			JOIN media_item mi ON mi.id = ps.item_id
			WHERE ps.user_id = ? AND ps.watched = 1 AND ps.updated_at >= ?
			  AND mi.kind = 'movie' AND mi.missing = 0
			UNION ALL
			-- Shows, through an episode finished: episode, season, show.
			SELECT show.id AS id, ps.updated_at AS finished
			FROM playback_state ps
			JOIN media_item ep ON ep.id = ps.item_id AND ep.kind = 'episode'
			JOIN media_item season ON season.id = ep.parent_id
			JOIN media_item show ON show.id = season.parent_id AND show.kind = 'show'
			WHERE ps.user_id = ? AND ps.watched = 1 AND ps.updated_at >= ?
		)
		WHERE id NOT IN (SELECT item_id FROM user_rating WHERE user_id = ?)
		GROUP BY id
		ORDER BY at DESC, id DESC
		LIMIT ?`, userID, since, userID, since, userID, limit*2)
	if err != nil {
		return nil, fmt.Errorf("to rate: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id, at int64
		if err := rows.Scan(&id, &at); err != nil {
			rows.Close()
			return nil, fmt.Errorf("to rate: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("to rate: %w", err)
	}

	// Twice the limit was read so that items the account may not see, which
	// GetItem refuses, do not leave the shelf short.
	out := []Item{}
	for _, id := range ids {
		if len(out) == limit {
			break
		}
		it, err := s.GetItem(ctx, id, userID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("to rate: %w", err)
		}
		out = append(out, *it)
	}
	return out, nil
}
