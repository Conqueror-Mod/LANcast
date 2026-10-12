package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Passkey is one registered WebAuthn credential (ADR 0082).
type Passkey struct {
	ID             string
	UserID         string
	CredentialID   []byte
	PublicKey      []byte
	Algorithm      int
	SignCount      uint32
	AAGUID         []byte
	BackupEligible bool
	BackupState    bool
	Name           string
	RPID           string
	CreatedAt      int64
	LastUsedAt     int64 // 0 when never used
}

// ErrPasskeyExists is returned when a credential id is already registered,
// to this account or another. A credential belongs to one account.
var ErrPasskeyExists = errors.New("passkey already registered")

// AddPasskey stores a new credential and returns its id.
func (s *Store) AddPasskey(ctx context.Context, p Passkey) (string, error) {
	id, err := newUserID()
	if err != nil {
		return "", err
	}
	id = "pk_" + id[:20]
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO passkey (id, user_id, credential_id, public_key, algorithm, sign_count,
			aaguid, backup_eligible, backup_state, name, rp_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, p.UserID, p.CredentialID, p.PublicKey, p.Algorithm, p.SignCount,
		p.AAGUID, p.BackupEligible, p.BackupState, p.Name, p.RPID, time.Now().Unix())
	if isUniqueViolation(err) {
		return "", ErrPasskeyExists
	}
	if err != nil {
		return "", fmt.Errorf("add passkey: %w", err)
	}
	return id, nil
}

const passkeyCols = `p.id, p.user_id, p.credential_id, p.public_key, p.algorithm, p.sign_count,
	p.aaguid, p.backup_eligible, p.backup_state, p.name, p.rp_id, p.created_at, COALESCE(p.last_used_at, 0)`

func scanPasskey(sc interface{ Scan(...any) error }) (*Passkey, error) {
	var p Passkey
	var count int64
	if err := sc.Scan(&p.ID, &p.UserID, &p.CredentialID, &p.PublicKey, &p.Algorithm, &count,
		&p.AAGUID, &p.BackupEligible, &p.BackupState, &p.Name, &p.RPID, &p.CreatedAt, &p.LastUsedAt); err != nil {
		return nil, err
	}
	p.SignCount = uint32(count)
	return &p, nil
}

// PasskeyByCredentialID finds the credential a sign-in names. It joins user,
// so a passkey whose account is gone does not resolve.
func (s *Store) PasskeyByCredentialID(ctx context.Context, credID []byte) (*Passkey, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+passkeyCols+`
		FROM passkey p JOIN user u ON u.id = p.user_id
		WHERE p.credential_id = ?`, credID)
	p, err := scanPasskey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("passkey by credential: %w", err)
	}
	return p, nil
}

// PasskeysByUser lists an account's passkeys, oldest first: the order they
// were added in is the order somebody remembers them in.
func (s *Store) PasskeysByUser(ctx context.Context, userID string) ([]Passkey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+passkeyCols+`
		FROM passkey p WHERE p.user_id = ? ORDER BY p.created_at, p.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list passkeys: %w", err)
	}
	defer rows.Close()
	out := []Passkey{}
	for rows.Next() {
		p, err := scanPasskey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// RecordPasskeyUse stores what a successful sign-in reported.
func (s *Store) RecordPasskeyUse(ctx context.Context, id string, count uint32, backupState bool) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE passkey SET sign_count = ?, backup_state = ?, last_used_at = ? WHERE id = ?`,
		int64(count), backupState, time.Now().Unix(), id)
	if err != nil {
		return fmt.Errorf("record passkey use: %w", err)
	}
	return nil
}

// RenamePasskey renames one of the account's own passkeys. Scoped by user so
// one account can never touch another's: ErrNotFound covers both "no such
// passkey" and "not yours".
func (s *Store) RenamePasskey(ctx context.Context, userID, id, name string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE passkey SET name = ? WHERE id = ? AND user_id = ?`, name, id, userID)
	if err != nil {
		return fmt.Errorf("rename passkey: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeletePasskey removes one of the account's own passkeys, scoped like
// RenamePasskey.
func (s *Store) DeletePasskey(ctx context.Context, userID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM passkey WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("delete passkey: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
