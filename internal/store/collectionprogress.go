package store

import (
	"context"
	"fmt"
	"strings"
)

/*
 * CollectionProgress is an account's standing in one collection.
 *
 * Total counts the same members child_count does (present ones), so a tile's
 * "2 of 5 watched" can never disagree with the "5 films" it replaces.
 * InProgress is members started and not finished: what tells a box set somebody
 * has begun apart from one they have not, when nothing in it is finished yet.
 */
type CollectionProgress struct {
	Total      int `json:"total"`
	Watched    int `json:"watched"`
	InProgress int `json:"in_progress"`
}

/*
 * AttachCollectionProgress fills CollectionProgress on every collection in a
 * page, in one query, like AttachUnwatchedEpisodes beside it.
 *
 * `watched = 1` is finished, as for an episode. In progress is a saved position
 * on a film not yet finished — the same reading Continue Watching makes.
 *
 * A collection with no films on disk is left nil: an empty box set is not a
 * finished one, and a tick on it would say the opposite of the truth.
 */
func (s *Store) AttachCollectionProgress(ctx context.Context, items []Item, userID string) error {
	var colls []*Item
	for i := range items {
		if items[i].Kind == "collection" {
			colls = append(colls, &items[i])
		}
	}
	if len(colls) == 0 {
		return nil
	}
	ph := make([]string, len(colls))
	// The user id leads: it is bound in the JOIN, before the id list.
	args := make([]any, 0, len(colls)+1)
	args = append(args, userID)
	for i, it := range colls {
		ph[i] = "?"
		args = append(args, it.ID)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT ic.collection_id,
		       COUNT(*),
		       SUM(CASE WHEN ps.watched = 1 THEN 1 ELSE 0 END),
		       SUM(CASE WHEN COALESCE(ps.watched, 0) = 0 AND COALESCE(ps.position_ms, 0) > 0
		                THEN 1 ELSE 0 END)
		FROM item_collection ic
		JOIN media_item m ON m.id = ic.item_id
		LEFT JOIN playback_state ps ON ps.item_id = m.id AND ps.user_id = ?
		WHERE ic.collection_id IN (`+strings.Join(ph, ",")+`) AND m.missing = 0
		GROUP BY ic.collection_id`, args...)
	if err != nil {
		return fmt.Errorf("attach collection progress: %w", err)
	}
	defer rows.Close()
	byID := map[int64]CollectionProgress{}
	for rows.Next() {
		var id int64
		var p CollectionProgress
		if err := rows.Scan(&id, &p.Total, &p.Watched, &p.InProgress); err != nil {
			return fmt.Errorf("attach collection progress: %w", err)
		}
		byID[id] = p
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, it := range colls {
		p, ok := byID[it.ID]
		if !ok || p.Total == 0 {
			continue
		}
		it.CollectionProgress = &p
	}
	return nil
}
