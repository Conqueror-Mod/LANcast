package store

import (
	"context"
	"testing"
	"time"
)

/*
 * Retention, tested on the boundary rather than in the middle.
 *
 * The interesting question is never "does DELETE work" — it is which rows a
 * cutoff catches, and whether "keep for ever" really means for ever. Both of
 * those are one comparison operator away from being silently wrong in the
 * direction that destroys data.
 */

func auditAt(t *testing.T, s *Store, at time.Time, summary string) {
	t.Helper()
	if err := s.AppendAudit(context.Background(), AuditEvent{
		At: at.Unix(), ActorID: "u1", ActorName: "chris",
		Action: "test.event", Summary: summary,
	}); err != nil {
		t.Fatalf("append audit: %v", err)
	}
}

func auditCount(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM audit_event`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestPruneAuditKeepsWhatIsInsideTheWindow(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()

	auditAt(t, s, now.AddDate(0, 0, -120), "four months ago")
	auditAt(t, s, now.AddDate(0, 0, -91), "just outside")
	auditAt(t, s, now.AddDate(0, 0, -89), "just inside")
	auditAt(t, s, now, "today")

	res, err := s.Prune(context.Background(), 90, now)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if res.AuditEvents != 2 {
		t.Errorf("removed %d audit events, want 2", res.AuditEvents)
	}
	if got := auditCount(t, s); got != 2 {
		t.Errorf("%d events left, want 2", got)
	}
}

/*
 * Zero is not "prune everything", which is the failure mode that would matter.
 * An operator who turns retention off is asking for the audit trail to be
 * permanent; reading that as a cutoff of now would delete all of it on the
 * first pass, and there is nowhere to get it back from.
 */
func TestPruneAuditZeroKeepsEverything(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()

	auditAt(t, s, now.AddDate(-3, 0, 0), "three years ago")
	auditAt(t, s, now, "today")

	res, err := s.Prune(context.Background(), 0, now)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if res.AuditEvents != 0 {
		t.Errorf("removed %d audit events with retention off, want 0", res.AuditEvents)
	}
	if got := auditCount(t, s); got != 2 {
		t.Errorf("%d events left, want 2", got)
	}
}

// The cache is pruned whatever the audit setting says. "Keep my audit log" is
// not a request to keep a cached provider response from last spring.
func TestPruneCacheRunsEvenWithAuditRetentionOff(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	ctx := context.Background()

	old := now.Add(-CacheMaxAge - time.Hour)
	fresh := now.Add(-time.Hour)
	for _, e := range []struct {
		key string
		at  time.Time
	}{{"stale", old}, {"current", fresh}} {
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO provider_cache (provider, key, payload, fetched_at)
			 VALUES ('tmdb', ?, ?, ?)`, e.key, []byte("{}"), e.at.Unix()); err != nil {
			t.Fatalf("seed cache: %v", err)
		}
	}

	res, err := s.Prune(ctx, 0, now)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if res.CacheRows != 1 {
		t.Fatalf("removed %d cache rows, want 1", res.CacheRows)
	}

	var key string
	if err := s.db.QueryRow(`SELECT key FROM provider_cache`).Scan(&key); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if key != "current" {
		t.Errorf("kept %q, want the fresh entry", key)
	}
}

// Any() is what decides whether a VACUUM happens, and a VACUUM takes an
// exclusive lock on the whole database. A pass that deleted nothing must not
// claim otherwise.
func TestPruneResultAnyGatesTheVacuum(t *testing.T) {
	if (PruneResult{}).Any() {
		t.Error("an empty result claims it removed something")
	}
	if !(PruneResult{AuditEvents: 1}).Any() {
		t.Error("audit removals do not register")
	}
	if !(PruneResult{CacheRows: 1}).Any() {
		t.Error("cache removals do not register")
	}
}

// A prune on an untouched database is a no-op rather than an error: this runs
// daily on every install, including the ones that have never done anything.
func TestPruneOnAnEmptyDatabase(t *testing.T) {
	s := openTestStore(t)
	res, err := s.Prune(context.Background(), 90, time.Now())
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if res.Any() {
		t.Errorf("removed something from an empty database: %+v", res)
	}
}

func TestVacuumRuns(t *testing.T) {
	s := openTestStore(t)
	if err := s.Vacuum(context.Background()); err != nil {
		t.Fatalf("vacuum: %v", err)
	}
}

/*
 * Due-ness, which is the part that shipped broken.
 *
 * v0.8.8 counted from process start. On a server that restarts more often than
 * the interval the clock never reached it — the machine it was written for
 * updated three times in seventeen hours and the pass never fired once. These
 * assert the property that fixes it: due-ness is a date.
 */
func TestPruneDue(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	const every = 24 * time.Hour

	cases := []struct {
		name string
		last time.Time
		want bool
	}{
		// Every installation upgrading into this has no row. If "never" were
		// not due, the first pass after an upgrade would be the one that never
		// happens — and those are the servers with the most to drop.
		{"never pruned", time.Time{}, true},
		{"pruned an hour ago", now.Add(-time.Hour), false},
		{"pruned just under a day ago", now.Add(-23 * time.Hour), false},
		{"pruned exactly a day ago", now.Add(-24 * time.Hour), true},
		{"pruned a week ago", now.AddDate(0, 0, -7), true},
		// A clock that moved backwards must not disable maintenance until the
		// calendar catches up.
		{"stamped in the future", now.Add(48 * time.Hour), true},
	}
	for _, c := range cases {
		if got := PruneDue(c.last, now, every); got != c.want {
			t.Errorf("%s: due = %v, want %v", c.name, got, c.want)
		}
	}
}

/*
 * The restart that broke v0.8.8, as a test.
 *
 * Nothing here simulates uptime, because that is the point: the stamp is read
 * back from the database, so a fresh process sees what the last one recorded.
 */
func TestPruneStampSurvivesARestart(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Now()

	if last, err := s.LastPrune(ctx); err != nil || !last.IsZero() {
		t.Fatalf("a fresh database claims a previous prune: %v %v", last, err)
	}
	if !PruneDue(mustLastPrune(t, s), now, 24*time.Hour) {
		t.Error("a database that has never pruned is not due")
	}

	if err := s.SetLastPrune(ctx, now); err != nil {
		t.Fatalf("set: %v", err)
	}

	// What a restarted process sees.
	if PruneDue(mustLastPrune(t, s), now.Add(time.Hour), 24*time.Hour) {
		t.Error("due again an hour after a prune — the restart bug is back")
	}
	if !PruneDue(mustLastPrune(t, s), now.Add(25*time.Hour), 24*time.Hour) {
		t.Error("not due a day later")
	}
}

// Writing twice updates rather than failing the primary key.
func TestSetLastPruneOverwrites(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	first := time.Now().Add(-48 * time.Hour)
	second := time.Now()

	if err := s.SetLastPrune(ctx, first); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := s.SetLastPrune(ctx, second); err != nil {
		t.Fatalf("second: %v", err)
	}
	if got := mustLastPrune(t, s); got.Unix() != second.Unix() {
		t.Errorf("last prune = %v, want %v", got, second)
	}
}

func mustLastPrune(t *testing.T, s *Store) time.Time {
	t.Helper()
	last, err := s.LastPrune(context.Background())
	if err != nil {
		t.Fatalf("last prune: %v", err)
	}
	return last
}
