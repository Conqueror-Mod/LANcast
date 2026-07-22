package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Session is an authenticated login.
type Session struct {
	UserID    string
	ExpiresAt int64
}

// CreateSession stores a session keyed by the hash of its token. The plaintext
// token exists only in the caller's cookie — a stolen database yields no usable
// sessions.
func (s *Store) CreateSession(ctx context.Context, tokenHash, userID string, ttl time.Duration) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO session (token_hash, user_id, created_at, expires_at, last_seen)
		VALUES (?, ?, ?, ?, ?)`,
		tokenHash, userID, now.Unix(), now.Add(ttl).Unix(), now.Unix())
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// LookupSession returns the session for a token hash if it exists and has not
// expired. Expired rows are treated as absent whether or not cleanup has run.
func (s *Store) LookupSession(ctx context.Context, tokenHash string) (*Session, error) {
	var sess Session
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id, expires_at FROM session WHERE token_hash = ?`, tokenHash).
		Scan(&sess.UserID, &sess.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup session: %w", err)
	}
	if time.Now().Unix() >= sess.ExpiresAt {
		return nil, ErrNotFound
	}
	return &sess, nil
}

// TouchSession records activity, extending the window so an in-use session
// does not expire mid-film.
func (s *Store) TouchSession(ctx context.Context, tokenHash string, ttl time.Duration) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx,
		`UPDATE session SET last_seen = ?, expires_at = ? WHERE token_hash = ?`,
		now.Unix(), now.Add(ttl).Unix(), tokenHash)
	return err
}

// DeleteSession logs one session out.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM session WHERE token_hash = ?`, tokenHash)
	return err
}

// DeleteAllSessions logs everyone out. Called on a password change, which is
// the whole reason sessions live server-side rather than in a signed cookie.
func (s *Store) DeleteAllSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM session`)
	return err
}

// PurgeExpiredSessions removes stale rows.
func (s *Store) PurgeExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM session WHERE expires_at < ?`, time.Now().Unix())
	return err
}
