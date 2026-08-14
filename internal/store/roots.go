package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ErrRootOverlaps is returned when a proposed root is the same directory as an
// existing one, or contains it, or sits inside it.
var ErrRootOverlaps = errors.New("that location overlaps a library location that already exists")

// ErrLastRoot is returned when removing a root would leave a library with none.
var ErrLastRoot = errors.New("a library must keep at least one location")

// LibraryRoot is one place a library's files live (ADR 0034).
type LibraryRoot struct {
	ID        int64  `json:"id"`
	LibraryID int64  `json:"library_id"`
	Path      string `json:"path"`
	CreatedAt int64  `json:"created_at"`
	// ItemCount is how many rows would go if this location were removed.
	//
	// Carried on the listing rather than fetched separately because it is what
	// the confirmation has to say, and a removal dialog that cannot name the
	// cost is a removal dialog nobody can answer honestly.
	ItemCount int `json:"item_count"`
}

/*
 * normalizeRoot puts a path into the form the overlap test compares.
 *
 * Deliberately separate from what is *stored*. `RepointRoot` is an exact prefix
 * swap on the stored strings, because the stored strings are what every other
 * query joins on, and a normalising rewrite would "fix" paths into a form the
 * filesystem layer never produced. This normalisation exists only to answer a
 * question — do these two overlap? — and its answer is never written down.
 *
 * Case-folded on Windows, where `D:\Media` and `d:\media` are one directory and
 * a case-sensitive comparison would happily accept both as separate roots, then
 * scan the same files twice under two ids. Not folded elsewhere, because on
 * Linux they really are two directories and refusing the second would be a
 * limitation invented by this function.
 */
func normalizeRoot(p string) string {
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

// within reports whether child is parent, or is inside it.
//
// Compared by path *components* rather than by string prefix: `/mnt/films` is
// a prefix of `/mnt/films2` while containing none of it, and a prefix test
// would reject a perfectly good second location for sharing a name with the
// first.
func within(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		// Different volumes on Windows. Unrelated by construction.
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// rootsOverlap reports whether two roots are the same directory or one holds
// the other.
func rootsOverlap(a, b string) bool {
	na, nb := normalizeRoot(a), normalizeRoot(b)
	return within(na, nb) || within(nb, na)
}

func scanRoots(rows *sql.Rows) ([]LibraryRoot, error) {
	defer rows.Close()
	out := []LibraryRoot{}
	for rows.Next() {
		var r LibraryRoot
		if err := rows.Scan(&r.ID, &r.LibraryID, &r.Path, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// rootItemCount counts the file-backed rows filed under a location. Written as
// a correlated subquery so a listing stays one round trip however many
// locations a library has.
const rootItemCount = `(SELECT COUNT(*) FROM media_item i WHERE i.root_id = r.id)`

func scanRootsWithCounts(rows *sql.Rows) ([]LibraryRoot, error) {
	defer rows.Close()
	out := []LibraryRoot{}
	for rows.Next() {
		var r LibraryRoot
		if err := rows.Scan(&r.ID, &r.LibraryID, &r.Path, &r.CreatedAt, &r.ItemCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RootsByLibrary returns every root grouped by library.
//
// One query for the whole listing rather than one per library: a server with a
// dozen libraries would otherwise pay a dozen round trips to render a page that
// is mostly names.
func (s *Store) RootsByLibrary(ctx context.Context) (map[int64][]LibraryRoot, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.id, r.library_id, r.path, r.created_at, `+rootItemCount+`
		 FROM library_root r ORDER BY r.library_id, r.id`)
	if err != nil {
		return nil, fmt.Errorf("roots by library: %w", err)
	}
	all, err := scanRootsWithCounts(rows)
	if err != nil {
		return nil, fmt.Errorf("roots by library: %w", err)
	}
	out := map[int64][]LibraryRoot{}
	for _, r := range all {
		out[r.LibraryID] = append(out[r.LibraryID], r)
	}
	return out, nil
}

// ListRoots returns a library's locations, oldest first — so the first is the
// one the library was created with, which is what Library.Path reports.
func (s *Store) ListRoots(ctx context.Context, libraryID int64) ([]LibraryRoot, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.id, r.library_id, r.path, r.created_at, `+rootItemCount+`
		 FROM library_root r WHERE r.library_id = ? ORDER BY r.id`, libraryID)
	if err != nil {
		return nil, fmt.Errorf("list roots: %w", err)
	}
	out, err := scanRootsWithCounts(rows)
	if err != nil {
		return nil, fmt.Errorf("list roots: %w", err)
	}
	return out, nil
}

// AllRoots returns every root in the database, across every library.
//
// The overlap check needs all of them, not just the target library's: two
// libraries pointed at the same directory would scan the same files twice, and
// `media_item.path` is UNIQUE, so the second library's items would fight the
// first's rather than coexist. Which library a directory belongs to is a
// question with one answer.
func (s *Store) AllRoots(ctx context.Context) ([]LibraryRoot, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, library_id, path, created_at FROM library_root ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("all roots: %w", err)
	}
	out, err := scanRoots(rows)
	if err != nil {
		return nil, fmt.Errorf("all roots: %w", err)
	}
	return out, nil
}

// GetRoot returns one root, or ErrNotFound.
func (s *Store) GetRoot(ctx context.Context, id int64) (*LibraryRoot, error) {
	var r LibraryRoot
	err := s.db.QueryRowContext(ctx,
		`SELECT id, library_id, path, created_at FROM library_root WHERE id = ?`, id).
		Scan(&r.ID, &r.LibraryID, &r.Path, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get root: %w", err)
	}
	return &r, nil
}

// RootForItem returns the root an item was scanned under.
//
// This is what the containment check resolves against (ADR 0034). It returns
// exactly one root, which is the entire point: a library with several
// locations must never answer "does *any* of my roots contain this path",
// because a row pointing under the wrong one would pass on the strength of some
// root matching.
func (s *Store) RootForItem(ctx context.Context, itemID int64) (*LibraryRoot, error) {
	var r LibraryRoot
	err := s.db.QueryRowContext(ctx, `
		SELECT r.id, r.library_id, r.path, r.created_at
		FROM library_root r JOIN media_item i ON i.root_id = r.id
		WHERE i.id = ?`, itemID).
		Scan(&r.ID, &r.LibraryID, &r.Path, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("root for item: %w", err)
	}
	return &r, nil
}

// AddRoot gives a library another location.
//
// Overlap is refused rather than resolved. A root nested inside another has no
// good answer at scan time: the files under the inner one are walked by both
// passes, `media_item.path` is UNIQUE so the second upsert fights the first,
// and `root_id` ends up belonging to whichever pass ran last — which is to say
// the containment check would resolve against a root chosen by scan ordering.
// Refusing at the boundary is the only place this is cheap.
func (s *Store) AddRoot(ctx context.Context, libraryID int64, path string) (*LibraryRoot, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("add root: path is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("add root: %w", err)
	}
	defer tx.Rollback()

	// The library has to exist. The foreign key would catch it, but as an
	// opaque constraint failure rather than as "no such library".
	var exists int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM library WHERE id = ?`, libraryID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("add root: %w", err)
	}
	if exists == 0 {
		return nil, ErrNotFound
	}

	// Checked inside the transaction against every root, so two roots added
	// concurrently cannot both pass a check the other invalidates.
	rows, err := tx.QueryContext(ctx, `SELECT id, library_id, path, created_at FROM library_root`)
	if err != nil {
		return nil, fmt.Errorf("add root: %w", err)
	}
	existing, err := scanRoots(rows)
	if err != nil {
		return nil, fmt.Errorf("add root: %w", err)
	}
	for _, r := range existing {
		if rootsOverlap(r.Path, path) {
			return nil, fmt.Errorf("%w: %s", ErrRootOverlaps, r.Path)
		}
	}

	now := time.Now().Unix()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO library_root (library_id, path, created_at) VALUES (?, ?, ?)`,
		libraryID, path, now)
	if err != nil {
		return nil, fmt.Errorf("add root: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("add root: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("add root: %w", err)
	}
	return &LibraryRoot{ID: id, LibraryID: libraryID, Path: path, CreatedAt: now}, nil
}

/*
 * RemoveRoot drops a location and everything scanned under it.
 *
 * This deletes rather than marking missing, and the distinction is the whole
 * reason it is safe to. "Scanning marks missing, never deletes" governs what
 * the server may *infer*: a scan deduces absence from not finding a file, and
 * that deduction is wrong when a drive is merely unplugged, so it must never be
 * destructive. Removing a root is not a deduction. It is a person saying this
 * location is not part of this library any more — the same class of act as
 * deleting the library, which already cascades.
 *
 * The last root cannot go. A library with no locations is not a degraded
 * library, it is a row that cannot be scanned, resolved or repointed, and the
 * honest way to remove the final location is to delete the library.
 */
func (s *Store) RemoveRoot(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("remove root: %w", err)
	}
	defer tx.Rollback()

	var libraryID int64
	err = tx.QueryRowContext(ctx,
		`SELECT library_id FROM library_root WHERE id = ?`, id).Scan(&libraryID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("remove root: %w", err)
	}

	var n int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM library_root WHERE library_id = ?`, libraryID).Scan(&n); err != nil {
		return fmt.Errorf("remove root: %w", err)
	}
	if n <= 1 {
		return ErrLastRoot
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM library_root WHERE id = ?`, id); err != nil {
		return fmt.Errorf("remove root: %w", err)
	}
	return tx.Commit()
}

// CountItemsInRoot reports how many items would go with a root.
//
// For the confirmation before RemoveRoot. Deleting a location silently is the
// one part of this that could lose watch history without saying so.
func (s *Store) CountItemsInRoot(ctx context.Context, rootID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM media_item WHERE root_id = ?`, rootID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count items in root: %w", err)
	}
	return n, nil
}

// KnownFilesInRoot is KnownFiles scoped to one location.
//
// Reconciliation is per root, so that an unplugged drive marks its own items
// missing and not one file on a root that is present. A scan that walked two of
// three locations must compare what it saw against what those two held, never
// against the library — which is the difference between "3 of 4 locations
// scanned" and silently emptying the fourth.
func (s *Store) KnownFilesInRoot(ctx context.Context, rootID int64) (map[string]FileState, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, kind, path, size_bytes, mtime, missing FROM media_item
		 WHERE root_id = ? AND container IS NOT NULL`, rootID)
	if err != nil {
		return nil, fmt.Errorf("known files in root: %w", err)
	}
	defer rows.Close()

	out := map[string]FileState{}
	for rows.Next() {
		var path string
		var missing int
		var st FileState
		if err := rows.Scan(&st.ID, &st.Kind, &path, &st.SizeBytes, &st.MTime, &missing); err != nil {
			return nil, fmt.Errorf("known files in root: %w", err)
		}
		st.Missing = missing != 0
		out[path] = st
	}
	return out, rows.Err()
}

// RootPaths maps a library's location ids to their paths.
//
// For the reconciliation passes, which run once over a whole library and must
// still derive each item's structure from the location that item is actually
// in. Read as a map rather than per item: these passes already hold every row
// in memory, and a lookup per episode would be a query per episode.
func (s *Store) RootPaths(ctx context.Context, libraryID int64) (map[int64]string, error) {
	roots, err := s.ListRoots(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]string, len(roots))
	for _, r := range roots {
		out[r.ID] = r.Path
	}
	return out, nil
}
