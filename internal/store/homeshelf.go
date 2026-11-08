package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

/*
 * Shelves you pin to Home (ADR 0119 §2, revision 91).
 *
 * A pinned shelf is a saved library view: a library, the query its grid
 * understood, and a title. What the query may contain is the API's to check
 * (it knows the grid's parameters); this file keeps the rows, their order and
 * the limit.
 *
 * Positions are kept dense from 0 for each account, so "move to position 2"
 * means the same thing however many shelves were unpinned before it.
 */

// MaxHomeShelves is how many shelves one account may pin. Each is a request
// on the first screen of the app, and twelve is more than a home page shows
// comfortably.
const MaxHomeShelves = 12

// ErrHomeShelfLimit is returned by AddHomeShelf at MaxHomeShelves.
var ErrHomeShelfLimit = errors.New("home shelf limit reached")

// ErrHomeShelfNotFound is returned for a shelf that is not this account's.
var ErrHomeShelfNotFound = errors.New("home shelf not found")

// HomeShelf is one pinned shelf.
type HomeShelf struct {
	ID        int64  `json:"id"`
	Position  int    `json:"position"`
	Title     string `json:"title"`
	LibraryID int64  `json:"library_id"`
	Query     string `json:"query"`
}

// HomeShelves returns the account's pinned shelves in order. Rows naming a
// built-in shelf (reserved for hiding one, not yet written) are left out.
func (s *Store) HomeShelves(ctx context.Context, userID string) ([]HomeShelf, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, position, title, library_id, query FROM home_shelf
		WHERE user_id = ? AND builtin IS NULL AND library_id IS NOT NULL
		ORDER BY position, id`, userID)
	if err != nil {
		return nil, fmt.Errorf("home shelves: %w", err)
	}
	defer rows.Close()
	out := []HomeShelf{}
	for rows.Next() {
		var h HomeShelf
		var q sql.NullString
		if err := rows.Scan(&h.ID, &h.Position, &h.Title, &h.LibraryID, &q); err != nil {
			return nil, fmt.Errorf("home shelves: %w", err)
		}
		h.Query = q.String
		out = append(out, h)
	}
	return out, rows.Err()
}

// AddHomeShelf pins a shelf at the end of the account's list.
func (s *Store) AddHomeShelf(ctx context.Context, userID string, libraryID int64, title, query string) (HomeShelf, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return HomeShelf{}, fmt.Errorf("add home shelf: %w", err)
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM home_shelf WHERE user_id = ? AND builtin IS NULL`, userID).Scan(&n); err != nil {
		return HomeShelf{}, fmt.Errorf("add home shelf: %w", err)
	}
	if n >= MaxHomeShelves {
		return HomeShelf{}, ErrHomeShelfLimit
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO home_shelf (user_id, position, title, library_id, query, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, userID, n, title, libraryID, query, time.Now().Unix())
	if err != nil {
		return HomeShelf{}, fmt.Errorf("add home shelf: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return HomeShelf{}, fmt.Errorf("add home shelf: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return HomeShelf{}, fmt.Errorf("add home shelf: %w", err)
	}
	return HomeShelf{ID: id, Position: n, Title: title, LibraryID: libraryID, Query: query}, nil
}

// RenameHomeShelf changes a shelf's title.
func (s *Store) RenameHomeShelf(ctx context.Context, userID string, id int64, title string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE home_shelf SET title = ? WHERE id = ? AND user_id = ? AND builtin IS NULL`,
		title, id, userID)
	if err != nil {
		return fmt.Errorf("rename home shelf: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrHomeShelfNotFound
	}
	return nil
}

// MoveHomeShelf puts a shelf at position (0-based), clamped to the list, and
// renumbers the rest densely around it.
func (s *Store) MoveHomeShelf(ctx context.Context, userID string, id int64, position int) error {
	return s.reorderHomeShelves(ctx, userID, func(ids []int64) ([]int64, error) {
		from := indexOf(ids, id)
		if from < 0 {
			return nil, ErrHomeShelfNotFound
		}
		rest := append(append([]int64{}, ids[:from]...), ids[from+1:]...)
		if position < 0 {
			position = 0
		}
		if position > len(rest) {
			position = len(rest)
		}
		out := append(append(append([]int64{}, rest[:position]...), id), rest[position:]...)
		return out, nil
	})
}

// DeleteHomeShelf unpins a shelf and closes the gap it leaves.
func (s *Store) DeleteHomeShelf(ctx context.Context, userID string, id int64) error {
	return s.reorderHomeShelves(ctx, userID, func(ids []int64) ([]int64, error) {
		at := indexOf(ids, id)
		if at < 0 {
			return nil, ErrHomeShelfNotFound
		}
		return append(append([]int64{}, ids[:at]...), ids[at+1:]...), nil
	}, id)
}

// reorderHomeShelves reads the account's shelves in order, lets change decide
// the new order, deletes any ids in drop, and writes positions 0..n-1.
func (s *Store) reorderHomeShelves(ctx context.Context, userID string,
	change func([]int64) ([]int64, error), drop ...int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("reorder home shelves: %w", err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM home_shelf WHERE user_id = ? AND builtin IS NULL ORDER BY position, id`, userID)
	if err != nil {
		return fmt.Errorf("reorder home shelves: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("reorder home shelves: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reorder home shelves: %w", err)
	}
	next, err := change(ids)
	if err != nil {
		return err
	}
	for _, id := range drop {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM home_shelf WHERE id = ? AND user_id = ?`, id, userID); err != nil {
			return fmt.Errorf("delete home shelf: %w", err)
		}
	}
	for i, id := range next {
		if _, err := tx.ExecContext(ctx,
			`UPDATE home_shelf SET position = ? WHERE id = ? AND user_id = ?`, i, id, userID); err != nil {
			return fmt.Errorf("reorder home shelves: %w", err)
		}
	}
	return tx.Commit()
}

func indexOf(ids []int64, id int64) int {
	for i, v := range ids {
		if v == id {
			return i
		}
	}
	return -1
}
