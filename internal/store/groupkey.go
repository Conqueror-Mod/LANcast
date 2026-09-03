package store

import (
	"context"
	"database/sql"
	"fmt"
)

/*
 * A track's stored grouping key (ADR 0056).
 *
 * Scanner working state, not truth about a record. The album *item* is the
 * truth; these are the values a tag read produced, kept so the next scan need
 * not reopen a file whose size and mtime have just been proven unchanged.
 *
 * Nothing outside `internal/scan` reads them and they are not in the API. A
 * grouping key that clients could see would be read as a statement about the
 * album, which is exactly what it is not.
 */

// GroupKey is what a tag read told the scanner about where a track belongs.
type GroupKey struct {
	// Artist is the **album** artist, which on a compilation differs from the
	// track's own performer. Grouping on the performer shatters a record into
	// one album per guest.
	Artist string
	Album  string
	// Dir is the folder the file sits in; AlbumFromFolder says the album name
	// was taken from that folder rather than from a tag; AlbumAtRoot says that
	// folder is a direct child of a library location. The three exist for
	// dropBucketAlbums, whose tells are properties of a folder.
	Dir             string
	AlbumFromFolder bool
	AlbumAtRoot     bool
}

/*
 * GroupKeys returns the stored keys for a library's tracks, by item id.
 *
 * Absent rather than zero when a track has none: a track scanned before
 * revision 39, or one whose file has changed, has no usable key and its file
 * must be read. Returning a zero-valued key would put it in an album named
 * empty string.
 */
func (s *Store) GroupKeys(ctx context.Context, libraryID int64) (map[int64]GroupKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, group_artist, group_album, group_dir,
		       group_album_from_folder, group_album_at_root
		FROM media_item
		WHERE library_id = ? AND kind = 'track' AND missing = 0
		  AND group_dir IS NOT NULL`, libraryID)
	if err != nil {
		return nil, fmt.Errorf("group keys: %w", err)
	}
	defer rows.Close()

	out := map[int64]GroupKey{}
	for rows.Next() {
		var (
			id                 int64
			artist, album, dir sql.NullString
			fromFolder, atRoot sql.NullBool
		)
		if err := rows.Scan(&id, &artist, &album, &dir, &fromFolder, &atRoot); err != nil {
			return nil, fmt.Errorf("group keys: %w", err)
		}
		out[id] = GroupKey{
			Artist:          artist.String,
			Album:           album.String,
			Dir:             dir.String,
			AlbumFromFolder: fromFolder.Bool,
			AlbumAtRoot:     atRoot.Bool,
		}
	}
	return out, rows.Err()
}

/*
 * SaveGroupKeys stores the keys a pass computed, in one transaction.
 *
 * Written for every track the pass grouped, not only the ones it read from
 * disk: a key recovered from storage is rewritten unchanged, which costs one
 * statement and keeps the invariant simple — a track the scanner grouped has a
 * stored key.
 */
func (s *Store) SaveGroupKeys(ctx context.Context, keys map[int64]GroupKey) error {
	if len(keys) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save group keys: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		UPDATE media_item SET
			group_artist = ?, group_album = ?, group_dir = ?,
			group_album_from_folder = ?, group_album_at_root = ?
		WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("save group keys: %w", err)
	}
	defer stmt.Close()

	for id, k := range keys {
		if _, err := stmt.ExecContext(ctx, k.Artist, k.Album, k.Dir,
			boolInt(k.AlbumFromFolder), boolInt(k.AlbumAtRoot), id); err != nil {
			return fmt.Errorf("save group keys: %w", err)
		}
	}
	return tx.Commit()
}

/*
 * ClearGroupKeys drops every stored key in a library.
 *
 * The escape hatch ADR 0056 requires. `dropBucketAlbums` runs over the
 * assembled groups on every scan, so changes to *it* take effect regardless;
 * a build that changes how a key is **extracted** from tags has to clear them,
 * the same way a build that learns a new probe field re-probes.
 */
func (s *Store) ClearGroupKeys(ctx context.Context, libraryID int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE media_item SET group_artist = NULL, group_album = NULL,
			group_dir = NULL, group_album_from_folder = NULL,
			group_album_at_root = NULL
		WHERE library_id = ? AND kind = 'track' AND group_dir IS NOT NULL`, libraryID)
	if err != nil {
		return 0, fmt.Errorf("clear group keys: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("clear group keys: %w", err)
	}
	return n, nil
}
