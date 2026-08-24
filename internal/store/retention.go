package store

import (
	"context"
	"fmt"
	"time"
)

/*
 * Keeping the database from growing for ever.
 *
 * Three tables here are append-only with a timestamp and no ceiling:
 * audit_event, provider_cache, and — already handled elsewhere — epg_program.
 * A server left running for a year accumulates all of it, and nothing ever
 * looked at the age of a row again.
 *
 * The two dealt with here are deliberately the two where age *is* relevance.
 * An audit event from March answers nothing anybody asks in December, and a
 * cached provider response older than the metadata it describes is worse than
 * no cache. Nothing else in the schema is like that: a media_item is not stale
 * because it is old, and a credit from 1974 is the answer.
 *
 * What this is not: a general "clean up the database" button. Every table it
 * touches must be one where a deleted row costs nothing that cannot be
 * recovered or was never worth keeping. provider_cache refetches. audit_event
 * is the one real loss, which is why it has an operator setting and a
 * keep-for-ever option, and why the default is generous.
 */

// CacheMaxAge is how long a cached provider response is worth keeping.
//
// Not a setting, unlike the audit window: this is a cache, every entry is
// refetchable, and losing one costs a request. Thirty days is far past the
// point where it is saving anybody anything — a title enriched last month is
// not being enriched again this month — while still covering a rescan of a
// large library, which is the one workload that reads the same keys twice.
const CacheMaxAge = 30 * 24 * time.Hour

// PruneResult is what one maintenance pass removed.
type PruneResult struct {
	AuditEvents int64
	CacheRows   int64
}

// Any reports whether the pass removed anything, which is what decides
// whether reclaiming the space is worth an exclusive lock.
func (r PruneResult) Any() bool { return r.AuditEvents > 0 || r.CacheRows > 0 }

// PruneAuditBefore deletes audit events older than `before`.
//
// Deliberately a hard delete rather than an archive. An audit log that quietly
// moves rows somewhere else is an audit log with two answers to "what
// happened", and the second one is the one nobody checks (ADR 0026).
func (s *Store) PruneAuditBefore(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM audit_event WHERE at < ?`, before.Unix())
	if err != nil {
		return 0, fmt.Errorf("prune audit: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// PruneProviderCacheBefore drops cached provider responses fetched before
// `before`.
func (s *Store) PruneProviderCacheBefore(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM provider_cache WHERE fetched_at < ?`, before.Unix())
	if err != nil {
		return 0, fmt.Errorf("prune provider cache: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// Prune runs one maintenance pass.
//
// auditDays of zero means keep audit events for ever, which is a real answer
// for somebody running this where an audit trail is the point. The cache is
// pruned either way: "keep my audit log" is not a request to keep a cached
// TMDB response from last spring.
func (s *Store) Prune(ctx context.Context, auditDays int, now time.Time) (PruneResult, error) {
	var out PruneResult

	if auditDays > 0 {
		n, err := s.PruneAuditBefore(ctx, now.AddDate(0, 0, -auditDays))
		if err != nil {
			return out, err
		}
		out.AuditEvents = n
	}

	n, err := s.PruneProviderCacheBefore(ctx, now.Add(-CacheMaxAge))
	if err != nil {
		return out, err
	}
	out.CacheRows = n
	return out, nil
}

/*
 * Vacuum rewrites the database, returning freed pages to the filesystem.
 *
 * This is the half that makes a prune a space saving rather than a bookkeeping
 * exercise. SQLite does not shrink a file when rows are deleted: the pages go
 * on a free list and are reused by later writes, so a database that grew to
 * 100MB of audit rows stays a 100MB file after every one of them is gone. The
 * disk sees nothing until the file is rewritten.
 *
 * It is separate from Prune, and called only when a prune actually removed
 * something, because it is not free: VACUUM takes an exclusive lock for the
 * length of a full rewrite and needs room for a second copy while it runs. On
 * a large library that is seconds during which nothing else can write. Running
 * it on every pass — including the overwhelming majority that delete nothing —
 * would be paying that price daily to reclaim zero bytes.
 */
func (s *Store) Vacuum(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `VACUUM`); err != nil {
		return fmt.Errorf("vacuum: %w", err)
	}
	return nil
}
