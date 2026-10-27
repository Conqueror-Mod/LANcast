package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

/*
 * Comics (ADR 0101): where each person is in each comic, and which comics
 * still want a cover.
 *
 * A page is not a position in milliseconds, so a comic's place is kept in its
 * own table rather than in playback_state's position_ms. Putting it there
 * would have put comics on Continue Watching with a progress bar computed from
 * a duration a comic does not have. Finishing one is still "watched" in
 * playback_state, so the tick, Mark as read and the watched filter all work
 * the way they do for everything else.
 */

// ComicPlace is where somebody is in a comic.
type ComicPlace struct {
	Page      int   `json:"page"`
	Pages     int   `json:"pages"`
	UpdatedAt int64 `json:"updated_at"`
}

// ComicPlaceFor reads a place; ok is false when they have not opened it.
func (s *Store) ComicPlaceFor(ctx context.Context, userID string, itemID int64) (ComicPlace, bool, error) {
	var p ComicPlace
	err := s.db.QueryRowContext(ctx,
		`SELECT page, pages, updated_at FROM comic_place WHERE user_id = ? AND item_id = ?`,
		userID, itemID).Scan(&p.Page, &p.Pages, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ComicPlace{}, false, nil
	}
	if err != nil {
		return ComicPlace{}, false, fmt.Errorf("comic place: %w", err)
	}
	return p, true, nil
}

// SetComicPlace records a place.
func (s *Store) SetComicPlace(ctx context.Context, userID string, itemID int64, page, pages int) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO comic_place (user_id, item_id, page, pages, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, item_id) DO UPDATE SET page = excluded.page, pages = excluded.pages,
		    updated_at = excluded.updated_at`,
		userID, itemID, page, pages, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("set comic place: %w", err)
	}
	return nil
}

// PendingComicCovers are comics whose cover has not been made yet.
func (s *Store) PendingComicCovers(ctx context.Context, limit int) ([]Item, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemCols+` FROM media_item
		WHERE kind = 'comic' AND cover_checked_at IS NULL AND missing = 0
		ORDER BY added_at LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("pending comic covers: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}
