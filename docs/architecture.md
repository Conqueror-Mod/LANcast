# Architecture

> **Status:** M1 is implemented and verified. `internal/meta` and
> `internal/artwork` are M2 and do not exist yet; they are marked below.

## Shape

LANcast is a single Go binary that owns a SQLite database and serves an HTTP
API. Clients are separate and thin. There is no daemon fleet, no message
broker, no external cache — a media server for a household does not need
distributed systems, and pretending otherwise is how self-hosted software
becomes unmaintainable.

```
                    ┌──────────────────────────────┐
   browser ────────▶│  internal/api    HTTP layer  │
   TV client        └──────┬───────────────┬───────┘
   3rd party               │               │
                    ┌──────▼──────┐  ┌─────▼──────────┐
                    │ internal/   │  │ internal/scan  │
                    │   store     │◀─┤  reconciler    │
                    └──────┬──────┘  └─────┬──────────┘
                           │               │
                    ┌──────▼──────┐  ┌─────▼──────────┐
                    │  SQLite     │  │ internal/media │
                    │  lancast.db │  │  name parsing  │
                    └─────────────┘  └────────────────┘
```

## Packages

**`cmd/lancastd`** — the only `main`. Parses flags, resolves config, opens the
store, constructs the scanner and API, serves, and shuts down gracefully on
interrupt. It contains wiring and nothing else; no logic lives here.

**`internal/config`** — resolves the data directory (platform config dir, or
`-data`) and the listen address. Creates the data directory if absent.

**`internal/store`** — the only package that speaks SQL. Owns `schema.sql`,
which is embedded and applied at open; every statement is `IF NOT EXISTS`, so
open is idempotent. Exposes typed methods (`CreateLibrary`, `UpsertItem`,
`ListItems`, `SaveProgress`, …) rather than a `*sql.DB`. This boundary is what
lets the storage layer change without a rewrite everywhere else.

**`internal/media`** — pure functions turning a path into a best guess:
`Parse(root, path) Info`, plus `IsVideo` and `SortTitle`. No I/O, no database,
fully unit-testable. Deliberately isolated: at M2 real metadata providers
overwrite these fields, and that must not require touching the scanner.

**`internal/scan`** — walks a library root, decides what changed, and
reconciles the database against the filesystem.

**`internal/meta`** *(M2)* — the provider contract and the merge engine. Holds
`Provider` (searchable remote sources) and `LocalSource` (path-based sidecar
readers) as separate interfaces, a `Registry`, confidence scoring, and
field-level precedence resolution. Everything downstream consumes a normalized
`Record` and never learns which source produced it. See
[metadata.md](metadata.md).

**`internal/artwork`** *(M2)* — content-addressed image cache. Fetches, hashes,
stores, and generates derived sizes on demand. Fully rebuildable: deleting the
cache directory must heal on next access.

**`internal/api`** — HTTP handlers over `net/http` with Go 1.22 pattern routing.
No third-party router; the standard library covers `GET /api/items/{id}` now.

**`internal/web`** — embedded client assets via `embed.FS`, so a deployed
LANcast is genuinely one file.

## Request lifecycle

1. `net/http` matches the method-and-pattern route.
2. Handler decodes and validates input. Validation happens here, not in `store`.
3. Handler calls one or more typed `store` methods.
4. Handler encodes JSON, or streams bytes for media.

Errors return a consistent JSON shape (see [api.md](api.md)). Handlers never
return raw SQL errors to clients.

## Streaming lifecycle

`GET /api/stream/{id}` is the one handler that turns database content into
filesystem access, so it carries an extra step:

1. Look up the item and its owning library.
2. Resolve the item path with `filepath.Abs`.
3. **Verify the resolved path is still inside the library root.** The database
   is trusted, but a bad or hand-edited row must not become arbitrary file read
   access. This check is mandatory and must never be optimized away.
4. Hand off to `http.ServeFile`, which provides correct `Range` handling,
   `If-Modified-Since`, and seeking for free.

At M1 this is direct play only — the file is served as-is. Transcoding (M3)
inserts a decision step between 3 and 4 without changing the contract.

## Scan lifecycle

1. Acquire the per-library scan lock. One scan per library at a time; a second
   request returns the in-progress status rather than queueing or racing.
2. `filepath.WalkDir` from the library root.
3. For each file where `media.IsVideo` holds, compare size and mtime against the
   stored row. Unchanged files are skipped without re-parsing — this is what
   makes rescans cheap on a large library.
4. Changed or new files go through `media.Parse` and are upserted, keyed on the
   unique `path` column.
5. Track every path seen. Anything in the library that was not seen is marked
   `missing = 1` — **never deleted.** A temporarily unmounted drive must not
   destroy library data, watch history, or user edits.
6. Progress is reported over a channel so the API can surface live scan state.

## Data model

Revision 1 lives in `internal/store/schema.sql`: `meta`, `library`,
`media_item`, `playback_state`.

Three choices there are deliberate and documented as ADRs rather than left to be
rediscovered:

- `media_item` is **one wide table** with nullable `series`/`season`/`episode`
  rather than separate movie and episode tables — see
  [ADR 0002](adr/0002-one-wide-media-item-table.md).
- `playback_state` is **keyed by `user_id`** even though M1 is single-user — see
  [ADR 0006](adr/0006-playback-state-keyed-by-user.md).
- File columns (`container`, `size_bytes`, `mtime`) are **nullable**, because M2
  introduces `media_item` rows that are directories rather than files — see
  [ADR 0010](adr/0010-shows-as-media-items.md).

The `meta` table carries `schema_version`. It exists from revision 1
specifically so the first migration does not have to guess what it is migrating
from. Migrations run in order inside a transaction, gated on that value.

Revision 2 (M2) adds match state, field locks, artwork, credits, genres, a
provider response cache, and `parent_id` for the show → season → episode
hierarchy. See [metadata.md](metadata.md).

## What is deliberately absent

No ORM, no dependency injection framework, no service mesh, no Redis, no
separate job queue. Each of these would add operational surface to software
whose whole promise is that it runs unattended on a NAS for years. When one
becomes genuinely necessary, it gets an ADR.
