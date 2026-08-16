package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

/*
 * Your own rating, and your own note about why.
 *
 * The roadmap defers "ratings/reviews" alongside viewer stats because both wait
 * on a decision about who may see whose viewing that nobody has made. This is
 * the smaller half of that decision, made deliberately and narrowly:
 *
 *   **Your rating is yours.** It is stored per user, returned to you, and
 *   aggregated for nobody. There is no household average, no "3 people rated
 *   this", and no way to read somebody else's score.
 *
 * That is not a limitation to be lifted casually later. Turning private
 * verdicts into visible ones changes what people are willing to write, so it is
 * a decision about the product rather than a feature flag — and it stays unmade
 * until someone makes it.
 *
 * Distinct from the provider rating already on media_item, which is TMDB's
 * opinion, and from the external ratings of ADR 0019, which are IMDb's and
 * Rotten Tomatoes'. Three numbers about one film is one too many to leave
 * unlabelled, so the API never mixes them into a single field.
 */

// MinScore and MaxScore bound a rating. Out of ten rather than five so a
// half-star interface needs no migration, and because the provider ratings this
// sits beside are already out of ten.
const (
	MinScore = 1
	MaxScore = 10
)

// Rating is one person's verdict on one item.
type Rating struct {
	ItemID int64 `json:"item_id"`
	Score  int   `json:"score"`
	// Review is a note to yourself. Nullable, because a score with no words is
	// the common case and an empty string would be indistinguishable from a
	// review somebody deliberately cleared.
	Review    *string `json:"review,omitempty"`
	UpdatedAt int64   `json:"updated_at"`
}

// RatedItem is a rating with the thing it is about, for the profile listing.
type RatedItem struct {
	Item   Item   `json:"item"`
	Rating Rating `json:"rating"`
}

// SetRating records or replaces one person's verdict. A review of "" clears the
// note while keeping the score: they are two separate things somebody may want
// to change independently.
func (s *Store) SetRating(ctx context.Context, itemID int64, userID string, score int, review string) error {
	if score < MinScore || score > MaxScore {
		return fmt.Errorf("rating score %d out of range %d–%d", score, MinScore, MaxScore)
	}
	var note any
	if review != "" {
		note = review
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_rating (item_id, user_id, score, review, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(item_id, user_id) DO UPDATE SET
		  score = excluded.score,
		  review = excluded.review,
		  updated_at = excluded.updated_at`,
		itemID, userID, score, note, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("set rating: %w", err)
	}
	return nil
}

// ClearRating removes a verdict entirely, which is different from scoring
// something low: "I have not rated this" and "I rated this 1" are different
// statements and the interface has to be able to say both.
func (s *Store) ClearRating(ctx context.Context, itemID int64, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM user_rating WHERE item_id = ? AND user_id = ?`, itemID, userID)
	if err != nil {
		return fmt.Errorf("clear rating: %w", err)
	}
	return nil
}

// GetRating returns this user's verdict, or nil when they have not given one.
func (s *Store) GetRating(ctx context.Context, itemID int64, userID string) (*Rating, error) {
	var r Rating
	err := s.db.QueryRowContext(ctx, `
		SELECT item_id, score, review, updated_at
		FROM user_rating WHERE item_id = ? AND user_id = ?`, itemID, userID).
		Scan(&r.ItemID, &r.Score, &r.Review, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get rating: %w", err)
	}
	return &r, nil
}

// ListRatings returns everything one person has rated, most recent first — the
// profile's "what you thought" list.
func (s *Store) ListRatings(ctx context.Context, userID string, limit int) ([]RatedItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemColsMI+`,
		    ur.score, ur.review, ur.updated_at
		FROM media_item mi
		JOIN user_rating ur ON ur.item_id = mi.id
		WHERE ur.user_id = ?
		-- Same stable tie-break as the history, and for the same reason: seconds
		-- collide, and an unstable sort under LIMIT shows a row twice.
		ORDER BY ur.updated_at DESC, mi.id DESC
		LIMIT ?`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list ratings: %w", err)
	}
	defer rows.Close()

	out := []RatedItem{}
	for rows.Next() {
		var e RatedItem
		it, err := scanItemThen(rows, &e.Rating.Score, &e.Rating.Review, &e.Rating.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("list ratings: %w", err)
		}
		e.Item = *it
		e.Rating.ItemID = it.ID
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountRatings is the profile's headline number.
func (s *Store) CountRatings(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM user_rating WHERE user_id = ?`, userID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count ratings: %w", err)
	}
	return n, nil
}
