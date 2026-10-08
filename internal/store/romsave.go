package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ROMSave describes one save slot of one person's game (ADR 0073). The bytes
// live in files under the data directory; this is what the server knows about
// them.
type ROMSave struct {
	Slot        string `json:"slot"`
	Core        string `json:"core,omitempty"`
	CoreVersion string `json:"core_version,omitempty"`
	SizeBytes   int64  `json:"size_bytes"`
	SHA256      string `json:"sha256"`
	UpdatedAt   int64  `json:"updated_at"`
	// Previous is the copy this one replaced, kept so a bad save costs one
	// step back rather than the game.
	Previous *ROMSavePrevious `json:"previous,omitempty"`
}

// ROMSavePrevious is the save a newer one replaced.
type ROMSavePrevious struct {
	Core        string `json:"core,omitempty"`
	CoreVersion string `json:"core_version,omitempty"`
	SizeBytes   int64  `json:"size_bytes"`
	SHA256      string `json:"sha256"`
	UpdatedAt   int64  `json:"updated_at"`
}

const romSaveCols = `slot, core, core_version, size_bytes, sha256, updated_at,
	prev_core, prev_core_version, prev_size_bytes, prev_sha256, prev_updated_at`

func scanROMSave(sc interface{ Scan(...any) error }) (ROMSave, error) {
	var s ROMSave
	var core, ver, pcore, pver, psha sql.NullString
	var psize, pat sql.NullInt64
	if err := sc.Scan(&s.Slot, &core, &ver, &s.SizeBytes, &s.SHA256, &s.UpdatedAt,
		&pcore, &pver, &psize, &psha, &pat); err != nil {
		return s, err
	}
	s.Core, s.CoreVersion = core.String, ver.String
	if psha.Valid {
		s.Previous = &ROMSavePrevious{
			Core: pcore.String, CoreVersion: pver.String,
			SizeBytes: psize.Int64, SHA256: psha.String, UpdatedAt: pat.Int64,
		}
	}
	return s, nil
}

/*
 * PutROMSave records a newly written save, moving what was current into the
 * previous columns — the same step the file store takes with the bytes, so
 * the row and the files agree about which copy is which.
 */
func (s *Store) PutROMSave(ctx context.Context, userID string, itemID int64, sv ROMSave) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO rom_save (user_id, item_id, slot, core, core_version, size_bytes, sha256, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, item_id, slot) DO UPDATE SET
			prev_core = rom_save.core, prev_core_version = rom_save.core_version,
			prev_size_bytes = rom_save.size_bytes, prev_sha256 = rom_save.sha256,
			prev_updated_at = rom_save.updated_at,
			core = excluded.core, core_version = excluded.core_version,
			size_bytes = excluded.size_bytes, sha256 = excluded.sha256,
			updated_at = excluded.updated_at`,
		userID, itemID, sv.Slot, nullIfEmpty(sv.Core), nullIfEmpty(sv.CoreVersion),
		sv.SizeBytes, sv.SHA256, sv.UpdatedAt)
	if err != nil {
		return fmt.Errorf("put rom save: %w", err)
	}
	return nil
}

// ROMSaves lists one person's saves for one game, by slot.
func (s *Store) ROMSaves(ctx context.Context, userID string, itemID int64) ([]ROMSave, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+romSaveCols+` FROM rom_save
		WHERE user_id = ? AND item_id = ? ORDER BY slot`, userID, itemID)
	if err != nil {
		return nil, fmt.Errorf("rom saves: %w", err)
	}
	defer rows.Close()
	out := []ROMSave{}
	for rows.Next() {
		sv, err := scanROMSave(rows)
		if err != nil {
			return nil, fmt.Errorf("rom saves: %w", err)
		}
		out = append(out, sv)
	}
	return out, rows.Err()
}

// GetROMSave returns one slot, or ErrNotFound.
func (s *Store) GetROMSave(ctx context.Context, userID string, itemID int64, slot string) (*ROMSave, error) {
	sv, err := scanROMSave(s.db.QueryRowContext(ctx, `SELECT `+romSaveCols+` FROM rom_save
		WHERE user_id = ? AND item_id = ? AND slot = ?`, userID, itemID, slot))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("rom save: %w", err)
	}
	return &sv, nil
}
