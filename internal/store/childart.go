package store

import (
	"context"
	"fmt"
)

/*
 * A child borrows its parent's image when it has none of its own.
 *
 * The mirror of inheritArtistPosters, which walks the other way — a container
 * with no image of its own borrows one from a child. Both exist for the same
 * reason: a tile that renders blank reads as broken, and the server knows an
 * image that belongs to the thing being drawn.
 *
 * The gap this closes is not small. On a real library **8,443 tracks** had no
 * artwork while their album did, which is every music tile on the home page,
 * in Continue Listening, in search results and in a trending shelf — all blank,
 * beside film posters that render. A row of empty rectangles next to a row of
 * posters does not read as "music has no cover art"; it reads as a broken page,
 * which is exactly the complaint that produced the split between the watching
 * and listening shelves in the first place.
 *
 * Only the poster is inherited. Fanart is a backdrop for a page that is *about*
 * that item — an album's backdrop behind one track is a claim about the track
 * that nothing supports — and a thumb is per-item by definition.
 */
func (s *Store) inheritParentPosters(ctx context.Context, items []Item) error {
	// Only rows that came back with nothing of their own, and only those with a
	// parent to ask.
	want := map[int64][]int{} // parent id -> indexes in items
	for i := range items {
		if items[i].Artwork != nil && items[i].Artwork.Poster != "" {
			continue
		}
		if items[i].ParentID == nil || *items[i].ParentID == 0 {
			continue
		}
		want[*items[i].ParentID] = append(want[*items[i].ParentID], i)
	}
	if len(want) == 0 {
		return nil
	}

	ids := make([]any, 0, len(want))
	for id := range want {
		ids = append(ids, id)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT ia.item_id, a.hash
		  FROM item_artwork ia
		  JOIN artwork a ON a.id = ia.artwork_id
		 WHERE ia.kind = 'poster' AND ia.selected = 1
		   AND ia.item_id IN (`+placeholders(len(ids))+`)`, ids...)
	if err != nil {
		return fmt.Errorf("inherit parent posters: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var parentID int64
		var hash string
		if err := rows.Scan(&parentID, &hash); err != nil {
			return fmt.Errorf("inherit parent posters: %w", err)
		}
		for _, i := range want[parentID] {
			if items[i].Artwork == nil {
				items[i].Artwork = &Artwork{}
			}
			// Guard rather than assume: a child may have arrived with a fanart
			// or thumb of its own, and only the empty poster is being filled.
			if items[i].Artwork.Poster == "" {
				items[i].Artwork.Poster = hash
			}
		}
	}
	return rows.Err()
}
