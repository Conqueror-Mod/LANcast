package store

import (
	"context"
	"fmt"
	"strings"
)

/*
 * Scrub-bar previews (ADR 0105): which films and episodes still need their
 * frames made.
 *
 * `preview_key` records the file a pass last looked at, as
 * "size:mtime:rev". An item is pending when its key is missing or no longer
 * matches the file it has now, so a replaced file is looked at again, and so
 * is everything when rev is raised to change how previews are made. A pass
 * that failed still writes the key: a file ffmpeg cannot read is tried again
 * when it changes, not on every pass.
 */

// PreviewKey is the key for an item's current file at rev, or "" when the
// item has no file stamp to key on.
func PreviewKey(it Item, rev int) string {
	if it.SizeBytes == nil || it.MTime == nil {
		return ""
	}
	return fmt.Sprintf("%d:%d:%d", *it.SizeBytes, *it.MTime, rev)
}

const previewPending = `
	kind IN ('movie', 'episode')
	AND missing = 0
	AND path IS NOT NULL
	AND probed_at IS NOT NULL
	AND duration_ms > 0
	AND size_bytes IS NOT NULL AND mtime IS NOT NULL
	AND (preview_key IS NULL OR preview_key != printf('%d:%d:%d', size_bytes, mtime, ?))`

// PendingPreviews is the next items to make previews for, newest first: a
// film just added is the one most likely to be watched next.
func (s *Store) PendingPreviews(ctx context.Context, rev, limit int) ([]Item, error) {
	if limit <= 0 || limit > 500 {
		limit = 25
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemCols+` FROM media_item WHERE `+previewPending+`
		ORDER BY added_at DESC, id DESC LIMIT ?`, rev, limit)
	if err != nil {
		return nil, fmt.Errorf("pending previews: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

// PendingPreviewsCount is how many are still waiting.
func (s *Store) PendingPreviewsCount(ctx context.Context, rev int) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM media_item WHERE `+previewPending, rev).Scan(&n); err != nil {
		return 0, fmt.Errorf("pending previews count: %w", err)
	}
	return n, nil
}

// SetPreviewKey records that a pass looked at an item's file.
func (s *Store) SetPreviewKey(ctx context.Context, itemID int64, key string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE media_item SET preview_key = ? WHERE id = ?`, key, itemID); err != nil {
		return fmt.Errorf("set preview key: %w", err)
	}
	return nil
}

// ExistingItems reports which of ids are still items, for pruning previews
// whose item has been deleted.
func (s *Store) ExistingItems(ctx context.Context, ids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	for start := 0; start < len(ids); start += 500 {
		end := min(start+500, len(ids))
		chunk := ids[start:end]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		rows, err := s.db.QueryContext(ctx, `SELECT id FROM media_item WHERE id IN (`+
			strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",")+`)`, args...)
		if err != nil {
			return nil, fmt.Errorf("existing items: %w", err)
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, fmt.Errorf("existing items: %w", err)
			}
			out[id] = true
		}
		rows.Close()
	}
	return out, nil
}
