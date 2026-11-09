package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

/*
 * The picture an account uploaded (ADR 0121, schema 94).
 *
 * The store keeps what internal/picture drew, never what was uploaded: two
 * JPEGs, 256 px and 64 px, and the hash a client fetches them by. It does not
 * decode anything itself, so nothing here can be handed raw bytes from a
 * request by mistake. SetPicture takes the two encoded images and trusts the
 * caller to have made them.
 */

// Picture sizes a client may ask for.
const (
	PictureLarge = 256
	PictureSmall = 64
)

// SetPicture records an account's picture, replacing any earlier one, and
// returns its hash.
func (s *Store) SetPicture(ctx context.Context, userID string, large, small []byte) (string, error) {
	if len(large) == 0 || len(small) == 0 {
		return "", errors.New("set picture: empty image")
	}
	sum := sha256.Sum256(large)
	hash := hex.EncodeToString(sum[:])
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_picture (user_id, hash, large, small, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
		  hash = excluded.hash, large = excluded.large, small = excluded.small,
		  updated_at = excluded.updated_at`,
		userID, hash, large, small, time.Now().Unix())
	if err != nil {
		return "", fmt.Errorf("set picture: %w", err)
	}
	return hash, nil
}

// ClearPicture removes an account's picture. Reports whether there was one.
func (s *Store) ClearPicture(ctx context.Context, userID string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM user_picture WHERE user_id = ?`, userID)
	if err != nil {
		return false, fmt.Errorf("clear picture: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// PictureHash returns the hash of an account's picture, or "" for none.
func (s *Store) PictureHash(ctx context.Context, userID string) (string, error) {
	var hash string
	err := s.db.QueryRowContext(ctx,
		`SELECT hash FROM user_picture WHERE user_id = ?`, userID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("picture hash: %w", err)
	}
	return hash, nil
}

// PictureHashes maps every account that has a picture to its hash.
func (s *Store) PictureHashes(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, hash FROM user_picture`)
	if err != nil {
		return nil, fmt.Errorf("picture hashes: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, hash string
		if err := rows.Scan(&id, &hash); err != nil {
			return nil, fmt.Errorf("picture hashes: %w", err)
		}
		out[id] = hash
	}
	return out, rows.Err()
}

// PictureByHash returns one size of the picture with this hash. Two accounts
// that uploaded the same image share a hash and the same bytes, so any row
// will do. ErrNotFound when no account has it.
func (s *Store) PictureByHash(ctx context.Context, hash string, size int) ([]byte, error) {
	col := "large"
	if size == PictureSmall {
		col = "small"
	}
	var b []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT `+col+` FROM user_picture WHERE hash = ? LIMIT 1`, hash).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("picture: %w", err)
	}
	return b, nil
}
