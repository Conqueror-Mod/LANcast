package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

/*
 * API keys (ADR 0061).
 *
 * Stored the way sessions are — the hash, never the key — so a stolen database
 * yields nothing that can be presented to a server. The plaintext exists once,
 * in the response that created it, and nowhere afterwards.
 *
 * What is deliberately *not* here is an expiry column. A key is for a machine
 * that runs unattended, and a credential that stops working on a date nobody
 * remembers is one that fails at the least convenient moment with no
 * explanation. Revocation is a person pressing a button, which is a decision
 * somebody made rather than a clock nobody watched.
 */

// APIKey is one key, without its secret. This is the shape a list returns, and
// it is everything a person needs to decide whether to revoke it.
type APIKey struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
	// LastUsed is 0 until the key is first presented. Zero means "never used"
	// rather than "used at the epoch", and the client must say so in words —
	// a key that has never been used is the safe one to revoke.
	LastUsed int64 `json:"last_used"`
}

// CreateAPIKey records a key for a user. The caller generates the token and
// passes only its hash; this never sees the secret.
func (s *Store) CreateAPIKey(ctx context.Context, tokenHash, userID, name string) (*APIKey, error) {
	// Generated here rather than by the caller, beside the one CreateUser makes
	// and by the same means — an identifier is the store's business, and two
	// generators would eventually disagree about what an id looks like.
	id, err := newUserID()
	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}
	now := time.Now().Unix()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO api_key (id, token_hash, user_id, name, created_at, last_used)
		VALUES (?, ?, ?, ?, ?, 0)`, id, tokenHash, userID, name, now)
	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}
	return &APIKey{ID: id, Name: name, CreatedAt: now}, nil
}

/*
 * LookupAPIKey resolves a key hash to the account it authenticates as.
 *
 * The inner join to user is the same one LookupSession makes, and for the same
 * reason: a deleted account's keys stop working immediately, without relying on
 * a cascade having run.
 *
 * It returns a Session rather than an APIKey, and that is the point. The
 * middleware puts this where a session goes, so every handler downstream reads
 * one shape and cannot accidentally authorize a key by a path that skipped a
 * check the session path makes.
 */
func (s *Store) LookupAPIKey(ctx context.Context, tokenHash string) (*Session, string, error) {
	var sess Session
	var keyID string
	err := s.db.QueryRowContext(ctx, `
		SELECT k.id, k.user_id, u.name, u.role
		FROM api_key k
		JOIN user u ON u.id = k.user_id
		WHERE k.token_hash = ?`, tokenHash).
		Scan(&keyID, &sess.UserID, &sess.Name, &sess.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("lookup api key: %w", err)
	}
	/*
	 * ExpiresAt is left at zero, and nothing reads it for a key.
	 *
	 * Filling it with a far-future time would make a key look like a session
	 * that happens to last a long while, and the first piece of code to treat
	 * the two the same is the one that logs every integration out when somebody
	 * changes their password.
	 */
	return &sess, keyID, nil
}

// TouchAPIKey records that a key was used. Best-effort: a failure here must
// never fail the request the key was authenticating.
func (s *Store) TouchAPIKey(ctx context.Context, id string) {
	_, _ = s.db.ExecContext(ctx,
		`UPDATE api_key SET last_used = ? WHERE id = ?`, time.Now().Unix(), id)
}

// ListAPIKeys returns one user's keys, newest first.
func (s *Store) ListAPIKeys(ctx context.Context, userID string) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, created_at, last_used
		FROM api_key WHERE user_id = ?
		ORDER BY created_at DESC, id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	out := []APIKey{}
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.Name, &k.CreatedAt, &k.LastUsed); err != nil {
			return nil, fmt.Errorf("list api keys: %w", err)
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

/*
 * DeleteAPIKey revokes a key, scoped to its owner.
 *
 * The user id is part of the WHERE clause rather than checked beforehand. An
 * ownership test that happens in the handler is one a second handler can be
 * written without, and the id here is a value the request supplies — so the
 * query itself refuses to delete somebody else's key.
 */
func (s *Store) DeleteAPIKey(ctx context.Context, id, userID string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM api_key WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
