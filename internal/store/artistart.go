package store

import (
	"context"
	"fmt"
	"sort"
)

// inheritArtistPosters gives artists with no image of their own a poster
// borrowed from one of their albums.
//
// Artists are the one container with nothing to source an image from. An album
// has a picture embedded in its tracks or a cover.jpg beside them; an artist has
// neither, and the images that do sit in an artist folder turn out to be a media
// player's per-album art cache — `AlbumArtSmall.jpg`, `AlbumArt_{GUID}_Large.jpg`
// — rather than a photograph of anyone. Measured on a real library, not assumed.
//
// So this is deliberately a *fallback at read time*, never a stored row:
//
//   - A real artist image, whenever a provider supplies one, simply wins. There
//     is nothing to migrate, expire, or clean up, because nothing was written.
//   - The borrowed poster follows its album. Re-run the cover worker and change
//     an album's art, and the artist changes with it rather than keeping a copy
//     of an image that no longer exists anywhere else.
//
// The chosen album is the one with the most tracks, which picks a record over a
// stray single, with sort title and id as tie-breakers so a tile does not change
// its face between two scans that found the same thing.
func (s *Store) inheritArtistPosters(ctx context.Context, items []Item) error {
	// Only artists, and only those that came back with nothing of their own.
	want := map[int64]int{} // artist id -> index in items
	for i := range items {
		if items[i].Kind != "artist" {
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

	rows, err := s.db.QueryContext(ctx, `
		SELECT ar.id, al.id, al.sort_title, a.hash,
		       (SELECT COUNT(*) FROM media_item t
		         WHERE t.parent_id = al.id AND t.kind = 'track' AND t.missing = 0)
		FROM media_item ar
		JOIN media_item al  ON al.parent_id = ar.id AND al.kind = 'album' AND al.missing = 0
		JOIN item_artwork ia ON ia.item_id = al.id AND ia.kind = 'poster' AND ia.selected = 1
		JOIN artwork a       ON a.id = ia.artwork_id
		WHERE ar.id IN (`+placeholders(len(ids))+`)`, ids...)
	if err != nil {
		return fmt.Errorf("inherit artist posters: %w", err)
	}
	defer rows.Close()

	type candidate struct {
		albumID   int64
		sortTitle string
		hash      string
		tracks    int
	}
	byArtist := map[int64][]candidate{}
	for rows.Next() {
		var artistID int64
		var c candidate
		if err := rows.Scan(&artistID, &c.albumID, &c.sortTitle, &c.hash, &c.tracks); err != nil {
			return fmt.Errorf("inherit artist posters: %w", err)
		}
		byArtist[artistID] = append(byArtist[artistID], c)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("inherit artist posters: %w", err)
	}

	for artistID, cands := range byArtist {
		sort.Slice(cands, func(i, j int) bool {
			if cands[i].tracks != cands[j].tracks {
				return cands[i].tracks > cands[j].tracks
			}
			if cands[i].sortTitle != cands[j].sortTitle {
				return cands[i].sortTitle < cands[j].sortTitle
			}
			return cands[i].albumID < cands[j].albumID
		})
		idx, ok := want[artistID]
		if !ok {
			continue
		}
		if items[idx].Artwork == nil {
			items[idx].Artwork = &Artwork{}
		}
		items[idx].Artwork.Poster = cands[0].hash
		items[idx].Artwork.Inherited = true
	}
	return nil
}
