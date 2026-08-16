package store

import (
	"context"
	"fmt"
	"time"
)

/*
 * What a library's people have been playing lately.
 *
 * Same source as the history and the profile totals: `playback_state`, one row
 * per item per user. No new table, for the reason history gives — a second
 * record of the same fact is free to disagree with the first.
 *
 * The honest limitation, said here rather than discovered later: because there
 * is one row per item per user, this counts **how many people** have played
 * something recently, not how many times it has been played. On a
 * single-account server that makes every count 1 and the list degenerates into
 * "recently played", which is why the API reports the number of contributing
 * accounts and lets the client decline to call it trending. A server with a
 * household on it is the case the feature is for, and there the count is
 * exactly the interesting number.
 *
 * Per library, because that is the only grouping that means anything: one list
 * ranking a Tuesday-night album against a Sunday film is a list about nothing.
 */

// TrendingItem is one entry with the evidence behind its rank.
type TrendingItem struct {
	Item Item `json:"item"`
	// Viewers is how many distinct accounts played it inside the window.
	Viewers int `json:"viewers"`
	// Finishers is how many of them finished it. A title many people start and
	// nobody finishes is a different fact from one everybody finished, and
	// collapsing them into a single "popularity" number destroys it.
	Finishers int   `json:"finishers"`
	LastAt    int64 `json:"last_at"`
}

// TrendingWindow is how far back "lately" reaches. Long enough that a quiet
// week does not empty the shelf, short enough that it is not a hall of fame.
const TrendingWindow = 30 * 24 * time.Hour

// Trending returns a library's most-played items within the window, most
// viewers first. Containers are excluded: a season is not a thing anybody
// played, it is where the episodes live, and a shelf offering "Season 2" beside
// four films reads as a mistake.
func (s *Store) Trending(ctx context.Context, libraryID int64, limit int, now time.Time) ([]TrendingItem, error) {
	if limit <= 0 || limit > 50 {
		limit = 12
	}
	since := now.Add(-TrendingWindow).Unix()

	rows, err := s.db.QueryContext(ctx, `SELECT `+itemColsMI+`,
		    COUNT(DISTINCT ps.user_id),
		    COALESCE(SUM(ps.watched), 0),
		    MAX(ps.updated_at)
		FROM media_item mi
		JOIN playback_state ps ON ps.item_id = mi.id
		WHERE mi.library_id = ?
		  AND mi.missing = 0
		  AND mi.kind NOT IN ('show', 'season', 'artist', 'album', 'gallery', 'playlist')
		  AND ps.updated_at >= ?
		  AND (ps.position_ms > 0 OR ps.watched = 1)
		GROUP BY mi.id
		-- Viewers first, then the most recent activity. The tie-break is not
		-- decoration: without it a page of items that all have one viewer comes
		-- back in whatever order SQLite feels like, which changes between
		-- requests and makes the shelf shuffle itself on every refresh.
		ORDER BY COUNT(DISTINCT ps.user_id) DESC, MAX(ps.updated_at) DESC, mi.id DESC
		LIMIT ?`, libraryID, since, limit)
	if err != nil {
		return nil, fmt.Errorf("trending: %w", err)
	}
	defer rows.Close()

	out := []TrendingItem{}
	for rows.Next() {
		var e TrendingItem
		it, err := scanItemThen(rows, &e.Viewers, &e.Finishers, &e.LastAt)
		if err != nil {
			return nil, fmt.Errorf("trending: %w", err)
		}
		e.Item = *it
		out = append(out, e)
	}
	return out, rows.Err()
}

// TrendingContributors counts the accounts that played anything in this library
// inside the window. It is what tells a client whether it is showing a trend or
// showing one person's history — and the client is entitled to know which,
// because the two deserve different words on screen.
func (s *Store) TrendingContributors(ctx context.Context, libraryID int64, now time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT ps.user_id)
		FROM playback_state ps
		JOIN media_item mi ON mi.id = ps.item_id
		WHERE mi.library_id = ? AND ps.updated_at >= ?
		  AND (ps.position_ms > 0 OR ps.watched = 1)`,
		libraryID, now.Add(-TrendingWindow).Unix()).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("trending contributors: %w", err)
	}
	return n, nil
}
