package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

/*
 * An account's playback preferences (ADR 0083).
 *
 * One so far: whether an intro skips itself. Stored nullable so the default
 * lives here rather than in every row — an account that never chose follows
 * DefaultSkipIntros, whatever it becomes.
 */

// DefaultSkipIntros is what an account that has never chosen gets.
const DefaultSkipIntros = true

// PlaybackPrefs is what an account has chosen about playback.
type PlaybackPrefs struct {
	SkipIntros bool `json:"skip_intros"`
	// SkipPreroll is this account's own "no pre-roll for me" (ADR 0099).
	// Null, the default, is false: the clip plays when the server has one.
	SkipPreroll bool `json:"skip_preroll"`
}

// PlaybackPrefsFor reads an account's playback preferences.
func (s *Store) PlaybackPrefsFor(ctx context.Context, userID string) (PlaybackPrefs, error) {
	var skip, preroll sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT skip_intros, skip_preroll FROM user WHERE id = ?`, userID).Scan(&skip, &preroll)
	if errors.Is(err, sql.ErrNoRows) {
		return PlaybackPrefs{}, ErrNotFound
	}
	if err != nil {
		return PlaybackPrefs{}, fmt.Errorf("playback preferences: %w", err)
	}
	p := PlaybackPrefs{SkipIntros: DefaultSkipIntros}
	if skip.Valid {
		p.SkipIntros = skip.Int64 != 0
	}
	p.SkipPreroll = preroll.Valid && preroll.Int64 != 0
	return p, nil
}

// SetPlaybackPrefs records an account's playback preferences.
func (s *Store) SetPlaybackPrefs(ctx context.Context, userID string, p PlaybackPrefs) error {
	v, pr := 0, 0
	if p.SkipIntros {
		v = 1
	}
	if p.SkipPreroll {
		pr = 1
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE user SET skip_intros = ?, skip_preroll = ? WHERE id = ?`, v, pr, userID)
	if err != nil {
		return fmt.Errorf("set playback preferences: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
