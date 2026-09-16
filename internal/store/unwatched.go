package store

import (
	"context"
	"fmt"
	"strings"
)

/*
 * How much of a show an account has left.
 *
 * This exists so a tile can show a finished-tick on a series without asking a
 * question per row. A film answers for itself — it has a playback_state row and
 * the flag is on it — but a show has no row of its own and never will: nobody
 * plays a show, they play its episodes. So "have I seen this series" is an
 * aggregate, and until now no listing carried one.
 *
 * The count is returned rather than a boolean, and that is worth a sentence.
 * "Finished" is one reading of it and the only one the tile uses today; "4
 * left" is the other, and a shelf that wanted to say so would otherwise need a
 * second query answering a question this one already asked.
 */

/*
 * How an episode hangs off a show.
 *
 * Two shapes, both real: usually the episode's parent is its season and the
 * season's parent is the show, but a loose episode files directly under the
 * show. NextEpisodeFor matches both with a LEFT JOIN and so does the query
 * below, resolving them to one id with COALESCE so the grouping is by show
 * either way.
 *
 * Matching the same pair matters more than it looks. If this counted a
 * different set of episodes than the code deciding what to play next, a show
 * could be ticked as finished while Continue Watching still offered it an
 * episode — two features disagreeing on screen, with nothing failing.
 */

/*
 * AttachUnwatchedEpisodes fills UnwatchedEpisodes on every show in a page.
 *
 * One query for the page, like AttachProgress and AttachChildCounts, because
 * the alternative is a query per tile on a grid of two hundred.
 *
 * Items that are not shows are left nil — the question does not apply to a
 * film, an album or a photograph, and answering zero for them would tick
 * every tile in a music library as finished. That is not hypothetical
 * caution: the rating ceiling shipped a bug of exactly this shape by
 * answering a video question for kinds that never had video.
 *
 * A show with no episodes at all is also left nil rather than zero. An empty
 * series is not a finished one, and "nothing left to watch" is a true sentence
 * about it that means the opposite of what a tick would say.
 */
func (s *Store) AttachUnwatchedEpisodes(ctx context.Context, items []Item, userID string) error {
	if len(items) == 0 {
		return nil
	}

	shows := make([]*Item, 0, len(items))
	for i := range items {
		if items[i].Kind == "show" {
			shows = append(shows, &items[i])
		}
	}
	if len(shows) == 0 {
		return nil
	}

	ph := make([]string, len(shows))
	// The user id leads, because it is bound inside the SELECT before the id
	// list reached by the WHERE.
	args := make([]any, 0, len(shows)+1)
	args = append(args, userID)
	for i, it := range shows {
		ph[i] = "?"
		args = append(args, it.ID)
	}
	in := strings.Join(ph, ",")

	/*
	 * Counted per show in one pass: total episodes, and how many of them this
	 * account has finished.
	 *
	 * The show an episode belongs to is `COALESCE(s.parent_id, e.parent_id)` —
	 * the season's parent where there is a season, and the episode's own parent
	 * where the episode hangs directly off the show. Same two shapes the join
	 * above matches, resolved to one id so the grouping is by show either way.
	 *
	 * `watched = 1` and nothing else. An episode stopped at 90% is not one you
	 * have seen, and the continue-watching semantics say the same — an
	 * in-progress episode is unwatched. A tick that appeared while the last
	 * episode was still half-finished would be wrong in the direction that
	 * matters, because the whole point of the mark is knowing what is left.
	 */
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(s.parent_id, e.parent_id) AS show_id,
		       COUNT(*) AS total,
		       SUM(CASE WHEN EXISTS (
		             SELECT 1 FROM playback_state ps
		             WHERE ps.item_id = e.id AND ps.user_id = ? AND ps.watched = 1
		           ) THEN 1 ELSE 0 END) AS seen
		FROM media_item e
		LEFT JOIN media_item s ON s.id = e.parent_id
		WHERE e.kind = 'episode' AND e.missing = 0
		  AND COALESCE(s.parent_id, e.parent_id) IN (`+in+`)
		GROUP BY show_id`, args...)
	if err != nil {
		return fmt.Errorf("attach unwatched episodes: %w", err)
	}
	defer rows.Close()

	type tally struct{ total, seen int }
	byShow := map[int64]tally{}
	for rows.Next() {
		var id int64
		var t tally
		if err := rows.Scan(&id, &t.total, &t.seen); err != nil {
			return fmt.Errorf("attach unwatched episodes: %w", err)
		}
		byShow[id] = t
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, it := range shows {
		t, ok := byShow[it.ID]
		if !ok || t.total == 0 {
			// No episodes on disk. Left nil: an empty series is not a finished
			// one.
			continue
		}
		left := t.total - t.seen
		if left < 0 {
			left = 0
		}
		n := left
		it.UnwatchedEpisodes = &n
	}
	return nil
}
