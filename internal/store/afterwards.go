package store

import (
	"context"
	"errors"
	"fmt"
)

/*
 * What the end of a film offers.
 *
 * Two answers, and they answer different questions, so neither is allowed to
 * stand in for the other:
 *
 *   - The next film in its collection: the franchise carrying on. Read from
 *     the collection's own order, counted from *this* film, so it does not
 *     depend on the finish having been written yet — the page asks at the
 *     same moment the player saves the film as watched, and a rule that read
 *     watch state for the film just finished would race that save.
 *   - Films like it from outside its franchise: "something else in this
 *     vein". The sequel is the obvious suggestion and has its own slot, so
 *     every film sharing a collection with this one is excluded here — a
 *     similarity list that is the rest of the box set says nothing new.
 *
 * Like the spotlight's recommendation (useHero.ts), this is not an engine and
 * phones nothing home. It is the film's own genres and credits read against
 * the same library, and every suggestion can say what it shares.
 */

// NextAfterInCollection is the first film after itemID, in release order, in a
// collection that holds it, that this account has not watched and may see. It
// carries ContinuesFrom naming the collection and the film it follows. nil when
// the film is the last of its collection, or in none.
func (s *Store) NextAfterInCollection(ctx context.Context, itemID int64, userID string) (*Item, error) {
	film, err := s.GetItem(ctx, itemID, userID)
	if err != nil {
		return nil, err
	}
	// Real collections only: a provider's collection of one is a duplicate
	// tile of the film, not a franchise with a next film in it.
	cols, err := s.CollectionsShownFor(ctx, itemID)
	if err != nil {
		return nil, err
	}
	for _, c := range cols {
		members, err := s.CollectionMembers(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		at := -1
		for i, m := range members {
			if m.ID == itemID {
				at = i
				break
			}
		}
		if at < 0 {
			continue
		}
		for _, m := range members[at+1:] {
			// GetItem carries this account's progress and applies its rating
			// ceiling: a film this account may not see is not the next one.
			it, err := s.GetItem(ctx, m.ID, userID)
			if errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("next after in collection: %w", err)
			}
			// Skipping a film already watched is the collection's own rule
			// (nextInSequence): forward only, and never back over one seen.
			if it.Progress != nil && it.Progress.Watched {
				continue
			}
			it.ContinuesFrom = &Continuation{
				CollectionID: c.ID, CollectionTitle: c.Title,
				AfterID: film.ID, AfterTitle: film.Title,
			}
			return it, nil
		}
	}
	return nil, nil
}

/*
 * SimilarFilms is up to limit unwatched films from itemID's library that share
 * something with it, best first, excluding everything in any collection it
 * belongs to.
 *
 * Shared means a genre, the director, or one of the first five billed actors —
 * the cast a person would recognise, not the forty-third credit. Weighted so a
 * shared director outranks a single shared genre and two shared genres outrank
 * one shared lead: genres are broad ("Drama" alone matches half a library), so
 * they count by how many are shared. Ties go to the nearer year, then the
 * higher rating, then the id, so the answer is stable.
 *
 * A film with nothing in common is never offered. A suggestion that cannot say
 * where it came from is the kind this project does not show.
 *
 * Another copy of this film (an edition, a second rip) shares every genre and
 * every credit and would top the list, so a film with the same sort title is
 * not "similar", it is the same film.
 */
func (s *Store) SimilarFilms(ctx context.Context, itemID int64, userID string, limit int) ([]Item, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH src AS (SELECT id, library_id, sort_title, year FROM media_item WHERE id = ?1),
		g AS (SELECT genre_id FROM item_genre WHERE item_id = ?1),
		p AS (SELECT person_id, role FROM credit
		      WHERE item_id = ?1 AND (role = 'director' OR (role = 'actor' AND ord < 5))),
		fam AS (SELECT b.item_id FROM item_collection a
		        JOIN item_collection b ON b.collection_id = a.collection_id
		        WHERE a.item_id = ?1),
		scored AS (
			SELECT mi.id, mi.year, mi.rating,
			  (SELECT COUNT(*) FROM item_genre ig JOIN g ON g.genre_id = ig.genre_id
			   WHERE ig.item_id = mi.id) AS genres,
			  (SELECT COUNT(*) FROM credit c JOIN p ON p.person_id = c.person_id AND p.role = c.role
			   WHERE c.item_id = mi.id AND c.role = 'director') AS directors,
			  (SELECT COUNT(*) FROM credit c JOIN p ON p.person_id = c.person_id AND p.role = c.role
			   WHERE c.item_id = mi.id AND c.role = 'actor' AND c.ord < 5) AS actors
			FROM media_item mi, src
			WHERE mi.library_id = src.library_id
			  AND mi.kind = 'movie' AND mi.missing = 0
			  AND mi.id <> src.id AND mi.sort_title <> src.sort_title
			  AND mi.id NOT IN (SELECT item_id FROM fam)
			  AND NOT EXISTS (SELECT 1 FROM playback_state ps
			                  WHERE ps.item_id = mi.id AND ps.user_id = ?2 AND ps.watched = 1)
		)
		SELECT scored.id FROM scored, src
		WHERE genres + directors + actors > 0
		ORDER BY 2*genres + 3*directors + actors DESC,
		  ABS(COALESCE(scored.year, 0) - COALESCE(src.year, 0)),
		  scored.rating IS NULL, scored.rating DESC,
		  scored.id
		-- Over-fetched: the rating ceiling is applied per film below, and a
		-- restricted account should still get a full answer.
		LIMIT ?3`, itemID, userID, limit*4)
	if err != nil {
		return nil, fmt.Errorf("similar films: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("similar films: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("similar films: %w", err)
	}

	out := make([]Item, 0, limit)
	for _, id := range ids {
		it, err := s.GetItem(ctx, id, userID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("similar films: %w", err)
		}
		out = append(out, *it)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}
