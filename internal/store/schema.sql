-- LANcast schema, revision 1.
--
-- Design notes live in docs/adr/. The short version:
--
--   * media_item is one wide table with a `kind` discriminator rather than a
--     table per media type. Library types are meant to become plugin-defined,
--     so the core must not hardcode a taxonomy. (ADR 0002)
--   * File columns are nullable because M2 introduces media_item rows that are
--     directories rather than files — shows and seasons. (ADR 0010)
--   * playback_state is keyed by user even though M1 is single-user, because
--     retrofitting that column later means discarding resume points. (ADR 0006)

CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Identifies the schema so the first migration doesn't have to guess what it
-- is migrating from.
INSERT OR IGNORE INTO meta (key, value) VALUES ('schema_version', '1');

CREATE TABLE IF NOT EXISTS library (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,
    kind       TEXT    NOT NULL,          -- movie | show | music | other
    path       TEXT    NOT NULL UNIQUE,
    created_at INTEGER NOT NULL,
    scanned_at INTEGER
);

CREATE TABLE IF NOT EXISTS media_item (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    library_id  INTEGER NOT NULL REFERENCES library(id) ON DELETE CASCADE,
    kind        TEXT    NOT NULL,         -- movie | episode | show | season | other
    path        TEXT    NOT NULL UNIQUE,

    title       TEXT    NOT NULL,
    sort_title  TEXT    NOT NULL,
    year        INTEGER,

    series      TEXT,                     -- episodes only, superseded by parent_id at M2
    season      INTEGER,
    episode     INTEGER,

    -- Null for rows that are directories rather than files (ADR 0010).
    container   TEXT,
    size_bytes  INTEGER,
    mtime       INTEGER,
    duration_ms INTEGER,                  -- filled by probe at M2

    added_at    INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    missing     INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_item_library ON media_item(library_id, missing);
CREATE INDEX IF NOT EXISTS idx_item_sort    ON media_item(sort_title);
CREATE INDEX IF NOT EXISTS idx_item_series  ON media_item(series, season, episode);

CREATE TABLE IF NOT EXISTS playback_state (
    item_id     INTEGER NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    user_id     TEXT    NOT NULL DEFAULT 'local',
    position_ms INTEGER NOT NULL DEFAULT 0,
    duration_ms INTEGER,
    watched     INTEGER NOT NULL DEFAULT 0,
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (item_id, user_id)
);
