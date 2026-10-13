package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

/*
 * A library kept from an account (ADR 0084).
 *
 * The second per-account limit, enforced where the first one is: in MayPlay,
 * which GetItem calls for every id that becomes an item, and in the listing
 * predicate, which ListItems and PermittedItems share. One rule with two
 * halves — the rating ceiling and the hidden libraries — so a surface that
 * applies one applies both, and cannot drift into applying only the first.
 *
 * A deny list. No rows means everything is visible, so a library added later
 * is visible to everyone until an administrator narrows somebody, which is the
 * default Chris chose (settings-plan §6, decision 4).
 *
 * Administrators are never restricted. They can create and repoint libraries,
 * so hiding one from them protects nothing; any rows an administrator holds are
 * ignored rather than deleted, so a promotion lifts them and a demotion puts
 * them back.
 */

// HiddenLibraries returns the libraries kept from an account, sorted. Empty
// for an administrator, for an id with no account (the unsecured loopback
// case, which has no accounts and no limits), and for anyone never narrowed.
func (s *Store) HiddenLibraries(ctx context.Context, userID string) ([]int64, error) {
	if userID == "" {
		return nil, nil
	}
	var role string
	if err := s.db.QueryRowContext(ctx,
		`SELECT role FROM user WHERE id = ?`, userID).Scan(&role); err != nil {
		// No account row means no limit, for the reason resolveCeiling records.
		return nil, nil
	}
	if role == RoleAdmin {
		return nil, nil
	}
	return s.hiddenRows(ctx, userID)
}

// hiddenRows is the stored rows, whatever the role: what an administrator
// sees when editing an account, including one that is currently an admin.
func (s *Store) hiddenRows(ctx context.Context, userID string) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT library_id FROM library_hidden WHERE user_id = ? ORDER BY library_id`, userID)
	if err != nil {
		return nil, fmt.Errorf("hidden libraries: %w", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("hidden libraries: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// StoredHiddenLibraries is the rows as an administrator set them, whatever
// the account's role, for the screen that edits them.
func (s *Store) StoredHiddenLibraries(ctx context.Context, userID string) ([]int64, error) {
	return s.hiddenRows(ctx, userID)
}

/*
 * SetHiddenLibraries replaces the libraries kept from an account.
 *
 * Replaces rather than adds, in one transaction, so the screen that sends the
 * whole set cannot leave half of it applied. An id that is not a library is
 * refused: a row that hides nothing looks like protection and is not.
 */
func (s *Store) SetHiddenLibraries(ctx context.Context, userID string, libraryIDs []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("set hidden libraries: %w", err)
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM user WHERE id = ?`, userID).Scan(&exists); err != nil {
		return fmt.Errorf("set hidden libraries: %w", err)
	}
	if exists == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM library_hidden WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("set hidden libraries: %w", err)
	}
	now := time.Now().Unix()
	seen := map[int64]bool{}
	for _, id := range libraryIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM library WHERE id = ?`, id).Scan(&n); err != nil {
			return fmt.Errorf("set hidden libraries: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("library %d: %w", id, ErrNotFound)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO library_hidden (user_id, library_id, hidden_at) VALUES (?, ?, ?)`,
			userID, id, now); err != nil {
			return fmt.Errorf("set hidden libraries: %w", err)
		}
	}
	return tx.Commit()
}

// hiddenPredicate restricts a listing to libraries not kept from the viewer.
// Empty when nothing is hidden, so the ordinary case adds no SQL.
func hiddenPredicate(hidden []int64) (string, []any) {
	if len(hidden) == 0 {
		return "", nil
	}
	args := make([]any, len(hidden))
	for i, id := range hidden {
		args[i] = id
	}
	return ` AND media_item.library_id NOT IN (` + placeholders(len(hidden)) + `)`, args
}

// libraryHidden reports whether one library is kept from an account.
func libraryHidden(hidden []int64, libraryID int64) bool {
	i := sort.Search(len(hidden), func(i int) bool { return hidden[i] >= libraryID })
	return i < len(hidden) && hidden[i] == libraryID
}

// MayOpenLibrary reports whether an account may see a library at all: the
// check every /api/libraries/{id}/ route makes before answering.
func (s *Store) MayOpenLibrary(ctx context.Context, userID string, libraryID int64) (bool, error) {
	hidden, err := s.HiddenLibraries(ctx, userID)
	if err != nil {
		return false, err
	}
	return !libraryHidden(hidden, libraryID), nil
}

// accountAccess is everything that limits what one account sees.
type accountAccess struct {
	ceiling string
	hidden  []int64
}

// accessFor reads both halves of an account's limits.
func (s *Store) accessFor(ctx context.Context, userID string) (accountAccess, error) {
	var a accountAccess
	if userID == "" {
		return a, nil
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT max_content_rating FROM user WHERE id = ?`, userID).Scan(&a.ceiling); err != nil {
		return accountAccess{}, nil
	}
	hidden, err := s.HiddenLibraries(ctx, userID)
	if err != nil {
		return accountAccess{}, err
	}
	a.hidden = hidden
	return a, nil
}

// predicate is the whole rule as one WHERE fragment. Empty when the account
// has no limits of either kind.
func (a accountAccess) predicate() (string, []any) {
	ceil, ceilArgs := ceilingPredicate(a.ceiling)
	hid, hidArgs := hiddenPredicate(a.hidden)
	return ceil + hid, append(ceilArgs, hidArgs...)
}

/*
 * hiddenClause excludes rows whose item sits in a library kept from the
 * account, for queries over that account's own records — its history, its
 * ratings, its year — which key on an item id rather than listing media_item.
 *
 * The ids are written into the SQL rather than bound. They are int64s read
 * back from the database, so formatting them cannot inject anything, and it
 * lets the clause drop into queries whose placeholder order is already fixed
 * without every caller re-threading its arguments. A NULL item (a history row
 * whose item has since been deleted) is kept: it belongs to no library.
 *
 * Empty for an account with nothing hidden, so the ordinary case is unchanged.
 */
func (s *Store) hiddenClause(ctx context.Context, userID, col string) (string, error) {
	hidden, err := s.HiddenLibraries(ctx, userID)
	if err != nil || len(hidden) == 0 {
		return "", err
	}
	ids := make([]string, len(hidden))
	for i, id := range hidden {
		ids[i] = strconv.FormatInt(id, 10)
	}
	return ` AND (` + col + ` IS NULL OR ` + col + ` NOT IN (SELECT id FROM media_item WHERE library_id IN (` +
		strings.Join(ids, ",") + `)))`, nil
}

// LibraryOfCluster is the library a face cluster belongs to, so its routes can
// be refused for a library kept from the caller. ErrNotFound for no cluster.
func (s *Store) LibraryOfCluster(ctx context.Context, clusterID int64) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT library_id FROM face_cluster WHERE id = ?`, clusterID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

// LibraryOfFace is the library the photograph holding a face belongs to.
func (s *Store) LibraryOfFace(ctx context.Context, faceID int64) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT mi.library_id FROM face f
		JOIN media_item mi ON mi.id = f.item_id WHERE f.id = ?`, faceID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}
