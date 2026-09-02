package store

import (
	"context"
	"fmt"
	"time"
)

/*
 * Markers: where a film or an episode stops being itself (ADR 0054).
 *
 * Stage 1 stores what the detector finds and nothing reads it to make a
 * decision. That is deliberate rather than unfinished — the rule has been
 * shown consistent across two independent samples and has never been checked
 * against a human watching a film, so acting on it would be shipping a guess
 * as a fact.
 */

// Marker is one detected boundary on an item.
type Marker struct {
	Kind string `json:"kind"`
	// StartMS is where the marked stretch begins.
	StartMS int64 `json:"start_ms"`
	// EndMS is where it finishes, or nil when the stretch runs to the end of
	// the file — which credits do, and which is why this is nullable rather
	// than defaulted to the duration.
	EndMS *int64 `json:"end_ms,omitempty"`
	// Source is the detector that produced it, so a wrong marker leads back to
	// the rule that made it.
	Source     string  `json:"source"`
	Confidence float64 `json:"confidence"`
	CreatedAt  int64   `json:"created_at"`
}

// Marker kinds. Intro is defined before anything writes one so the two stages
// cannot disagree about the spelling.
const (
	MarkerCredits = "credits"
	MarkerIntro   = "intro"
)

/*
 * PendingMarkers returns items still to be examined, oldest-added first.
 *
 * Probed items only. Detection needs the file's real duration to say where
 * 88% is, and media_item.duration_ms is only trustworthy once ffprobe has
 * written it — it was TMDB's runtime on every film in a real library until
 * v0.8.51. An unprobed item is not skipped for ever, it is simply not ready.
 */
func (s *Store) PendingMarkers(ctx context.Context, limit int) ([]Item, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemCols+`
		FROM media_item
		WHERE kind IN ('movie', 'episode')
		  AND markers_at IS NULL
		  AND missing = 0
		  AND path IS NOT NULL
		  AND probed_at IS NOT NULL
		  AND duration_ms > 0
		ORDER BY added_at
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("pending markers: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

// PendingMarkersCount is how many items are still waiting.
func (s *Store) PendingMarkersCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_item
		WHERE kind IN ('movie', 'episode') AND markers_at IS NULL AND missing = 0
		  AND path IS NOT NULL AND probed_at IS NOT NULL AND duration_ms > 0`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("pending markers count: %w", err)
	}
	return n, nil
}

/*
 * SaveMarkers replaces an item's markers of the kinds given and stamps it as
 * examined.
 *
 * Stamping is the point, and it happens even when markers is empty. A film
 * whose credits begin on a cut produces nothing, and without the stamp it
 * would be decoded again on every pass for ever — the same trap faces_at
 * exists to avoid. "Looked and found nothing" is an answer worth remembering.
 *
 * kinds says which kinds this pass is authoritative about, so the credits
 * detector cannot delete an intro marker it knows nothing about by writing an
 * empty list.
 */
func (s *Store) SaveMarkers(ctx context.Context, itemID int64, kinds []string, markers []Marker) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save markers: %w", err)
	}
	defer tx.Rollback()

	for _, k := range kinds {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM item_marker WHERE item_id = ? AND kind = ?`, itemID, k); err != nil {
			return fmt.Errorf("save markers: %w", err)
		}
	}
	now := time.Now().Unix()
	for _, m := range markers {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO item_marker (item_id, kind, start_ms, end_ms, source, confidence, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			itemID, m.Kind, m.StartMS, m.EndMS, m.Source, m.Confidence, now); err != nil {
			return fmt.Errorf("save markers: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE media_item SET markers_at = ? WHERE id = ?`, now, itemID); err != nil {
		return fmt.Errorf("save markers: %w", err)
	}
	return tx.Commit()
}

// MarkersFor returns an item's markers.
func (s *Store) MarkersFor(ctx context.Context, itemID int64) ([]Marker, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT kind, start_ms, end_ms, source, confidence, created_at
		FROM item_marker WHERE item_id = ? ORDER BY start_ms`, itemID)
	if err != nil {
		return nil, fmt.Errorf("markers for: %w", err)
	}
	defer rows.Close()

	out := []Marker{}
	for rows.Next() {
		var m Marker
		if err := rows.Scan(&m.Kind, &m.StartMS, &m.EndMS, &m.Source,
			&m.Confidence, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("markers for: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

/*
 * ClearMarkers queues items to be examined again, and reports how many.
 *
 * The stored markers are left alone until the pass replaces them, the same
 * choice ClearProbe makes: deleting them here would only widen the window in
 * which an item has no marker at all, and re-detection is expensive enough
 * that the window is not short.
 *
 * It exists because the rule is expected to change. The window and the length
 * thresholds are tuned numbers, and a build that moves them has to be able to
 * ask every film the new question.
 */
func (s *Store) ClearMarkers(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE media_item SET markers_at = NULL
		WHERE markers_at IS NOT NULL AND missing = 0 AND path IS NOT NULL`)
	if err != nil {
		return 0, fmt.Errorf("clear markers: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("clear markers: %w", err)
	}
	return n, nil
}
