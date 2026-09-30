package store

import (
	"context"
	"fmt"
)

/*
 * The picture an account chose for itself (schema 52).
 *
 * A key from a fixed set rather than an image. The client draws each one; the
 * server only remembers which. That keeps a member from putting arbitrary bytes
 * in front of everyone else on the server, and leaves nothing to upload, store,
 * resize or serve.
 */

// Avatars are the keys an account may choose. Empty, not in this list, means
// none chosen.
var Avatars = []string{"fox", "owl", "cat", "wolf", "bear", "rabbit"}

// ValidAvatar reports whether key is one an account may choose. Empty is valid:
// it clears the choice.
func ValidAvatar(key string) bool {
	if key == "" {
		return true
	}
	for _, a := range Avatars {
		if a == key {
			return true
		}
	}
	return false
}

// SetAvatar records an account's chosen picture. The caller validates the key;
// this refuses one anyway, since the column is read straight back to clients.
func (s *Store) SetAvatar(ctx context.Context, userID, key string) error {
	if !ValidAvatar(key) {
		return fmt.Errorf("set avatar: %q is not a known avatar", key)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE user SET avatar = ? WHERE id = ?`, key, userID)
	if err != nil {
		return fmt.Errorf("set avatar: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Avatar returns an account's chosen picture, or "" for none.
func (s *Store) Avatar(ctx context.Context, userID string) (string, error) {
	var key string
	if err := s.db.QueryRowContext(ctx,
		`SELECT avatar FROM user WHERE id = ?`, userID).Scan(&key); err != nil {
		return "", fmt.Errorf("get avatar: %w", err)
	}
	return key, nil
}
