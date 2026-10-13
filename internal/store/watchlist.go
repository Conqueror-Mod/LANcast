package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

/*
 * The watchlist (ADR 0085): titles an account means to watch.
 *
 * Every statement takes the account, as tags do (ADR 0062): the list is
 * private, and there is no shape of caller that can read somebody else's.
 */

// ErrNotWatchlistable is a kind the watchlist does not take. A season or an
// episode is part of a show somebody means to watch; a collection is a list
// already.
var ErrNotWatchlistable = errors.New("only films and shows go on a watchlist")

// AddToWatchlist puts a title on an account's watchlist. Adding one already
// there is not an error and does not move it to the top: the date is when it
// was decided on.
func (s *Store) AddToWatchlist(ctx context.Context, userID string, itemID int64) error {
	var kind string
	err := s.db.QueryRowContext(ctx, `SELECT kind FROM media_item WHERE id = ?`, itemID).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("add to watchlist: %w", err)
	}
	if kind != "movie" && kind != "show" {
		return ErrNotWatchlistable
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO user_watchlist (user_id, item_id, added_at) VALUES (?, ?, ?)`,
		userID, itemID, time.Now().Unix()); err != nil {
		return fmt.Errorf("add to watchlist: %w", err)
	}
	return nil
}

// RemoveFromWatchlist takes a title off. Removing one that is not there is not
// an error: the answer to "is it off the list" is yes either way.
func (s *Store) RemoveFromWatchlist(ctx context.Context, userID string, itemID int64) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM user_watchlist WHERE user_id = ? AND item_id = ?`, userID, itemID); err != nil {
		return fmt.Errorf("remove from watchlist: %w", err)
	}
	return nil
}

// Watchlist is an account's list, newest first. Titles whose files have gone
// missing are left out rather than shown as tiles that will not play; their
// rows stay, so a remounted drive brings them back.
func (s *Store) Watchlist(ctx context.Context, userID string, limit int) ([]Item, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemColsMI+`
		FROM user_watchlist w
		JOIN media_item mi ON mi.id = w.item_id
		WHERE w.user_id = ? AND mi.missing = 0
		-- Ties on the second broken by when the row was written, not by the
		-- title's id: two titles added in one second are still newest first.
		ORDER BY w.added_at DESC, w.rowid DESC
		LIMIT ?`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("watchlist: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

/*
 * settleWatchlist takes what was just finished off the list (ADR 0085 §3).
 *
 * Called inside SaveProgress's transaction on the edge from not-finished to
 * finished, so it runs once per viewing and agrees with the viewing log about
 * what counted. A film leaves at once. A show leaves when the account has no
 * unwatched episode left in it — finishing the pilot is not finishing the
 * show. Missing episodes do not count against it: an episode whose file is
 * gone cannot be watched, and waiting for it would keep the show for ever.
 */
func settleWatchlist(ctx context.Context, tx *sql.Tx, userID string, itemID int64) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM user_watchlist WHERE user_id = ? AND item_id = ?`, userID, itemID); err != nil {
		return fmt.Errorf("settle watchlist: %w", err)
	}
	var show sql.NullInt64
	err := tx.QueryRowContext(ctx,
		`SELECT (`+showOf+`) FROM media_item mi WHERE mi.id = ? AND mi.kind = 'episode'`,
		itemID).Scan(&show)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !show.Valid) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("settle watchlist: %w", err)
	}
	var left int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM media_item e
		LEFT JOIN playback_state ps ON ps.item_id = e.id AND ps.user_id = ?
		WHERE e.kind = 'episode' AND e.missing = 0
		  AND (e.parent_id = ? OR e.parent_id IN (SELECT id FROM media_item WHERE parent_id = ?))
		  AND COALESCE(ps.watched, 0) = 0`,
		userID, show.Int64, show.Int64).Scan(&left); err != nil {
		return fmt.Errorf("settle watchlist: %w", err)
	}
	if left > 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM user_watchlist WHERE user_id = ? AND item_id = ?`, userID, show.Int64); err != nil {
		return fmt.Errorf("settle watchlist: %w", err)
	}
	return nil
}
