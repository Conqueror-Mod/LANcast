package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// itemColsE is the item column list qualified for the episode alias used
// throughout this file, so a join cannot make a bare column ambiguous.
var itemColsE = qualifyCols(itemCols, "e")

/*
 * Where a show is up to.
 *
 * This is the query behind Continue, and the rule it implements is the whole
 * point of the feature. The failure being designed against is a real one, lived
 * with daily in Plex: press continue on a seventeen-season show and land three
 * episodes back, on something already watched, over and over.
 *
 * That is a *stale read* — the server knows episode 14 was watched and answers
 * with 11 anyway, because something in between is holding an older picture of
 * the truth. So the defence is not a better cache. It is having no cache: this
 * is computed from playback_state at the moment it is asked, on every press,
 * and the handler says no-store so nothing between here and the button may keep
 * the answer.
 */

// NextEpisode is where a show should resume.
type NextEpisode struct {
	// Item is the episode to play. Nil when the show is finished.
	Item *Item
	// Resume is true when this episode was already started, so the client can
	// say "resume" rather than "play" and the player can seek.
	Resume bool
	// Exhausted is true when every episode has been watched — a finished show
	// rather than an empty one, which the UI has to word differently.
	Exhausted bool
}

/*
 * NextEpisodeFor answers where to continue a show for one user.
 *
 * The rule, in order:
 *
 *  1. **Something in progress wins**, most recently touched first. That is the
 *     episode being watched right now, and it is the answer whatever else the
 *     numbering says.
 *  2. Otherwise, **the first unwatched episode after the furthest one watched**.
 *     Not simply "the earliest unwatched" — that is the backtracking bug written
 *     as a query. Skip episode 5 and watch through 13, and earliest-unwatched
 *     sends you back to 5 every time you press continue, which is precisely the
 *     complaint. Progress through a series only ever moves forward.
 *  3. Nothing watched at all: the first episode.
 *  4. Everything watched: exhausted, and the caller offers to start again rather
 *     than silently replaying the finale.
 *
 * Ordering is by season then episode, with the row id as a final tiebreak so it
 * is total: two episodes with the same numbers must not swap places between
 * calls, or "next" is not a function.
 *
 * Specials are included, and that is a judgement rather than an oversight:
 * season 0 sorts first, so a show whose specials are numbered that way begins
 * with them. Anyone who does not want that can watch past them once, and rule 2
 * then keeps them behind.
 */
func (s *Store) NextEpisodeFor(ctx context.Context, showID int64, userID string) (NextEpisode, error) {
	/*
	 * Episodes belong to a season, which belongs to the show, but a library can
	 * legitimately hold an episode parented straight to the show (shapecheck
	 * allows the loose case), so both are matched rather than assuming two
	 * levels.
	 */
	const from = `
		FROM media_item e
		LEFT JOIN media_item s ON s.id = e.parent_id
		WHERE e.kind = 'episode' AND e.missing = 0
		  AND (e.parent_id = ? OR s.parent_id = ?)`

	// 1. In progress, most recently touched.
	row := s.db.QueryRowContext(ctx, `SELECT `+itemColsE+from+`
		  AND EXISTS (SELECT 1 FROM playback_state ps
		              WHERE ps.item_id = e.id AND ps.user_id = ?
		                AND ps.watched = 0 AND ps.position_ms > 0)
		ORDER BY (SELECT ps.updated_at FROM playback_state ps
		          WHERE ps.item_id = e.id AND ps.user_id = ?) DESC,
		         e.season, e.episode, e.id
		LIMIT 1`, showID, showID, userID, userID)
	it, err := scanItem(row)
	if err == nil {
		// Re-read through GetItem so the caller gets the saved position with the
		// episode: the client seeks to it, and a resume without a position is
		// just a play.
		full, perr := s.GetItem(ctx, it.ID, userID)
		if perr != nil {
			return NextEpisode{}, perr
		}
		return NextEpisode{Item: full, Resume: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return NextEpisode{}, fmt.Errorf("next episode (in progress): %w", err)
	}

	/*
	 * 2. The first unwatched episode that sits after the furthest watched one.
	 *
	 * Compared as a pair — season first, then episode — because a plain
	 * "episode > highest" would step across a season boundary wrongly, and a
	 * plain "watched = 0 ordered by season, episode" is the backtracking bug.
	 */
	row = s.db.QueryRowContext(ctx, `
		WITH watched AS (
			SELECT COALESCE(MAX(e.season), -1) AS s
			`+from+`
			  AND EXISTS (SELECT 1 FROM playback_state ps
			              WHERE ps.item_id = e.id AND ps.user_id = ? AND ps.watched = 1)
		),
		furthest AS (
			SELECT (SELECT s FROM watched) AS season,
			       COALESCE((SELECT MAX(e.episode) `+from+`
			                   AND e.season = (SELECT s FROM watched)
			                   AND EXISTS (SELECT 1 FROM playback_state ps
			                               WHERE ps.item_id = e.id AND ps.user_id = ?
			                                 AND ps.watched = 1)), -1) AS episode
		)
		SELECT `+itemColsE+from+`
		  AND NOT EXISTS (SELECT 1 FROM playback_state ps
		                  WHERE ps.item_id = e.id AND ps.user_id = ? AND ps.watched = 1)
		  AND (e.season > (SELECT season FROM furthest)
		       OR (e.season = (SELECT season FROM furthest)
		           AND e.episode > (SELECT episode FROM furthest)))
		ORDER BY e.season, e.episode, e.id
		LIMIT 1`,
		showID, showID, userID, // watched
		showID, showID, userID, // furthest
		showID, showID, userID) // the selection
	it, err = scanItem(row)
	if err == nil {
		full, perr := s.GetItem(ctx, it.ID, userID)
		if perr != nil {
			return NextEpisode{}, perr
		}
		return NextEpisode{Item: full}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return NextEpisode{}, fmt.Errorf("next episode (after furthest): %w", err)
	}

	/*
	 * 3/4. Nothing after the furthest watched. Either the show has never been
	 * touched — in which case the first episode is the answer — or it is
	 * finished, and saying so is better than replaying the finale.
	 */
	first, err := s.FirstEpisodeOf(ctx, showID, userID)
	if err != nil {
		return NextEpisode{}, err
	}
	if first == nil {
		return NextEpisode{}, nil
	}
	watchedAny, err := s.hasWatchedEpisode(ctx, showID, userID)
	if err != nil {
		return NextEpisode{}, err
	}
	if watchedAny {
		return NextEpisode{Exhausted: true}, nil
	}
	return NextEpisode{Item: first}, nil
}

// FirstEpisodeOf is the show's opening episode, which is also what "play from
// the beginning" starts on.
func (s *Store) FirstEpisodeOf(ctx context.Context, showID int64, userID string) (*Item, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+itemColsE+`
		FROM media_item e
		LEFT JOIN media_item s ON s.id = e.parent_id
		WHERE e.kind = 'episode' AND e.missing = 0
		  AND (e.parent_id = ? OR s.parent_id = ?)
		ORDER BY e.season, e.episode, e.id
		LIMIT 1`, showID, showID)
	it, err := scanItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("first episode: %w", err)
	}
	return s.GetItem(ctx, it.ID, userID)
}

// hasWatchedEpisode reports whether any episode of the show has been finished,
// which is what tells a finished show from an untouched one.
func (s *Store) hasWatchedEpisode(ctx context.Context, showID int64, userID string) (bool, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM media_item e
			LEFT JOIN media_item s ON s.id = e.parent_id
			JOIN playback_state ps ON ps.item_id = e.id AND ps.user_id = ?
			WHERE e.kind = 'episode' AND e.missing = 0
			  AND (e.parent_id = ? OR s.parent_id = ?) AND ps.watched = 1)`,
		userID, showID, showID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("has watched episode: %w", err)
	}
	return ok, nil
}

/*
 * EpisodesOf returns a show's episodes in playing order.
 *
 * For the two queue buttons: play from the beginning, and randomize. Ordered
 * the same way NextEpisodeFor orders, so "next" and "the queue" cannot disagree
 * about what follows what.
 */
func (s *Store) EpisodesOf(ctx context.Context, showID int64) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemColsE+`
		FROM media_item e
		LEFT JOIN media_item s ON s.id = e.parent_id
		WHERE e.kind = 'episode' AND e.missing = 0
		  AND (e.parent_id = ? OR s.parent_id = ?)
		ORDER BY e.season, e.episode, e.id`, showID, showID)
	if err != nil {
		return nil, fmt.Errorf("episodes of: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}
