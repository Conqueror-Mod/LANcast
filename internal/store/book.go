package store

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// LibraryBookParts returns every present audio file in an audiobooks library
// (ADR 0116), unpaged, for the book reconciler. Unpaged for the reason
// LibraryPhotoFiles gives: a book is decided by its whole folder.
func (s *Store) LibraryBookParts(ctx context.Context, libraryID int64) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+itemCols+` FROM media_item
		 WHERE library_id = ? AND kind = 'book_part' AND container IS NOT NULL AND missing = 0
		 ORDER BY path`, libraryID)
	if err != nil {
		return nil, fmt.Errorf("library book parts: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

// BookDetails is what the files say about a book.
//
// The wide table's existing columns hold it, so books need no schema change
// (ADR 0116): the author is `artist`, as an album's maker is; the series is
// `series` and the book's number in it `episode`, as an episode's are; and
// "Abridged" is `edition`, the way a film's cut is.
type BookDetails struct {
	Title     string
	SortTitle string
	Author    string
	Series    string
	Number    int
	Edition   string
	Year      int
}

// SetBookDetails writes a book's details, leaving every locked field alone
// (CLAUDE.md), and touches the row only when something differs, so a rescan
// of an unchanged library writes nothing.
func (s *Store) SetBookDetails(ctx context.Context, bookID int64, d BookDetails) error {
	locked, err := s.LockedFields(ctx, bookID)
	if err != nil {
		return err
	}
	var sets, differs []string
	var args, cmp []any
	set := func(field, col string, v any) {
		if slices.Contains(locked, field) {
			return
		}
		sets = append(sets, col+" = ?")
		args = append(args, v)
		differs = append(differs, col+" IS NOT ?")
		cmp = append(cmp, v)
	}
	set("title", "title", d.Title)
	set("sort_title", "sort_title", d.SortTitle)
	set("artist", "artist", nullIfEmpty(d.Author))
	set("series", "series", nullIfEmpty(d.Series))
	set("episode", "episode", nullIfZero(d.Number))
	set("edition", "edition", nullIfEmpty(d.Edition))
	set("year", "year", nullIfZero(d.Year))
	if len(sets) == 0 {
		return nil
	}
	q := `UPDATE media_item SET ` + strings.Join(sets, ", ") + `, updated_at = ?
		WHERE id = ? AND kind = 'book' AND (` + strings.Join(differs, " OR ") + `)`
	all := append(append(args, time.Now().Unix(), bookID), cmp...)
	if _, err := s.db.ExecContext(ctx, q, all...); err != nil {
		return fmt.Errorf("set book details: %w", err)
	}
	return nil
}

// SetBookPart hangs a part under its book with its place and name. The title
// is left alone when somebody locked it.
func (s *Store) SetBookPart(ctx context.Context, partID, bookID int64, number int, title, sortTitle string) error {
	locked, err := s.LockedFields(ctx, partID)
	if err != nil {
		return err
	}
	q := `UPDATE media_item SET parent_id = ?, episode = ?, updated_at = ?`
	args := []any{bookID, number, time.Now().Unix()}
	differs := `parent_id IS NOT ? OR episode IS NOT ?`
	cmp := []any{bookID, number}
	if !slices.Contains(locked, "title") {
		q += `, title = ?, sort_title = ?`
		args = append(args, title, sortTitle)
		differs += ` OR title IS NOT ?`
		cmp = append(cmp, title)
	}
	q += ` WHERE id = ? AND kind = 'book_part' AND (` + differs + `)`
	args = append(append(args, partID), cmp...)
	if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("set book part: %w", err)
	}
	return nil
}

func nullIfZero(n int) any {
	if n == 0 {
		return nil
	}
	return n
}
