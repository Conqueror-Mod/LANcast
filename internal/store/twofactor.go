package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

/*
 * Two-factor sign-in (ADR 0100): what the store keeps about an account's
 * authenticator.
 *
 * The secret arrives already sealed (internal/secret), because it is the one
 * value here that would let somebody produce codes; the store never sees it in
 * the clear. Recovery codes are kept as hashes, the way session tokens are, so
 * they survive a backup restored on another machine — where the sealed secret
 * cannot be opened and they are the only way in.
 */

// TwoFactor is an account's two-factor state. Enabled is false, and the rest
// empty, for an account that never turned it on.
type TwoFactor struct {
	Enabled   bool
	Sealed    []byte
	LastStep  int64
	EnabledAt int64
	// Recovery holds the hashes of the unused recovery codes.
	Recovery []string
}

// TwoFactorFor reads an account's two-factor state.
func (s *Store) TwoFactorFor(ctx context.Context, userID string) (TwoFactor, error) {
	var sealed []byte
	var last, at sql.NullInt64
	var rec sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT totp_secret, totp_last_step, totp_enabled_at, totp_recovery FROM user WHERE id = ?`, userID).
		Scan(&sealed, &last, &at, &rec)
	if errors.Is(err, sql.ErrNoRows) {
		return TwoFactor{}, ErrNotFound
	}
	if err != nil {
		return TwoFactor{}, fmt.Errorf("two-factor: %w", err)
	}
	tf := TwoFactor{Enabled: len(sealed) > 0, Sealed: sealed, LastStep: last.Int64, EnabledAt: at.Int64}
	if rec.Valid && rec.String != "" {
		if err := json.Unmarshal([]byte(rec.String), &tf.Recovery); err != nil {
			return TwoFactor{}, fmt.Errorf("two-factor recovery codes: %w", err)
		}
	}
	return tf, nil
}

// EnableTwoFactor stores a sealed secret and the hashes of fresh recovery
// codes, replacing anything there before.
func (s *Store) EnableTwoFactor(ctx context.Context, userID string, sealed []byte, recoveryHashes []string, now int64) error {
	rec, err := json.Marshal(recoveryHashes)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE user SET totp_secret = ?, totp_last_step = 0, totp_enabled_at = ?, totp_recovery = ? WHERE id = ?`,
		sealed, now, string(rec), userID)
	if err != nil {
		return fmt.Errorf("enable two-factor: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DisableTwoFactor removes it entirely.
func (s *Store) DisableTwoFactor(ctx context.Context, userID string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE user SET totp_secret = NULL, totp_last_step = NULL, totp_enabled_at = NULL, totp_recovery = NULL WHERE id = ?`,
		userID)
	if err != nil {
		return fmt.Errorf("disable two-factor: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UseTwoFactorStep records a step as spent, and reports whether it was still
// unspent. One statement, so two sign-ins racing with the same code cannot
// both succeed.
func (s *Store) UseTwoFactorStep(ctx context.Context, userID string, step int64) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE user SET totp_last_step = ? WHERE id = ? AND totp_secret IS NOT NULL AND COALESCE(totp_last_step, 0) < ?`,
		step, userID, step)
	if err != nil {
		return false, fmt.Errorf("use two-factor step: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// UseRecoveryCode spends one recovery code by its hash, and reports whether it
// was there to spend. In a transaction, so a code works exactly once.
func (s *Store) UseRecoveryCode(ctx context.Context, userID, hash string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var rec sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT totp_recovery FROM user WHERE id = ?`, userID).Scan(&rec); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, err
	}
	var hashes []string
	if rec.Valid && rec.String != "" {
		if err := json.Unmarshal([]byte(rec.String), &hashes); err != nil {
			return false, err
		}
	}
	kept := hashes[:0]
	found := false
	for _, h := range hashes {
		if !found && h == hash {
			found = true
			continue
		}
		kept = append(kept, h)
	}
	if !found {
		return false, nil
	}
	out, _ := json.Marshal(kept)
	if _, err := tx.ExecContext(ctx, `UPDATE user SET totp_recovery = ? WHERE id = ?`, string(out), userID); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
