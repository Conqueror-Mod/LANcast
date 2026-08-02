package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Roles. The split is a real privilege boundary (ADR 0015): only an admin can
// create libraries — which is arbitrary filesystem read access — reach settings,
// or manage users. A member browses, plays, and owns their own watch state.
const (
	RoleAdmin  = "admin"
	RoleMember = "member"
)

// LocalUserID is the id the pre-multi-user owner is migrated to. It matches the
// 'local' default that session and playback_state have carried since revision 1,
// so seeding it preserves every existing session and resume point.
const LocalUserID = "local"

// ErrDuplicate is returned when a unique constraint would be violated — a
// username already in use.
var ErrDuplicate = errors.New("already exists")

// User is an account. PasswordHash is never serialized to a client.
type User struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"`
	CreatedAt    int64  `json:"created_at"`
}

// ValidRole reports whether r is a role the system recognises.
func ValidRole(r string) bool { return r == RoleAdmin || r == RoleMember }

// CreateUser inserts a user with the given id, or a fresh random id when id is
// empty. It returns ErrDuplicate if the name is already taken.
func (s *Store) CreateUser(ctx context.Context, id, name, passwordHash, role string) (*User, error) {
	if id == "" {
		var err error
		if id, err = newUserID(); err != nil {
			return nil, err
		}
	}
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user (id, name, password_hash, role, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, name, passwordHash, role, now)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicate
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	return &User{ID: id, Name: name, PasswordHash: passwordHash, Role: role, CreatedAt: now}, nil
}

// UserByName looks up a user for login. The name match is case-insensitive
// because the column is COLLATE NOCASE.
func (s *Store) UserByName(ctx context.Context, name string) (*User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, name, password_hash, role, created_at FROM user WHERE name = ?`, name))
}

// UserByID looks up a user by id.
func (s *Store) UserByID(ctx context.Context, id string) (*User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT id, name, password_hash, role, created_at FROM user WHERE id = ?`, id))
}

func (s *Store) scanUser(row *sql.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Name, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan user: %w", err)
	}
	return &u, nil
}

// ListUsers returns all users, oldest first. Password hashes are populated but
// tagged json:"-", so a handler serializing the slice never leaks them.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, password_hash, role, created_at FROM user ORDER BY created_at, name`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.PasswordHash, &u.Role, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CountUsers reports how many accounts exist. Zero means the instance is
// unconfigured — the same condition that keeps it bound to loopback.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

// CountSessions reports how many live sessions exist, so a reset can say what
// it is about to revoke before it does it.
func (s *Store) CountSessions(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM session`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count sessions: %w", err)
	}
	return n, nil
}

// DeleteAllUsers removes every account and every session, returning the counts
// removed. It is the recovery path for an operator locked out of their own
// server, and it is deliberately not reachable over HTTP — an authenticated
// caller does not need it, and an unauthenticated one must never have it.
//
// playback_state is left alone. Those rows are the library's watch history, not
// account data, and the first admin created afterwards takes LocalUserID — the
// id they already carry — so the history reconnects to the new account instead
// of being orphaned. DeleteUser drops one user's history because deleting a
// person means deleting their data; wiping the account table to get back in
// does not.
//
// The instance is unconfigured afterwards, which is the same state a fresh
// install is in: loopback-only until an account exists.
func (s *Store) DeleteAllUsers(ctx context.Context) (users, sessions int64, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("reset auth: %w", err)
	}
	defer tx.Rollback()

	sres, err := tx.ExecContext(ctx, `DELETE FROM session`)
	if err != nil {
		return 0, 0, fmt.Errorf("reset auth: delete sessions: %w", err)
	}
	ures, err := tx.ExecContext(ctx, `DELETE FROM user`)
	if err != nil {
		return 0, 0, fmt.Errorf("reset auth: delete users: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("reset auth: %w", err)
	}

	sessions, _ = sres.RowsAffected()
	users, _ = ures.RowsAffected()
	return users, sessions, nil
}

// CountAdmins reports how many admins exist, so the last one cannot be deleted
// or demoted into a lockout.
func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user WHERE role = ?`, RoleAdmin).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count admins: %w", err)
	}
	return n, nil
}

// SetUserPassword replaces a user's password hash. Callers revoke that user's
// sessions separately — the two are distinct decisions (an admin reset should
// log the user out; a routine change need not be tangled with it here).
func (s *Store) SetUserPassword(ctx context.Context, id, passwordHash string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE user SET password_hash = ? WHERE id = ?`, passwordHash, id)
	if err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteUser removes an account together with its sessions and playback state,
// in one transaction. There is no database-level foreign key from those tables
// to user (their user_id predates the user table), so the cleanup is explicit
// rather than a cascade — and atomic, so a deleted user can never keep acting on
// an already-issued cookie.
func (s *Store) DeleteUser(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM session WHERE user_id = ?`, id); err != nil {
		return fmt.Errorf("delete user sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM playback_state WHERE user_id = ?`, id); err != nil {
		return fmt.Errorf("delete user playback: %w", err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM user WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// DeleteUserSessions logs one user out everywhere without touching other users.
func (s *Store) DeleteUserSessions(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM session WHERE user_id = ?`, id)
	return err
}

func newUserID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate user id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// isUniqueViolation reports whether err is a UNIQUE constraint failure. The
// pure-Go driver surfaces this in the message rather than a typed code, so a
// substring match is the portable check.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
