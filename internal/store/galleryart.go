package store

import (
	"context"
	"fmt"
)

// inheritGalleryPosters gives a gallery a cover borrowed from one of its photos.
//
// The same shape as inheritArtistPosters and for the same reason: a gallery is
// a folder, and a folder has no image of its own. It is a read-time fallback
// rather than a stored row, so nothing has to be migrated, expired or cleaned
// up, and re-running the thumbnail worker changes the cover with the photo
// rather than leaving a copy of an image that no longer exists.
//
// The chosen photo is the first by sort title, which is stability rather than
// taste. "The newest" or "the largest" would be prettier and would change the
// face of a gallery every time a photo is added — a tile that looks different
// on every visit reads as a bug, and there is no editorial signal in a folder of
// UUID filenames worth chasing for it.
func (s *Store) inheritGalleryPosters(ctx context.Context, items []Item) error {
	want := map[int64]int{} // gallery id -> index in items
	for i := range items {
		if items[i].Kind != "gallery" {
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

	// One row per gallery: the winner is decided in SQL by the same ordering the
	// comment describes, so a page of 60 galleries is one query rather than 60.
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.id, (
			SELECT a.hash
			FROM media_item p
			JOIN item_artwork ia ON ia.item_id = p.id AND ia.kind = 'poster' AND ia.selected = 1
			JOIN artwork a       ON a.id = ia.artwork_id
			WHERE p.parent_id = g.id AND p.kind = 'photo' AND p.missing = 0
			ORDER BY p.sort_title, p.id
			LIMIT 1
		)
		FROM media_item g
		WHERE g.id IN (`+placeholders(len(ids))+`)`, ids...)
	if err != nil {
		return fmt.Errorf("inherit gallery posters: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var hash *string
		if err := rows.Scan(&id, &hash); err != nil {
			return fmt.Errorf("inherit gallery posters: %w", err)
		}
		if hash == nil || *hash == "" {
			continue
		}
		idx, ok := want[id]
		if !ok {
			continue
		}
		if items[idx].Artwork == nil {
			items[idx].Artwork = &Artwork{}
		}
		items[idx].Artwork.Poster = *hash
		items[idx].Artwork.Inherited = true
	}
	return rows.Err()
}
