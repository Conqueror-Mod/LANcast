package store

import (
	"context"
	"testing"
)

/*
 * The write-ahead log gives the disk back after a burst.
 *
 * WAL mode reuses its log rather than shrinking it, so a file that grew during
 * a scan keeps that space for ever. Found on a real install: a 118MB database
 * with a **122MB** WAL beside it, idle — larger than the database itself, and
 * not a leak. Every row-returning query in this package closes its rows; the
 * file had simply never been asked to shrink.
 *
 * Asserted against the database rather than against the DSN string, because
 * the DSN is the part that can be right while nothing happens: a pragma the
 * driver does not recognise is not an error, it is silence. This asks SQLite
 * what it actually has.
 */
func TestWALIsTruncatedBackToALimit(t *testing.T) {
	s := openTestStore(t)

	var limit int64
	if err := s.db.QueryRowContext(context.Background(),
		`PRAGMA journal_size_limit`).Scan(&limit); err != nil {
		t.Fatalf("read journal_size_limit: %v", err)
	}

	if limit <= 0 {
		t.Fatalf("journal_size_limit is %d, so the log is never handed back.\n\n"+
			"This was found as a 122MB WAL beside a 118MB database on an idle "+
			"server. The DSN may still contain the pragma — check that the "+
			"driver applied it rather than ignoring it.", limit)
	}
	if want := int64(32 << 20); limit != want {
		t.Errorf("journal_size_limit is %d, want %d", limit, want)
	}
}

// And the mode it depends on, so a failure says which half broke.
func TestTheDatabaseIsInWALMode(t *testing.T) {
	s := openTestStore(t)

	var mode string
	if err := s.db.QueryRowContext(context.Background(),
		`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode is %q, want wal", mode)
	}
}
