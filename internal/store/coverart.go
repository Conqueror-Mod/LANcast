package store

import (
	"context"
	"fmt"
	"time"
)

// PendingCoverArt returns albums whose artwork has not been looked for yet.
//
// The queue is a query rather than a table, the same as probing and enrichment,
// which makes it restart-safe by construction. `cover_checked_at` is what keeps
// it draining: an album with no embedded picture and no cover.jpg would
// otherwise reappear in every batch forever. Absence of artwork is not the
// same as absence of an attempt, and only the attempt is recorded here.
//
// Albums only. An artist row has no directory of its own to search and no file
// to extract from; if artist images arrive they will come from a provider, not
// from the filesystem.
func (s *Store) PendingCoverArt(ctx context.Context, limit int) ([]Item, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemCols+` FROM media_item
		WHERE kind = 'album' AND cover_checked_at IS NULL AND missing = 0
		ORDER BY added_at LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("pending cover art: %w", err)
	}
	defer rows.Close()

	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, fmt.Errorf("pending cover art: %w", err)
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// PendingCoverArtCount is how many albums still await a look.
//
// Reported separately from the worker's batch length for the reason
// PendingCount already documents: telling the user 100 when there are 400 is
// worse than telling them nothing.
func (s *Store) PendingCoverArtCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_item
		WHERE kind = 'album' AND cover_checked_at IS NULL AND missing = 0`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("pending cover art count: %w", err)
	}
	return n, nil
}

// AlbumTrackPaths returns the file paths of an album's tracks, in track order.
//
// Order matters because the first track is the one most likely to be tried for
// embedded art, and "first" should mean the first track of the record rather
// than whichever row the database happened to return.
//
// A track's disc and track numbers live in `season` and `episode` — the wide
// table reuses those columns rather than adding music-specific ones (ADR 0002),
// which is why ordering an album reads like ordering a TV season.
func (s *Store) AlbumTrackPaths(ctx context.Context, albumID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT path FROM media_item
		WHERE parent_id = ? AND kind = 'track' AND missing = 0 AND path IS NOT NULL
		ORDER BY COALESCE(season, 0), COALESCE(episode, 0), title`, albumID)
	if err != nil {
		return nil, fmt.Errorf("album track paths: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("album track paths: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// MarkArtworkChecked records that an album was looked at, whether or not
// anything was found. Called on the failure path too — an album whose art
// cannot be read must not be retried on every pass forever.

// MarkArtworkChecked records that this row's artwork has been looked for, so
// the queue does not hand it back forever. Shared by album covers and photo
// thumbnails: both answer the same question about the same column, and a photo's
// artwork is the photo (ADR 0028).
func (s *Store) MarkArtworkChecked(ctx context.Context, itemID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE media_item SET cover_checked_at = ? WHERE id = ?`,
		time.Now().Unix(), itemID)
	if err != nil {
		return fmt.Errorf("mark artwork checked: %w", err)
	}
	return nil
}

// ClearCoverArtChecks re-queues albums for another look, optionally within one
// library. It is the counterpart to re-probing: a build that learns to read a
// picture format it could not read before has no other way to revisit albums it
// already gave up on, because the pending query is "cover_checked_at IS NULL"
// and theirs is set.
func (s *Store) ClearCoverArtChecks(ctx context.Context, libraryID int64) (int64, error) {
	q := `UPDATE media_item SET cover_checked_at = NULL WHERE kind = 'album'`
	args := []any{}
	if libraryID > 0 {
		q += ` AND library_id = ?`
		args = append(args, libraryID)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, fmt.Errorf("clear cover art checks: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("clear cover art checks: %w", err)
	}
	return n, nil
}
