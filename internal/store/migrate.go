package store

import (
	"database/sql"
	"fmt"
)

// CurrentSchemaVersion is the revision this build expects.
const CurrentSchemaVersion = 16

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
