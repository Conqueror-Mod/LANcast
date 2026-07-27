# HTTP API

> **Status:** M1 and M2 are **implemented and verified**. Only the theme-audio
> section remains unbuilt and is marked as such.
>
> This file and `internal/api/` must agree exactly — update both in the same commit.

Base path: `/api`. All request and response bodies are JSON except media
streams. Times are Unix seconds. Durations and positions are milliseconds.

## Versioning

The full policy is [ADR 0018](adr/0018-api-contract-and-versioning.md). In short:

- **`/api` is permanently version 1.** A breaking revision ships at `/api/v2`;
  `/api` never changes meaning. `GET /api/health` reports `api_version` so a
  client can assert the contract it was built against.
- **Additive changes are non-breaking** and may ship at any time: a new response
  field, a new endpoint, a new value in an **open set** (`kind`, `match_state`),
  a new optional parameter, or a new error `code`. **Breaking** — removing or
  renaming a field, changing a type or status code, tightening validation,
  changing the meaning of an existing `kind`/`match_state`/`code` — requires
  `/api/v2`, and v1 then keeps working for at least one release.
- **Two client obligations make this safe:** clients **must ignore unknown
  response fields**, and **must tolerate unknown `kind`, `match_state`, and
  error `code` values**, degrading gracefully rather than crashing. New media
  types (`collection`, `part`, `serial` — [ADR 0017](adr/0017-collections-and-multi-part-works.md))
  arrive this way, so a client with an exhaustive `kind` switch is relying on a
  guarantee this contract does not give.

"Clients are thin" is only true if the contract they are thin against is stable.

## Authentication

Every endpoint requires a session cookie except `GET /api/health`,
`GET /api/auth/status`, `POST /api/auth/setup`, `POST /api/auth/login`, and the
web assets. Unauthenticated calls return `401 unauthorized`.

While no account exists the API is open — but the server binds `127.0.0.1`
only, so it is reachable solely from the machine it runs on.

**State-changing methods are origin-checked.** `POST`, `PUT`, `PATCH`, and
`DELETE` must carry an `Origin` or `Referer` matching the request host, or the
call returns `403 forbidden`. A request with neither header is allowed, so
non-browser clients work normally.

| Route | Purpose |
|---|---|
| `GET /api/auth/status` | `{configured, authenticated, lan_enabled, user?}` |
| `POST /api/auth/setup` | `{username, password}` → creates the first admin; only while unconfigured |
| `POST /api/auth/login` | `{username, password}` → session cookie. Throttled per IP |
| `POST /api/auth/logout` | Ends this session |
| `POST /api/auth/password` | `{current_password, new_password}`; changes **your own** password and revokes **your** sessions |

When a session is active, `status`, `setup`, and `login` include
`user: {id, name, role}`. `setup` returns `restart_required: true` when the
server is still loopback-bound, so the client can explain why other devices
cannot connect yet. A wrong username and a wrong password are reported
identically as `401 unauthorized`.

### Roles

Every account is `admin` or `member` ([ADR 0015](adr/0015-multi-user-accounts.md)).

- **admin** — everything, including the management surfaces below.
- **member** — browse, play, and their own watch state. A member calling an
  admin-only endpoint gets `403 forbidden`.

Admin-only endpoints: `GET /api/browse`; `POST`/`DELETE /api/libraries…`, library
`scan` and `refresh`; item metadata mutation (`PATCH /api/items/{id}`, lock
delete, `match`, item `refresh`); `GET`/`PUT /api/settings`; and all of
`/api/users`. Everything else a signed-in member may call.

### Users (admin only)

| Route | Purpose |
|---|---|
| `GET /api/users` | `{users: [{id, name, role}]}` |
| `POST /api/users` | `{username, password, role?}` → `201 {id, name, role}`. Role defaults to `member`. `409` if the name is taken |
| `DELETE /api/users/{id}` | Removes the account, its sessions, and its watch state. `409` if it is the last admin |
| `POST /api/users/{id}/password` | `{new_password}` → resets that user's password and revokes their sessions |

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
{ "status": "ok", "version": "0.2.0", "api_version": 1 }
```

`version` is the application release (semver); `api_version` is the HTTP
contract revision and changes only when a new `/api/vN` prefix ships.

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

### `DELETE /api/libraries/{id}`

Forgets a library: its rows and, by `ON DELETE CASCADE`, its items, playback
state, and subtitles. **Never deletes media from disk** — LANcast only ever
stored paths, so this is "stop tracking this folder", not a destroy. `204` on
success, `404` if unknown, `409` if a scan is running for it.

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
  "items_changed": 12, "items_missing": 0, "skipped": 2,
  "issues": [ { "path": "Kids/broken", "reason": "unreadable" } ],
  "started_at": 1753228800 }
```

`skipped` counts files or directories the scan could not process; `issues`
lists them (capped) with a **library-relative** path — never the absolute
server path, held back for the same privacy reason item paths are. This is the
diagnostic answer to "the scan finished but some files are missing — why?"

---

## Items

### `GET /api/items`

| Parameter | Meaning |
|---|---|
| `library_id` | Restrict to one library |
| `kind` | `movie`, `episode`, `show`, `season`, `serial`, `part`, `chapter`, `collection`, `track`, `other` |
| `parent_id` | Return the children of one item — a show's episodes, a work's parts |
| `collection_id` | Return a collection's members (many-to-many; not `parent_id`) |
| `q` | Case-insensitive substring match on title and series |
| `sort` | `title` (default), `year`, `added` |
| `limit` / `offset` | Pagination; `limit` defaults to 100, max 500 |

**By default the listing is top-level only** — rows with no parent. Children
(seasons, episodes, and the `part`/`chapter` pieces of a multi-part work) have a
parent and are reached through `parent_id`, never returned loose in the grid, so
a container's pieces do not appear as if they were features ([ADR 0010](adr/0010-shows-as-media-items.md),
[ADR 0017](adr/0017-collections-and-multi-part-works.md)). Passing an explicit
`kind` lifts that default, for a deliberate cross-cutting query (every episode,
say). Passing `parent_id` returns exactly that item's children in hierarchy
order.

`kind` is an **open set** — new media types (`collection`, `part`, `serial`, …)
appear without an API version bump, so a client must tolerate a `kind` it does
not recognise ([ADR 0018](adr/0018-api-contract-and-versioning.md)) rather than
assume the list above is exhaustive.

A **collection** (`kind: "collection"`) groups otherwise-independent items —
a film series, a franchise — through many-to-many membership, not `parent_id`;
its members stay top-level and may belong to more than one collection. Fetch its
members with `?collection_id=`, **not** `?parent_id=` (which is always empty for
a collection). A collection is shown in the top-level grid only when it groups
**at least two present members**: a provider supplies a franchise even when the
library holds a single film from it, and a collection of one is just a duplicate
tile of that film.

A **multi-part work** (a two-part film, a serial, a miniseries) instead
*contains* its pieces through `parent_id`: the parent carries the identity, the
`part` / `chapter` children carry the files.

`child_count` counts whichever applies — `parent_id` children for a work or
show, join-table members for a collection — so a client can treat any container
uniformly.

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

`child_count` is the number of present items that name this one as parent —
nonzero for a container (a show, a season, a collection, a multi-part work). It
is omitted when zero. A client uses it to tell a container from a leaf: a
container opens its children (via `parent_id`) and offers no Play, so a
`movie`-kind parent of `part` children — a two-part film ([ADR 0017](adr/0017-collections-and-multi-part-works.md))
— is not given a dead-end Play button. `kind` alone cannot express that, which
is why the count is part of the item shape.

### `GET /api/continue`

The user's in-progress items, most recently played first — the home screen's
first shelf. `limit` defaults to 20 (max 100).

```json
{ "items": [ { "id": 87, "title": "Arrival", ...,
  "progress": { "position_ms": 1284000, "watched": false } } ] }
```

"In progress" is a saved position past zero with `watched` unset: an item played
to the end drops off rather than inviting a replay. Progress and artwork are
included so a tile draws its resume bar and poster without a second call.

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

### `DELETE /api/items/{id}?mode=`

Removes a title. **Admin only.** `mode` is required:

- `ignore` — the files stay on disk; their paths are added to a per-library
  ignore list so a rescan never re-adds them. The non-destructive "stop
  tracking this" removal.
- `delete` — the files are removed from disk. Each is re-verified as contained
  within its library root first, so a bad row can never delete outside the
  library; if any path fails that check nothing is deleted (`500`). A file
  already gone is not an error.

A container (a show, a multi-part work) removes its whole subtree — every
episode or part. A collection is a grouping with no file of its own, so
removing one drops only the grouping row, never the member films. `204` on
success, `404` if unknown, `400` if `mode` is missing or invalid.

---

## Subtitles

> M3. Format classification, conversion, and matching live in
> `internal/subtitle`.

### `GET /api/items/{id}/subtitles`

Every track for an item — embedded and external, in one list.

```json
{ "item_id": 87, "tracks": [
  { "key": "embedded-2", "label": "English", "language": "en",
    "source": "embedded", "codec": "subrip", "forced": false,
    "default": true, "available": true },
  { "key": "embedded-3", "label": "English", "language": "en",
    "source": "embedded", "codec": "hdmv_pgs_subtitle", "available": false,
    "reason": "image-based subtitles (HDMV_PGS_SUBTITLE) cannot be shown as text — search for a subtitle file instead" }
] }
```

**Unavailable tracks are listed, not hidden.** PGS and VOBSUB are images of
text, so there is nothing to convert without OCR. Omitting them would leave a
viewer wondering why a film they know has subtitles appears to have none.

### `GET /api/items/{id}/subtitles/{key}.vtt`

The track as WebVTT, the only subtitle format browsers render. SubRip is
converted in Go; ASS and embedded tracks go through ffmpeg. Results are cached.

Returns `422 unsupported` for bitmap tracks and `503` when ffmpeg is needed but
absent.

### `GET /api/items/{id}/subtitles/search`

Searches OpenSubtitles. `?q=` overrides the query, `?language=` the language.

```json
{ "item_id": 87, "hash_used": true, "auto_match": true,
  "candidates": [ { "file_id": 99, "release": "Film.2020.1080p.BluRay-GROUP",
                    "language": "en", "download_count": 4210, "fps": 23.976,
                    "hash_match": true, "score": 1,
                    "reason": "matches this exact file" } ] }
```

The OpenSubtitles movie hash — file size plus the first and last 64KB — is
computed and sent with every search. A hash match means the subtitle was timed
against these exact bytes, so it scores 1.0 and short-circuits the rest.

**A candidate for a different film is rejected before anything else is
weighed.** The provider is asked for this title, but a hash query returns
whatever is tagged with that hash and a title query returns near-title noise, so
subtitles for other movies routinely appear; if their release traits happen to
agree they would otherwise score past the auto-apply line. The candidate's
parsed title is cross-checked against the item's, and a disagreement overrides
every other signal — including a claimed hash match, since a hash mapping to
another movie's file is bad provider data. Such candidates are demoted (`reason`
begins `different title`), never auto-applied, but stay listed in case the
item's own title is wrong.

The **year** is checked the same way, for the same reason: "Aladdin (1992)" and
"Aladdin (2019)" share a title but not a single cue timing, and the title check
alone cannot separate them. When both years are known and differ, the candidate
is demoted (`reason` begins `different year`) and cannot auto-apply; a candidate
that omits its year is not penalised.

Without a hash match, candidates score on what predicts sync: frame rate
(0.35), edition (0.25), source (0.20), release group (0.15), resolution (0.05).
**Download count is a tiebreak worth at most 0.10** and can never carry a
candidate over the auto-apply line — the most-downloaded entry is frequently
for a different release.

`auto_match` is true only at 0.90 or above. A subtitle that does not sync is
distracting for two hours; a prompt costs one click.

Returns `503` without an API key, `429` when the daily quota is spent.

### `POST /api/items/{id}/subtitles/download`

`{file_id, language, file_name}` → downloads and attaches the subtitle,
returning its `key`. Files are written to the data directory, **never beside
the media** — the same rule NFO writing follows.

### `DELETE /api/items/{id}/subtitles/{key}`

Removes a **downloaded** subtitle: its row and its file. `204` on success.

Only downloaded subtitles can be removed. An embedded track lives inside the
video, and a sidecar lives in the user's library — deleting files there is the
line the scanner refuses to cross (*marks missing, never deletes*). A wrong
download is entirely the server's own, so it is the one safe case. Embedded or
malformed keys return `400`; a sidecar returns `403`; an id belonging to another
item returns `404`, since the lookup is scoped to `{id}`.

---

## Playback decisions

> M3. Design reference: [ADR 0012](adr/0012-probe-before-transcode.md).

### `GET /api/items/{id}/playback`

How this file would be delivered to a browser, and why.

```json
{ "item_id": 87, "probed": true,
  "decision": { "method": "transcode",
                "reason": "audio codec eac3 is not supported",
                "video_action": "copy", "audio_action": "encode",
                "target_format": "mp4" } }
```

`method` is `direct`, `remux`, or `transcode`. The actions matter
independently: a `transcode` with `video_action: "copy"` re-encodes only the
audio, which is a fraction of the cost of a full re-encode and covers about a
third of a typical library.

`reason` is always populated. "Why is this transcoding" should not require
reading server logs.

An unprobed item returns `direct` — the same behavior LANcast had before
probing existed, rather than guessing at a transcode for a file nothing has
inspected.

### `GET /api/probe`

Background probing progress.

```json
{ "available": true, "running": false, "probed": 225,
  "failed": 0, "remaining": 0, "total": 225 }
```

`available` is false when ffprobe is not installed. That is a supported
configuration, not an error: playback decisions fall back to direct play.

Item responses gain `duration_ms`, `width`, `height`, `video_codec`,
`video_profile`, `video_bitrate`, `audio_codec`, `audio_channels`, and
`probed_at`. The detail response also carries `streams` — the full track list,
including subtitle and alternate audio tracks.

---

## Streaming

### `GET /api/stream/{id}`

Returns the media file. Supports `Range` requests — this is what makes seeking
work, and it is the first thing to test when playback misbehaves. Returns
`unavailable` if the file is gone from disk.

Direct play only: bytes are served as stored, with range support. Files that a
browser cannot play use the transcode endpoints below, chosen by the client
after consulting `/playback`.

### `GET /api/stream/{id}/transcode`

Streams a progressive fragmented MP4 produced by ffmpeg on demand. Plays in any
browser with no client library. `?t=` seconds sets a start offset; `?audio=`
selects a specific track by absolute index.

`Accept-Ranges: none` — a live transcode has no length and cannot be
range-served, since bytes do not exist until ffmpeg produces them. Seeking
forward restarts the stream from a new `?t=`.

Returns `409 conflict` for a file that can be played directly (transcoding it
would be wasted CPU), `503` if ffmpeg is not installed, and `429` past the
concurrent-transcode limit.

### `GET /api/stream/{id}/hls/index.m3u8`

The same transcode as an HLS playlist with fMP4 segments, for clients that
speak HLS. Segment URLs point back at
`GET /api/stream/{id}/hls/{session}/{name}`.

### `GET /api/transcode`

Lists running transcode sessions, and whether ffmpeg is available.

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

Search the provider for re-match candidates. **Omit `q`** to search by what the
file is named — the identity re-derived from the filename — not by the current
stored title, which after a wrong match *is* the wrong film; scoring against it
would make the search circle the wrong identity. A title the user locked by hand
is honoured instead. Passing `q` overrides the title (and drops the year), for a
fresh user-driven search; a TMDB id or URL in `q` targets exactly.

**The search spans both film and television**, regardless of the item's own
kind, and each candidate reports its `Kind` (`movie` or `show`). A TV miniseries
scanned into a movie library — Storm of the Century as a multi-part work — can
only be corrected if Fix match can reach TMDB's TV data; a movie-scoped search
returns only the wrong, same-named film. The client labels each candidate Movie
or TV so the two are distinguishable.

Each candidate carries a `Breakdown`: the sub-scores that combine, by their
weights (title 0.60, year 0.30, popularity 0.10), into the total. This is what
lets the UI explain a score — "title matched, but the year is 27 off" — rather
than present a bare number.

```json
[ { "Provider": "tmdb", "ExternalID": "335984", "Kind": "movie",
    "Title": "Blade Runner 2049", "Year": 2017, "Score": 0.94,
    "PosterURL": "https://…", "Overview": "Thirty years after…",
    "Breakdown": { "title": 1.0, "year": 1.0, "popularity": 0.31,
                   "total": 0.94, "year_gap": 0 } } ]
```

### `POST /api/items/{id}/match`

Apply a chosen candidate. Fetches that exact record from the provider and
applies it immediately (honouring locked fields), then sets `match_state` to
`locked` so the item is never re-scored or re-searched by any later scan. The
response is the updated item, already carrying the new metadata. Applying is
synchronous and deliberately does not go through the background pass, which
skips locked items and re-searches — that would re-pick the rejected candidate.

`kind` is the chosen candidate's kind and may differ from the item's own — this
is how a movie-scanned miniseries is corrected to its TV entry, fetched from the
provider's TV endpoint. Omit it to fetch as the item's existing kind.

```json
{ "provider": "tmdb", "external_id": "335984", "kind": "movie" }
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
