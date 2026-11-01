package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// RetroAccount is a LANcast account's connected RetroAchievements account
// (ADR 0090). APIKeySealed is sealed by internal/secret; the store never sees
// the key itself. TokenSealed is the login token that lets the player earn
// achievements (ADR 0111), sealed the same way; nil while earning is off.
type RetroAccount struct {
	UserID       string
	RAUser       string
	RAULID       string
	APIKeySealed []byte
	TokenSealed  []byte
	ConnectedAt  int64
}

// SetRetroAccount connects, or reconnects, an account's RetroAchievements.
// A token is kept across a reconnect to the same RetroAchievements account
// and dropped when the account is a different one: it was issued to the
// other.
func (s *Store) SetRetroAccount(ctx context.Context, a RetroAccount) error {
	if a.UserID == "" || a.RAUser == "" || a.RAULID == "" || len(a.APIKeySealed) == 0 {
		return errors.New("store: a RetroAchievements account needs a user, a name, a ULID and a key")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO retro_account (user_id, ra_user, ra_ulid, api_key_sealed, connected_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
		  ra_user = excluded.ra_user, ra_ulid = excluded.ra_ulid,
		  api_key_sealed = excluded.api_key_sealed, connected_at = excluded.connected_at,
		  token_sealed = CASE WHEN retro_account.ra_ulid = excluded.ra_ulid
		                      THEN retro_account.token_sealed END`,
		a.UserID, a.RAUser, a.RAULID, a.APIKeySealed, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("set retroachievements account: %w", err)
	}
	return nil
}

// GetRetroAccount is the account's connection, or nil when there is none.
func (s *Store) GetRetroAccount(ctx context.Context, userID string) (*RetroAccount, error) {
	a := RetroAccount{UserID: userID}
	err := s.db.QueryRowContext(ctx, `
		SELECT ra_user, ra_ulid, api_key_sealed, token_sealed, connected_at
		FROM retro_account WHERE user_id = ?`, userID).
		Scan(&a.RAUser, &a.RAULID, &a.APIKeySealed, &a.TokenSealed, &a.ConnectedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get retroachievements account: %w", err)
	}
	return &a, nil
}

// SetRetroToken turns earning on with a sealed token, or off with nil. It
// changes nothing for an account that has not connected.
func (s *Store) SetRetroToken(ctx context.Context, userID string, sealed []byte) error {
	var v any
	if len(sealed) > 0 {
		v = sealed
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE retro_account SET token_sealed = ? WHERE user_id = ?`, v, userID); err != nil {
		return fmt.Errorf("set retroachievements token: %w", err)
	}
	if v == nil {
		// Unlocks waiting for a token that is gone can never be sent.
		if _, err := s.db.ExecContext(ctx, `DELETE FROM retro_unlock_queue WHERE user_id = ?`, userID); err != nil {
			return fmt.Errorf("clear retroachievements unlocks: %w", err)
		}
	}
	return nil
}

// DeleteRetroAccount disconnects: the sealed key and token go with the row,
// and any unlocks still waiting with them.
func (s *Store) DeleteRetroAccount(ctx context.Context, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete retroachievements account: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM retro_account WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("delete retroachievements account: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM retro_unlock_queue WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("delete retroachievements unlocks: %w", err)
	}
	return tx.Commit()
}

// RetroUnlock is an award waiting to reach RetroAchievements (ADR 0111 §5).
// Body is the player's request with its token field empty.
type RetroUnlock struct {
	ID       int64
	UserID   string
	Body     string
	QueuedAt int64
	Tries    int
}

// maxQueuedUnlocks bounds one account's queue. An achievement set has at
// most a few hundred, so this is a runaway client, not a long offline spell.
const maxQueuedUnlocks = 1000

// QueueRetroUnlock keeps an award to send later.
func (s *Store) QueueRetroUnlock(ctx context.Context, userID, body string) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM retro_unlock_queue WHERE user_id = ?`, userID).Scan(&n); err != nil {
		return fmt.Errorf("count retroachievements unlocks: %w", err)
	}
	if n >= maxQueuedUnlocks {
		return errors.New("store: too many unlocks waiting to be sent")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO retro_unlock_queue (user_id, body, queued_at) VALUES (?, ?, ?)`,
		userID, body, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("queue retroachievements unlock: %w", err)
	}
	return nil
}

// DueRetroUnlocks are the unlocks whose next try is due, oldest first.
func (s *Store) DueRetroUnlocks(ctx context.Context, now time.Time, limit int) ([]RetroUnlock, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, body, queued_at, tries FROM retro_unlock_queue
		WHERE next_at <= ? ORDER BY id LIMIT ?`, now.Unix(), limit)
	if err != nil {
		return nil, fmt.Errorf("due retroachievements unlocks: %w", err)
	}
	defer rows.Close()
	var out []RetroUnlock
	for rows.Next() {
		var u RetroUnlock
		if err := rows.Scan(&u.ID, &u.UserID, &u.Body, &u.QueuedAt, &u.Tries); err != nil {
			return nil, fmt.Errorf("due retroachievements unlocks: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// DoneRetroUnlock removes an unlock RetroAchievements has answered.
func (s *Store) DoneRetroUnlock(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM retro_unlock_queue WHERE id = ?`, id); err != nil {
		return fmt.Errorf("done retroachievements unlock: %w", err)
	}
	return nil
}

// RetryRetroUnlock records a failed try and when to try again.
func (s *Store) RetryRetroUnlock(ctx context.Context, id int64, next time.Time, problem string) error {
	if len(problem) > 300 {
		problem = problem[:300]
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE retro_unlock_queue SET tries = tries + 1, next_at = ?, last_error = ? WHERE id = ?`,
		next.Unix(), problem, id); err != nil {
		return fmt.Errorf("retry retroachievements unlock: %w", err)
	}
	return nil
}

// CountRetroUnlocks is how many of an account's unlocks are waiting.
func (s *Store) CountRetroUnlocks(ctx context.Context, userID string) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM retro_unlock_queue WHERE user_id = ?`, userID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count retroachievements unlocks: %w", err)
	}
	return n, nil
}
