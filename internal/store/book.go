package store

import (
	"context"
	"database/sql"
	"errors"
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

// BookParts returns a book's present parts in the order they play (ADR 0116).
// A part's place is its `episode`, written by the scan.
func (s *Store) BookParts(ctx context.Context, bookID int64) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+itemCols+` FROM media_item
		 WHERE parent_id = ? AND kind = 'book_part' AND missing = 0
		 ORDER BY COALESCE(episode, 0), sort_title`, bookID)
	if err != nil {
		return nil, fmt.Errorf("book parts: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

// PlaybackSpeed is the rate this account last chose for this item, and false
// when it never chose one. A book never played starts at 1× (ADR 0116, answer
// 9), so "never chose" is an answer, not a default to fill in here.
func (s *Store) PlaybackSpeed(ctx context.Context, itemID int64, userID string) (float64, bool, error) {
	var v sql.NullFloat64
	err := s.db.QueryRowContext(ctx,
		`SELECT speed FROM playback_state WHERE item_id = ? AND user_id = ?`,
		itemID, userID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("playback speed: %w", err)
	}
	return v.Float64, v.Valid, nil
}

// SetPlaybackSpeed keeps an account's rate for one item beside its position:
// speed is per book and per person (ADR 0116), so it lives on the play state,
// not on the item. A row made here holds no position, which is no position.
func (s *Store) SetPlaybackSpeed(ctx context.Context, itemID int64, userID string, speed float64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO playback_state (item_id, user_id, position_ms, watched, updated_at, speed)
		VALUES (?, ?, 0, 0, ?, ?)
		ON CONFLICT(item_id, user_id) DO UPDATE SET speed = excluded.speed`,
		itemID, userID, time.Now().Unix(), speed)
	if err != nil {
		return fmt.Errorf("set playback speed: %w", err)
	}
	return nil
}

// RoleNarrator is the credit role a book's readers hold (ADR 0116 step 3).
const RoleNarrator = "narrator"

/*
 * SetBookNarrators makes a book's narrator credits exactly these names, in
 * this order.
 *
 * Only the narrator rows are replaced: anything else credited on the book is
 * left where it is. A `credits` lock leaves all of them alone, the rule every
 * locked field follows (CLAUDE.md). People are keyed by name under the
 * "tags" provider, so the same reader across forty books is one person, and
 * the narrator filter finds every one of them by that person's id.
 *
 * Nothing is written when the names already match, so a rescan of an
 * unchanged book leaves the table untouched.
 */
func (s *Store) SetBookNarrators(ctx context.Context, bookID int64, names []string) error {
	locked, err := s.LockedFields(ctx, bookID)
	if err != nil {
		return err
	}
	if slices.Contains(locked, "credits") {
		return nil
	}
	have, err := s.bookNarratorNames(ctx, bookID)
	if err != nil {
		return err
	}
	if slices.Equal(have, names) {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("set book narrators: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM credit WHERE item_id = ? AND role = ?`, bookID, RoleNarrator); err != nil {
		return fmt.Errorf("set book narrators: %w", err)
	}
	for i, n := range names {
		if n == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO person (provider, external_id, name) VALUES ('tags', ?, ?)`,
			n, n); err != nil {
			return fmt.Errorf("set book narrators: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO credit (item_id, person_id, role, ord)
			VALUES (?, (SELECT id FROM person WHERE provider = 'tags' AND external_id = ?), ?, ?)`,
			bookID, n, RoleNarrator, i); err != nil {
			return fmt.Errorf("set book narrators: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) bookNarratorNames(ctx context.Context, bookID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.name FROM credit c JOIN person p ON p.id = c.person_id
		WHERE c.item_id = ? AND c.role = ? ORDER BY c.ord`, bookID, RoleNarrator)
	if err != nil {
		return nil, fmt.Errorf("book narrators: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("book narrators: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

/*
 * BookTagsRev is the revision of the book pass's tag rules a library was last
 * read with, or 0 for never.
 *
 * The book pass skips a library when nothing on disk changed, which is right
 * for a rescan and wrong the first time the pass reads a tag it ignored
 * before: the narrator arrived with step 3, and an unchanged library would
 * otherwise never be read for it. Kept in meta, per library, so no schema
 * changes for a marker.
 */
func (s *Store) BookTagsRev(ctx context.Context, libraryID int64) (int, error) {
	var v string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM meta WHERE key = ?`, bookTagsKey(libraryID)).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("book tags rev: %w", err)
	}
	var n int
	if _, err := fmt.Sscan(v, &n); err != nil {
		// Unreadable is never, which the next pass repairs.
		return 0, nil
	}
	return n, nil
}

// SetBookTagsRev records that a library's books were read under rev.
func (s *Store) SetBookTagsRev(ctx context.Context, libraryID int64, rev int) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		bookTagsKey(libraryID), fmt.Sprint(rev))
	if err != nil {
		return fmt.Errorf("set book tags rev: %w", err)
	}
	return nil
}

func bookTagsKey(libraryID int64) string {
	return fmt.Sprintf("book_tags_rev:%d", libraryID)
}
