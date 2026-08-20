package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

/*
 * Revision 27 — an upgrade must not make anybody visible.
 *
 * The account that matters here is one that existed *before* federation did.
 * ADR 0035 names this as the failure that cannot be taken back — "no existing
 * history becomes visible as a side effect of an update" — and revision 27
 * extends the same rule to a new kind of disclosure: appearing in the roster
 * one server hands another.
 *
 * A column default is easy to write and easy to get wrong in a way nothing
 * notices until somebody is listed on a stranger's machine, so it is asserted
 * against a database that genuinely predates the column rather than against a
 * row created afterwards.
 */

// openAtRevision26 builds a database as the build before federation left it.
func openAtRevision26(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rev26.db")
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(schemaSQL); err != nil {
		t.Fatalf("schema: %v", err)
	}
	for _, m := range migrations {
		if m.version > 26 {
			break
		}
		if _, err := db.Exec(m.sql); err != nil {
			t.Fatalf("revision %d: %v", m.version, err)
		}
	}
	if _, err := db.Exec(`UPDATE meta SET value = '26' WHERE key = 'schema_version'`); err != nil {
		t.Fatalf("set version: %v", err)
	}
	return db
}

func TestUpgradeLeavesExistingAccountsInvisible(t *testing.T) {
	db := openAtRevision26(t)

	// Two accounts from before federation existed, one of whom had already
	// opted into the *other* kind of sharing. That is the case worth pinning:
	// agreeing to publish what you finished is not agreeing to be listed to
	// another server, and revision 27 must not read one as the other.
	for _, u := range []struct {
		id, name string
		shares   int
	}{
		{"local", "Chris", 1},
		{"u2", "Georgia", 0},
	} {
		if _, err := db.Exec(`
			INSERT INTO user (id, name, password_hash, role, created_at, share_activity)
			VALUES (?, ?, 'hash', 'member', 1, ?)`, u.id, u.name, u.shares); err != nil {
			t.Fatalf("seed %s: %v", u.name, err)
		}
	}

	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	rows, err := db.Query(`SELECT name, visible_to_peers FROM user ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var name string
		var visible int
		if err := rows.Scan(&name, &visible); err != nil {
			t.Fatal(err)
		}
		seen++
		if visible != 0 {
			t.Errorf("%s became visible to peers by upgrading", name)
		}
	}
	if seen != 2 {
		t.Fatalf("found %d accounts after the migration, want 2", seen)
	}
}

// The tables arrive empty. A server that has never been introduced to anybody
// has no peers, which sounds obvious and is the thing a stray INSERT in a
// migration would break.
func TestUpgradeAddsNoPeers(t *testing.T) {
	db := openAtRevision26(t)
	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, table := range []string{"peer", "peer_address", "remote_person"} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s has %d rows after an upgrade, want none", table, n)
		}
	}
}

// Migrating twice must be safe: a half-applied upgrade that is retried is the
// ordinary way this code runs in anger.
func TestRevision27IsIdempotent(t *testing.T) {
	db := openAtRevision26(t)
	if err := migrate(db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := migrate(db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	var v int
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != CurrentSchemaVersion {
		t.Errorf("schema_version = %d, want %d", v, CurrentSchemaVersion)
	}
}

// And the store works against a database that arrived by upgrade rather than by
// being created at 27 — the path every existing install takes.
func TestPeersWorkOnAnUpgradedDatabase(t *testing.T) {
	db := openAtRevision26(t)
	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := &Store{db: db}
	ctx := context.Background()

	if err := st.AddPeer(ctx, Peer{
		Fingerprint: fpA, Name: "Utopia", Addrs: []string{"10.0.0.1:8080"},
	}); err != nil {
		t.Fatalf("AddPeer on an upgraded database: %v", err)
	}
	peers, err := st.Peers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 1 || len(peers[0].Addrs) != 1 {
		t.Errorf("peers = %v", peers)
	}
}
