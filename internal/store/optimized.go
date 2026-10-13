package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

/*
 * Optimized copies (ADR 0087): which titles have one, want one, or failed to
 * get one. The files are the worker's; this is their bookkeeping.
 */

// The states a copy moves through: queued → converting → ready, or failed.
// Waiting for space is queued: it is still wanted, and goes as soon as there
// is room.
const (
	OptimizedQueued     = "queued"
	OptimizedConverting = "converting"
	OptimizedReady      = "ready"
	OptimizedFailed     = "failed"
)

// OptimizedVersion is one title's copy.
type OptimizedVersion struct {
	ItemID      int64  `json:"item_id"`
	Title       string `json:"title"`
	Kind        string `json:"kind"`
	State       string `json:"state"`
	SizeBytes   int64  `json:"size_bytes"`
	Error       string `json:"error,omitempty"`
	RequestedAt int64  `json:"requested_at"`
	FinishedAt  *int64 `json:"finished_at,omitempty"`
	// DurationMS is the title's length, so a queue can estimate its size.
	DurationMS *int64 `json:"duration_ms,omitempty"`
	// ProbeJSON is the finished copy's ffprobe output. Not sent to clients.
	ProbeJSON string `json:"-"`
}

// RequestOptimized queues titles for a copy. One already queued, converting
// or ready is left alone; one that failed is queued again, which is what
// asking again means. Returns how many were newly queued.
func (s *Store) RequestOptimized(ctx context.Context, itemIDs []int64) (int, error) {
	now := time.Now().Unix()
	n := 0
	for _, id := range itemIDs {
		res, err := s.db.ExecContext(ctx, `
			INSERT INTO optimized_version (item_id, state, requested_at) VALUES (?, ?, ?)
			ON CONFLICT(item_id) DO UPDATE SET state = excluded.state, error = '',
			    requested_at = excluded.requested_at
			WHERE optimized_version.state = ?`,
			id, OptimizedQueued, now, OptimizedFailed)
		if err != nil {
			return n, fmt.Errorf("request optimized: %w", err)
		}
		if c, _ := res.RowsAffected(); c > 0 {
			n++
		}
	}
	return n, nil
}

// RemoveOptimized forgets titles' copies. The worker's sweep removes the
// files, and a copy being converted is stopped by its row going.
func (s *Store) RemoveOptimized(ctx context.Context, itemIDs []int64) error {
	for _, id := range itemIDs {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM optimized_version WHERE item_id = ?`, id); err != nil {
			return fmt.Errorf("remove optimized: %w", err)
		}
	}
	return nil
}

const optimizedCols = `ov.item_id, mi.title, mi.kind, ov.state, ov.size_bytes, ov.error,
	ov.requested_at, ov.finished_at, mi.duration_ms, COALESCE(ov.probe_json, '')`

func scanOptimized(sc interface{ Scan(...any) error }) (OptimizedVersion, error) {
	var v OptimizedVersion
	err := sc.Scan(&v.ItemID, &v.Title, &v.Kind, &v.State, &v.SizeBytes, &v.Error,
		&v.RequestedAt, &v.FinishedAt, &v.DurationMS, &v.ProbeJSON)
	return v, err
}

// ListOptimized is every copy and request: converting first, then queued in
// the order they will run, then the rest newest first.
func (s *Store) ListOptimized(ctx context.Context) ([]OptimizedVersion, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+optimizedCols+`
		FROM optimized_version ov JOIN media_item mi ON mi.id = ov.item_id
		ORDER BY CASE ov.state WHEN 'converting' THEN 0 WHEN 'queued' THEN 1 ELSE 2 END,
		         CASE WHEN ov.state = 'queued' THEN ov.requested_at END ASC,
		         ov.requested_at DESC, ov.item_id`)
	if err != nil {
		return nil, fmt.Errorf("list optimized: %w", err)
	}
	defer rows.Close()
	out := []OptimizedVersion{}
	for rows.Next() {
		v, err := scanOptimized(rows)
		if err != nil {
			return nil, fmt.Errorf("list optimized: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// NextOptimized is the oldest queued request, or ErrNotFound when none is.
func (s *Store) NextOptimized(ctx context.Context) (OptimizedVersion, error) {
	v, err := scanOptimized(s.db.QueryRowContext(ctx, `SELECT `+optimizedCols+`
		FROM optimized_version ov JOIN media_item mi ON mi.id = ov.item_id
		WHERE ov.state = 'queued' ORDER BY ov.requested_at, ov.item_id LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return OptimizedVersion{}, ErrNotFound
	}
	return v, err
}

// ReadyOptimized is a title's finished copy, or ErrNotFound.
func (s *Store) ReadyOptimized(ctx context.Context, itemID int64) (OptimizedVersion, error) {
	v, err := scanOptimized(s.db.QueryRowContext(ctx, `SELECT `+optimizedCols+`
		FROM optimized_version ov JOIN media_item mi ON mi.id = ov.item_id
		WHERE ov.item_id = ? AND ov.state = 'ready'`, itemID))
	if errors.Is(err, sql.ErrNoRows) {
		return OptimizedVersion{}, ErrNotFound
	}
	return v, err
}

// OptimizedBytes is the space finished copies take, for the limit.
func (s *Store) OptimizedBytes(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(size_bytes), 0) FROM optimized_version WHERE state = 'ready'`).Scan(&n)
	return n, err
}

// OptimizedIDs is every title with a row, for the sweep that removes files no
// row accounts for.
func (s *Store) OptimizedIDs(ctx context.Context) (map[int64]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT item_id FROM optimized_version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// MarkOptimized records a state change. Only a row that still exists moves:
// a copy removed while it was converting stays removed. Reports whether it
// did.
func (s *Store) MarkOptimized(ctx context.Context, itemID int64, state string, size int64, probeJSON, errMsg string) (bool, error) {
	var finished any
	if state == OptimizedReady || state == OptimizedFailed {
		finished = time.Now().Unix()
	}
	var probe any
	if probeJSON != "" {
		probe = probeJSON
	}
	res, err := s.db.ExecContext(ctx, `UPDATE optimized_version
		SET state = ?, size_bytes = ?, probe_json = COALESCE(?, probe_json), error = ?, finished_at = ?
		WHERE item_id = ?`, state, size, probe, errMsg, finished, itemID)
	if err != nil {
		return false, fmt.Errorf("mark optimized: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ResumeOptimized puts conversions a restart interrupted back in the queue:
// their files were never finished, so they start again.
func (s *Store) ResumeOptimized(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE optimized_version SET state = 'queued' WHERE state = 'converting'`)
	return err
}
