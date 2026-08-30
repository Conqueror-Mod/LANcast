package store

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

/*
 * Answering the collision report.
 *
 * ADR 0042 decided LANcast reports a shared identity and never resolves it —
 * never merges, never ranks, never deletes — because two of the thirteen pairs
 * it was written against were not duplicates at all. That holds. What it left
 * out is that a person who *has* looked, and decided the pair is fine, had no
 * way to say so: a film in two parts and a second edition kept on purpose were
 * listed again every time the page opened.
 *
 * A report that cannot be answered is one people stop reading, and the cost of
 * that falls on the entries that do want attention. Dismissing is not resolving
 * — nothing is merged, ranked or deleted, and both files stay exactly as they
 * are. It records that somebody looked.
 */

/*
 * dismissKey identifies the exact set of rows somebody accepted.
 *
 * Sorted, so the key does not depend on the order the query happened to return
 * them in — which is by size and then id, and would change the moment a file
 * was replaced with a differently sized copy of itself.
 */
func dismissKey(itemIDs []int64) string {
	ids := append([]int64(nil), itemIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}

// DismissCollision records that somebody has looked at exactly these rows and
// accepted them.
func (s *Store) DismissCollision(ctx context.Context, itemIDs []int64, now int64) error {
	if len(itemIDs) < 2 {
		// A "collision" of one row is not one, and recording it would leave a
		// key nothing can ever match — an invisible row that outlives its own
		// meaning.
		return fmt.Errorf("dismiss collision: needs at least two items, got %d", len(itemIDs))
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO dismissed_collision (members, dismissed_at) VALUES (?, ?)
		 ON CONFLICT(members) DO UPDATE SET dismissed_at = excluded.dismissed_at`,
		dismissKey(itemIDs), now)
	if err != nil {
		return fmt.Errorf("dismiss collision: %w", err)
	}
	return nil
}

// RestoreCollision puts a dismissed collision back in the report.
func (s *Store) RestoreCollision(ctx context.Context, itemIDs []int64) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM dismissed_collision WHERE members = ?`, dismissKey(itemIDs)); err != nil {
		return fmt.Errorf("restore collision: %w", err)
	}
	return nil
}

// dismissedKeys is every accepted member set, for filtering a listing.
func (s *Store) dismissedKeys(ctx context.Context) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT members, dismissed_at FROM dismissed_collision`)
	if err != nil {
		return nil, fmt.Errorf("dismissed collisions: %w", err)
	}
	defer rows.Close()

	out := map[string]int64{}
	for rows.Next() {
		var k string
		var at int64
		if err := rows.Scan(&k, &at); err != nil {
			return nil, fmt.Errorf("dismissed collisions: %w", err)
		}
		out[k] = at
	}
	return out, rows.Err()
}
