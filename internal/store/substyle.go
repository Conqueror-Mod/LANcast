package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

/*
 * How subtitles look, for one account (docs/settings-plan.md, decided
 * 2026-10-12: per account). It follows the person to every device, beside
 * their subtitle language, so a TV and a phone signed in as the same person
 * draw subtitles alike.
 *
 * The subtitle *offset* is not here. It is a timing correction for one file
 * on one device, not a style, and it stays a device preference.
 *
 * Stored as one JSON column rather than four, because nothing queries inside
 * it: it is read whole and written whole, and a style that gains a field
 * should not need a migration.
 */

// SubtitleStyle is an account's subtitle look.
type SubtitleStyle struct {
	// Color is a #rrggbb colour.
	Color string `json:"color"`
	// Font is one of SubtitleFonts.
	Font string `json:"font"`
	// Size is a multiplier of the player's base size.
	Size float64 `json:"size"`
	// Position is how far up from the bottom the cues sit, as a percentage of
	// the picture's height.
	Position int `json:"position"`
}

// SubtitleFonts are the typefaces the players can draw.
var SubtitleFonts = []string{"sans", "serif", "mono"}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Validate refuses a style no player can draw.
func (st SubtitleStyle) Validate() error {
	if !hexColor.MatchString(st.Color) {
		return fmt.Errorf("color %q is not #rrggbb", st.Color)
	}
	known := false
	for _, f := range SubtitleFonts {
		if st.Font == f {
			known = true
		}
	}
	if !known {
		return fmt.Errorf("font %q is not one of %v", st.Font, SubtitleFonts)
	}
	if st.Size < 0.5 || st.Size > 3 {
		return fmt.Errorf("size %v is outside 0.5 to 3", st.Size)
	}
	if st.Position < 0 || st.Position > 40 {
		return fmt.Errorf("position %d is outside 0 to 40", st.Position)
	}
	return nil
}

// SubtitleStyleFor returns an account's subtitle style, or nil when it has
// never set one — in which case a device keeps its own.
func (s *Store) SubtitleStyleFor(ctx context.Context, userID string) (*SubtitleStyle, error) {
	var raw sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT subtitle_style FROM user WHERE id = ?`, userID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("subtitle style: %w", err)
	}
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var st SubtitleStyle
	if err := json.Unmarshal([]byte(raw.String), &st); err != nil {
		// A value this build cannot read is the same as none: the device's own
		// style applies, and the next save replaces it.
		return nil, nil
	}
	return &st, nil
}

// SetSubtitleStyle stores an account's subtitle style.
func (s *Store) SetSubtitleStyle(ctx context.Context, userID string, st SubtitleStyle) error {
	if err := st.Validate(); err != nil {
		return fmt.Errorf("set subtitle style: %w", err)
	}
	b, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("set subtitle style: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE user SET subtitle_style = ? WHERE id = ?`, string(b), userID)
	if err != nil {
		return fmt.Errorf("set subtitle style: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
