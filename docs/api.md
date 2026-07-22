# HTTP API

> **Status:** M1 and M2 are **implemented and verified**. Only the theme-audio
> section remains unbuilt and is marked as such.
>
> This file and `internal/api/` must agree exactly — update both in the same commit.

Base path: `/api`. All request and response bodies are JSON except media
streams. Times are Unix seconds. Durations and positions are milliseconds.

## Versioning

Until the first external client exists, the API may change freely and this
document is the record of what it is. Once anything third-party depends on it,
breaking changes require a version prefix (`/api/v2`) and the previous version
keeps working for at least one release. "Clients are thin" is only true if the
contract they are thin against is stable.

## Authentication

Every endpoint requires a session cookie except `GET /api/health`,
`GET /api/auth/status`, `POST /api/auth/setup`, `POST /api/auth/login`, and the
web assets. Unauthenticated calls return `401 unauthorized`.

While no password is set the API is open — but the server binds `127.0.0.1`
only, so it is reachable solely from the machine it runs on.

**State-changing methods are origin-checked.** `POST`, `PUT`, `PATCH`, and
`DELETE` must carry an `Origin` or `Referer` matching the request host, or the
call returns `403 forbidden`. A request with neither header is allowed, so
non-browser clients work normally.

| Route | Purpose |
|---|---|
| `GET /api/auth/status` | `{configured, authenticated, lan_enabled}` |
| `POST /api/auth/setup` | Set the first password; only while unconfigured |
| `POST /api/auth/login` | `{password}` → session cookie. Throttled per IP |
| `POST /api/auth/logout` | Ends this session |
| `POST /api/auth/password` | `{current_password, new_password}`; **revokes all sessions** |

`setup` returns `restart_required: true` when the server is still loopback-bound,
so the client can explain why other devices cannot connect yet.

---

## Errors

Every error returns a consistent shape. Handlers never surface raw SQL errors.

```json
{ "error": { "code": "not_found", "message": "no item with id 412" } }
```

| Code | HTTP | Meaning |
|---|---|---|
| `bad_request` | 400 | Malformed body or invalid parameter |
| `not_found` | 404 | No such resource |
| `conflict` | 409 | Scan already running; duplicate library path |
| `unavailable` | 503 | File missing from disk |
| `internal` | 500 | Unexpected failure |

---

## Health

### `GET /api/health`

```json
{ "status": "ok", "version": "0.1.0" }
```

---

## Browse

### `GET /api/browse?path=`

Lists directories so clients can offer a folder picker. Omit `path` to get the
roots — drive letters on Windows, `/` elsewhere.

```json
{ "path": "D:\\Media", "parent": "D:\\",
  "entries": [ { "name": "Films", "path": "D:\\Media\\Films" } ] }
```

`parent` is `null` at the root listing and `""` at a filesystem root, so "up"
always leads somewhere rather than stranding the picker on one drive.

Directories only — never files, and never file contents. Dotfiles and
Windows hidden/system directories are omitted, so `$RECYCLE.BIN` and
`System Volume Information` are not offered as library candidates.

> **Security note.** This endpoint discloses filesystem layout to anyone who
> can reach the server, and there is no authentication yet. It grants no
> capability `POST /api/libraries` did not already have — that endpoint accepts
> and scans any path — but it makes enumeration convenient. Both belong behind
> auth before LANcast is exposed beyond a trusted LAN. See the security and
> remote-access areas in [roadmap.md](roadmap.md).

---

## Libraries

### `GET /api/libraries`

```json
[
  { "id": 1, "name": "Films", "kind": "movie", "path": "D:/Media/Films",
    "created_at": 1753142400, "scanned_at": 1753228800, "item_count": 412 }
]
```

### `POST /api/libraries`

`kind` is one of `movie`, `show`, `music`, `other`. The path must exist and be a
directory; both are validated before insert.

```json
{ "name": "Films", "kind": "movie", "path": "D:/Media/Films" }
```

Returns `201` with the created library. Returns `conflict` if the path is
already registered.

### `POST /api/libraries/{id}/scan`

Starts an asynchronous scan and returns `202` immediately. Returns `conflict`
if a scan is already running for that library — scans are not queued.

```json
{ "library_id": 1, "state": "running", "started_at": 1753228800 }
```

### `GET /api/libraries/{id}/scan`

Live scan progress. `state` is `idle`, `running`, or `failed`.

```json
{ "library_id": 1, "state": "running", "files_seen": 318,
  "items_added": 12, "items_updated": 3, "started_at": 1753228800 }
```

---

## Items

### `GET /api/items`

| Parameter | Meaning |
|---|---|
| `library_id` | Restrict to one library |
| `kind` | `movie`, `episode`, `track`, `other` |
| `q` | Case-insensitive substring match on title and series |
| `sort` | `title` (default), `year`, `added` |
| `limit` / `offset` | Pagination; `limit` defaults to 100, max 500 |

```json
{ "total": 412, "items": [ { "id": 87, "library_id": 1, "kind": "movie",
  "title": "Arrival", "year": 2016, "container": "mkv",
  "duration_ms": 6960000, "size_bytes": 8123456789,
  "series": null, "season": null, "episode": null,
  "added_at": 1753142400, "missing": false,
  "progress": { "position_ms": 1284000, "watched": false } } ] }
```

`path` is deliberately **not** exposed. Clients have no use for server
filesystem paths, and withholding them keeps the layout private when the server
is reachable beyond the LAN.

### `GET /api/items/{id}`

The list shape plus M2 metadata and a `theme` block (both below). Returns
`not_found` if the item does not exist; an item flagged `missing` still returns
`200` with `"missing": true`, so the UI can explain the situation rather than
pretend the item was never there.

From M2 the response also carries:

```json
{ "overview": "Thirty years after…", "rating": 7.5,
  "genres": ["Science Fiction", "Drama"],
  "credits": [ { "name": "Ryan Gosling", "character": "K", "role": "actor" } ],
  "artwork": { "poster": "9f2c4a…", "fanart": "3b81ee…" },
  "parent_id": null,
  "match_state": "locked", "match_score": 0.94,
  "locked_fields": ["title", "year"] }
```

`match_state` is one of:

| State | Meaning |
|---|---|
| `matched` | Confident provider match, applied silently |
| `review` | Applied, but uncertain — the UI should say so |
| `unmatched` | Looked and found nothing good enough |
| `locked` | User-confirmed; never re-scored |
| `local` | Resolved from an NFO sidecar; nothing to review |

**`metadata_updated_at` is null until enrichment has run.** Clients must check
it before treating `unmatched` as "no match found" — it is the default value,
so a freshly scanned item carries it before anything has looked at the item at
all. Reporting those as match failures buries the real ones.

### `PUT /api/items/{id}/progress`

```json
{ "position_ms": 1284000, "watched": false }
```

Returns `204`. Clients should throttle to roughly one call per five seconds
during playback.

---

## Streaming

### `GET /api/stream/{id}`

Returns the media file. Supports `Range` requests — this is what makes seeking
work, and it is the first thing to test when playback misbehaves. Returns
`unavailable` if the file is gone from disk.

At M1 this is direct play only: bytes are served as stored. Transcoding at M3
adds negotiation without changing the URL.

---

## Metadata and artwork

> M2. Design reference: [metadata.md](metadata.md).

### `PATCH /api/items/{id}`

Edit fields. **Every edited field is locked**, and locked fields are never
overwritten by a provider refresh — see
[ADR 0008](adr/0008-field-level-locking.md). Responses include `locked_fields`
so the client can show lock indicators; a lock the user cannot see or release is
indistinguishable from a bug.

```json
{ "title": "Blade Runner 2049", "year": 2017 }
```

### `DELETE /api/items/{id}/locks/{field}`

Release one lock. Returns `204`. The field resumes updating on the next refresh.

### `GET /api/items/{id}/candidates?q=`

Search the provider for re-match candidates. `q` defaults to the item's current
title. Accepts a TMDB id or URL directly in `q` for exact targeting.

```json
[ { "provider": "tmdb", "external_id": "335984", "title": "Blade Runner 2049",
    "year": 2017, "score": 0.94, "poster_hash": "9f2c4a…",
    "overview": "Thirty years after…" } ]
```

### `POST /api/items/{id}/match`

Apply a chosen candidate. Sets `match_state` to `locked`, so the item is never
re-scored or re-searched by any later scan.

```json
{ "provider": "tmdb", "external_id": "335984" }
```

### `GET /api/review?library_id=`

The review queue: items in `review` or `unmatched` state, with the parsed
filename alongside the proposed match so the two can be compared directly.

### `POST /api/items/{id}/refresh` · `POST /api/libraries/{id}/refresh`

Re-fetch metadata, honoring all field locks. Returns `202`.

### `GET /api/artwork/{hash}?size=`

`size` is one of `thumb`, `poster`, `poster2x`, `fanart`, `original`. Derived
sizes are generated on first request and cached.

Served with `ETag: "{hash}"` and `Cache-Control: public, max-age=31536000,
immutable`. Content addressing makes indefinite caching safe — the bytes behind
a hash cannot change.

### `GET` / `PUT /api/settings/providers`

TMDB key, rate limit, and the per-library NFO write toggle.

**The key is write-only.** `GET` returns `{"tmdb": {"configured": true}}` and
never the value itself. It is stored in the config file at `0600`, never in the
database.

---

## Theme audio

> Requires M2 metadata. Specified here because it shapes the item detail
> response.

### `theme` block on `GET /api/items/{id}`

```json
{ "theme": { "available": true, "source": "local",
  "track_title": "Main Title", "composer": "Jóhann Jóhannsson",
  "album": "Arrival (Original Motion Picture Soundtrack)" } }
```

`source` is `local`, `fetched`, or `none`. `available` refers to **audio**;
identification metadata may be present with `"available": false`, and that is a
normal, expected state rather than a failure. TV themes have a real network
source; film score audio does not, so films are identified but play only from a
local file. See [ADR 0005](adr/0005-theme-music-sourcing.md).

### `GET /api/items/{id}/theme`

Streams the resolved theme audio. Returns `not_found` when
`theme.available` is false — clients must treat that as silence, never as an
error to display.
