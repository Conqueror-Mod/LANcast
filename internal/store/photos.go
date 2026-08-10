package store

import (
	"context"
	"fmt"
	"time"
)

// The thumbnail queue, shaped exactly like the cover-art one and sharing its
// stamp column (ADR 0028).
//
// `cover_checked_at` rather than a new column, because it already means "we
// have looked for this row's artwork and are not going to look again until
// something changes". A photo's artwork is the photo. Adding a second column
// with that meaning is how a schema starts disagreeing with itself — the same
// reasoning that stopped revision 16 adding a width it already had.

// PendingPhotos returns photos with no thumbnail attempt recorded.
func (s *Store) PendingPhotos(ctx context.Context, limit int) ([]Item, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemCols+` FROM media_item
		WHERE kind = 'photo' AND cover_checked_at IS NULL AND missing = 0
		ORDER BY added_at LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("pending photos: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

// PendingPhotoCount is the queue depth, for the activity panel.
func (s *Store) PendingPhotoCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_item
		WHERE kind = 'photo' AND cover_checked_at IS NULL AND missing = 0`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("pending photo count: %w", err)
	}
	return n, nil
}

// SetPhotoMeta records what one decode pass learned.
//
// Written even when the thumbnail later fails: the dimensions and the capture
// time came from the same pass and are useful on their own — a photo with no
// thumbnail still sorts by when it was taken.
//
// Zero taken_at is stored as NULL rather than 1970. Callers fall back to mtime,
// and a sentinel date that sorts first would put every EXIF-less wallpaper at
// the top of a date-ordered library.
func (s *Store) SetPhotoMeta(ctx context.Context, itemID int64, width, height int, takenAt int64) error {
	var taken any
	if takenAt > 0 {
		taken = takenAt
	}
	var w, h any
	if width > 0 {
		w = width
	}
	if height > 0 {
		h = height
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE media_item SET width = ?, height = ?, taken_at = ?, updated_at = ? WHERE id = ?`,
		w, h, taken, time.Now().Unix(), itemID)
	if err != nil {
		return fmt.Errorf("set photo meta: %w", err)
	}
	return nil
}
