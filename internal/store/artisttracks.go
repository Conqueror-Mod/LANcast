package store

import (
	"context"
	"fmt"
)

// itemColsT is the item column list qualified for the track alias below.
var itemColsT = qualifyCols(itemCols, "t")

/*
 * TracksOf returns every track under an artist, in the order the artist page
 * shows them: records in the page's order, and each record's tracks in the
 * order it plays.
 *
 * Two depths, because a track sits at either. Most hang off an album, and an
 * album off the artist; a file in the artist's folder with no album folder
 * around it is parented straight to the artist. The obvious
 * `?parent_id=<artist>` answers with the albums and those loose tracks, so
 * every client wanting the artist's songs would walk the albums itself — the
 * walk showEpisodes exists so nobody reimplements for a show.
 *
 * The page lists an artist's children by sort_title, albums and loose tracks
 * together. So a track is ordered by its album's sort_title, or by its own when
 * it has no album, which keeps a loose single where the page draws it. Then the
 * album id, so two records with the same title do not interleave, then disc,
 * track number and row id, so the order is total.
 *
 * Missing files are left out: this is what Play all queues, and a missing file
 * is one the player cannot open.
 *
 * Given an album rather than an artist it answers with that album's tracks,
 * since the two-depth rule finds them at depth one. Nothing relies on that, but
 * it is a right answer rather than an empty one.
 */
func (s *Store) TracksOf(ctx context.Context, artistID int64) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemColsT+`
		FROM media_item t
		LEFT JOIN media_item a ON a.id = t.parent_id
		WHERE t.kind = 'track' AND t.missing = 0
		  AND (t.parent_id = ? OR a.parent_id = ?)
		ORDER BY
		  CASE WHEN t.parent_id = ? THEN t.sort_title ELSE a.sort_title END,
		  CASE WHEN t.parent_id = ? THEN 0 ELSE a.id END,
		  t.season, t.episode, t.sort_title, t.id`,
		artistID, artistID, artistID, artistID)
	if err != nil {
		return nil, fmt.Errorf("tracks of: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}
