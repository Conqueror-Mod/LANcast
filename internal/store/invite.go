package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

/*
 * Invites (ADR 0104): a link that makes one member account, once.
 *
 * The secret in the link is never stored, only its hash, so a copy of the
 * database is not a set of working links. Redeeming creates the account,
 * applies the invite's limits and deletes the invite in one transaction: two
 * people racing the same link get one account between them, and nobody gets
 * an account without its limits.
 */

// Invite is an outstanding invite as an administrator sees it. Never the
// secret, which exists only in the answer that made it.
type Invite struct {
	ID               string  `json:"id"`
	Note             string  `json:"note"`
	MaxContentRating string  `json:"max_content_rating,omitempty"`
	HiddenLibraries  []int64 `json:"hidden_libraries"`
	CreatedBy        string  `json:"created_by"`
	CreatedAt        int64   `json:"created_at"`
	ExpiresAt        int64   `json:"expires_at"`
}

// ErrInviteUnusable is an invite that is unknown, spent, withdrawn or
// expired: one error for all four, so nothing that reports it can say which.
var ErrInviteUnusable = errors.New("this invite has been used, withdrawn, or has expired")

func joinIDs(ids []int64) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	return strings.Join(parts, ",")
}

func splitIDs(s string) []int64 {
	out := []int64{}
	for _, p := range strings.Split(s, ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// CreateInvite records an invite under the hash of its secret.
func (s *Store) CreateInvite(ctx context.Context, inv Invite, tokenHash []byte) (*Invite, error) {
	if inv.ID == "" {
		id, err := newUserID()
		if err != nil {
			return nil, err
		}
		inv.ID = "i" + strings.TrimPrefix(id, "u")
	}
	if inv.HiddenLibraries == nil {
		inv.HiddenLibraries = []int64{}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO invite (id, token_hash, note, max_content_rating, hidden_libraries, created_by, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		inv.ID, tokenHash, inv.Note, inv.MaxContentRating, joinIDs(inv.HiddenLibraries),
		inv.CreatedBy, inv.CreatedAt, inv.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("create invite: %w", err)
	}
	return &inv, nil
}

// Invites lists the outstanding invites, newest first, deleting any that
// have expired on the way: an expired invite is not outstanding, and its row
// has nothing left to say.
func (s *Store) Invites(ctx context.Context, now time.Time) ([]Invite, error) {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM invite WHERE expires_at <= ?`, now.Unix()); err != nil {
		return nil, fmt.Errorf("prune invites: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, note, max_content_rating, hidden_libraries, created_by, created_at, expires_at
		FROM invite ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		var inv Invite
		var hidden string
		if err := rows.Scan(&inv.ID, &inv.Note, &inv.MaxContentRating, &hidden,
			&inv.CreatedBy, &inv.CreatedAt, &inv.ExpiresAt); err != nil {
			return nil, fmt.Errorf("list invites: %w", err)
		}
		inv.HiddenLibraries = splitIDs(hidden)
		out = append(out, inv)
	}
	return out, rows.Err()
}

// DeleteInvite withdraws an invite. ErrNotFound when there is none.
func (s *Store) DeleteInvite(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM invite WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete invite: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// InviteByToken is the invite behind a secret's hash, if it can still be
// used.
func (s *Store) InviteByToken(ctx context.Context, tokenHash []byte, now time.Time) (*Invite, error) {
	var inv Invite
	var hidden string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, note, max_content_rating, hidden_libraries, created_by, created_at, expires_at
		FROM invite WHERE token_hash = ? AND expires_at > ?`, tokenHash, now.Unix()).Scan(
		&inv.ID, &inv.Note, &inv.MaxContentRating, &hidden, &inv.CreatedBy, &inv.CreatedAt, &inv.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInviteUnusable
	}
	if err != nil {
		return nil, fmt.Errorf("read invite: %w", err)
	}
	inv.HiddenLibraries = splitIDs(hidden)
	return &inv, nil
}

/*
 * RedeemInvite spends an invite on a new member account: name, password hash,
 * the invite's content ceiling and hidden libraries, and the invite deleted,
 * all in one transaction.
 *
 * ErrInviteUnusable if the invite cannot be used (including when another
 * redemption won the race); ErrDuplicate if the name is taken, in which case
 * the invite is untouched and can be tried again with another name. A
 * library the invite named that has since been removed is skipped: keeping a
 * library that no longer exists from somebody needs no row.
 */
func (s *Store) RedeemInvite(ctx context.Context, tokenHash []byte, name, passwordHash string, now time.Time) (*User, *Invite, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("redeem invite: %w", err)
	}
	defer tx.Rollback()

	var inv Invite
	var hidden string
	err = tx.QueryRowContext(ctx, `
		SELECT id, note, max_content_rating, hidden_libraries, created_by, created_at, expires_at
		FROM invite WHERE token_hash = ? AND expires_at > ?`, tokenHash, now.Unix()).Scan(
		&inv.ID, &inv.Note, &inv.MaxContentRating, &hidden, &inv.CreatedBy, &inv.CreatedAt, &inv.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrInviteUnusable
	}
	if err != nil {
		return nil, nil, fmt.Errorf("redeem invite: %w", err)
	}
	inv.HiddenLibraries = splitIDs(hidden)

	// Deleted first: if this deletes nothing, another redemption already
	// spent it between the read and here.
	res, err := tx.ExecContext(ctx, `DELETE FROM invite WHERE id = ?`, inv.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("redeem invite: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, nil, ErrInviteUnusable
	}

	id, err := newUserID()
	if err != nil {
		return nil, nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO user (id, name, password_hash, role, created_at, max_content_rating)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, name, passwordHash, RoleMember, now.Unix(), inv.MaxContentRating); err != nil {
		if isUniqueViolation(err) {
			return nil, nil, ErrDuplicate
		}
		return nil, nil, fmt.Errorf("redeem invite: %w", err)
	}
	for _, lib := range inv.HiddenLibraries {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO library_hidden (user_id, library_id, hidden_at)
			SELECT ?, id, ? FROM library WHERE id = ?`, id, now.Unix(), lib); err != nil {
			return nil, nil, fmt.Errorf("redeem invite: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("redeem invite: %w", err)
	}
	return &User{ID: id, Name: name, PasswordHash: passwordHash, Role: RoleMember,
		CreatedAt: now.Unix(), MaxContentRating: inv.MaxContentRating}, &inv, nil
}
