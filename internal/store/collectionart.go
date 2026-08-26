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

/*
 * Choosing which of its films a collection wears.
 *
 * The default above is right for almost every franchise and wrong for some: the
 * Marvel Cinematic Universe wearing Iron Man (2008) is defensible and is not
 * what somebody who has looked at it wants. So the rule stays the default and
 * this is the override — the same shape as every other correction in this
 * project, where a heuristic is good enough to ship and a person is allowed to
 * disagree with it.
 *
 * **It is a selection, not a copy.** Artwork is content-addressed and shared,
 * so pointing the collection at the film's existing `artwork` row costs one
 * `item_artwork` row and no bytes. Nothing is downloaded and nothing is
 * duplicated, which also means the picture the collection shows and the picture
 * the film shows cannot drift apart.
 *
 * **And it locks.** Field locks are how this project records "a person decided
 * this" (ADR 0008), and without one the next artwork write would quietly
 * replace the choice — PutArtwork deselects every row of the kind before
 * selecting its own. A choice a refresh can undo is not a choice; it is a
 * preference that survives until something happens.
 *
 * The member must actually be in the collection. The id arrives from a client
 * and this is the boundary where a bad one becomes "any item's poster on any
 * collection" -- so it is checked here rather than trusted, and a non-member is
 * an error rather than a silent no-op.
 */
func (s *Store) SetCollectionPoster(ctx context.Context, collectionID, memberID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("set collection poster: %w", err)
	}
	defer tx.Rollback()

	var artworkID int64
	err = tx.QueryRowContext(ctx, `
		SELECT ia.artwork_id
		FROM item_collection ic
		JOIN item_artwork ia ON ia.item_id = ic.item_id
		     AND ia.kind = 'poster' AND ia.selected = 1
		WHERE ic.collection_id = ? AND ic.item_id = ?`,
		collectionID, memberID).Scan(&artworkID)
	if err != nil {
		return fmt.Errorf("set collection poster: no poster for item %d in collection %d: %w",
			memberID, collectionID, err)
	}

	// The same deselect-then-select PutArtwork does, and for the same reason:
	// the primary key is (item_id, artwork_id, kind), so a different image
	// inserts a second row rather than replacing the first, and two rows
	// carrying selected = 1 leave which one wins to whatever SQLite returns
	// last.
	if _, err := tx.ExecContext(ctx,
		`UPDATE item_artwork SET selected = 0 WHERE item_id = ? AND kind = 'poster'`,
		collectionID); err != nil {
		return fmt.Errorf("set collection poster: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO item_artwork (item_id, artwork_id, kind, selected)
		VALUES (?, ?, 'poster', 1)`, collectionID, artworkID); err != nil {
		return fmt.Errorf("set collection poster: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO item_lock (item_id, field) VALUES (?, 'artwork')
		ON CONFLICT DO NOTHING`, collectionID); err != nil {
		return fmt.Errorf("set collection poster: %w", err)
	}
	return tx.Commit()
}

/*
 * ClearCollectionPoster puts a collection back to the default.
 *
 * The undo half, and it is not optional: an override somebody cannot take back
 * is a trap, and the default is a rule that improves — a franchise whose first
 * film arrives later should be able to start wearing it again.
 *
 * Deselecting rather than deleting, matching PutArtwork: the row is cheap, the
 * bytes are shared, and keeping it means the previous choice is still there for
 * a picker to show. The lock goes, because the whole point is to stop recording
 * that a person decided.
 */
func (s *Store) ClearCollectionPoster(ctx context.Context, collectionID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("clear collection poster: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`UPDATE item_artwork SET selected = 0 WHERE item_id = ? AND kind = 'poster'`,
		collectionID); err != nil {
		return fmt.Errorf("clear collection poster: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM item_lock WHERE item_id = ? AND field = 'artwork'`,
		collectionID); err != nil {
		return fmt.Errorf("clear collection poster: %w", err)
	}
	return tx.Commit()
}
