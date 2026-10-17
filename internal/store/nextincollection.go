package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

/*
 * Where a collection is up to.
 *
 * A franchise is watched in order the way a show is, and the question is the
 * same one Continue answers for a show: which film now? So the answer has the
 * same shape (NextEpisode, whose Item is a film here) and the same rule, read
 * over the collection's members in release order — the order CollectionMembers
 * lists them in, which is how a franchise is watched:
 *
 *  1. A film in progress wins, most recently touched first.
 *  2. Otherwise the first unwatched film after the one finished most
 *     recently. Forward only: skipping the second film and watching the
 *     third does not send you back to the second every time, which is the
 *     backtracking bug NextEpisodeFor was written against.
 *  3. Nothing watched: the first film.
 *  4. Everything after the one finished most recently is watched: exhausted.
 *
 * Rule 2 was "after the furthest watched", which is the show's rule, and a
 * franchise is not watched like a show. Reported: finishing Saw V offered
 * Jigsaw, not Saw VI. Saw 3D had been watched out of order, before Saw IV,
 * and as the furthest film watched it outranked everything finished after
 * it. The film just finished is the one the shelf's sentence names
 * ("Because you watched Saw V"), so it is the one to count from.
 *
 * Computed on every ask, with no cache, for the reason the show's is.
 */
func (s *Store) NextInCollectionFor(ctx context.Context, collectionID int64, userID string) (NextEpisode, error) {
	next, _, err := s.nextInCollection(ctx, collectionID, userID)
	return next, err
}

// nextInCollection is NextInCollectionFor plus the film the answer follows:
// the one finished most recently, which rule 2 counts from.
func (s *Store) nextInCollection(ctx context.Context, collectionID int64, userID string) (NextEpisode, *Item, error) {
	members, err := s.CollectionMembers(ctx, collectionID)
	if err != nil {
		return NextEpisode{}, nil, err
	}
	/*
	 * Each member through GetItem, for two reasons: it carries this user's
	 * progress, which the members query does not, and it applies the
	 * account's rating ceiling — a film this account may not see is not the
	 * next one in its franchise, it is absent.
	 */
	seen := make([]Item, 0, len(members))
	touched := make([]int64, 0, len(members))
	for _, m := range members {
		it, err := s.GetItem(ctx, m.ID, userID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return NextEpisode{}, nil, fmt.Errorf("next in collection: %w", err)
		}
		at, err := s.progressUpdatedAt(ctx, it.ID, userID)
		if err != nil {
			return NextEpisode{}, nil, err
		}
		seen = append(seen, *it)
		touched = append(touched, at)
	}
	next, from := nextInSequence(seen, touched)
	var after *Item
	if next.Item != nil && from >= 0 {
		after = &seen[from]
	}
	return next, after, nil
}

/*
 * nextInSequence is the rule, over members already in watching order, with
 * when each one's progress was last written (0 for never). Pure, so the rule
 * is tested without a database. from is the index of the film the answer
 * counts from (the one finished most recently), or -1.
 */
func nextInSequence(items []Item, touched []int64) (next NextEpisode, from int) {
	started := false
	latest := -1
	inProgress := -1
	for i, it := range items {
		p := it.Progress
		if p == nil {
			continue
		}
		if p.Watched {
			started = true
			if latest < 0 || touched[i] >= touched[latest] {
				latest = i
			}
			continue
		}
		if p.PositionMS > 0 {
			started = true
			if inProgress < 0 || touched[i] > touched[inProgress] {
				inProgress = i
			}
		}
	}
	if inProgress >= 0 {
		it := items[inProgress]
		return NextEpisode{Item: &it, Resume: true, Started: true}, -1
	}
	for i := latest + 1; i < len(items); i++ {
		if p := items[i].Progress; p != nil && p.Watched {
			continue
		}
		it := items[i]
		return NextEpisode{Item: &it, Started: started}, latest
	}
	if len(items) == 0 {
		return NextEpisode{}, -1
	}
	return NextEpisode{Exhausted: true, Started: true}, -1
}

// progressUpdatedAt is when this user's play state for an item was last
// written, or 0 when it never has been.
func (s *Store) progressUpdatedAt(ctx context.Context, itemID int64, userID string) (int64, error) {
	var at int64
	err := s.db.QueryRowContext(ctx,
		`SELECT updated_at FROM playback_state WHERE item_id = ? AND user_id = ?`,
		itemID, userID).Scan(&at)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("progress time: %w", err)
	}
	return at, nil
}

/*
 * collectionNext is a collection's row on Continue Watching: the film after
 * the one just finished, as Next up is for a show (settled 2026-10-12).
 *
 * finishedAt is when a film in it was last finished. The row is the next
 * film itself, not the collection: it is what pressing the tile plays, and
 * it draws its own poster. Absent when:
 *
 *   - the collection is exhausted, or nothing follows;
 *   - the next film is already in progress, because then it is on the shelf
 *     on its own account, with its bar;
 *   - the next film's own state was written after that finish. That is how
 *     "Remove from Continue Watching" works here with nothing new to store
 *     removing writes the film's state (unwatched, position zero), which is
 *     later than the finish, and finishing another film in the collection
 *     later brings the row back for the film after that one.
 */
func (s *Store) collectionNext(ctx context.Context, collectionID, finishedAt int64, userID string) (*Item, bool, error) {
	next, after, err := s.nextInCollection(ctx, collectionID, userID)
	if err != nil {
		return nil, false, err
	}
	if next.Item == nil || next.Exhausted || next.Resume || after == nil {
		return nil, false, nil
	}
	at, err := s.progressUpdatedAt(ctx, next.Item.ID, userID)
	if err != nil {
		return nil, false, err
	}
	if at >= finishedAt {
		return nil, false, nil
	}
	c, err := s.GetItem(ctx, collectionID, userID)
	if err != nil {
		return nil, false, err
	}
	next.Item.ContinuesFrom = &Continuation{
		CollectionID: collectionID, CollectionTitle: c.Title,
		AfterID: after.ID, AfterTitle: after.Title,
	}
	return next.Item, true, nil
}
