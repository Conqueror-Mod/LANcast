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
// the key itself.
type RetroAccount struct {
	UserID       string
	RAUser       string
	RAULID       string
	APIKeySealed []byte
	ConnectedAt  int64
	// ConnectTokenSealed is the token earning uses (ADR 0112), sealed like
	// the key. Nil while earning is off.
	ConnectTokenSealed []byte
}

// SetRetroAccount connects, or reconnects, an account's RetroAchievements.
// Reconnecting the same RetroAchievements account (by ULID, which survives a
// rename) keeps earning on; connecting a different one turns it off, because
// the token belongs to the account it was issued for. Unlocks queued under
// the old one are dropped by the sender, which checks the name on each.
func (s *Store) SetRetroAccount(ctx context.Context, a RetroAccount) error {
	if a.UserID == "" || a.RAUser == "" || a.RAULID == "" || len(a.APIKeySealed) == 0 {
		return errors.New("store: a RetroAchievements account needs a user, a name, a ULID and a key")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO retro_account (user_id, ra_user, ra_ulid, api_key_sealed, connected_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
		  connect_token_sealed = CASE WHEN ra_ulid = excluded.ra_ulid
		    THEN connect_token_sealed ELSE NULL END,
		  ra_user = excluded.ra_user, ra_ulid = excluded.ra_ulid,
		  api_key_sealed = excluded.api_key_sealed, connected_at = excluded.connected_at`,
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
		SELECT ra_user, ra_ulid, api_key_sealed, connected_at, connect_token_sealed
		FROM retro_account WHERE user_id = ?`, userID).
		Scan(&a.RAUser, &a.RAULID, &a.APIKeySealed, &a.ConnectedAt, &a.ConnectTokenSealed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get retroachievements account: %w", err)
	}
	return &a, nil
}

// DeleteRetroAccount disconnects: the sealed key and token go with the row,
// and so does anything still queued, which could no longer be sent.
func (s *Store) DeleteRetroAccount(ctx context.Context, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM retro_account WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("delete retroachievements account: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM retro_unlock_queue WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("delete retroachievements queue: %w", err)
	}
	return tx.Commit()
}

// ErrNoRetroAccount is SetRetroConnectToken on an account with no
// RetroAchievements connection to earn with.
var ErrNoRetroAccount = errors.New("store: no RetroAchievements account is connected")

// SetRetroConnectToken turns earning on with a sealed token, or off with nil.
// Off also drops the queue: without a token nothing in it can be sent, and
// turning earning off is the promise that nothing more will be (ADR 0112
// section 4).
func (s *Store) SetRetroConnectToken(ctx context.Context, userID string, sealed []byte) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var token any
	if len(sealed) > 0 {
		token = sealed
	}
	res, err := tx.ExecContext(ctx, `UPDATE retro_account SET connect_token_sealed = ? WHERE user_id = ?`, token, userID)
	if err != nil {
		return fmt.Errorf("set retroachievements token: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoRetroAccount
	}
	if token == nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM retro_unlock_queue WHERE user_id = ?`, userID); err != nil {
			return fmt.Errorf("clear retroachievements queue: %w", err)
		}
	}
	return tx.Commit()
}

// RetroQueued is an unlock or leaderboard entry waiting for RetroAchievements.
type RetroQueued struct {
	ID        int64
	UserID    string
	RAUser    string
	Request   string // the dorequest.php r value
	Form      string // the form rcheevos built, without a token
	UserAgent string // the player's, which names the core
	CreatedAt int64
	Attempts  int
	LastError string
}

// EnqueueRetro keeps an unlock for later. CreatedAt is when it was made,
// which the sender turns into RetroAchievements' "seconds ago".
func (s *Store) EnqueueRetro(ctx context.Context, q RetroQueued) (int64, error) {
	if q.UserID == "" || q.RAUser == "" || q.Request == "" || q.Form == "" || q.UserAgent == "" {
		return 0, errors.New("store: a queued unlock needs an account, a name, a request, its form and a user agent")
	}
	if q.CreatedAt == 0 {
		q.CreatedAt = time.Now().Unix()
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO retro_unlock_queue (user_id, ra_user, request, form, user_agent, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, q.UserID, q.RAUser, q.Request, q.Form, q.UserAgent, q.CreatedAt)
	if err != nil {
		return 0, fmt.Errorf("queue retroachievements unlock: %w", err)
	}
	return res.LastInsertId()
}

// RetroQueue is everything waiting, oldest first, at most limit rows.
func (s *Store) RetroQueue(ctx context.Context, limit int) ([]RetroQueued, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, ra_user, request, form, user_agent, created_at, attempts, COALESCE(last_error, '')
		FROM retro_unlock_queue ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("read retroachievements queue: %w", err)
	}
	defer rows.Close()
	var out []RetroQueued
	for rows.Next() {
		var q RetroQueued
		if err := rows.Scan(&q.ID, &q.UserID, &q.RAUser, &q.Request, &q.Form, &q.UserAgent, &q.CreatedAt, &q.Attempts, &q.LastError); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// RetroQueueCount is how many unlocks an account has waiting.
func (s *Store) RetroQueueCount(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM retro_unlock_queue WHERE user_id = ?`, userID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count retroachievements queue: %w", err)
	}
	return n, nil
}

// DeleteRetroQueued removes one row: sent, or turned down for good.
func (s *Store) DeleteRetroQueued(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM retro_unlock_queue WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete queued retroachievements unlock: %w", err)
	}
	return nil
}

// NoteRetroAttempt records a try that RetroAchievements could not take.
func (s *Store) NoteRetroAttempt(ctx context.Context, id int64, problem string) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE retro_unlock_queue SET attempts = attempts + 1, last_error = ? WHERE id = ?`, problem, id); err != nil {
		return fmt.Errorf("note retroachievements attempt: %w", err)
	}
	return nil
}
