package store

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A scan reads the library's known files, walks the disk, and then writes what
// changed. Between the read and the write, a background worker — enrichment,
// probing, album art — commits. That is not a rare interleaving; it is the
// normal state of this server, because a scan finishing is what kicks
// enrichment off in the first place.
//
// SQLite refuses to upgrade a read transaction whose snapshot has been
// overtaken, and it refuses *immediately*: SQLITE_BUSY_SNAPSHOT (517) is not
// covered by busy_timeout, which only defers plain SQLITE_BUSY (5). So the
// scan does not wait and retry. It fails, and the library is left half
// reconciled with "database is locked" as its only explanation.
//
// Observed in the wild on 2026-08-08: a TV Shows scan died at 15 files with
// `database is locked (517)`.
func TestWriteAfterReadSurvivesConcurrentCommit(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	lib, err := st.CreateLibrary(ctx, "Shows", "show", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.UpsertItem(ctx, ScanFile{
		LibraryID: lib.ID, Path: filepath.Join(lib.Path, "a.mkv"), Kind: "movie",
		Title: "A", SortTitle: "a", Container: "mkv", SizeBytes: 1, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	// The scan's transaction: read first, so a deferred transaction fixes a
	// snapshot here.
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	var seen int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM media_item WHERE library_id = ?`, lib.ID).Scan(&seen); err != nil {
		t.Fatalf("read inside transaction: %v", err)
	}

	// A background worker writes on another connection while the scan's
	// transaction is open. It must not be refused either — with the write lock
	// taken up front it waits for the commit below, which is the whole point:
	// two overlapping writers, both of which finish.
	worker := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		_, err := st.db.ExecContext(ctx,
			`UPDATE media_item SET updated_at = updated_at + 1 WHERE id = ?`, id)
		worker <- err
	}()
	<-started

	// Give the worker time to get its write in. With the lock taken up front it
	// cannot, and simply waits — the sleep costs a moment and nothing else. With
	// a deferred transaction it commits here, overtaking the snapshot, which is
	// precisely the race being pinned. The two cases cannot share one
	// interleaving: the fix exists to make the losing one impossible.
	time.Sleep(150 * time.Millisecond)

	// The scan writes what it found. Under a deferred transaction this is the
	// upgrade that fails with 517 the moment the worker above has committed.
	if _, err := tx.ExecContext(ctx,
		`UPDATE media_item SET missing = 1 WHERE id = ?`, id); err != nil {
		t.Fatalf("scan write failed while a worker wrote alongside it: %v\n\n"+
			"This is the bug: busy_timeout does not cover SQLITE_BUSY_SNAPSHOT, "+
			"so a deferred transaction that reads before it writes aborts "+
			"instead of waiting.", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := <-worker; err != nil {
		t.Fatalf("background worker was refused: %v", err)
	}
}

// A scan's reconciliation hammered against background writers.
//
// This does NOT reproduce the 517 above — it passes with and without the fix,
// because MarkMissing's transaction writes first and never holds a stale
// snapshot. It is kept as the invariant it does test: scan-path store calls and
// background workers must be able to write concurrently without either being
// refused. The test that pins the bug is the one above.
func TestScanReconcileRacesBackgroundWrites(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	lib, err := st.CreateLibrary(ctx, "Shows", "show", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, name := range []string{"a.mkv", "b.mkv", "c.mkv", "d.mkv"} {
		id, err := st.UpsertItem(ctx, ScanFile{
			LibraryID: lib.ID, Path: filepath.Join(lib.Path, name), Kind: "movie",
			Title: name, SortTitle: name, Container: "mkv", SizeBytes: 1, MTime: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	stop := make(chan struct{})

	// Background workers, writing the way enrichment and probing do.
	for w := 0; w < 3; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := st.db.ExecContext(ctx,
					`UPDATE media_item SET updated_at = updated_at + 1 WHERE library_id = ?`,
					lib.ID); err != nil {
					errs <- err
					return
				}
				// Yield. Without this the workers write in a tight loop with no
				// gap at all, which is not what enrichment and probing do —
				// they write once per item, between reads and network calls —
				// and the difference is the whole flake.
				time.Sleep(time.Millisecond)
			}
		}()
	}

	// The scan side: read the known files, then reconcile.
	//
	// Forty rounds against three writers that never pause is far more
	// contention than a real scan meets, and on a busy CI runner it can exhaust
	// the 5s busy_timeout in the DSN — which failed the build twice with
	// SQLITE_BUSY and taught everyone to re-run it, which is the worst thing a
	// test can teach. The invariant is "these two can write concurrently", not
	// "SQLite never queues", so the loop is shorter and the writers yield
	// between statements. Contention is still constant; it is no longer a
	// benchmark of the lock.
	var scanErr error
	for i := 0; i < 12 && scanErr == nil; i++ {
		if _, err := st.KnownFiles(ctx, lib.ID); err != nil {
			scanErr = err
			break
		}
		scanErr = st.MarkMissing(ctx, ids)
	}
	close(stop)
	wg.Wait()
	close(errs)

	if scanErr != nil {
		t.Fatalf("scan write failed against concurrent workers: %v\n\n"+
			"A scan must not abort because a background worker committed.", scanErr)
	}
	for err := range errs {
		if err != nil && strings.Contains(err.Error(), "locked") {
			t.Fatalf("background worker was locked out: %v", err)
		}
	}
}

// testing.TB rather than *testing.T so benchmarks can use it too: every
// method it calls — Helper, TempDir, Fatal, Cleanup — is on the interface.
func openTestStore(t testing.TB) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}
