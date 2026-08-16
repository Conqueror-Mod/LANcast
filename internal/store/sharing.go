package store

import (
	"context"
	"fmt"
)

/*
 * Who may see whose viewing (ADR 0035).
 *
 * Private by default, shared only by an explicit per-account opt-in. Every
 * query here reads `share_activity` rather than trusting a caller to have
 * checked it, because the failure mode is silent and unrecoverable: a listing
 * that forgets the filter publishes somebody's evening, and you cannot un-show
 * a history.
 *
 * The opt-in shares **what was watched and finished** — titles, and when. It
 * does not share ratings or reviews, which stay private unconditionally, and it
 * does not share resume positions, which say where somebody stopped rather than
 * what they watched.
 */

// Person is another account as it appears to somebody else on this server.
type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
	// Sharing says whether this person publishes their activity. Reported even
	// when false, so a people page can say "has not shared" rather than showing
	// an empty list that reads as "watches nothing".
	Sharing bool `json:"sharing"`
	// Watched is how many titles they have finished, and is zero unless they
	// share. A count is still a fact about a person.
	Watched  int   `json:"watched"`
	JoinedAt int64 `json:"joined_at"`
}

// SetShareActivity records one person's own decision. There is deliberately no
// admin-facing variant: a switch somebody else can flip is not consent.
func (s *Store) SetShareActivity(ctx context.Context, userID string, on bool) error {
	v := 0
	if on {
		v = 1
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE user SET share_activity = ? WHERE id = ?`, v, userID)
	if err != nil {
		return fmt.Errorf("set share activity: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SharesActivity(ctx context.Context, userID string) (bool, error) {
	var v int
	err := s.db.QueryRowContext(ctx,
		`SELECT share_activity FROM user WHERE id = ?`, userID).Scan(&v)
	if err != nil {
		return false, fmt.Errorf("get share activity: %w", err)
	}
	return v != 0, nil
}

/*
 * People lists the other accounts on this server.
 *
 * The caller is excluded: a people page is about everybody else, and a row for
 * yourself in a list of other people is noise you have to read past every time.
 *
 * The watched count is computed only for those who share. Doing it in the query
 * rather than filtering afterwards means there is no branch where the number is
 * fetched and then dropped — the shape that eventually leaks it.
 */
func (s *Store) People(ctx context.Context, excludeUserID string) ([]Person, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.name, u.role, u.share_activity, u.created_at,
		       CASE WHEN u.share_activity = 1 THEN (
		         SELECT COUNT(*) FROM playback_state ps
		         WHERE ps.user_id = u.id AND ps.watched = 1
		       ) ELSE 0 END
		FROM user u
		WHERE u.id != ?
		ORDER BY u.name`, excludeUserID)
	if err != nil {
		return nil, fmt.Errorf("list people: %w", err)
	}
	defer rows.Close()

	out := []Person{}
	for rows.Next() {
		var p Person
		var sharing int
		if err := rows.Scan(&p.ID, &p.Name, &p.Role, &sharing, &p.JoinedAt, &p.Watched); err != nil {
			return nil, fmt.Errorf("list people: %w", err)
		}
		p.Sharing = sharing != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

/*
 * SharedActivity returns what one person has been watching — and refuses unless
 * they have opted in.
 *
 * The check is inside the query rather than before it, so there is no arrangement
 * of calls that returns rows for somebody who did not consent. A caller that
 * forgets to check gets an empty list, which is the correct answer.
 *
 * Finished titles only. A resume position says where somebody stopped, which is
 * a different and more intrusive fact than what they watched, and ADR 0035
 * excludes it.
 */
func (s *Store) SharedActivity(ctx context.Context, userID string, limit int) ([]HistoryEntry, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemColsMI+`,
		    ps.position_ms, ps.watched, ps.updated_at
		FROM media_item mi
		JOIN playback_state ps ON ps.item_id = mi.id
		JOIN user u ON u.id = ps.user_id
		WHERE ps.user_id = ?
		  AND u.share_activity = 1
		  AND ps.watched = 1
		ORDER BY ps.updated_at DESC, mi.id DESC
		LIMIT ?`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("shared activity: %w", err)
	}
	defer rows.Close()

	out := []HistoryEntry{}
	for rows.Next() {
		var e HistoryEntry
		var watched int
		it, err := scanItemThen(rows, &e.PositionMS, &watched, &e.PlayedAt)
		if err != nil {
			return nil, fmt.Errorf("shared activity: %w", err)
		}
		e.Item = *it
		e.Watched = watched != 0
		// The position is deliberately not reported onward: it is fetched only
		// because the row carries it, and ADR 0035 excludes it from sharing.
		e.PositionMS = 0
		out = append(out, e)
	}
	return out, rows.Err()
}
