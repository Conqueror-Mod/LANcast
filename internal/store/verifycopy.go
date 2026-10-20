package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

/*
 * VerifyCopy proves that a copied database opens, and folds its write-ahead
 * log into it (ADR 0092 §4).
 *
 * A library that moves between the service's folder and the person's own is
 * copied under a name nothing treats as a library, and is renamed into place
 * only after this has passed. So it is the line between "a copy was made" and
 * "a library is here", and it has to answer for the copy as a database rather
 * than as a file: the bytes arriving says nothing about whether the WAL that
 * travelled beside it still belongs to it.
 *
 * Deliberately not Open. Open applies the schema and migrates, which would
 * change the copy in the course of asking about it, and a copy written by a
 * newer build would be "upgraded" by an older one that does not know its
 * shape. The schema version is read and refused when newer, exactly as a
 * backup is (InspectSnapshot).
 *
 * The checkpoint is the one write it makes, and it is the point: afterwards
 * the database file is the whole library, the -wal is empty, and renaming the
 * one file into place cannot leave its log behind under the old name.
 */
func VerifyCopy(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("verify %s: %w", path, err)
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("verify %s: %w", abs, err)
	}
	// No journal_mode pragma: setting one is itself a change, and a copy of a
	// WAL database opens in WAL because that is what its header says.
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", abs))
	if err != nil {
		return fmt.Errorf("verify %s: %w", abs, err)
	}
	// One connection, so the checkpoint below and the close that follows are
	// the same connection and the last one: SQLite removes the -wal when the
	// last connection to a WAL database closes cleanly.
	db.SetMaxOpenConns(1)
	defer db.Close()

	var check string
	if err := db.QueryRow(`PRAGMA quick_check`).Scan(&check); err != nil {
		return fmt.Errorf("verify %s: %w", abs, err)
	}
	if check != "ok" {
		return fmt.Errorf("verify %s: the copy is damaged: %s", abs, check)
	}

	var version int
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&version); err != nil {
		return fmt.Errorf("verify %s: %w (%v)", abs, ErrNotSnapshot, err)
	}
	if version > CurrentSchemaVersion {
		return &SnapshotTooNewError{Found: version, Supported: CurrentSchemaVersion}
	}

	var busy, logFrames, checkpointed int
	if err := db.QueryRow(`PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &checkpointed); err != nil {
		return fmt.Errorf("verify %s: checkpoint: %w", abs, err)
	}
	if busy != 0 {
		// Nothing else should have this file open — it has a name no server
		// opens. A busy checkpoint means something does, and renaming a file
		// out from under it is not safe.
		return fmt.Errorf("verify %s: the copy is in use by another process", abs)
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("verify %s: %w", abs, err)
	}
	return nil
}
