package store

import (
	"database/sql"
	"fmt"
)

// CurrentSchemaVersion is the revision this build expects.
const CurrentSchemaVersion = 3

// migration is one forward step. There are deliberately no down migrations:
// rolling a media library's schema backwards loses data that a rescan cannot
// regenerate (watch history, locks, corrections). Restore a backup instead.
type migration struct {
	version int
	sql     string
}

// migrations run in order, each inside its own transaction. schema.sql creates
// revision 1 on a fresh database; these carry an existing one forward.
var migrations = []migration{
	{version: 2, sql: schemaRevision2},
	{version: 3, sql: schemaRevision3},
}

// migrate brings the database up to CurrentSchemaVersion.
func migrate(db *sql.DB) error {
	var current int
	err := db.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&current)
	if err != nil {
		return fmt.Errorf("read schema_version: %w", err)
	}
	if current > CurrentSchemaVersion {
		// A newer LANcast has touched this database. Refusing is the only safe
		// move: applying an older build's assumptions would corrupt data the
		// user cannot regenerate.
		return fmt.Errorf("database is schema version %d but this build supports %d — upgrade LANcast",
			current, CurrentSchemaVersion)
	}

	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		if err := applyMigration(db, m); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(db *sql.DB, m migration) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("migration %d: begin: %w", m.version, err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(m.sql); err != nil {
		return fmt.Errorf("migration %d: %w", m.version, err)
	}
	if _, err := tx.Exec(`UPDATE meta SET value = ? WHERE key = 'schema_version'`, m.version); err != nil {
		return fmt.Errorf("migration %d: record version: %w", m.version, err)
	}
	return tx.Commit()
}

// SchemaVersion reports the database's current revision.
func (s *Store) SchemaVersion() (int, error) {
	var v int
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`).Scan(&v)
	return v, err
}

// Revision 2 — metadata, artwork, and the show hierarchy.
//
// Shows and seasons are media_item rows rather than their own tables, so
// artwork, locking, match state, and theme music work identically for them
// (ADR 0010). The hierarchy lives in parent_id, not in table structure.
const schemaRevision2 = `
ALTER TABLE media_item ADD COLUMN provider TEXT;
ALTER TABLE media_item ADD COLUMN external_id TEXT;
ALTER TABLE media_item ADD COLUMN match_score REAL;
ALTER TABLE media_item ADD COLUMN match_state TEXT NOT NULL DEFAULT 'unmatched';
ALTER TABLE media_item ADD COLUMN parent_id INTEGER REFERENCES media_item(id);
ALTER TABLE media_item ADD COLUMN overview TEXT;
ALTER TABLE media_item ADD COLUMN rating REAL;
ALTER TABLE media_item ADD COLUMN content_rating TEXT;
ALTER TABLE media_item ADD COLUMN released_at INTEGER;
ALTER TABLE media_item ADD COLUMN metadata_updated_at INTEGER;

-- Field-level locks. A locked field is never overwritten by any refresh,
-- which is what makes correcting metadata safe (ADR 0008).
CREATE TABLE IF NOT EXISTS item_lock (
    item_id INTEGER NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    field   TEXT    NOT NULL,
    PRIMARY KEY (item_id, field)
);

-- Artwork is content-addressed: the SHA-256 of the source bytes is its
-- identity, so a shared backdrop is stored once and an upstream URL change
-- orphans nothing.
CREATE TABLE IF NOT EXISTS artwork (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    hash       TEXT    NOT NULL UNIQUE,
    kind       TEXT    NOT NULL,
    source_url TEXT,
    width      INTEGER,
    height     INTEGER,
    bytes      INTEGER,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS item_artwork (
    item_id    INTEGER NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    artwork_id INTEGER NOT NULL REFERENCES artwork(id) ON DELETE CASCADE,
    kind       TEXT    NOT NULL,
    selected   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (item_id, artwork_id, kind)
);

CREATE TABLE IF NOT EXISTS person (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    provider    TEXT,
    external_id TEXT,
    name        TEXT NOT NULL,
    thumb_hash  TEXT,
    UNIQUE (provider, external_id)
);

CREATE TABLE IF NOT EXISTS credit (
    item_id   INTEGER NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    person_id INTEGER NOT NULL REFERENCES person(id) ON DELETE CASCADE,
    role      TEXT    NOT NULL,
    character TEXT,
    ord       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (item_id, person_id, role)
);

CREATE TABLE IF NOT EXISTS genre (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS item_genre (
    item_id  INTEGER NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    genre_id INTEGER NOT NULL REFERENCES genre(id) ON DELETE CASCADE,
    PRIMARY KEY (item_id, genre_id)
);

-- Raw provider responses, so a rescan, a re-match, and a refresh of the same
-- title cost one API call rather than three.
CREATE TABLE IF NOT EXISTS provider_cache (
    provider   TEXT NOT NULL,
    key        TEXT NOT NULL,
    payload    BLOB NOT NULL,
    fetched_at INTEGER NOT NULL,
    PRIMARY KEY (provider, key)
);

CREATE INDEX IF NOT EXISTS idx_item_review  ON media_item(match_state)
    WHERE match_state IN ('review', 'unmatched');
CREATE INDEX IF NOT EXISTS idx_item_parent  ON media_item(parent_id, season, episode);
CREATE INDEX IF NOT EXISTS idx_item_enrich  ON media_item(metadata_updated_at, missing);
CREATE INDEX IF NOT EXISTS idx_credit_item  ON credit(item_id, ord);
`

// Revision 3 — sessions.
//
// Only a SHA-256 of the session token is stored. A stolen database then yields
// no usable sessions, which matters because the database is the easiest thing
// to walk off with (it is one file, and backups of it exist by design).
//
// Server-side sessions rather than signed cookies: they are revocable. Changing
// the password can invalidate every existing session, which a self-contained
// signed cookie cannot express without rotating a signing key.
const schemaRevision3 = `
CREATE TABLE IF NOT EXISTS session (
    token_hash TEXT    PRIMARY KEY,
    user_id    TEXT    NOT NULL DEFAULT 'local',
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    last_seen  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_session_expires ON session(expires_at);
`
