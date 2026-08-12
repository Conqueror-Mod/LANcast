package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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

// AppendPlaylistEntries adds tracks to the end of a playlist, in this order.
//
// The counterpart to SetPlaylistEntries, and the only other write worth having:
// "add this to the playlist" is the one edit whose position the caller does not
// need to decide, because the answer is always "at the end". Everything else —
// reorder, remove, insert in the middle — is a caller who has already decided
// the whole sequence, and says so with SetPlaylistEntries.
//
// One transaction, and the starting position is read inside it, so two
// simultaneous appends cannot both claim the same ord and collide on the
// primary key.
func (s *Store) AppendPlaylistEntries(ctx context.Context, playlistID int64, itemIDs []int64) error {
	if len(itemIDs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("append playlist entries: %w", err)
	}
	defer tx.Rollback()

	// COALESCE because MAX over no rows is NULL, and an empty playlist is an
	// ordinary thing here (ADR 0030).
	var next int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(ord) + 1, 0) FROM playlist_entry WHERE playlist_id = ?`,
		playlistID).Scan(&next); err != nil {
		return fmt.Errorf("append playlist entries: next position: %w", err)
	}
	for i, id := range itemIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO playlist_entry (playlist_id, item_id, ord) VALUES (?, ?, ?)`,
			playlistID, id, next+i); err != nil {
			return fmt.Errorf("append playlist entries: insert %d: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("append playlist entries: commit: %w", err)
	}
	return nil
}

// RemovePlaylistEntry deletes the entry at one position and closes the gap.
//
// By position, not by item id, because an item id does not identify an entry —
// a playlist may hold the same track twice, and "remove Wish You Were Here"
// from a set that opens and closes with it is not an answerable request. The
// caller points at the row it is looking at.
//
// Positions are resequenced so ord stays a dense 0..n-1 run. Leaving holes
// would work — the ORDER BY does not care — but then a position is no longer
// an index into what the client rendered, and every later edit has to think
// about that. The renumber happens in the same transaction, back to front, so
// it cannot collide with a row it has not moved yet.
//
// Returns ErrNotFound when there is no entry at that position, so a stale
// client clicking remove twice gets a 404 rather than silently removing
// whatever slid into the slot.
func (s *Store) RemovePlaylistEntry(ctx context.Context, playlistID int64, pos int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("remove playlist entry: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`DELETE FROM playlist_entry WHERE playlist_id = ? AND ord = ?`, playlistID, pos)
	if err != nil {
		return fmt.Errorf("remove playlist entry: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE playlist_entry SET ord = ord - 1
		 WHERE playlist_id = ? AND ord > ?`, playlistID, pos); err != nil {
		return fmt.Errorf("remove playlist entry: resequence: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("remove playlist entry: commit: %w", err)
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

// CreatePlaylist makes a new, empty playlist that no file on disk seeded.
//
// The path is synthetic and *opaque* — random, not derived from the title —
// which is the one place this differs from every other derived container. A
// scanner-made container needs a deterministic identity so a rescan finds the
// same row again; a playlist a person typed a name into is never re-derived
// from anything, so determinism buys nothing and costs the two things it would
// break: two playlists may share a name (they are not identities), and renaming
// one must not strand its old name as a path that a later create collides with.
func (s *Store) CreatePlaylist(ctx context.Context, libraryID int64, title, sortTitle string) (int64, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, fmt.Errorf("create playlist: %w", err)
	}
	path := fmt.Sprintf("::playlist=%x", b)
	return s.EnsurePlaylist(ctx, libraryID, path, title, sortTitle)
}

// ExistingItemIDs reports which of these ids are real rows.
//
// So a caller can reject a bad membership write with the ids it could not find,
// rather than letting the foreign key fail the insert and turning a client's
// stale id into a 500 with no clue in it.
func (s *Store) ExistingItemIDs(ctx context.Context, ids []int64) (map[int64]bool, error) {
	found := make(map[int64]bool, len(ids))
	if len(ids) == 0 {
		return found, nil
	}
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i] = "?"
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM media_item WHERE id IN (`+strings.Join(ph, ",")+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("existing item ids: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("existing item ids: %w", err)
		}
		found[id] = true
	}
	return found, rows.Err()
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
