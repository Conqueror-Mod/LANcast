package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

/*
 * Retro games (ADR 0073): what the identify worker reads and writes.
 *
 * A ROM is identified by its own worker rather than by enrichment, for the
 * reason album art has one: no provider in the registry can search for a ROM,
 * so enrichment would never reach it. The queue is a query over
 * rom_checked_at, as album art's is over cover_checked_at.
 */

// ROMHash is what reading a ROM's bytes produced. Every field may be empty: a
// disc is identified by its serial and is not hashed, and a file that could
// not be read leaves no hash at all.
type ROMHash struct {
	CRC32, SHA1       string
	AltCRC32, AltSHA1 string
	Serial            string
	InnerName         string
}

// PendingROMs returns ROMs nothing has identified yet, oldest first.
func (s *Store) PendingROMs(ctx context.Context, limit int) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemCols+` FROM media_item
		WHERE kind = 'rom' AND rom_checked_at IS NULL AND missing = 0
		ORDER BY added_at, id LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("pending roms: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

// PendingROMCount is how many ROMs PendingROMs would eventually return.
func (s *Store) PendingROMCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_item
		WHERE kind = 'rom' AND rom_checked_at IS NULL AND missing = 0`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("pending rom count: %w", err)
	}
	return n, nil
}

// GetROMHash returns the stored hash for a ROM, or nil when it has not been
// read since its bytes last changed.
func (s *Store) GetROMHash(ctx context.Context, itemID int64) (*ROMHash, error) {
	var h ROMHash
	var crc, sha, acrc, asha, serial, inner sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT crc32, sha1, alt_crc32, alt_sha1, serial, inner_name
		FROM rom_hash WHERE item_id = ?`, itemID).Scan(&crc, &sha, &acrc, &asha, &serial, &inner)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("rom hash %d: %w", itemID, err)
	}
	h.CRC32, h.SHA1, h.AltCRC32, h.AltSHA1 = crc.String, sha.String, acrc.String, asha.String
	h.Serial, h.InnerName = serial.String, inner.String
	return &h, nil
}

// PutROMHash records what reading a ROM produced, replacing any earlier read.
func (s *Store) PutROMHash(ctx context.Context, itemID int64, h ROMHash) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO rom_hash (item_id, crc32, sha1, alt_crc32, alt_sha1, serial, inner_name, hashed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(item_id) DO UPDATE SET
			crc32 = excluded.crc32, sha1 = excluded.sha1,
			alt_crc32 = excluded.alt_crc32, alt_sha1 = excluded.alt_sha1,
			serial = excluded.serial, inner_name = excluded.inner_name,
			hashed_at = excluded.hashed_at`,
		itemID, nullIfEmpty(h.CRC32), nullIfEmpty(h.SHA1), nullIfEmpty(h.AltCRC32),
		nullIfEmpty(h.AltSHA1), nullIfEmpty(h.Serial), nullIfEmpty(h.InnerName), time.Now().Unix())
	if err != nil {
		return fmt.Errorf("put rom hash %d: %w", itemID, err)
	}
	return nil
}

// SetPlatform records a ROM's console when reading the file found one the
// name could not — a zip in a folder that says nothing, whose inner file has
// an extension that does.
func (s *Store) SetPlatform(ctx context.Context, itemID int64, platform string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE media_item SET platform = ?, updated_at = ? WHERE id = ? AND kind = 'rom'`,
		platform, time.Now().Unix(), itemID)
	if err != nil {
		return fmt.Errorf("set platform %d: %w", itemID, err)
	}
	return nil
}

// MarkROMChecked stamps a ROM so the identify worker does not ask again.
// Every outcome stamps — matched, unmatched, unreadable — because the queue is
// a query, and a ROM that is never stamped is returned for ever.
func (s *Store) MarkROMChecked(ctx context.Context, itemID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE media_item SET rom_checked_at = ? WHERE id = ?`, time.Now().Unix(), itemID)
	if err != nil {
		return fmt.Errorf("mark rom checked %d: %w", itemID, err)
	}
	return nil
}

/*
 * RequeueROMs sends every ROM back to the identify worker, except those whose
 * identity a person settled.
 *
 * Called when the DAT files are installed or replaced: a ROM checked before
 * there was anything to check it against was stamped unmatched, and is worth
 * asking about again. Its hash is kept, so this costs a lookup per ROM rather
 * than a read of every file. A locked match is left alone because a rescan —
 * or a new DAT — reconciles files and never re-litigates a decision.
 */
func (s *Store) RequeueROMs(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE media_item SET rom_checked_at = NULL
		WHERE kind = 'rom' AND rom_checked_at IS NOT NULL
		  AND COALESCE(match_state, '') != 'locked'`)
	if err != nil {
		return 0, fmt.Errorf("requeue roms: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
