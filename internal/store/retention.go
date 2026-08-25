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
// refetchable, and losing one costs a request.
//
// Seven days, revised down from thirty after measuring a real installation.
// The original comment argued thirty was "far past the point where it is
// saving anybody anything", which was right in spirit and far too generous in
// magnitude: on that machine provider_cache held 5,389 rows and **80.6 MB of a
// 101.9 MB database — four fifths of the file** — and not one row was old
// enough for a thirty-day cutoff to touch. The database would have grown for
// another fortnight and then shed most of itself in one lurch.
//
// A week still covers what the cache is actually for, which is an enrichment
// burst: a first scan, a big import, a provider pass over a library. What it
// stops covering is the long tail, where the cache is holding tens of
// megabytes against the chance that something enriched last month is enriched
// again — which is not a thing that happens, and if it does it costs one
// request.
const CacheMaxAge = 7 * 24 * time.Hour

/*
 * The policy a stamp was written under.
 *
 * Recorded beside the timestamp because "I pruned yesterday" stops being
 * evidence that nothing is stale the moment the rules change. Without this,
 * shortening the cache window or the audit window in Settings does nothing
 * visible for up to a day, and the natural reading of that is that the setting
 * does not work.
 *
 * A stamp in the old bare-integer format parses to an empty policy, which
 * never matches and is therefore due — which is what an upgrade into a changed
 * default should do anyway.
 */
func PrunePolicy(auditDays int) string {
	return fmt.Sprintf("audit=%d cache=%s", auditDays, CacheMaxAge)
}

// PolicyChanged reports whether the rules differ from the ones a stamp was
// written under, which makes a pass due regardless of when it last ran.
func PolicyChanged(recorded, current string) bool { return recorded != current }

/*
 * lastPruneKey records when a pass last completed, in the meta table.
 *
 * It is persisted because the alternative -- a ticker counting from process
 * start -- does not survive a restart, and a server that restarts more often
 * than the interval then never prunes at all. That is not hypothetical: the
 * first version shipped with a 24-hour ticker and never fired once on the
 * machine it was written for, which updated itself three times in seventeen
 * hours. Every restart put the clock back to zero.
 *
 * A daily job whose schedule is uptime is really a job that only runs on
 * servers nobody touches.
 */
const lastPruneKey = "last_prune_at"

// LastPrune reports when a pass last completed and under which policy, or the
// zero time if never.
//
// A missing row is "never", not an error: every existing installation has one
// of those, and treating it as a failure would mean the first pass after an
// upgrade is the one that does not happen.
//
// The stored form is "<unix> <policy>". A bare integer is the older form and
// yields an empty policy, which never matches the current one and is therefore
// due -- correct, because an upgrade is exactly when the rules may have moved.
func (s *Store) LastPrune(ctx context.Context) (time.Time, string, error) {
	var v string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM meta WHERE key = ?`, lastPruneKey).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, "", nil
	}
	if err != nil {
		return time.Time{}, "", fmt.Errorf("last prune: %w", err)
	}
	stamp, policy, _ := strings.Cut(v, " ")
	n, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil {
		// A meta row nobody can parse is not worth failing a maintenance pass
		// over. Treat it as never, which self-repairs on the next write.
		return time.Time{}, "", nil
	}
	return time.Unix(n, 0), policy, nil
}

// SetLastPrune records that a pass completed at t under `policy`.
func (s *Store) SetLastPrune(ctx context.Context, t time.Time, policy string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO meta (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		lastPruneKey, strconv.FormatInt(t.Unix(), 10)+" "+policy)
	if err != nil {
		return fmt.Errorf("set last prune: %w", err)
	}
	return nil
}

/*
 * PruneDue reports whether a pass should run now.
 *
 * Time-based, not uptime-based, which is the whole point of persisting `last`.
 * A zero `last` -- never pruned, including every installation upgrading into
 * this -- is due immediately, because the rows it would drop have been
 * accumulating since before the feature existed.
 */
func PruneDue(last, now time.Time, every time.Duration) bool {
	if last.IsZero() {
		return true
	}
	// A `last` in the future means a clock that moved backwards. Treat it as
	// due rather than waiting the difference out: the alternative is a server
	// whose maintenance is disabled until the calendar catches up.
	if last.After(now) {
		return true
	}
	return now.Sub(last) >= every
}

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
