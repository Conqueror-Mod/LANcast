package store

import (
	"context"
	"fmt"
)

/*
 * Seasons waiting for intro detection (ADR 0055).
 *
 * The unit of work here is a season rather than a file, which is the whole
 * difference between this and every other worker in the project. An episode
 * cannot be examined alone: an intro is what several episodes *share*, so one
 * episode carries no evidence about itself.
 */

// Season is a group of episodes to compare against one another.
type Season struct {
	ShowID   int64
	ShowName string
	// Season is the season number. Episodes with no season number are grouped
	// under a single bucket rather than dropped — a flat show directory is a
	// real shape, and its episodes still share an intro.
	Season   int
	Episodes []Item
}

/*
 * PendingIntroSeasons returns seasons where some episode has not been examined.
 *
 * A season is offered whole even when only one of its episodes is pending,
 * because the comparison needs the others. That means an episode added later
 * re-examines its season, which is correct: a new episode is new evidence, and
 * a season whose intro was found from four episodes is not harmed by asking
 * five.
 *
 * Seasons with fewer than minEpisodes are skipped rather than failed. One
 * episode has nothing to compare against, and saying so by absence keeps them
 * out of the queue for ever instead of failing on every pass.
 */
func (s *Store) PendingIntroSeasons(ctx context.Context, minEpisodes, limit int) ([]Season, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	if minEpisodes < 2 {
		minEpisodes = 2
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT sh.id, sh.title, COALESCE(e.season, 0) AS se
		FROM media_item e
		LEFT JOIN media_item par ON par.id = e.parent_id
		JOIN media_item sh ON sh.id = COALESCE(par.parent_id, e.parent_id)
		WHERE e.kind = 'episode' AND e.missing = 0 AND sh.kind = 'show'
		  AND e.path IS NOT NULL AND e.probed_at IS NOT NULL
		GROUP BY sh.id, se
		HAVING COUNT(*) >= ?
		   AND SUM(CASE WHEN e.intros_at IS NULL THEN 1 ELSE 0 END) > 0
		ORDER BY sh.title, se
		LIMIT ?`, minEpisodes, limit)
	if err != nil {
		return nil, fmt.Errorf("pending intro seasons: %w", err)
	}
	defer rows.Close()

	var out []Season
	for rows.Next() {
		var s2 Season
		if err := rows.Scan(&s2.ShowID, &s2.ShowName, &s2.Season); err != nil {
			return nil, fmt.Errorf("pending intro seasons: %w", err)
		}
		out = append(out, s2)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range out {
		eps, err := s.seasonEpisodes(ctx, out[i].ShowID, out[i].Season)
		if err != nil {
			return nil, err
		}
		out[i].Episodes = eps
	}
	return out, nil
}

// seasonEpisodes lists one season's episodes in playing order.
func (s *Store) seasonEpisodes(ctx context.Context, showID int64, season int) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+qualifyCols(itemCols, "e")+`
		FROM media_item e
		LEFT JOIN media_item par ON par.id = e.parent_id
		WHERE e.kind = 'episode' AND e.missing = 0
		  AND COALESCE(par.parent_id, e.parent_id) = ?
		  AND COALESCE(e.season, 0) = ?
		  AND e.path IS NOT NULL AND e.probed_at IS NOT NULL
		ORDER BY e.episode, e.id`, showID, season)
	if err != nil {
		return nil, fmt.Errorf("season episodes: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

/*
 * MarkIntrosExamined stamps every episode of a season as looked at.
 *
 * Stamped in one statement over the whole season, and stamped even where no
 * intro was found, for the reason SaveMarkers stamps an abstention: a show
 * with no shared audio is an answer, and re-deciding it on every pass is how a
 * library of such shows never stops decoding.
 */
func (s *Store) MarkIntrosExamined(ctx context.Context, episodeIDs []int64, at int64) error {
	if len(episodeIDs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mark intros examined: %w", err)
	}
	defer tx.Rollback()
	for _, id := range episodeIDs {
		if _, err := tx.ExecContext(ctx,
			`UPDATE media_item SET intros_at = ? WHERE id = ?`, at, id); err != nil {
			return fmt.Errorf("mark intros examined: %w", err)
		}
	}
	return tx.Commit()
}

// ClearIntros queues every examined episode to be looked at again.
func (s *Store) ClearIntros(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE media_item SET intros_at = NULL
		WHERE intros_at IS NOT NULL AND kind = 'episode' AND missing = 0`)
	if err != nil {
		return 0, fmt.Errorf("clear intros: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("clear intros: %w", err)
	}
	return n, nil
}
