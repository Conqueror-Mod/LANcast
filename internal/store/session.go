package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Session is an authenticated login, resolved together with the account it
// belongs to. Role and Name come from the joined user row, so every handler can
// authorize without a second query.
type Session struct {
	UserID    string
	Name      string
	Role      string
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

// LookupSession returns the session for a token hash if it exists, has not
// expired, and still belongs to a live user. Expired rows are treated as absent
// whether or not cleanup has run. The inner join to user means a deleted
// account's sessions stop resolving immediately, without a schema-level cascade.
func (s *Store) LookupSession(ctx context.Context, tokenHash string) (*Session, error) {
	var sess Session
	err := s.db.QueryRowContext(ctx, `
		SELECT s.user_id, u.name, u.role, s.expires_at
		FROM session s
		JOIN user u ON u.id = s.user_id
		WHERE s.token_hash = ?`, tokenHash).
		Scan(&sess.UserID, &sess.Name, &sess.Role, &sess.ExpiresAt)
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
