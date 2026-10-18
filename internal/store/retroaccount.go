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
}

// SetRetroAccount connects, or reconnects, an account's RetroAchievements.
func (s *Store) SetRetroAccount(ctx context.Context, a RetroAccount) error {
	if a.UserID == "" || a.RAUser == "" || a.RAULID == "" || len(a.APIKeySealed) == 0 {
		return errors.New("store: a RetroAchievements account needs a user, a name, a ULID and a key")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO retro_account (user_id, ra_user, ra_ulid, api_key_sealed, connected_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
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
		SELECT ra_user, ra_ulid, api_key_sealed, connected_at
		FROM retro_account WHERE user_id = ?`, userID).
		Scan(&a.RAUser, &a.RAULID, &a.APIKeySealed, &a.ConnectedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get retroachievements account: %w", err)
	}
	return &a, nil
}

// DeleteRetroAccount disconnects: the sealed key goes with the row.
func (s *Store) DeleteRetroAccount(ctx context.Context, userID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM retro_account WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("delete retroachievements account: %w", err)
	}
	return nil
}
