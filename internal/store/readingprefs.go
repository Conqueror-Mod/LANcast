package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

/*
 * Reading and listening defaults per account (ADR 0120, settings-3-plan
 * step 4).
 *
 * Defaults, not settings: a book that has a speed of its own keeps it, and
 * the sleep timer can still be changed or cleared in the book player. These
 * say what happens when nothing more specific has been chosen.
 *
 * Each is nullable, and nil means "never chosen": the client shows 1×, no
 * sleep timer, one page fitted to the screen.
 */

// ReadingPrefs is what an account has chosen about books and comics.
type ReadingPrefs struct {
	BookSpeed   *float64 `json:"book_speed"`
	BookSleep   *string  `json:"book_sleep"`
	ComicLayout *string  `json:"comic_layout"`
	ComicFit    *string  `json:"comic_fit"`
}

// ReadingPrefsFor reads an account's reading and listening defaults.
func (s *Store) ReadingPrefsFor(ctx context.Context, userID string) (ReadingPrefs, error) {
	var speed sql.NullFloat64
	var sleep, layout, fit sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT book_speed, book_sleep, comic_layout, comic_fit FROM user WHERE id = ?`, userID).
		Scan(&speed, &sleep, &layout, &fit)
	if errors.Is(err, sql.ErrNoRows) {
		return ReadingPrefs{}, ErrNotFound
	}
	if err != nil {
		return ReadingPrefs{}, fmt.Errorf("reading preferences: %w", err)
	}
	var p ReadingPrefs
	if speed.Valid {
		p.BookSpeed = &speed.Float64
	}
	if sleep.Valid {
		p.BookSleep = &sleep.String
	}
	if layout.Valid {
		p.ComicLayout = &layout.String
	}
	if fit.Valid {
		p.ComicFit = &fit.String
	}
	return p, nil
}

// SetReadingPrefs applies a change: a nil field is left as it is. Values must
// already be valid; the API checks them.
func (s *Store) SetReadingPrefs(ctx context.Context, userID string, u ReadingPrefs) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("set reading preferences: %w", err)
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM user WHERE id = ?`, userID).Scan(&exists); err != nil {
		return fmt.Errorf("set reading preferences: %w", err)
	}
	if exists == 0 {
		return ErrNotFound
	}
	// Column names are constants here, never input.
	for _, c := range []struct {
		col string
		v   any
		ok  bool
	}{
		{"book_speed", orNil(u.BookSpeed), u.BookSpeed != nil},
		{"book_sleep", orNil(u.BookSleep), u.BookSleep != nil},
		{"comic_layout", orNil(u.ComicLayout), u.ComicLayout != nil},
		{"comic_fit", orNil(u.ComicFit), u.ComicFit != nil},
	} {
		if !c.ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE user SET `+c.col+` = ? WHERE id = ?`, c.v, userID); err != nil {
			return fmt.Errorf("set reading preferences: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("set reading preferences: %w", err)
	}
	return nil
}

func orNil[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}
