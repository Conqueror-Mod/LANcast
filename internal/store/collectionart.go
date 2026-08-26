package store

import (
	"context"
	"fmt"
)

/*
 * A collection with no image of its own wears its first film's poster.
 *
 * The same shape as inheritArtistPosters and inheritGalleryPosters, and it
 * exists because smart collections have nothing to fetch. A TMDB collection is
 * a real record with a poster and a backdrop behind it, and those download at
 * creation. A *keyword* is a tag: 180547 is `marvel cinematic universe (mcu)`
 * and there is no image anywhere behind it. So the Marvel Cinematic Universe
 * tile rendered as its own title on an empty rectangle, in a grid of 176
 * collections that all have art.
 *
 * That was a deliberate omission — "no artwork is fetched: a keyword has no
 * poster" — and the reasoning was right about the *provider* and wrong about
 * the *tile*. A container that renders blank reads as broken however honest the
 * reason, which is the finding inheritArtistPosters and inheritGalleryPosters
 * were both written from.
 *
 * **The first film, by release, and that is stability rather than taste.**
 * "Highest rated" or "most recent" would be prettier and would change the face
 * of a franchise whenever a film is added or a score moves — a tile that looks
 * different on every visit reads as a bug. A franchise's first film is also the
 * one that names it: the Marvel Cinematic Universe wearing Iron Man (2008) is
 * the honest answer to "what is this".
 *
 * Read-time and flagged `inherited`, like both of its neighbours, so nothing is
 * stored, nothing has to be migrated or expired, and a real poster arriving
 * later simply supersedes it with nothing to clean up (ADR 0025).
 *
 * Only the poster. A backdrop belongs to a page that is *about* one work, and a
 * film's fanart behind a franchise page is a claim about the franchise that
 * nothing supports.
 */
func (s *Store) inheritCollectionPosters(ctx context.Context, items []Item) error {
	want := map[int64]int{} // collection id -> index in items
	for i := range items {
		if items[i].Kind != "collection" {
			continue
		}
		if items[i].Artwork != nil && items[i].Artwork.Poster != "" {
			continue
		}
		want[items[i].ID] = i
	}
	if len(want) == 0 {
		return nil
	}

	ids := make([]any, 0, len(want))
	for id := range want {
		ids = append(ids, id)
	}

	/*
	 * One row per collection, ranked by the same order the members list uses.
	 *
	 * ROW_NUMBER rather than GROUP BY, because a bare column beside an
	 * aggregate is an arbitrary row in SQLite -- it would have returned *a*
	 * member's poster and looked correct on any collection small enough to
	 * check by eye.
	 *
	 * Present films only: a franchise must not wear the poster of a file that
	 * is gone, which is exactly the row the collection page has just stopped
	 * listing. An undated film ranks last for the same reason it sorts last
	 * there -- NULL leads in SQLite, and the earliest-looking member should not
	 * be the one nothing is known about.
	 */
	rows, err := s.db.QueryContext(ctx, `
		SELECT collection_id, hash FROM (
			SELECT ic.collection_id AS collection_id, a.hash AS hash,
			       ROW_NUMBER() OVER (
			           PARTITION BY ic.collection_id
			           ORDER BY m.year IS NULL, m.year, m.sort_title, m.id
			       ) AS rn
			FROM item_collection ic
			JOIN media_item m ON m.id = ic.item_id AND m.missing = 0
			JOIN item_artwork ia ON ia.item_id = m.id
			     AND ia.kind = 'poster' AND ia.selected = 1
			JOIN artwork a ON a.id = ia.artwork_id
			WHERE ic.collection_id IN (`+placeholders(len(ids))+`)
		) WHERE rn = 1`, ids...)
	if err != nil {
		return fmt.Errorf("inherit collection posters: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var collectionID int64
		var hash string
		if err := rows.Scan(&collectionID, &hash); err != nil {
			return fmt.Errorf("inherit collection posters: %w", err)
		}
		i, ok := want[collectionID]
		if !ok {
			continue
		}
		if items[i].Artwork == nil {
			items[i].Artwork = &Artwork{}
		}
		items[i].Artwork.Poster = hash
		items[i].Artwork.Inherited = true
	}
	return rows.Err()
}
