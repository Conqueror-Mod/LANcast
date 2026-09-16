package store

import (
	"context"
	"fmt"
)

/*
 * Which artwork the library still refers to.
 *
 * The cache on disk is content-addressed and nothing has ever removed anything
 * from it, so it holds every poster of every deleted film and every poster a
 * corrected match replaced. Reclaiming that needs an answer to one question,
 * and getting the question wrong deletes a poster somebody is looking at.
 *
 * So it is answered in two steps, each of which is simple on its own:
 *
 *  1. The database drops `artwork` rows no item points at. `item_artwork`
 *     cascades when an item is deleted, which leaves the `artwork` row behind
 *     — orphaned by the schema's own definition rather than by a guess of
 *     mine.
 *  2. The file sweep keeps every hash the database still holds, from *either*
 *     table.
 *
 * The second step is deliberately conservative. A hash still named anywhere in
 * the database is kept, even if this code cannot work out what points at it —
 * because the cost of being wrong that way is some disk, and the cost of being
 * wrong the other way is a missing picture and a provider round trip to get it
 * back.
 */

/*
 * PruneOrphanArtworkRows deletes artwork nothing points at, and reports how
 * many rows went.
 *
 * Rows only: the files are the caller's job, because this package does not know
 * where the cache lives and should not.
 *
 * `person.thumb_hash` is checked as well as `item_artwork`, and it is the one
 * easy to forget: a cast photograph is artwork with no item behind it, and a
 * version of this that looked only at item_artwork would delete every face on
 * every detail page.
 */
func (s *Store) PruneOrphanArtworkRows(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM artwork
		 WHERE id NOT IN (SELECT artwork_id FROM item_artwork)
		   AND hash NOT IN (SELECT thumb_hash FROM person WHERE thumb_hash IS NOT NULL)`)
	if err != nil {
		return 0, fmt.Errorf("prune orphan artwork rows: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune orphan artwork rows: %w", err)
	}
	return n, nil
}

/*
 * ReferencedArtworkHashes is every hash the database still names.
 *
 * Both tables, and everything in `artwork` rather than only what an item points
 * at — see the note above about which direction to be wrong in. Run after
 * PruneOrphanArtworkRows, this is the set of files worth keeping.
 *
 * Returns an error rather than a partial set on any failure. A caller that
 * treated a failed read as "nothing is referenced" would delete the whole
 * cache, so there is deliberately no way to get a half-answer out of this.
 */
func (s *Store) ReferencedArtworkHashes(ctx context.Context) (map[string]bool, error) {
	live := map[string]bool{}

	rows, err := s.db.QueryContext(ctx, `
		SELECT hash FROM artwork WHERE hash IS NOT NULL AND hash <> ''
		UNION
		SELECT thumb_hash FROM person WHERE thumb_hash IS NOT NULL AND thumb_hash <> ''`)
	if err != nil {
		return nil, fmt.Errorf("referenced artwork: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, fmt.Errorf("referenced artwork: %w", err)
		}
		live[h] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("referenced artwork: %w", err)
	}
	return live, nil
}
