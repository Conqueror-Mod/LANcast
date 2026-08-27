package store

import (
	"database/sql"
	"fmt"
)

// CurrentSchemaVersion is the revision this build expects.
const CurrentSchemaVersion = 30

// migration is one forward step. There are deliberately no down migrations:
// rolling a media library's schema backwards loses data that a rescan cannot
// regenerate (watch history, locks, corrections). Restore a backup instead.
type migration struct {
	version int
	sql     string

	/*
	 * rebuildsTable turns foreign keys off for the duration.
	 *
	 * SQLite cannot ALTER TABLE ... DROP COLUMN a column carrying a UNIQUE
	 * constraint, so removing one means the documented twelve-step dance:
	 * create the replacement, copy, drop the original, rename. With
	 * `foreign_keys` on — which this store sets, and wants — the DROP is not a
	 * schema operation but a data one: SQLite treats dropping a parent table as
	 * deleting all of its rows, and `media_item.library_id` cascades. Dropping
	 * `library` would take every item, and therefore every watch position and
	 * every lock, with it.
	 *
	 * The pragma cannot be changed inside a transaction, so it is set around
	 * the whole thing and `foreign_key_check` runs before the commit rather
	 * than enforcement running during it. That check is what makes this safe:
	 * it fails the migration if the rebuild left a single dangling reference,
	 * and it is the reason this is a declared property of the migration rather
	 * than something a migration could do quietly with a stray PRAGMA.
	 */
	rebuildsTable bool
}

// migrations run in order, each inside its own transaction. schema.sql creates
// revision 1 on a fresh database; these carry an existing one forward.
var migrations = []migration{
	{version: 2, sql: schemaRevision2},
	{version: 3, sql: schemaRevision3},
	{version: 4, sql: schemaRevision4},
	{version: 5, sql: schemaRevision5},
	{version: 6, sql: schemaRevision6},
	{version: 7, sql: schemaRevision7},
	{version: 8, sql: schemaRevision8},
	{version: 9, sql: schemaRevision9},
	{version: 10, sql: schemaRevision10},
	{version: 11, sql: schemaRevision11},
	{version: 12, sql: schemaRevision12},
	{version: 13, sql: schemaRevision13},
	{version: 14, sql: schemaRevision14},
	{version: 15, sql: schemaRevision15},
	{version: 16, sql: schemaRevision16},
	{version: 17, sql: schemaRevision17},
	{version: 18, sql: schemaRevision18, rebuildsTable: true},
	{version: 19, sql: schemaRevision19},
	{version: 20, sql: schemaRevision20},
	{version: 21, sql: schemaRevision21},
	{version: 22, sql: schemaRevision22},
	{version: 23, sql: schemaRevision23},
	{version: 24, sql: schemaRevision24},
	{version: 25, sql: schemaRevision25},
	{version: 26, sql: schemaRevision26},
	{version: 27, sql: schemaRevision27},
	{version: 28, sql: schemaRevision28},
	{version: 29, sql: schemaRevision29},
	{version: 30, sql: schemaRevision30},
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
	if m.rebuildsTable {
		// Outside the transaction, because SQLite ignores the pragma inside
		// one. Restored on every path out, including a failed migration: a
		// connection left with foreign keys off would enforce nothing for the
		// rest of the process, which is a far worse outcome than the migration
		// that failed.
		if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
			return fmt.Errorf("migration %d: disable foreign keys: %w", m.version, err)
		}
		defer func() {
			if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
				panic(fmt.Sprintf("migration %d: could not re-enable foreign keys: %v", m.version, err))
			}
		}()
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("migration %d: begin: %w", m.version, err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(m.sql); err != nil {
		return fmt.Errorf("migration %d: %w", m.version, err)
	}

	// With enforcement off for the rebuild, this is what actually checks the
	// work — and it runs before the commit, so a rebuild that orphaned a row
	// rolls back rather than shipping a database whose references are wrong.
	if m.rebuildsTable {
		rows, err := tx.Query(`PRAGMA foreign_key_check`)
		if err != nil {
			return fmt.Errorf("migration %d: foreign key check: %w", m.version, err)
		}
		bad := false
		for rows.Next() {
			bad = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("migration %d: foreign key check: %w", m.version, err)
		}
		if bad {
			return fmt.Errorf("migration %d: rebuild left dangling references", m.version)
		}
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

// Revision 4 — probe results.
//
// The summary columns on media_item are denormalized from media_stream on
// purpose: a grid rendering 500 tiles needs codec and resolution without a
// join per tile, and the playback decision reads only the primary streams.
// media_stream keeps the full track list for the audio and subtitle pickers.
const schemaRevision4 = `
ALTER TABLE media_item ADD COLUMN probed_at      INTEGER;
ALTER TABLE media_item ADD COLUMN video_codec    TEXT;
ALTER TABLE media_item ADD COLUMN video_profile  TEXT;
ALTER TABLE media_item ADD COLUMN width          INTEGER;
ALTER TABLE media_item ADD COLUMN height         INTEGER;
ALTER TABLE media_item ADD COLUMN video_bitrate  INTEGER;
ALTER TABLE media_item ADD COLUMN audio_codec    TEXT;
ALTER TABLE media_item ADD COLUMN audio_channels INTEGER;

CREATE TABLE IF NOT EXISTS media_stream (
    item_id    INTEGER NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    idx        INTEGER NOT NULL,
    kind       TEXT    NOT NULL,          -- video | audio | subtitle
    codec      TEXT    NOT NULL,
    profile    TEXT,
    language   TEXT,
    title      TEXT,
    is_default INTEGER NOT NULL DEFAULT 0,
    forced     INTEGER NOT NULL DEFAULT 0,
    width      INTEGER,
    height     INTEGER,
    channels   INTEGER,
    bit_rate   INTEGER,
    PRIMARY KEY (item_id, idx)
);

CREATE INDEX IF NOT EXISTS idx_stream_item ON media_stream(item_id, kind);
CREATE INDEX IF NOT EXISTS idx_item_probe  ON media_item(probed_at, missing);
`

// Revision 5 — external subtitles.
//
// Kept separate from media_stream, which describes tracks inside a container.
// An external file has a path, can be added or removed without the video
// changing, and may later be downloaded rather than found — none of which fits
// a row describing a container's internals.
const schemaRevision5 = `
CREATE TABLE IF NOT EXISTS external_subtitle (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    item_id   INTEGER NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    path      TEXT    NOT NULL,
    language  TEXT,
    title     TEXT,
    forced    INTEGER NOT NULL DEFAULT 0,
    format    TEXT    NOT NULL,
    source    TEXT    NOT NULL DEFAULT 'sidecar',   -- sidecar | downloaded
    added_at  INTEGER NOT NULL,
    UNIQUE (item_id, path)
);

CREATE INDEX IF NOT EXISTS idx_subtitle_item ON external_subtitle(item_id);
`

// Revision 6 — video frame rate.
//
// Stored on the item rather than only in media_stream because it is needed for
// subtitle matching on every candidate comparison, and a mismatched frame rate
// is the failure that drifts progressively worse through a film rather than
// being a constant offset a viewer could ignore.
const schemaRevision6 = `
ALTER TABLE media_item ADD COLUMN video_frame_rate REAL;
`

// Revision 7 — user accounts (ADR 0015).
//
// The schema has carried session.user_id and playback_state.user_id since
// revision 1 (ADR 0006), defaulting to 'local'. This turns that latent column
// into real accounts. No back-fill is needed here: the migration only creates
// the table, and a startup step seeds the existing single password as a 'local'
// admin so every pre-existing session and resume point keeps resolving.
//
// name is COLLATE NOCASE so "Chris" and "chris" are the same account — a login
// name that depends on capitalisation is a support call waiting to happen. No
// foreign key from session to user is added: LookupSession joins to user, so a
// deleted user's sessions simply stop resolving without a schema-level cascade
// that SQLite cannot add to an existing table cleanly.
const schemaRevision7 = `
CREATE TABLE IF NOT EXISTS user (
    id            TEXT    PRIMARY KEY,
    name          TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT    NOT NULL,
    role          TEXT    NOT NULL,          -- admin | member
    created_at    INTEGER NOT NULL
);
`

// Revision 8 — collections and multi-part works (ADR 0017).
//
// Two grouping relationships, two mechanisms. Multi-part works (Baahubali,
// serials, miniseries) are pure containment and need no DDL: they are new
// `kind` values ('part', 'chapter', 'serial') hanging off the existing
// parent_id, exactly as episodes hang off a show. This migration adds only the
// membership side.
//
// A collection is a media_item with kind = 'collection' — it earns its own
// artwork, overview, locks, and match state for free, the ADR 0010 payoff.
// Membership is many-to-many because a film stays a top-level, independently
// browsable item that may belong to more than one collection (a franchise and a
// themed set), which a single-parent column cannot express. This is the side
// table ADR 0002 reserved for exactly this long-tail case.
//
// Both columns reference media_item(id); ON DELETE CASCADE on each keeps the
// join clean whether the collection or a member is removed.
const schemaRevision8 = `
CREATE TABLE IF NOT EXISTS item_collection (
    item_id       INTEGER NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    collection_id INTEGER NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    ord           INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (item_id, collection_id)
);

CREATE INDEX IF NOT EXISTS idx_collection_members ON item_collection(collection_id, ord);
`

// Revision 9 — the ignore list.
//
// A user can remove a title from the server while leaving its file on disk. Its
// path is recorded here, and the scanner skips any file at or beneath an ignored
// path, so a rescan never re-adds it. This is the non-destructive counterpart to
// deleting the file: "stop tracking this" versus "erase it". Paths are stored as
// scanned (the absolute path on the server) since that is what the walk compares.
const schemaRevision9 = `
CREATE TABLE IF NOT EXISTS ignored_path (
    library_id INTEGER NOT NULL REFERENCES library(id) ON DELETE CASCADE,
    path       TEXT    NOT NULL,
    added_at   INTEGER NOT NULL,
    PRIMARY KEY (library_id, path)
);
`

// Revision 10 — external ratings (ADR 0019).
//
// Two additions. First, media_item.imdb_id: the join key third-party rating
// services key on, populated from TMDB's external_ids. Nullable because not
// every item resolves one, and it doubles as the id the OpenSubtitles search can
// fall back to when a file hash misses.
//
// Second, item_rating: a source-keyed side table rather than a column per
// service, because the set of sources is open (ADR 0002) — a new one is a row
// value, not a migration. `score` is normalized to 0–10 so any sort or aggregate
// stays a pure numeric operation; `display` keeps the source-native form ("92%",
// "74") so the UI renders each in its own scale. media_item.rating stays the
// canonical scalar the badge and sort read, so nothing built so far changes.
const schemaRevision10 = `
ALTER TABLE media_item ADD COLUMN imdb_id TEXT;

CREATE TABLE IF NOT EXISTS item_rating (
    item_id    INTEGER NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    source     TEXT    NOT NULL,          -- imdb | rotten_tomatoes | metacritic | tmdb | …
    score      REAL    NOT NULL,          -- normalized 0–10
    display    TEXT    NOT NULL,          -- source-native string for the UI
    votes      INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (item_id, source)
);
`

// Revision 11 — installed plugins and their capability grants (ADR 0021).
//
// The row is the record of consent: granted_http and granted_secrets are what
// the operator approved at install, stored as JSON so the effective authority is
// the grant, never re-read from the (possibly changed) manifest. digest is the
// bundle's content identity; the unpacked module lives under a dir keyed by it,
// and re-computing it at load detects on-disk tampering. A re-install with a
// changed manifest is a new digest and replaces the row, which is what forces a
// fresh grant — authority never escalates silently.
const schemaRevision11 = `
CREATE TABLE IF NOT EXISTS installed_plugin (
    name            TEXT    PRIMARY KEY,
    version         TEXT    NOT NULL,
    kind            TEXT    NOT NULL,
    digest          TEXT    NOT NULL,
    signer          TEXT    NOT NULL,          -- first_party | pinned | unsigned
    enabled         INTEGER NOT NULL DEFAULT 1,
    granted_http    TEXT    NOT NULL DEFAULT '[]',
    granted_secrets TEXT    NOT NULL DEFAULT '[]',
    installed_at    INTEGER NOT NULL
);
`

// Revision 12 — pix_fmt on media_stream.
//
// Bit depth is the signal that decides whether an H.264 file direct-plays, and
// pix_fmt is the only reliable place to read it: profile names are inconsistent
// across encoders and a probe often reports none. Without this the 10-bit check
// falls back to matching profile strings, which misses files that then play as
// a black rectangle. A nullable column, so existing rows stay valid and
// re-probing fills them in.
const schemaRevision12 = `
ALTER TABLE media_stream ADD COLUMN pix_fmt TEXT;
`

// Revision 13 — artist on media_item.
//
// A track's performer is not the album's: a compilation has one album artist
// and a different artist per track, which is exactly the case that made ADR
// 0024 group on album_artist. The album artist lives on the container row; this
// column is the track's own, so a compilation can show who actually played
// without the grouping fracturing.
//
// Nullable, and empty for every video item, which is why this is a column
// addition rather than a shape change.
const schemaRevision13 = `
ALTER TABLE media_item ADD COLUMN artist TEXT;
`

// Revision 14 — cover_checked_at on media_item.
//
// The cover-art worker's queue is a query rather than a table, the same as
// probing and enrichment, which makes it restart-safe by construction. That
// only works if an attempt leaves a mark: an album with no embedded picture
// and no cover.jpg would otherwise come back in every batch forever and the
// queue would never drain.
//
// So this records that an album was *looked at*, which is not the same as it
// having artwork. "Has a row in item_artwork" cannot answer the question,
// because the honest answer for a great many albums is that there was nothing
// to find.
//
// Nullable, and empty for everything that is not an album, which is why this is
// a column addition rather than a shape change.
const schemaRevision14 = `
ALTER TABLE media_item ADD COLUMN cover_checked_at INTEGER;
`

// Revision 15 — the audit log (ADR 0026).
//
// "What emptied this library" was unanswerable during v0.4.x testing: current
// state is in the database, but nothing recorded how it got there. Multi-user
// accounts (ADR 0015) made attribution ambiguous, and plugin capability grants
// (ADR 0021) made trust decisions worth recording.
//
// actor_name is denormalised on purpose. A foreign key to users would either
// cascade-delete the evidence when an account is removed or block the removal,
// and "who deleted this library" has to survive the deletion of the account
// that did it — that is the case this table exists for. The id is kept beside
// it for joining while the account still exists.
//
// target_id is TEXT because targets are not uniformly integers: plugins are
// named, users have string ids, items and libraries are numbers. One column
// that holds all of them beats four nullable typed ones.
//
// summary is written already resolved, so a deleted library still reads
// "Removed library Films (14 items)" rather than an id that no longer resolves.
const schemaRevision15 = `
CREATE TABLE IF NOT EXISTS audit_event (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    at          INTEGER NOT NULL,
    actor_id    TEXT    NOT NULL,
    actor_name  TEXT    NOT NULL,
    action      TEXT    NOT NULL,
    target_kind TEXT,
    target_id   TEXT,
    summary     TEXT    NOT NULL,
    detail      TEXT
);
CREATE INDEX IF NOT EXISTS idx_audit_at ON audit_event(at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_event(action, at DESC);
`

// Revision 16 — pictures (ADR 0028).
//
// One column. width and height were already added in revision 4 for video
// resolution, and a picture's pixel dimensions are the same fact about a
// different medium — a second pair would be two columns meaning one thing,
// which is how a schema starts disagreeing with itself. The thumbnail worker
// fills the existing ones.
//
// taken_at is not added_at: one is when the picture was made, the other when it
// reached this disk. A photo library sorted by the second is sorted by when you
// copied things, which is almost never the question being asked. Null where
// absent — which is every file in the reference library — and callers fall back
// to mtime.
//
// Deliberately no gps column. It is location data about the user, nothing here
// needs it, and the surest way never to leak it is never to load it.
const schemaRevision16 = `
ALTER TABLE media_item ADD COLUMN taken_at INTEGER;
CREATE INDEX IF NOT EXISTS idx_item_taken ON media_item(library_id, taken_at DESC);
`

// Playlist membership (ADR 0030).
//
// A playlist is kind = 'playlist' on media_item — no new item table, the third
// media concept to manage that after music and pictures. Its membership cannot
// reuse item_collection, and the reason is the whole point of this revision:
// that table is keyed (item_id, collection_id), so it physically cannot hold
// the same track twice. Correct for a collection, where a film belongs to a
// franchise once. Wrong for a playlist, where a repeat is ordinary — a reprise,
// a track that opens and closes a set.
//
// So this is keyed on POSITION rather than on membership, which is the
// difference between the two concepts written in SQL.
//
// ON DELETE CASCADE on item_id means deleting a track removes it from every
// playlist holding it. The alternative is a playlist that silently plays eleven
// of its twelve entries, which is worse than one that visibly got shorter.
const schemaRevision17 = `
CREATE TABLE IF NOT EXISTS playlist_entry (
    playlist_id INTEGER NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    item_id     INTEGER NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    ord         INTEGER NOT NULL,
    PRIMARY KEY (playlist_id, ord)
);

CREATE INDEX IF NOT EXISTS idx_playlist_entry_item ON playlist_entry(item_id);
`

// Revision 18 — a library in more than one place (ADR 0034).
//
// `library.path` becomes rows in `library_root`, and every item records which
// root it came from.
//
// **The root_id column is the reason this design was chosen over the obvious
// one.** A library with several roots could resolve a file by asking "does any
// of these roots contain this path?", and that is a weaker property than the
// single-root check it replaces: a row pointing under root B while belonging to
// root A would pass, because some root matched. The containment check is the
// boundary where a bad row becomes arbitrary file access, and a loop that
// accepts on first match is how a boundary stops being one. With the root on
// the item there is never a search — it stays one root, one check, and only the
// lookup moves.
//
// It also scopes reconciliation. An unplugged drive must mark its own items
// missing and not a single file on the other root, which is a `WHERE root_id`
// rather than a judgement call at scan time.
//
// `library.path` is dropped rather than kept alongside the new table. Two
// places holding the same truth is a bug factory, and this migration has to
// rewrite every library row regardless.
//
// The backfill gives every existing library exactly one root at its current
// path, so a single-root library is indistinguishable afterwards — which is the
// property the migration test asserts, because every library in the field is
// one today.
//
// root_id stays nullable despite never being null after this runs. A NOT NULL
// column would make a half-applied migration unreadable rather than merely
// incomplete, and this project's rule is that a database is restored from
// backup rather than migrated backwards — so the failure mode worth optimising
// for is "can still be opened and inspected".
const schemaRevision18 = `
CREATE TABLE IF NOT EXISTS library_root (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    library_id INTEGER NOT NULL REFERENCES library(id) ON DELETE CASCADE,
    path       TEXT    NOT NULL UNIQUE,
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_library_root_lib ON library_root(library_id);

INSERT INTO library_root (library_id, path, created_at)
SELECT id, path, created_at FROM library;

ALTER TABLE media_item ADD COLUMN root_id INTEGER REFERENCES library_root(id) ON DELETE CASCADE;

UPDATE media_item
   SET root_id = (SELECT r.id FROM library_root r WHERE r.library_id = media_item.library_id);

CREATE INDEX IF NOT EXISTS idx_item_root ON media_item(root_id);

-- SQLite cannot DROP COLUMN on a UNIQUE column, so the library table is
-- rebuilt without it. Foreign keys are off for this (see
-- migration.rebuildsTable) and PRAGMA foreign_key_check runs before the commit.
--
-- The child tables keep referencing the name "library" throughout: they are
-- never touched, the replacement is renamed into place, and the check at the
-- end is what proves that held.
CREATE TABLE library_new (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,
    kind       TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    scanned_at INTEGER
);

INSERT INTO library_new (id, name, kind, created_at, scanned_at)
SELECT id, name, kind, created_at, scanned_at FROM library;

DROP TABLE library;

ALTER TABLE library_new RENAME TO library;
`

// Revision 19 — colour metadata on media_stream (ADR 0033).
//
// The server cannot currently tell an HDR file from an SDR one. pix_fmt is the
// only colour-ish thing recorded, and `yuv420p10le` is what HDR10 reports *and*
// what 10-bit SDR reports — so every HDR file is converted by dropping bits
// with no tone map, which produces a washed-out picture and an output still
// carrying the source's PQ tags. Nothing can be fixed until the difference is
// visible, and these three columns are what make it visible.
//
// Three nullable columns rather than one derived `is_hdr` flag. The transfer
// function is the fact; "is this HDR" is a rule applied to it, and rules change
// — Dolby Vision profile 5 is not PQ and is out of scope today, and storing a
// verdict rather than the evidence would mean re-probing an entire library to
// change the rule. Same shape as revision 12, which recorded pix_fmt rather
// than a `needs_transcode` boolean for the same reason.
//
// NULL means "not probed since this landed", not "SDR". IsHDR answers false for
// it either way, so a library reads exactly as it did until re-probed.
const schemaRevision19 = `
ALTER TABLE media_stream ADD COLUMN color_transfer TEXT;
ALTER TABLE media_stream ADD COLUMN color_primaries TEXT;
ALTER TABLE media_stream ADD COLUMN color_space TEXT;
`

/*
 * Revision 20 — the last scan's verdict on a library's shape.
 *
 * A nullable column rather than a table: there is exactly one of these per
 * library, it is replaced wholesale by the next scan, and it has no history
 * worth keeping — the question it answers is "is this library the kind it says
 * it is", which has one current answer.
 *
 * It has to be stored at all because the warning was previously only in the
 * scanner's in-memory progress, which is lost on restart. That made a permanent
 * mistake — kind cannot be changed — announced by a message that survived until
 * the next time the server stopped. A library scanned on Tuesday and looked at
 * on Wednesday showed nothing wrong with it.
 *
 * JSON in one column rather than three, because the shape of a warning belongs
 * to the rule that produces it: codes will be added, and a client already has
 * to handle a code it does not know. Splitting it into columns would make every
 * new field a migration.
 */
const schemaRevision20 = `
ALTER TABLE library ADD COLUMN shape_warning TEXT;
`

/*
 * Revision 21 — what you thought of it.
 *
 * A table rather than columns on media_item, because a rating belongs to a
 * person and an item, not to an item. The same film is a five for one person in
 * the house and a two for another, and a column could only hold one of those —
 * which is how "your rating" quietly becomes "the last person to rate it".
 *
 * Keyed on (item_id, user_id) for the same reason playback_state is: one
 * verdict per person per thing, replaced when they change their mind. There is
 * no history here, deliberately. "What did I used to think of this" is not a
 * question anybody has asked, and a rating that accumulated rows would need a
 * pruning rule nobody wants to design.
 *
 * `score` is 1–10 rather than 1–5, so that it can carry a half-star UI without
 * a migration, and because the provider ratings it sits beside are already out
 * of ten (TMDB) — one scale in the database beats two and a conversion.
 *
 * `review` is nullable and unbounded-ish: a note to yourself about why. It is
 * the one free-text field a user owns in this system, which is exactly why it
 * is not shown to anybody else. Sharing ratings across a household is the
 * decision the roadmap says nobody has made, and this revision does not make it.
 */
const schemaRevision21 = `
CREATE TABLE IF NOT EXISTS user_rating (
    item_id    INTEGER NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    user_id    TEXT    NOT NULL,
    score      INTEGER NOT NULL,
    review     TEXT,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (item_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_rating_user ON user_rating(user_id, updated_at);
`

/*
 * Revision 22 — whether an account shares what it has watched (ADR 0035).
 *
 * One nullable-by-default column rather than a settings table, because it is
 * one boolean per account and the account row is where "facts about this
 * person" already live.
 *
 * **Defaults to 0, and that is the whole point.** A server upgraded into this
 * shares nothing until somebody deliberately turns it on: no existing history
 * becomes visible as a side effect of an update, which is the one failure here
 * that could not be taken back. You cannot un-show a history.
 */
const schemaRevision22 = `
ALTER TABLE user ADD COLUMN share_activity INTEGER NOT NULL DEFAULT 0;
`

/*
 * Revision 23 — Live TV channels.
 *
 * A channel is **not** a media_item, and that is the modelling decision rather
 * than a convenience. media_item is the widest table in the system and every
 * column on it describes a *work*: a title that a provider could match, a
 * duration, a file on disk, a position you stopped at. A channel has none of
 * those. It is a name, a logo and a URL that is different every time you look
 * at it.
 *
 * Putting it on media_item would mean six more nullable columns, a new `kind`
 * that every listing has to learn to exclude, and a row that answers "how long
 * is it" with nothing. ADR 0002 chose one wide table for things that are works;
 * this is the case that is not one.
 *
 * `source_id` groups channels by where they came from, so re-importing a
 * playlist can replace exactly that source's channels without touching another.
 * `position` preserves the order the source listed them in — channel order is
 * meaningful to the person who curated the list, and alphabetical is not an
 * improvement on it.
 */
const schemaRevision23 = `
CREATE TABLE IF NOT EXISTS channel_source (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT    NOT NULL,
    url          TEXT    NOT NULL,
    created_at   INTEGER NOT NULL,
    refreshed_at INTEGER,
    channel_count INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS channel (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id  INTEGER NOT NULL REFERENCES channel_source(id) ON DELETE CASCADE,
    name       TEXT    NOT NULL,
    url        TEXT    NOT NULL,
    logo_url   TEXT,
    group_name TEXT,
    position   INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_channel_source ON channel(source_id, position);
`

/*
 * Revision 24 — the EPG (ADR 0031).
 *
 * A programme is not a media_item for the same reason a channel is not one, and
 * then one reason more: there are hundreds of thousands of them, they are
 * replaced wholesale every time the guide refreshes, and none of them is a work
 * anybody can play. Putting a fortnight of listings for six hundred channels
 * into the table that carries watch state and locks would mean half a million
 * rows on the hot path of every library query.
 *
 * `epg_program.channel_id` references `channel`, resolved **at import** from
 * XMLTV's channel id against `channel.tvg_id`. Storing the raw XMLTV id instead
 * and joining at read time was the alternative and it is worse: every guide
 * query would then carry a string join, and a listing for a channel nobody
 * subscribes to would be stored for ever. Resolving at import means the guide
 * holds exactly the schedule of channels that exist, and the cascade deletes
 * it when the source goes.
 *
 * The index is on (channel_id, start_at) because every read this table has is
 * "what is on channel N around time T" — now/next for a grid, or a day's
 * schedule for one channel. `stop_at` is deliberately not in it: a range query
 * bounded on start alone plus one row of slack answers both.
 */
const schemaRevision24 = `
ALTER TABLE channel ADD COLUMN tvg_id TEXT;

ALTER TABLE channel_source ADD COLUMN epg_url TEXT;
ALTER TABLE channel_source ADD COLUMN epg_refreshed_at INTEGER;
ALTER TABLE channel_source ADD COLUMN program_count INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS epg_program (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id INTEGER NOT NULL REFERENCES channel(id) ON DELETE CASCADE,
    start_at   INTEGER NOT NULL,
    stop_at    INTEGER NOT NULL,
    title      TEXT    NOT NULL,
    description TEXT,
    category   TEXT,
    season     INTEGER,
    episode    INTEGER,
    icon_url   TEXT
);

CREATE INDEX IF NOT EXISTS idx_epg_channel ON epg_program(channel_id, start_at);
CREATE INDEX IF NOT EXISTS idx_epg_window  ON epg_program(start_at);
`

/*
 * Revision 25 records when a row was last re-parsed.
 *
 * Without it a re-parse cannot tell "this row has never been re-parsed" from
 * "this row was re-parsed and enrichment has since written the provider's
 * answer back over the guess". Those look identical — the stored title differs
 * from the filename either way — so every run rewrote the same rows and asked
 * the provider the same question again. Measured on a real library: 32 rows
 * flipped back and forth on every run, indefinitely.
 *
 * A nullable column rather than a new table: this is one fact about a row, and
 * the shape of the data model does not change.
 */
const schemaRevision25 = `
ALTER TABLE media_item ADD COLUMN reparsed_at INTEGER;
`

/*
 * Revision 26 undoes the seasons that were matched by name.
 *
 * Until this revision a season was searched for like any other item, using its
 * own title — which is "Season 2", a position inside a show and not the name of
 * a work. TMDB answers such a query with real shows that happen to carry the
 * phrase in their title; those normalize to an exact title match, clear the
 * 0.85 auto-apply threshold, and are written over the season: title, year,
 * overview, poster and fanart. Because the query depends only on the season
 * number, the same wrong show won for every show in the library — one Thai
 * drama became the poster for season 2 of nine unrelated series.
 *
 * The search is gone (see tmdb.Search and enrich.fetchSeason), but the rows it
 * wrote are still in the database and nothing would revisit them: they are
 * stamped matched, so they are not pending, and seasons are excluded from the
 * review queue, so no human is offered them either. They would sit there
 * looking correct for ever.
 *
 * So the provider verdict is stripped and metadata_updated_at cleared, which is
 * what puts a row back in the enrichment queue to be resolved properly from its
 * parent show. Locked fields are left alone — a season a person has edited is
 * their decision, and a cleanup is still a rescan-class event, which does not
 * re-litigate decisions.
 *
 * The title is reset to "Season N" rather than recovered from the folder name.
 * The folder name is not the season's title — "S02 480p Bluray" never was — and
 * rebuilding it here would put filename guessing in a SQL string, which is
 * exactly what internal/media exists to prevent (CLAUDE.md). "Season N" is what
 * the row actually is, and the re-enrichment that follows replaces it with the
 * provider's own name for that season.
 *
 * The season number itself is the scanner's, not the provider's, and was never
 * wrong — so it is what the reset title is built from.
 */
const schemaRevision26 = `
DELETE FROM item_artwork
WHERE item_id IN (
  SELECT id FROM media_item
  WHERE kind = 'season'
    AND provider IS NOT NULL
    AND id NOT IN (SELECT item_id FROM item_lock WHERE field = 'artwork')
);

UPDATE media_item
SET title = 'Season ' || season,
    sort_title = printf('season %03d', season),
    year = NULL,
    overview = NULL,
    rating = NULL,
    released_at = NULL,
    imdb_id = NULL,
    series = NULL,
    provider = NULL,
    external_id = NULL,
    match_state = 'unmatched',
    match_score = 0,
    metadata_updated_at = NULL
WHERE kind = 'season'
  AND provider IS NOT NULL
  AND id NOT IN (SELECT item_id FROM item_lock);
`

/*
 * Revision 27 — peers, the people on them, and who is willing to be seen.
 *
 * Federation's storage ([ADR 0044](../../docs/adr/0044-server-identity-and-peering.md)
 * and the phase 2 half of the federation plan). Three things, and the shape of
 * each is a decision rather than a default.
 *
 * **The fingerprint is the primary key.** Not a surrogate id with the
 * fingerprint beside it. ADR 0044 §5 says the address is a hint and the
 * fingerprint is the identity, and a table keyed that way cannot express the
 * thing that would contradict it — two rows for one peer that moved. A peer
 * that changes address keeps its row because it never stopped being the same
 * peer.
 *
 * **Addresses are a side table, ordered.** They are plural because a machine on
 * an overlay network has more than one way to be reached and because a peer
 * that moves is still the same peer. `ord` preserves the sender's order, which
 * carries real information: the first is the one they expect to work. The same
 * shape item_collection uses, for the same reason — a list that belongs to a
 * row and is replaced wholesale.
 *
 * **remote_person cascades from peer, and that is the revocation mechanism.**
 * ADR 0046 makes unpairing the single act that revokes everything, "immediately,
 * with no per-person cleanup". This is what makes that true rather than
 * aspirational: delete the peer and the people it vouched for go with it, so no
 * grant can name somebody who is no longer reachable through anybody. A grant
 * table added in phase 3 will cascade from here in turn.
 *
 * The person's `id` is assigned by the *owning* server and is meaningless here
 * beyond being stable — it is how "Georgia" survives being renamed. It is also
 * a stranger's string, so nothing may assume its shape.
 *
 * **visible_to_peers defaults to 0**, exactly as share_activity did in revision
 * 22 and for the same reason: appearing in a roster one server hands another is
 * a disclosure, and no upgrade may make somebody visible as a side effect. An
 * account that has not opted in cannot be named by anybody's grant, in either
 * direction.
 */
const schemaRevision27 = `
CREATE TABLE IF NOT EXISTS peer (
    fingerprint TEXT    PRIMARY KEY,
    name        TEXT    NOT NULL,
    state       TEXT    NOT NULL,          -- added | paired
    added_at    INTEGER NOT NULL,
    last_seen   INTEGER                    -- NULL until it has ever answered
);

CREATE TABLE IF NOT EXISTS peer_address (
    fingerprint TEXT    NOT NULL REFERENCES peer(fingerprint) ON DELETE CASCADE,
    ord         INTEGER NOT NULL,
    addr        TEXT    NOT NULL,
    PRIMARY KEY (fingerprint, ord)
);

CREATE TABLE IF NOT EXISTS remote_person (
    fingerprint TEXT    NOT NULL REFERENCES peer(fingerprint) ON DELETE CASCADE,
    id          TEXT    NOT NULL,
    name        TEXT    NOT NULL,
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (fingerprint, id)
);

ALTER TABLE user ADD COLUMN visible_to_peers INTEGER NOT NULL DEFAULT 0;
`

/*
 * Revision 28 — who may see me watching.
 *
 * The grant table [ADR 0045](../../docs/adr/0045-live-presence-between-paired-servers.md)
 * §2 requires, and the shape is the decision: **one row per (local account,
 * remote person)**, not a `share_presence` column on `user`.
 *
 * A single boolean would be a grant to *everybody* — the per-server answer this
 * project considered and declined — wearing a per-account disguise. The unit of
 * consent in ADR 0035 is a person throughout, and it stays a person here. A
 * table also makes the absence of a row the default, which is how "off by
 * default" ends up being true rather than merely intended: there is no column
 * to have been initialised wrongly, and no migration in which anybody starts
 * being visible.
 *
 * **It cascades twice, and both are the revocation mechanism.** From
 * `remote_person`, which itself cascades from `peer`, so unpairing a server
 * drops every grant to everybody on it in one statement — ADR 0045 §5's
 * "immediately, with no per-person cleanup". And from `user`, so deleting an
 * account takes its grants with it rather than leaving rows naming somebody who
 * is gone.
 *
 * **Nothing here records presence itself.** This table says who *may* see;
 * what they see is `internal/presence`, which is a map and a mutex and is never
 * written down. Keeping the permission durable and the observation ephemeral is
 * the whole architecture of ADR 0045 — a `last_seen_watching` column added here
 * would collapse it, which is exactly why the ADR names that column as the
 * request that will arrive.
 *
 * `granted_at` is for showing a person what they have agreed to and when. It is
 * a fact about the grant, not about any viewing, and reveals nothing about what
 * anybody watched.
 */
const schemaRevision28 = `
CREATE TABLE IF NOT EXISTS presence_grant (
    user_id     TEXT    NOT NULL REFERENCES user(id) ON DELETE CASCADE,
    fingerprint TEXT    NOT NULL,
    person_id   TEXT    NOT NULL,
    granted_at  INTEGER NOT NULL,
    PRIMARY KEY (user_id, fingerprint, person_id),
    FOREIGN KEY (fingerprint, person_id)
        REFERENCES remote_person(fingerprint, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_presence_grant_person
    ON presence_grant(fingerprint, person_id);
`

/*
 * Revision 29 — the edition marker is kept instead of thrown away (ADR 0042).
 *
 * `stripEditionSuffix` has always *found* "(Alternate Cut)", "Director's Cut",
 * "DC", "SE" and the rest, and then discarded the finding to keep the shortened
 * title. That is what makes an edition match the work it is an edition of, and
 * it is right — but it also means two editions of one film arrive as two rows
 * that are identical in every field a person can see.
 *
 * Nullable and additive, so every existing row reads as "no edition stated" and
 * behaves exactly as it does now. No backfill: the column is populated as files
 * are re-parsed, and a rescan reconciles files rather than re-litigating
 * identity.
 *
 * **It is a label, never a grouping key.** The file that motivated ADR 0042
 * called itself an alternate cut and was a byte-for-byte copy of the theatrical
 * file -- so the marker is a thing the user wrote, and it is displayed rather
 * than trusted. Nothing joins on it, nothing dedupes by it, and nothing may
 * start.
 */
const schemaRevision29 = `
ALTER TABLE media_item ADD COLUMN edition TEXT;
`

/*
 * Revision 30 — forgetting what you watched is not pretending you never did.
 *
 * `ProfileStatistics` derives every total from `playback_state`, so clearing
 * the history cleared the statistics with it: the profile page reported zero
 * things started, zero finished and no time watched, for somebody who had
 * watched hundreds of hours. Reported the day the reset shipped.
 *
 * That conflates two different requests. "Forget what I have watched" is about
 * the *list* — the record of which titles, which somebody may want gone
 * because a shared account watched something for them, or because the server
 * is changing hands. "I have never watched anything" is a claim about the
 * person, and no one asked to make it.
 *
 * So a reset now banks the totals it is about to destroy, and the profile
 * reports the banked figures plus whatever is still live. The history list is
 * genuinely gone; the totals stay true.
 *
 * `first_at` is kept as a minimum rather than summed, because it is the oldest
 * playback this account has ever had and that does not change by forgetting
 * the row that carried it — it is what lets the page say what period the
 * numbers cover instead of implying they cover all time.
 *
 * One row per account, created on first reset. Nothing exists here for an
 * account that has never cleared anything, which is why the read has to
 * tolerate its absence rather than expect a zero row.
 */
const schemaRevision30 = `
CREATE TABLE IF NOT EXISTS profile_totals (
    user_id    TEXT PRIMARY KEY,
    started    INTEGER NOT NULL DEFAULT 0,
    finished   INTEGER NOT NULL DEFAULT 0,
    watched_ms INTEGER NOT NULL DEFAULT 0,
    first_at   INTEGER
);
`
