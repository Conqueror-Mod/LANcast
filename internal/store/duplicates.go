package store

import (
	"context"
	"fmt"
)

/*
 * Exact duplicate photos (ADR 0075).
 *
 * Exact means the same bytes: two files whose SHA-256 agree. Measured on a real
 * library of 3,079 photos before this was built: 38 groups, 44 extra copies,
 * and most of them the same photo filed in two albums — Animals and Me & Us —
 * which is a person's filing rather than a mistake. So a group is reported with
 * every copy's album beside it, and nothing here decides which copy should go.
 *
 * Near copies — the same picture resized or re-saved — are not here, on
 * evidence: a perceptual hash could not tell a resized copy from two different
 * screenshots of the same screen on that library, at 64 bits or at 256. That is
 * a measurement waiting to be made against the image embeddings, not a
 * threshold waiting to be tuned.
 */

// DuplicateCopy is one file in a group of identical photos.
type DuplicateCopy struct {
	Item Item `json:"item"`
	// Album is the gallery the copy sits in — its folder (ADR 0028) — or nil
	// for a photo loose at the library root.
	Album *string `json:"album"`
}

// DuplicateGroup is two or more photos with the same bytes.
type DuplicateGroup struct {
	SHA256    string          `json:"sha256"`
	SizeBytes int64           `json:"size_bytes"`
	Copies    []DuplicateCopy `json:"copies"`
}

/*
 * PhotoDuplicates returns a picture library's groups of identical photos,
 * largest groups first, each group's copies in path order.
 *
 * A photo marked sensitive takes no part (ADR 0051), as it takes no part in
 * the timeline: listing it beside an unmarked copy would show the picture the
 * mark exists to obscure. A missing photo takes no part either — there is
 * nothing on disk to be a duplicate of anything.
 */
func (s *Store) PhotoDuplicates(ctx context.Context, libraryID int64) ([]DuplicateGroup, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH live AS (
			SELECT h.sha256, mi.id, mi.path, mi.size_bytes, mi.parent_id
			  FROM photo_hash h
			  JOIN media_item mi ON mi.id = h.item_id
			 WHERE mi.library_id = ? AND mi.kind = 'photo'
			   AND mi.missing = 0 AND mi.sensitive_effective = 0
		), shared AS (
			SELECT sha256, COUNT(*) AS n FROM live GROUP BY sha256 HAVING COUNT(*) > 1
		)
		SELECT live.sha256, live.id, COALESCE(live.size_bytes, 0), g.title
		  FROM live
		  JOIN shared ON shared.sha256 = live.sha256
		  LEFT JOIN media_item g ON g.id = live.parent_id AND g.kind = 'gallery'
		 ORDER BY shared.n DESC, live.sha256, live.path`, libraryID)
	if err != nil {
		return nil, fmt.Errorf("photo duplicates: %w", err)
	}
	type row struct {
		sha   string
		id    int64
		size  int64
		album *string
	}
	var found []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.sha, &r.id, &r.size, &r.album); err != nil {
			rows.Close()
			return nil, fmt.Errorf("photo duplicates: %w", err)
		}
		found = append(found, r)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("photo duplicates: %w", err)
	}
	if len(found) == 0 {
		return []DuplicateGroup{}, nil
	}

	ids := make([]any, len(found))
	for i, r := range found {
		ids[i] = r.id
	}
	itemRows, err := s.db.QueryContext(ctx,
		`SELECT `+itemCols+` FROM media_item WHERE id IN (`+placeholders(len(ids))+`)`, ids...)
	if err != nil {
		return nil, fmt.Errorf("photo duplicates: %w", err)
	}
	items, err := scanItems(itemRows)
	itemRows.Close()
	if err != nil {
		return nil, fmt.Errorf("photo duplicates: %w", err)
	}
	byID := make(map[int64]Item, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}

	out := []DuplicateGroup{}
	for _, r := range found {
		if len(out) == 0 || out[len(out)-1].SHA256 != r.sha {
			out = append(out, DuplicateGroup{SHA256: r.sha, SizeBytes: r.size})
		}
		g := &out[len(out)-1]
		g.Copies = append(g.Copies, DuplicateCopy{Item: byID[r.id], Album: r.album})
	}
	return out, nil
}
