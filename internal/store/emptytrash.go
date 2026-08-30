package store

import (
	"context"
	"fmt"
)

/*
 * Removing rows whose files are gone.
 *
 * "Scanning marks missing, never deletes" is one of this project's oldest
 * rules, and it is about an *unmounted drive* rather than about tidiness: a
 * scan that deleted what it could not see would destroy a library the first
 * time a share failed to mount. That rule is not relaxed here. What changes is
 * that a person can ask for the tidying, having been told what it costs.
 *
 * The distinction matters because a missing row is not junk. It is the record
 * of a film somebody watched, with its position, its rating and its history —
 * which is why the detail page still shows one, and why `TrashCount` exists to
 * price this before it happens rather than after.
 *
 * Deliberately not automatic without asking, and deliberately not the default.
 */

// TrashCount is how many rows a library would lose. Priced before it is
// performed, as every expensive-or-irreversible action in this project is.
func (s *Store) TrashCount(ctx context.Context, libraryID int64) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM media_item WHERE library_id = ? AND missing = 1`,
		libraryID).Scan(&n); err != nil {
		return 0, fmt.Errorf("trash count: %w", err)
	}
	return n, nil
}

/*
 * EmptyTrash removes a library's missing rows and reports how many went.
 *
 * The caller decides *whether* — the safety rules live in the scanner, which is
 * the half that knows whether this scan was in a position to judge what is
 * missing. Putting them here would mean this method had to be trusted by every
 * future caller instead of stating a single thing plainly.
 */
func (s *Store) EmptyTrash(ctx context.Context, libraryID int64) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM media_item WHERE library_id = ? AND missing = 1`, libraryID)
	if err != nil {
		return 0, fmt.Errorf("empty trash: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("empty trash: %w", err)
	}
	return n, nil
}
