package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

/*
 * A library's own options (ADR 0123): the metadata language a film or show
 * library is fetched in, which way a comics library reads, and the series in
 * it that read the other way.
 *
 * Validation is the caller's. The store keeps what it is given, and "" clears
 * an option back to NULL, which means "as the server says" for the language
 * and left to right for the direction.
 */

// SetLibraryLanguage sets a library's metadata language; "" follows the server.
func (s *Store) SetLibraryLanguage(ctx context.Context, id int64, code string) error {
	return s.setLibraryOption(ctx, id, "metadata_language", code)
}

// SetLibraryDirection sets which way a comics library reads; "" is left to right.
func (s *Store) SetLibraryDirection(ctx context.Context, id int64, dir string) error {
	return s.setLibraryOption(ctx, id, "reading_direction", dir)
}

// setLibraryOption writes one option column. col is one of the two names
// above, never a caller's string.
func (s *Store) setLibraryOption(ctx context.Context, id int64, col, v string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE library SET `+col+` = ? WHERE id = ?`, nullableStr(v), id)
	if err != nil {
		return fmt.Errorf("set library %s: %w", col, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.announce(ChangeLibraries)
	return nil
}

// LibraryLanguage is a library's own metadata language, or "" when it follows
// the server's — including when there is no such library.
func (s *Store) LibraryLanguage(ctx context.Context, id int64) (string, error) {
	var v sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT metadata_language FROM library WHERE id = ?`, id).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("library language: %w", err)
	}
	return v.String, nil
}

// SetSeriesDirection overrides one series' reading direction within its
// library; "" removes the override, so the series reads the library's way.
func (s *Store) SetSeriesDirection(ctx context.Context, libraryID int64, series, dir string) error {
	if dir == "" {
		_, err := s.db.ExecContext(ctx,
			`DELETE FROM comic_series_direction WHERE library_id = ? AND series = ?`, libraryID, series)
		if err != nil {
			return fmt.Errorf("clear series direction: %w", err)
		}
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO comic_series_direction (library_id, series, direction) VALUES (?, ?, ?)
		ON CONFLICT (library_id, series) DO UPDATE SET direction = excluded.direction`,
		libraryID, series, dir)
	if err != nil {
		return fmt.Errorf("set series direction: %w", err)
	}
	return nil
}

// SeriesDirection is one series' override, or "" when it has none.
func (s *Store) SeriesDirection(ctx context.Context, libraryID int64, series string) (string, error) {
	var dir string
	err := s.db.QueryRowContext(ctx,
		`SELECT direction FROM comic_series_direction WHERE library_id = ? AND series = ?`,
		libraryID, series).Scan(&dir)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("series direction: %w", err)
	}
	return dir, nil
}
