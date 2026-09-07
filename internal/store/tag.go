package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

/*
 * Tags and favourites (ADR 0062).
 *
 * EVERY QUERY IN THIS FILE TAKES A userID, AND THAT IS THE FEATURE
 *
 * Tags are private. Not "filtered in the handler" private — the account is part
 * of every statement, so there is no shape of caller that can read somebody
 * else's, and no later call site that can forget to.
 *
 * The name lives on a row owned by a user rather than in a shared vocabulary
 * with a per-user join, which is the tidier schema and the wrong one: a shared
 * name table leaks the names. Anything listing them either returns everybody's
 * or remembers to filter, and the first thing that forgets discloses the
 * existence of a tag its reader can reach nothing under. The existence is the
 * private part.
 */

// Tag is one account's tag. Count is how many of that account's items carry it,
// filled by ListTags and left at zero elsewhere.
type Tag struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

/*
 * foldTag is the comparison key: trimmed, internal whitespace collapsed,
 * lowercased.
 *
 * Deliberately not `internal/media`'s clean/SortTitle. That is the *title*
 * normalizer, and the one-normalizer rule is about not having two disagreeing
 * answers to the same question — this is a different question, and a title
 * normalizer would give tags opinions about leading articles and sort order.
 */
func foldTag(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

// ErrEmptyTag is a name that folds to nothing. Refused rather than stored,
// because a tag nobody can type is one nobody can remove.
var ErrEmptyTag = errors.New("a tag needs a name")

/*
 * AddTag puts one of a user's tags on an item, creating the tag if this is the
 * first time that account has used the word.
 *
 * The display spelling is whatever the account first typed; a later `Christmas`
 * joins the existing `christmas` rather than making a second row, because they
 * fold the same.
 */
func (s *Store) AddTag(ctx context.Context, userID string, itemID int64, name string) (*Tag, error) {
	folded := foldTag(name)
	if folded == "" {
		return nil, ErrEmptyTag
	}
	display := strings.Join(strings.Fields(name), " ")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("add tag: %w", err)
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM tag WHERE user_id = ? AND folded = ?`, userID, folded).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		res, ierr := tx.ExecContext(ctx,
			`INSERT INTO tag (user_id, name, folded) VALUES (?, ?, ?)`,
			userID, display, folded)
		if ierr != nil {
			return nil, fmt.Errorf("add tag: %w", ierr)
		}
		if id, ierr = res.LastInsertId(); ierr != nil {
			return nil, fmt.Errorf("add tag: %w", ierr)
		}
	} else if err != nil {
		return nil, fmt.Errorf("add tag: %w", err)
	} else {
		// Keep the spelling already on record. Two people are not involved —
		// this is one account — so the first spelling is that account's own
		// choice and re-typing it differently is not a request to rename.
		if err := tx.QueryRowContext(ctx,
			`SELECT name FROM tag WHERE id = ?`, id).Scan(&display); err != nil {
			return nil, fmt.Errorf("add tag: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO item_tag (item_id, tag_id) VALUES (?, ?)`,
		itemID, id); err != nil {
		return nil, fmt.Errorf("add tag: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("add tag: %w", err)
	}
	return &Tag{ID: id, Name: display}, nil
}

/*
 * RemoveTag takes one of a user's tags off an item, and removes the tag itself
 * when nothing carries it any more.
 *
 * The pruning is not tidiness. Without it the filter list accumulates every
 * typo anybody ever made, and a feature meant to make things findable becomes a
 * thing to scroll past.
 */
func (s *Store) RemoveTag(ctx context.Context, userID string, itemID, tagID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("remove tag: %w", err)
	}
	defer tx.Rollback()

	// The ownership is in the statement rather than checked beforehand: a
	// handler-side test is one a second handler can be written without.
	res, err := tx.ExecContext(ctx, `
		DELETE FROM item_tag
		 WHERE item_id = ? AND tag_id IN (
			SELECT id FROM tag WHERE id = ? AND user_id = ?)`,
		itemID, tagID, userID)
	if err != nil {
		return fmt.Errorf("remove tag: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("remove tag: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM tag
		 WHERE id = ? AND user_id = ?
		   AND NOT EXISTS (SELECT 1 FROM item_tag WHERE tag_id = tag.id)`,
		tagID, userID); err != nil {
		return fmt.Errorf("remove tag: %w", err)
	}
	return tx.Commit()
}

// ItemTags returns one account's tags on one item. Another account's tags on
// the same item are not merely hidden — they are not selected.
func (s *Store) ItemTags(ctx context.Context, userID string, itemID int64) ([]Tag, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.name
		  FROM tag t
		  JOIN item_tag it ON it.tag_id = t.id
		 WHERE t.user_id = ? AND it.item_id = ?
		 ORDER BY t.folded`, userID, itemID)
	if err != nil {
		return nil, fmt.Errorf("item tags: %w", err)
	}
	defer rows.Close()

	out := []Tag{}
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, fmt.Errorf("item tags: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListTags returns every tag one account has, with how many items carry each.
// This is the list a filter row is built from, which is why it must never see
// another account's names.
func (s *Store) ListTags(ctx context.Context, userID string) ([]Tag, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.name, COUNT(it.item_id)
		  FROM tag t
		  LEFT JOIN item_tag it ON it.tag_id = t.id
		 WHERE t.user_id = ?
		 GROUP BY t.id, t.name, t.folded
		 ORDER BY t.folded`, userID)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer rows.Close()

	out := []Tag{}
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Count); err != nil {
			return nil, fmt.Errorf("list tags: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetFavourite marks or unmarks an item for one account.
func (s *Store) SetFavourite(ctx context.Context, userID string, itemID int64, on bool) error {
	var err error
	if on {
		_, err = s.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO user_favourite (user_id, item_id, added_at)
			VALUES (?, ?, ?)`, userID, itemID, time.Now().Unix())
	} else {
		_, err = s.db.ExecContext(ctx,
			`DELETE FROM user_favourite WHERE user_id = ? AND item_id = ?`,
			userID, itemID)
	}
	if err != nil {
		return fmt.Errorf("set favourite: %w", err)
	}
	return nil
}

// IsFavourite reports whether one account has marked an item.
func (s *Store) IsFavourite(ctx context.Context, userID string, itemID int64) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM user_favourite WHERE user_id = ? AND item_id = ?`,
		userID, itemID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("is favourite: %w", err)
	}
	return true, nil
}
