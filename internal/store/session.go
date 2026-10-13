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
	return s.CreateSessionFrom(ctx, tokenHash, userID, ttl, "", "")
}

// CreateSessionFrom is CreateSession with what signed in and from where, for
// Authorized devices. Either may be empty.
func (s *Store) CreateSessionFrom(ctx context.Context, tokenHash, userID string, ttl time.Duration, device, address string) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO session (token_hash, user_id, created_at, expires_at, last_seen, device, address)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		tokenHash, userID, now.Unix(), now.Add(ttl).Unix(), now.Unix(),
		nullIfEmpty(device), nullIfEmpty(address))
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
func (s *Store) TouchSession(ctx context.Context, tokenHash string, ttl time.Duration, address string) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx,
		`UPDATE session SET last_seen = ?, expires_at = ?, address = COALESCE(?, address) WHERE token_hash = ?`,
		now.Unix(), now.Add(ttl).Unix(), nullIfEmpty(address), tokenHash)
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

// SessionInfo is one signed-in device, as Authorized devices lists it. ID is
// a short public handle for signing it out: the leading characters of the
// token's hash, which is itself one-way — neither the token nor the full hash
// ever leaves the server.
type SessionInfo struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	UserName  string `json:"user_name"`
	Device    string `json:"device"`
	Address   string `json:"address,omitempty"`
	CreatedAt int64  `json:"created_at"`
	LastSeen  int64  `json:"last_seen"`
	Current   bool   `json:"current"`
}

// sessionIDLength is how much of the hash is the public handle: 16 hex
// characters, 64 bits, with nothing to gain by guessing one.
const sessionIDLength = 16

// SessionHandle is the public handle for a stored token hash.
func SessionHandle(tokenHash string) string {
	if len(tokenHash) < sessionIDLength {
		return tokenHash
	}
	return tokenHash[:sessionIDLength]
}

// ListSessions lists live sessions, most recently used first: one account's
// when userID is set, everyone's when it is empty.
func (s *Store) ListSessions(ctx context.Context, userID string) ([]SessionInfo, error) {
	q := `SELECT s.token_hash, s.user_id, u.name, COALESCE(s.device, ''), COALESCE(s.address, ''),
	             s.created_at, s.last_seen
	        FROM session s JOIN user u ON u.id = s.user_id
	       WHERE s.expires_at > ?`
	args := []any{time.Now().Unix()}
	if userID != "" {
		q += ` AND s.user_id = ?`
		args = append(args, userID)
	}
	q += ` ORDER BY s.last_seen DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	out := []SessionInfo{}
	for rows.Next() {
		var hash string
		var si SessionInfo
		if err := rows.Scan(&hash, &si.UserID, &si.UserName, &si.Device, &si.Address,
			&si.CreatedAt, &si.LastSeen); err != nil {
			return nil, fmt.Errorf("list sessions: %w", err)
		}
		si.ID = SessionHandle(hash)
		out = append(out, si)
	}
	return out, rows.Err()
}

// DeleteSessionByHandle signs out one session by its public handle: only one
// of userID's when userID is set, anyone's when it is empty. It reports
// whether anything was removed.
func (s *Store) DeleteSessionByHandle(ctx context.Context, userID, handle string) (bool, error) {
	if len(handle) != sessionIDLength {
		return false, nil
	}
	q := `DELETE FROM session WHERE substr(token_hash, 1, ?) = ?`
	args := []any{sessionIDLength, handle}
	if userID != "" {
		q += ` AND user_id = ?`
		args = append(args, userID)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return false, fmt.Errorf("sign out session: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeleteOtherSessions signs out every one of userID's sessions except the one
// whose hash is keep, and reports how many went.
func (s *Store) DeleteOtherSessions(ctx context.Context, userID, keep string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM session WHERE user_id = ? AND token_hash <> ?`, userID, keep)
	if err != nil {
		return 0, fmt.Errorf("sign out other sessions: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
