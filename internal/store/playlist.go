package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Playlists (ADR 0030).
//
// A playlist is an ordinary media_item with kind = 'playlist'. Only its
// membership is special, and only in one way: playlist_entry is keyed on
// position rather than on membership, so the same track may appear more than
// once. item_collection cannot express that, which is the entire reason this
// file exists rather than reusing the collection helpers next door.

// PlaylistEntries returns a playlist's tracks in playing order.
//
// Repeats are returned as repeats. A JOIN that deduplicated would quietly turn
// a set with a reprise into a shorter set, and nothing downstream would know a
// track had been dropped.
func (s *Store) PlaylistEntries(ctx context.Context, playlistID int64) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemColsMI+`
		FROM playlist_entry pe
		JOIN media_item mi ON mi.id = pe.item_id
		WHERE pe.playlist_id = ?
		ORDER BY pe.ord`, playlistID)
	if err != nil {
		return nil, fmt.Errorf("playlist entries: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

// SetPlaylistEntries replaces a playlist's membership with exactly this list,
// in this order.
//
// Replace rather than merge, in one transaction. A playlist is an ordered
// sequence, and there is no sensible way to merge two orderings — an
// incremental API would have to answer "where does the new track go" on every
// call, and the answer is always "wherever the caller already decided".
//
// The delete-then-insert is inside the transaction because the primary key is
// (playlist_id, ord): reordering existing rows in place would collide with
// itself halfway through unless every write happened at once.
func (s *Store) SetPlaylistEntries(ctx context.Context, playlistID int64, itemIDs []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("set playlist entries: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM playlist_entry WHERE playlist_id = ?`, playlistID); err != nil {
		return fmt.Errorf("set playlist entries: clear: %w", err)
	}
	for i, id := range itemIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO playlist_entry (playlist_id, item_id, ord) VALUES (?, ?, ?)`,
			playlistID, id, i); err != nil {
			return fmt.Errorf("set playlist entries: insert %d: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("set playlist entries: commit: %w", err)
	}
	return nil
}

// PlaylistCount returns how many entries a playlist holds, counting repeats.
//
// Read from the join rather than from child_count, which counts parent_id
// children — a playlist has none, because its members keep their real parents.
// A track belongs to its album; being in a playlist does not move it.
func (s *Store) PlaylistCount(ctx context.Context, playlistID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM playlist_entry WHERE playlist_id = ?`, playlistID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("playlist count: %w", err)
	}
	return n, nil
}

// EnsurePlaylist creates or finds a playlist row.
//
// Keyed by path like every other derived container, and for a playlist imported
// from disk that path is the .m3u file itself — which makes re-importing the
// same file update one playlist rather than accumulating a new one per scan.
// A playlist created in the application gets a synthetic path in the same shape
// the artist and season rows use (ADR 0010, ADR 0024).
func (s *Store) EnsurePlaylist(ctx context.Context, libraryID int64, path, title, sortTitle string) (int64, error) {
	return s.EnsureDerivedContainer(ctx, libraryID, "playlist", path, title, sortTitle, nil)
}

// ItemIDByPath finds an item by its exact path, or 0 when there is none.
//
// Zero rather than sql.ErrNoRows: for a playlist import "this line does not
// match anything in the library" is the ordinary case, not an error. An .m3u
// written on another machine, or before a re-rip, will have several. They get
// counted and reported (ADR 0030), which is not a thing you want to express by
// unwrapping an error per line.
func (s *Store) ItemIDByPath(ctx context.Context, path string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM media_item WHERE path = ?`, path).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("item by path: %w", err)
	}
	return id, nil
}
