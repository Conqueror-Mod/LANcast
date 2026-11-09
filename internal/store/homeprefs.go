package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"
)

/*
 * What an account has chosen about its Home page (ADR 0120, settings-3-plan
 * step 3).
 *
 * Spotlight and spoilers were stored per device, in the browser, because
 * there was no per-account store for them; both said they should move once
 * there was. They are about the person, so they follow the person now.
 *
 * Each is nullable: nil means "never chosen". The client then shows the
 * default, and a device that still holds an older choice of its own carries
 * it over once. The first device to do so wins, which is the only rule that
 * does not need a device to know about the others.
 *
 * Hidden built-in shelves are rows in home_shelf with `builtin` set, as ADR
 * 0119 reserved the column for: one row per hidden shelf, keyed "new", "rate",
 * "watchlist" or "unwatched:<library id>". An Unwatched row carries its
 * library, so deleting the library takes the row with it.
 */

// HomePrefs is what an account has chosen about Home. A nil field has never
// been chosen.
type HomePrefs struct {
	Spotlight     *string  `json:"spotlight"`
	SpotlightItem *int64   `json:"spotlight_item"`
	Spoilers      *string  `json:"spoilers"`
	HiddenShelves []string `json:"hidden_shelves"`
}

// HomePrefsFor reads an account's Home preferences.
func (s *Store) HomePrefsFor(ctx context.Context, userID string) (HomePrefs, error) {
	var spot, spoil sql.NullString
	var item sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT home_spotlight, home_spotlight_item, spoilers FROM user WHERE id = ?`, userID).
		Scan(&spot, &item, &spoil)
	if errors.Is(err, sql.ErrNoRows) {
		return HomePrefs{}, ErrNotFound
	}
	if err != nil {
		return HomePrefs{}, fmt.Errorf("home preferences: %w", err)
	}
	p := HomePrefs{HiddenShelves: []string{}}
	if spot.Valid {
		p.Spotlight = &spot.String
	}
	if item.Valid {
		p.SpotlightItem = &item.Int64
	}
	if spoil.Valid {
		p.Spoilers = &spoil.String
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT builtin FROM home_shelf WHERE user_id = ? AND builtin IS NOT NULL ORDER BY builtin`, userID)
	if err != nil {
		return HomePrefs{}, fmt.Errorf("home preferences: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return HomePrefs{}, fmt.Errorf("home preferences: %w", err)
		}
		p.HiddenShelves = append(p.HiddenShelves, k)
	}
	return p, rows.Err()
}

// HomePrefsUpdate is a change to an account's Home preferences: a nil field
// is left as it is. HiddenShelves, when set, replaces the whole list; each
// key must already be valid, and an Unwatched key's library id is given in
// HiddenLibraries under the same key.
type HomePrefsUpdate struct {
	Spotlight       *string
	SpotlightItem   *int64
	Spoilers        *string
	HiddenShelves   *[]string
	HiddenLibraries map[string]int64
}

// SetHomePrefs applies a change to an account's Home preferences.
func (s *Store) SetHomePrefs(ctx context.Context, userID string, u HomePrefsUpdate) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("set home preferences: %w", err)
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM user WHERE id = ?`, userID).Scan(&exists); err != nil {
		return fmt.Errorf("set home preferences: %w", err)
	}
	if exists == 0 {
		return ErrNotFound
	}
	set := func(col string, v any) error {
		_, err := tx.ExecContext(ctx, `UPDATE user SET `+col+` = ? WHERE id = ?`, v, userID)
		return err
	}
	if u.Spotlight != nil {
		if err := set("home_spotlight", *u.Spotlight); err != nil {
			return fmt.Errorf("set home preferences: %w", err)
		}
	}
	if u.SpotlightItem != nil {
		if err := set("home_spotlight_item", *u.SpotlightItem); err != nil {
			return fmt.Errorf("set home preferences: %w", err)
		}
	}
	if u.Spoilers != nil {
		if err := set("spoilers", *u.Spoilers); err != nil {
			return fmt.Errorf("set home preferences: %w", err)
		}
	}
	if u.HiddenShelves != nil {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM home_shelf WHERE user_id = ? AND builtin IS NOT NULL`, userID); err != nil {
			return fmt.Errorf("set home preferences: %w", err)
		}
		keys := append([]string(nil), *u.HiddenShelves...)
		sort.Strings(keys)
		now := time.Now().Unix()
		seen := map[string]bool{}
		for _, k := range keys {
			if seen[k] {
				continue
			}
			seen[k] = true
			var lib any
			if id, ok := u.HiddenLibraries[k]; ok {
				lib = id
			}
			// Position is not used for these rows; title names the shelf for
			// anyone reading the table by hand.
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO home_shelf (user_id, position, title, library_id, query, builtin, created_at)
				VALUES (?, 0, ?, ?, NULL, ?, ?)`, userID, k, lib, k, now); err != nil {
				return fmt.Errorf("set home preferences: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("set home preferences: %w", err)
	}
	return nil
}
