# Architecture

> **Status:** M0, M1, and M2 are built. M3 (transcoding + real client) is in
> progress — probe, transcode, subtitles, and auth have landed. Schema is at
> revision 6.

## Shape

LANcast is a single Go binary that owns a SQLite database and serves an HTTP
API. Clients are separate and thin. There is no daemon fleet, no message
broker, no external cache — a media server for a household does not need
distributed systems, and pretending otherwise is how self-hosted software
becomes unmaintainable.

```
                         ┌───────────────────────────────┐
   browser ─────────────▶│  internal/api    HTTP layer   │
   TV client             │  (internal/auth gates it)     │
   3rd party             └───┬──────────┬──────────┬─────┘
                             │          │          │
                  ┌──────────▼──┐  ┌────▼───────┐  │
                  │ internal/   │  │ internal/  │  │
                  │   store     │◀─┤   scan     │  │
                  └──────┬──────┘  └────┬───────┘  │
                         │              │          │
                  ┌──────▼──────┐  ┌────▼───────┐  │
                  │  SQLite     │  │ internal/  │  │
                  │  lancast.db │  │   media    │  │
                  └─────────────┘  └────────────┘  │
                         ▲                         │
              workers    │              playback   │
        ┌────────────────┴────┐      ┌─────────────▼──────────┐
        │ enrich → meta,      │      │ probe (decide) →       │
        │          artwork    │      │ transcode, subtitle    │
        └─────────────────────┘      └────────────────────────┘
```

Two background workers (`enrich`, and probing) and the playback path both read
and write through `store`; nothing bypasses it.

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

**`internal/meta`** — the provider contract and the merge engine, and the
project's first real extension point. Holds `Provider` (searchable remote
sources) and `LocalSource` (path-based sidecar readers) as separate interfaces,
a `Registry`, confidence scoring, and field-level precedence resolution.
Everything downstream consumes a normalized `Record` and never learns which
source produced it. Subpackages `nfo/` and `tmdb/` are implementations behind
those interfaces. At M4 the plugin runtime registers into the same registry. See
[metadata.md](metadata.md).

**`internal/enrich`** — the worker that fills in metadata and artwork after a
scan. Scanning and enriching are separate phases on purpose: a large first scan
populates the grid from filenames in seconds while metadata fills in behind it.

**`internal/artwork`** — content-addressed image cache. The SHA-256 of the
source bytes is the image's identity, so a backdrop shared by several items is
stored once. Generates derived sizes on demand and is fully rebuildable:
deleting the cache directory must heal on next access.

**`internal/auth`** — password verification and server-side session tokens.
bcrypt cost 12 in `config.json` at `0600`; sessions are rows keyed by the
SHA-256 of a 32-byte token, so a stolen database grants no sessions. The schema
is already keyed by user (ADR 0006), so real multi-user is an extension rather
than a rewrite. See [ADR 0011](adr/0011-single-password-with-server-sessions.md).

**`internal/probe`** — wraps ffprobe, persists results, and exposes `Decide()`,
which returns direct play, remux, or transcode *with a stated reason*.
`ParseJSON` is pure, so the decision rules are tested against fixtures with no
ffmpeg installed and no media on disk. Runs in its own worker, not inside
enrichment. See [ADR 0012](adr/0012-probe-before-transcode.md).

**`internal/transcode`** — runs ffmpeg to deliver media a client cannot play
directly. Argument construction is separated from process execution, so the
command line — where the subtle mistakes live — is testable without spawning
anything. One session machinery produces two outputs: progressive fragmented
MP4 (the default, no client library needed) and HLS with fMP4 segments
alongside. See [ADR 0013](adr/0013-transcode-pipeline.md).

**`internal/subtitle`** — discovery, extraction, and conversion. Browsers render
exactly one subtitle format, WebVTT; everything here exists to get text into
that format or to say clearly why it cannot. Includes OpenSubtitles search with
hash-first matching.

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

`/stream` remains direct play — the file is served as-is. As of M3 the decision
step lives beside it rather than inside it: the client asks
`GET /api/items/{id}/playback` for the probe's verdict and then picks its
source. Direct play uses the range-served `/stream`; anything else uses
`/stream/{id}/transcode` (progressive fMP4) or the HLS endpoints. A direct-play
guess that fails falls back to transcoding once rather than showing a black
rectangle. The containment check above applies identically on every one of
those paths.

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

Later revisions, each a forward-only migration in its own transaction (there are
deliberately no down migrations):

| Rev | Adds |
|---|---|
| 2 | match state, field locks, artwork, credits, genres, provider response cache, and `parent_id` for show → season → episode — see [metadata.md](metadata.md) |
| 3 | `session` — server-side auth sessions |
| 4 | probe results on `media_item`: `probed_at`, codecs, profile, dimensions, bitrate |
| 5 | `external_subtitle` |
| 6 | `video_frame_rate` |

`CurrentSchemaVersion` is the single source of truth for where the chain ends,
and `internal/store` has a test asserting a freshly-created database matches it —
so `schema.sql` and the migration chain cannot silently diverge.

## What is deliberately absent

No ORM, no dependency injection framework, no service mesh, no Redis, no
separate job queue. Each of these would add operational surface to software
whose whole promise is that it runs unattended on a NAS for years. When one
becomes genuinely necessary, it gets an ADR.

**No hls.js**, specifically. The server produces HLS, but making it the default
client path would mean vendoring ~300KB of unaudited third-party JavaScript into
a server whose premise is that you own what runs. Progressive fMP4 covers the
default client instead. This is a stated trade
([ADR 0013](adr/0013-transcode-pipeline.md)), not an omission to correct.

ffmpeg and ffprobe are external binaries rather than linked libraries — the one
deliberate process dependency, which is also why parsing and argument
construction are kept pure and separately testable.
