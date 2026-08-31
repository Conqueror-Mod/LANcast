package store

import (
	"context"
	"fmt"
)

/*
 * Sensitive marks (ADR 0051).
 *
 * A person marks a folder or a photo; everything beneath a marked folder is
 * obscured with it. The mark is theirs, so it obeys the locked-fields rule —
 * no scan, refresh or merge may clear it, and nothing here is reachable from
 * the scanner except RefreshSensitivity, which only ever recomputes what the
 * marks already imply.
 *
 * This obscures. It does not restrict: anyone who can see the library can still
 * open the folder, they just have to say so first. Building it as though it
 * restricted would produce a privacy control that quietly does not work.
 */

// SetSensitive marks or unmarks one item, then re-resolves its library.
//
// Unmarking writes 0 rather than NULL. The distinction is real — NULL is "no
// one has considered this", 0 is "somebody looked and said no" — and it is the
// difference between a folder that has never been asked about and one that has
// been deliberately cleared.
func (s *Store) SetSensitive(ctx context.Context, id int64, on bool) error {
	v := 0
	if on {
		v = 1
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE media_item SET sensitive = ? WHERE id = ?`, v, id)
	if err != nil {
		return fmt.Errorf("set sensitive on item %d: %w", id, err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("set sensitive: no item %d", id)
	}

	var libraryID int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT library_id FROM media_item WHERE id = ?`, id).Scan(&libraryID); err != nil {
		return fmt.Errorf("set sensitive on item %d: read library: %w", id, err)
	}
	return s.RefreshSensitivity(ctx, libraryID)
}

/*
 * RefreshSensitivity re-resolves one library's inherited marks.
 *
 * Top-down from the library's roots: a row is obscured when it carries a mark
 * of its own or when anything above it does. Written as a whole-library
 * recompute rather than an incremental update on purpose.
 *
 * The incremental version has to run at three moments that do not reliably
 * happen in the order it needs. An item is inserted before it is given a
 * parent, so inheriting at insert inherits from nothing; a folder can be marked
 * before the scan that fills it, so the photos arrive after the propagation
 * that would have covered them; and a rescan can move a file between folders
 * without either folder being touched by a mark. Each of those is a quiet
 * wrong answer that nothing fails on — the picture is simply shown.
 *
 * A recompute cannot be out of date for a reason nobody thought of. It is O(n)
 * over a library that has just been walked file by file, and it is skipped
 * entirely when the library holds no marks at all, which is every library
 * belonging to everyone who never turns this on.
 */
func (s *Store) RefreshSensitivity(ctx context.Context, libraryID int64) error {
	// The common case, and worth its own query: with nothing marked there is
	// nothing to inherit, and the only rows that can be wrong are ones left
	// over from a mark that has since been cleared.
	var marks int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM media_item WHERE library_id = ? AND sensitive = 1`,
		libraryID).Scan(&marks); err != nil {
		return fmt.Errorf("refresh sensitivity of library %d: %w", libraryID, err)
	}
	if marks == 0 {
		_, err := s.db.ExecContext(ctx,
			`UPDATE media_item SET sensitive_effective = 0
			 WHERE library_id = ? AND sensitive_effective <> 0`, libraryID)
		if err != nil {
			return fmt.Errorf("refresh sensitivity of library %d: %w", libraryID, err)
		}
		return nil
	}

	/*
	 * The walk starts at rows with no parent *within the library*, which is not
	 * quite "parent_id IS NULL": a photo can hang off a gallery that hangs off
	 * nothing, and a film has no parent at all. Both are roots for this purpose.
	 *
	 * Only rows whose answer changes are written, so a rescan of an unmarked
	 * library writes nothing and a marked one writes only what moved.
	 */
	_, err := s.db.ExecContext(ctx, `
		WITH RECURSIVE tree(id, eff) AS (
			SELECT id, COALESCE(sensitive, 0)
			  FROM media_item
			 WHERE library_id = ? AND parent_id IS NULL
			UNION ALL
			SELECT m.id,
			       CASE WHEN COALESCE(m.sensitive, 0) = 1 OR t.eff = 1 THEN 1 ELSE 0 END
			  FROM media_item m
			  JOIN tree t ON m.parent_id = t.id
		)
		UPDATE media_item
		   SET sensitive_effective = tree.eff
		  FROM tree
		 WHERE media_item.id = tree.id
		   AND media_item.sensitive_effective <> tree.eff`, libraryID)
	if err != nil {
		return fmt.Errorf("refresh sensitivity of library %d: %w", libraryID, err)
	}
	return nil
}

// SensitiveMarksExist reports whether anything in the library carries a mark.
// The client asks so it can leave the whole apparatus out of a library nobody
// has marked anything in.
func (s *Store) SensitiveMarksExist(ctx context.Context, libraryID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM media_item WHERE library_id = ? AND sensitive = 1`,
		libraryID).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("count sensitive marks in library %d: %w", libraryID, err)
	}
	return n > 0, nil
}
