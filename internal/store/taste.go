package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

/*
 * What somebody has been finishing lately, as one genre (ADR 0119 §1).
 *
 * The Suggested spotlight seeds from what you are part-way through, and a
 * household that finishes what it starts has nothing part-way through. This is
 * its fallback: the genre your recent finishes share most, and how many of
 * them share it, so the spotlight can say "Because you've finished 6 Thriller
 * titles lately" and have the number be true.
 *
 * Counted by title, not by row. Ten episodes of one show are one title; a
 * binge would otherwise outvote a month of films. An episode counts as its
 * show, whose genres are the ones a provider gave.
 *
 * Estimated rows are counted: a rough date is fine for "lately", where it is
 * not for "on this day" (ADR 0119 §3).
 */

// TasteWindow is how many recent finishes the taste is read from.
const TasteWindow = 20

// TasteMinimum is the fewest titles a genre needs before the spotlight may
// claim it as a pattern. Below it, "lately" would be a claim about one film.
const TasteMinimum = 3

// Taste is the genre a person's recent finishes share most.
type Taste struct {
	Genre string
	// Titles is how many of the recent finished titles carry the genre.
	Titles int
}

// RecentTaste reads the genre the account's last TasteWindow finished films
// and episodes share most. ok is false when no genre reaches TasteMinimum.
// Ties go to the genre finished most recently.
func (s *Store) RecentTaste(ctx context.Context, userID string) (Taste, bool, error) {
	var t Taste
	err := s.db.QueryRowContext(ctx, `
		WITH recent AS (
			SELECT v.finished_at, v.id,
			       CASE WHEN mi.kind = 'episode' THEN (`+showOf+`) ELSE mi.id END AS title_id
			FROM viewing v
			JOIN media_item mi ON mi.id = v.item_id
			WHERE v.user_id = ? AND v.kind IN ('movie', 'episode')
			ORDER BY v.finished_at DESC, v.id DESC
			LIMIT ?
		),
		titles AS (
			SELECT title_id, MAX(finished_at) AS last
			FROM recent WHERE title_id IS NOT NULL
			GROUP BY title_id
		)
		SELECT g.name, COUNT(*) AS n
		FROM titles t
		JOIN item_genre ig ON ig.item_id = t.title_id
		JOIN genre g ON g.id = ig.genre_id
		GROUP BY g.id
		HAVING n >= ?
		ORDER BY n DESC, MAX(t.last) DESC, g.name
		LIMIT 1`,
		userID, TasteWindow, TasteMinimum).Scan(&t.Genre, &t.Titles)
	if errors.Is(err, sql.ErrNoRows) {
		return Taste{}, false, nil
	}
	if err != nil {
		return Taste{}, false, fmt.Errorf("recent taste: %w", err)
	}
	return t, true, nil
}
