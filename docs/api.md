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
| `GET /api/auth/status` | `{configured, authenticated, lan_enabled, restart_required, user?}` |
| `POST /api/auth/setup` | `{username, password}` → creates the first admin; only while unconfigured |
| `POST /api/auth/login` | `{username, password}` → session cookie. Throttled per IP |
| `POST /api/auth/logout` | Ends this session |
| `POST /api/auth/password` | `{current_password, new_password}`; changes **your own** password and revokes **your** sessions |

When a session is active, `status`, `setup`, and `login` include
`user: {id, name, role}`. A wrong username and a wrong password are reported
identically as `401 unauthorized`.

`restart_required` is returned by both `status` and `setup`, and is true only
when restarting would actually bind wider than the server is bound right now —
the loopback restriction is what is holding it back, *and* the configured
address reaches further once lifted. A server the operator deliberately bound
to a loopback address reports `false`: a restart would change nothing there,
and a client that promises otherwise sends them to do something that cannot
work. It is not the inverse of `lan_enabled`.

`lan_enabled` reports whether the socket actually reaches beyond this machine —
not whether a password is set. An unsecured server is always `false`, because
it is forced onto `127.0.0.1`. A secured server the operator deliberately bound
to a loopback address is also `false`, and stays plain HTTP: TLS turns on when
the server becomes reachable by someone else, which is the same boundary that
gates LAN binding ([ADR 0014](adr/0014-transport-security.md)).

### Roles

Every account is `admin` or `member` ([ADR 0015](adr/0015-multi-user-accounts.md)).

- **admin** — everything, including the management surfaces below.
- **member** — browse, play, and their own watch state. A member calling an
  admin-only endpoint gets `403 forbidden`.

Admin-only endpoints: `GET /api/browse`; `POST` and `DELETE` on `/api/libraries`, library
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
{ "status": "ok", "version": "0.3.0", "api_version": 1 }
```

`version` is the application release (semver), stamped from the release tag at
build time — a build from source reports `dev`. `api_version` is the HTTP
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

`kind` is one of `movie`, `show`, `music`, `picture`, `other`. The path must exist and be a
directory; both are validated before insert.

```json
{ "name": "Films", "kind": "movie", "path": "D:/Media/Films" }
```

Returns `201` with the created library. Returns `conflict` if the path is
already registered.

### `PATCH /api/libraries/{id}`

Edits a library. **Admin only.** Returns the updated library.

```json
{ "name": "Films", "path": "E:\Movies" }
```

Both fields are optional; omitted means unchanged.

**`kind` cannot be changed** and sending a different one is a `400`, not a
silent no-op — a client that sends a kind believes it is changing one. A kind
decides which scanner runs, which provider is asked, and what the top level of
the browse is; changing it would leave a library describing itself as something
its rows are not. Add the folder again as the type you meant.

**Changing `path` moves the library and its contents.** Every item path under
the old root is rewritten to the new one in a single transaction, along with the
ignore list — so a drive letter change keeps every match, every piece of
artwork, every watch position and every playlist that referenced those files.
Nothing is deleted and nothing is marked missing; a rescan afterwards reconciles
files exactly as it always does. A path that does not exist, or is not a
directory, is refused **before** anything is rewritten: a typo must not mark a
whole library missing.

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

**`kind` is permanent.** It decides which files are scanned at all — a `music`
library indexes audio, a `picture` library images, everything else video — and
it biases matching between films and TV. There is no endpoint to change it:
altering it would mean a rescan re-litigating identity for an entire library,
which is what field locking exists to prevent. Remove the library and add it
again.

### `GET /api/libraries/{id}/scan`

Live scan progress. `state` is `idle`, `running`, or `failed`.

```json
{ "library_id": 1, "state": "running", "files_seen": 318,
  "items_changed": 12, "items_missing": 0, "skipped": 2,
  "skipped_kind": 0,
  "episodes_in_movie_library": 0,
  "issues": [ { "path": "Kids/broken", "reason": "unreadable" } ],
  "started_at": 1753228800 }
```

`skipped` counts files or directories the scan could not process; `issues`
lists them (capped) with a **library-relative** path — never the absolute
server path, held back for the same privacy reason item paths are. This is the
diagnostic answer to "the scan finished but some files are missing — why?"

`skipped_kind` counts media the library's **kind** excludes: audio files in a
movie or show library, video files in a music library. It is deliberately not
folded into `skipped`, because nothing failed — those files were read fine and
correctly ignored. It answers a different question: "the scan finished and the
library is empty — why?" A music library created as a movie library discards
every track, and without this number the scan reports zero items as though the
folder were empty.

Only files that are media of the other sort are counted. Artwork, `.nfo`
sidecars and subtitles are ignored by every library and would bury the signal.

`episodes_in_movie_library` is the other half of that warning, and the half that
is easy to miss. A music library created as a movie library reports an empty
library, which is loud. A **shows** library created as a movie library imports
everything and looks fine: every episode becomes a film, loose in the grid with
no series and no seasons, and nothing says why. Counted, never corrected — the
parse is right and the *library* is wrong, and a scan that changed the kind
would be re-litigating identity for a whole library, which the locked-fields
rule forbids. Kind cannot be changed afterwards, so being loud at the moment it
happens is the only defence there is.

### `GET /api/items/{id}/photo`

The picture itself, at full resolution. Photos only; anything else is `404`.

Serves the **original file** when a browser can render it — jpeg, png, webp,
gif, bmp — and the cached rendition when it cannot. HEIC is the case that
matters: Chromium and Firefox do not decode it, so handing over the original
bytes would be technically correct and useless. The rendition is the 1600px copy
the thumbnail worker made, and until that worker has reached the file the
endpoint answers `503` with a plain reason rather than serving bytes nothing can
draw.

Like `/api/stream/{id}`, this handler turns a database row into filesystem
access, so it **re-verifies containment** within the owning library root after
resolving the path. A row pointing outside its library is `404`, not a file.

The original is served with `Cache-Control: private, max-age=0,
must-revalidate` — it is addressed by item id, and the file behind an id can be
replaced on disk without the id changing. The rendition is content-addressed and
is served `immutable`.

Items in a picture library also carry `width`, `height` and `taken_at`.
`taken_at` is EXIF capture time and is absent when the file carries none, which
is most of a wallpaper or AI-art library — it is not `added_at`, which is when
the file reached this disk.

`sort=taken` orders by capture time, newest first, falling back to file mtime so
that pictures without EXIF stay among the dated ones rather than forming one
undifferentiated block.

### `GET /api/libraries/{id}/facets`

The filter values a library's browse view offers — genres, decades, and content
ratings actually present among its top-level items, so a chosen filter never
yields an empty grid. Genres and content ratings are sorted; decades are
newest-first.

```json
{ "genres": ["Comedy", "Drama", "Science Fiction"], "decades": [2010, 1990],
  "content_ratings": ["PG", "PG-13", "R"], "has_watched": true }
```

`has_watched` is true when the calling user has finished at least one top-level
item in the library, so the client offers the unwatched-only toggle only when it
would actually remove something rather than being a silent no-op. `genres`,
`decades`, and `content_ratings` are always present, empty when nothing applies.

---

## Items

### `GET /api/items`

| Parameter | Meaning |
|---|---|
| `library_id` | Restrict to one library |
| `initial` | The A–Z rail: one letter, or `#` for titles starting with anything that is not a Latin letter. Matches on `sort_title`, case-insensitively. A **filter, not a scroll offset** — the grid pages in as you scroll, so "jump to S" cannot mean "scroll to a row that has not loaded". `GET /api/libraries/{id}/facets` returns `initials`, the letters actually present, so a client never offers one that finds nothing |
| `exclude_kind` | Drops one kind from the listing. The browse grid passes `collection`: a franchise groups films rather than being one, and a collection tile beside its own members made a curated shelf read as an unsorted one. Collections have their own page, asked for with `kind=collection` |
| `kind` | `movie`, `episode`, `show`, `season`, `serial`, `part`, `chapter`, `collection`, `artist`, `album`, `track`, `gallery`, `photo`, `playlist`, `other`. **An open set** ([ADR 0018](adr/0018-api-contract-and-versioning.md)) — new kinds arrive without a major version, so a client with an exhaustive switch is relying on a guarantee it does not have |
| `parent_id` | Return the children of one item — a show's episodes, a work's parts |
| `collection_id` | Return a collection's members (many-to-many; not `parent_id`) |
| `playlist_id` | Return a playlist's entries **in playing order** ([ADR 0030](adr/0030-playlists-and-m3u.md)). The only listing that may repeat an item id — see below |
| `q` | Case-insensitive substring match on title and series |
| `genre` | Restrict to items carrying this exact genre name. **Repeatable** — `genre=A&genre=B` matches either |
| `decade` | Restrict to a decade — `1990` means 1990–1999. **Repeatable**; a non-numeric value is `400` |
| `content_rating` | Restrict to this exact content rating (PG, R, TV-MA…). **Repeatable** |
| `watched` | `watched=false` restricts to items the calling user has not finished; any other value is ignored |
| `sort` | `title` (default), `year`, `added`, `rating` (highest first; unrated last), `track` (disc then track number — see Music items) |
| `limit` / `offset` | Pagination; `limit` defaults to 100, max 500 |

Repeatable filters are **OR within a facet and AND across facets**: two genres
widen the grid, adding a decade narrows it. A blank value (`genre=`) is dropped
rather than treated as a filter for the empty string. `watched` keys off the
leaf's own play state, so it filters movies and episodes; a container (a show)
carries no watched flag and is unaffected.

**By default the listing is top-level only** — rows with no parent. Children
(seasons, episodes, and the `part`/`chapter` pieces of a multi-part work) have a
parent and are reached through `parent_id`, never returned loose in the grid, so
a container's pieces do not appear as if they were features ([ADR 0010](adr/0010-shows-as-media-items.md),
[ADR 0017](adr/0017-collections-and-multi-part-works.md)). Passing an explicit
`kind` lifts that default, for a deliberate cross-cutting query (every episode,
say). Passing `parent_id` returns exactly that item's children.

**A container's children are not automatically in hierarchy order.** The default
sort leads with title, and episodes come back in season order only because they
share their series' sort title and therefore tie, letting the order fall through
to season and episode. Tracks keep their own titles, so an album asked for
without a sort comes back **alphabetically**. Use `?sort=track` for an album.

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

The item **detail** response carries `file_name` — the base name of the file,
never the directory. A title whose metadata is wrong cannot be corrected if there
is no way to tell which file it is (`01 Magnetic Rose` against its siblings), and
the name alone gives that without disclosing where anything lives.

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
  "locked_fields": ["title", "year"],
  "ratings": [ { "source": "rotten_tomatoes", "score": 8.8, "display": "88%" },
               { "source": "imdb", "score": 8.0, "display": "8.0", "votes": 634000 } ] }
```

`artwork` may carry `"inherited": true`, which means the poster is **borrowed
rather than owned**. Today that is only an artist wearing one of its albums'
covers: an album has a picture embedded in its tracks or a `cover.jpg` beside
them, and an artist has neither — the images that do sit in an artist folder
turn out to be a media player's per-album art cache rather than a photograph of
anyone. The borrowed album is the one with the most tracks, so a record is
chosen over a stray single, with sort title and id as tie-breakers so a tile
does not change its face between two reads.

The flag is reported rather than hidden so a client can treat it as the
placeholder it is. Nothing is stored: the fallback stops applying by itself the
moment an artist has a real image, so a future provider needs nothing cleaned
up. An artist whose albums all lack art has no `artwork` at all, which is the
honest state rather than an invented one.

`ratings` is the external scores from third-party sources ([ADR 0019](adr/0019-external-ratings.md)),
highest normalized `score` (0–10) first, each with a source-native `display`
string (`"88%"`, `"81"`, `"8.0"`) and an optional `votes` count. `source` is an
**open set** — `imdb`, `rotten_tomatoes`, `metacritic`, and more later — so a
client renders whatever arrives rather than switching on a fixed list. The field
is **omitted** when no external ratings are known (no OMDb key, or a title the
source does not cover), which is a normal state, not an error. `rating` (the
single TMDB scalar) is unchanged and independent.

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

How this file would be delivered to a client, and why.

```json
{ "item_id": 87, "probed": true, "profile": "browser",
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

`audio_only` is present and `true` when the file has no video stream — a music
track, or a video file with only audio. Embedded cover art does not count: it
is stored as a video stream and is ignored, because treating a still frame as
the picture would have ffmpeg encode it for the length of the track. A client
can use this to attach the source to an `<audio>` element; a non-direct stream
of audio-only content is served as `audio/mp4` rather than `video/mp4`.

Takes the same `?profile=` and `?audio=` parameters as the stream endpoints,
and echoes the resolved profile back. Call it with the parameters you intend to
stream with: an explanation of a decision the server would not actually make
sends you looking in the wrong place. `?audio=` naming a track that does not
exist returns `400 bad_request`.

An unprobed item returns `direct` — the same behavior LANcast had before
probing existed, rather than guessing at a transcode for a file nothing has
inspected.

#### Client profiles

`?profile=` names what the client can play. Unknown or absent falls back to
`browser`; guessing generously for a client the server cannot identify is how
black rectangles happen.

| Profile | Video | Audio | Containers |
| --- | --- | --- | --- |
| `browser` (default) | h264, vp8, vp9, av1 | aac, mp3, opus, vorbis, flac, pcm_s16le, pcm_u8 | mp4, webm, mov, mp3, flac, ogg, wav |
| `safari` | h264, hevc, av1 | aac, mp3, ac3, eac3, flac, opus, alac, pcm_s16le, pcm_s24le, pcm_u8 | mp4, mov, mp3, flac, wav |
| `tv` | h264, hevc, vp9, av1, mpeg2video | aac, mp3, opus, vorbis, flac, alac, ac3, eac3, dts, truehd, pcm_s16le, pcm_s24le, pcm_u8 | mp4, matroska, webm, mov, mpegts, mp3, flac, ogg, wav, aac |

`browser` excludes HEVC deliberately: Chrome's support is conditional on
hardware and Firefox has none, so claiming it for an unidentified client trades
a cheap remux for an unexplained failure. Clients that know better say so — with
`?can=`.

#### `?can=` — what this client can also play

A comma-separated list of extra capabilities, applied **on top of** the named
profile:

```
GET /api/items/87/playback?can=hevc,ac3
```

| Claim | Adds |
| --- | --- |
| `hevc` | HEVC video, and the `matroska` container it usually arrives in |
| `ac3`, `eac3`, `dts` | that audio codec |
| `matroska` | the container alone, for a client with a real demuxer |

**It only ever widens.** A claim cannot remove anything the profile already
allows, an unrecognised claim is ignored rather than refused, and an absent
parameter behaves exactly as before — so this is additive under
[ADR 0018](adr/0018-api-contract-and-versioning.md) and a client that never
learns about it is unaffected. An older server meeting a newer client serves the
file rather than rejecting the request.

**Send it to every endpoint that decides, or none.** `/api/items/{id}/playback`
decides how a file will be delivered and `/api/stream/{id}/transcode` decides
again when it is asked for. Claiming HEVC on the first and not the second means
being told "direct play" and then handed a re-encode, or getting `409` for a
transcode the server no longer thinks is needed.

**A claim is a claim.** `canPlayType` answers "probably", and HEVC support
depends on the GPU and sometimes on an OS codec extension. A client that claims
something it cannot decode gets a failure only it sees — nothing else on the
LAN is affected — and is expected to stop claiming it and ask again. The
shipped client does exactly that: it drops the capability, remembers the
refusal, and re-requests the file as a conversion.

The bare audio containers exist for music, where the container *is* the codec:
an `.mp3` probes as container `mp3`, a `.flac` as `flac`, an `.m4a` as `mov`.
Without them every track fails the container check and rewraps into MP4 — and
because MP4 cannot carry FLAC (see below), a lossless file would be re-encoded
to AAC to deliver a format the client already plays natively. `safari` carries
ALAC and drops Ogg, matching what Apple ships decoders for. PCM is claimed at
16-bit for `browser` and 24-bit only for `safari` and `tv`.

Two rules apply on top of the profile, and both are about what happens *after*
the decision rather than what the client can decode:

- **10-bit H.264 is never direct-played.** The codec name matches and browsers
  advertise H.264 support, but High 10 is outside every browser's baseline.
  Detected from `pix_fmt` where the probe reports one. HEVC, VP9 and AV1 carry
  10-bit fine and are not penalised.
- **A stream is only copied if MP4 can carry it.** Every non-direct path
  rewraps into fragmented MP4, and "the client decodes this codec" is not the
  same claim as "MP4 holds it". VP8, Vorbis, FLAC and Opus are re-encoded
  rather than copied even when the profile allows them — ffmpeg refuses to
  start on an impossible mux, which surfaces as a dead player with no reason.

### `GET /api/probe`

Background probing progress.

```json
{ "available": true, "running": false, "probed": 225,
  "failed": 0, "remaining": 0, "total": 225 }
```

`available` is false when ffprobe is not installed. That is a supported
configuration, not an error: playback decisions fall back to direct play.

### `POST /api/probe/refresh`

Queues already-probed items to be probed again, and starts a pass. Admin only.

```json
{ "scope": "incomplete", "queued": 412 }
```

Needed because a probe is only as good as the build that made it. The pending
queue is "never probed", so when the prober learns to record a field the
decision engine depends on, every item probed by an older build keeps a
decision made without it and nothing revisits them.

`?scope=` is `incomplete` (the default) or `all`:

- `incomplete` re-probes only items a current build would learn something
  from — today, video streams stored without `pix_fmt`. This is the narrow,
  cheap option and the one to reach for.
- `all` re-probes everything, optionally narrowed with `?library=`. Re-probing
  a large library is hours of ffprobe, which is why it has to be asked for by
  name and is never something the server decides to do on its own.

Stream rows are kept while an item is queued. Deleting them would widen the
window in which an item has no codec information at all and every playback
decision for it falls back to direct play.

Returns `503` if ffprobe is not installed and `400` for an unknown scope.

### `GET /api/activity`

What the server is doing right now, in one request.

```json
{
  "active": true,
  "tasks": [
    { "kind": "scan", "id": "scan:3", "title": "Scanning Films",
      "state": "running", "done": 812, "total": 0, "detail": "40 changed",
      "library_id": 3, "started_at": 1754630000 },
    { "kind": "enrich", "id": "enrich", "title": "Fetching metadata",
      "state": "running", "done": 120, "total": 400 }
  ]
}
```

The per-worker endpoints above each answer for one worker, which means a client
that wants to show "what is happening" has to know the whole list of workers and
poll each one — including `/api/libraries/{id}/scan` once per library. This
answers the question without that knowledge, in one shape:

- `kind` is `scan`, `enrich`, `probe`, `coverart`, or `transcode`. New workers
  add new values; a client that does not recognise one still has a title and a
  progress pair, which is the point of normalizing.
- `id` is stable for the task's lifetime, so a list can be keyed by it.
- `title` is resolved server-side — a scan names its library, because a client
  showing the row should not have to join an id back to a name.
- `state` is `running` or `failed`. Only those appear: a finished task is not
  activity. A failed scan stays listed, because a failure with nowhere to appear
  is the failure shape this project keeps being bitten by.
- `total` of `0` means indeterminate. A scan knows how many files it has seen
  and never how many it will see, so `done` is a count and there is no
  percentage to render.
- `library_id` is set for scans only.

Nothing here is persisted. A restarted server reports an idle one, which is the
truth: the workers are in-process and a restart ended their work.

Reading progress needs no special role. The endpoints that *start* work
(`POST /api/libraries/{id}/scan`, `POST /api/probe/refresh`) remain admin only.

### `GET /api/logs`

The tail of `lancastd.log`. **Admin only.**

```json
{ "lines": ["time=... level=INFO msg=listening addr=..."],
  "complete": false, "path": "lancastd.log" }
```

`?lines=` defaults to 300 and is clamped to 2000; a value that is not a positive
whole number is `400`. Lines are oldest first.

`complete` is `false` when older entries exist that this response does not
carry — the difference between "this is the log" and "this is the end of the
log". A client that assumes the first sends its reader looking for a startup
line that was never withheld from them.

Admin only because the log names filesystem paths, library roots and provider
errors: that is server-operator information, not viewer information.

A server that has never opened a log — one that has only ever run in a terminal,
where the log goes to the terminal — returns an empty `lines` array and
`complete: true`. That is a supported configuration, not an error.

This exists because the log has been written beside the database since v0.4.2
and could only be read by finding the data directory in a file manager, which is
the wrong ask for the case it serves: the log matters most when the server is
running as a service and something is wrong.

### `GET /api/audit`

The audit log: who changed what, and when. **Admin only** (ADR 0026).

```json
{
  "events": [
    { "id": 42, "at": 1786220000,
      "actor_id": "local", "actor_name": "chris",
      "action": "library.delete", "target_kind": "library", "target_id": "3",
      "summary": "Removed library \"Films\" (1226 items) — files left on disk",
      "detail": "{\"path\":\"D:\\Media\",\"kind\":\"movie\",\"item_count\":1226}" }
  ],
  "total": 1,
  "actions": ["library.delete"]
}
```

Newest first. `?limit=` defaults to 100 and is capped at 500; `?offset=` pages.
`?action=` and `?actor=` filter. A non-numeric or negative value for either
paging parameter is `400`.

`actions` lists the distinct actions actually present, so a client can build a
filter from what happened rather than from a hardcoded list that drifts from the
handlers.

**What is recorded** — deliberate acts only:

| Action | Recorded when |
|---|---|
| `library.create`, `library.delete` | A library is added or forgotten |
| `item.delete` | A title is removed, whether ignored or deleted from disk |
| `item.match` | An identity is overridden via Fix match |
| `item.edit`, `item.unlock` | Fields are edited (and locked), or a lock is released |
| `user.create`, `user.delete`, `user.password_reset` | Account changes |
| `auth.password_change` | Someone changes their own password |
| `plugin.install`, `plugin.grant`, `plugin.enable`, `plugin.disable`, `plugin.remove` | The trust decisions of ADR 0021 |
| `settings.update` | Settings change — **field names only, never values** |

**Reads are never recorded.** Browsing, playback and progress are the normal
operation of a media server; auditing them would bury the events that matter.
Scans are not recorded either — the activity view already reports them, and a
scan is not a decision anyone needs attributed.

**`summary` is resolved at write time**, so an event stays readable after the
row it names is gone. `actor_name` is frozen for the same reason: "who deleted
this library" must survive the deletion of the account that did it.

**A failed audit write does not fail the request.** The mutation has already
happened; refusing it after the fact would turn a full disk into a denial of the
user's own deletions. Failures are logged at `ERROR` server-side. This is a
stated trade, not an oversight.

### `GET /api/update` · `POST /api/update/check`

Whether a newer release exists. **Admin only.**

```json
{ "supported": true, "current": "0.6.1", "latest": "v0.7.0", "available": true,
  "url": "https://github.com/…/releases/tag/v0.7.0", "checked_at": 1786254604,
  "checking": false, "error": "", "download_error": "", "can_verify": false,
  "enabled": true }
```

### Playlists

A playlist is an ordinary item with `kind` `playlist`
([ADR 0030](adr/0030-playlists-and-m3u.md)). Its entries are **not** `parent_id`
children — a track belongs to its album, and being in a playlist does not move
it — so they are fetched with `?playlist_id=`, the way a collection's members
are fetched with `?collection_id=`.

**A playlist may contain the same item twice**, and this is the one listing in
the API that can return a duplicate id. A reprise, or a track that opens and
closes a set, is ordinary. Clients keying a list on item id will collapse those
into one row and silently shorten the playlist; key on position.

Entries come back in playing order, so no `sort` is needed or accepted here.

`child_count` is the **number of entries**, counting repeats — so a set that
opens and closes with the same song counts it twice, exactly as the entry
listing returns it twice.

It was 0 before v0.6.12, on the grounds that the field counts `parent_id`
children and a playlist has none. That was true about the implementation and
useless to a client, which reads the field as "how many things are in this" and
had no way to ask. Entries whose file is currently **missing** are counted: a
playlist does not shorten itself because a drive is unplugged.

Playlists found on disk as `.m3u` files are imported on scan, and the database
is the source of truth afterwards — editing one in LANcast locks its membership
so a later scan cannot undo the edit. `.m3u8` files containing `#EXT-X-` tags
are HLS playlists (LANcast writes them itself) and are never imported.

#### `POST /api/playlists`

Creates an empty playlist. `library_id` is required — every item belongs to a
library, and a server with films and music has no defensible default.

```json
{ "title": "Road Trip", "library_id": 2 }
```

Responds `200` with the new item, in the shape `GET /api/items/{id}` returns.

#### `DELETE /api/playlists/{id}`

Deletes the playlist and its entries. `204`.

Its own route rather than `DELETE /api/items/{id}`, which takes a `mode`
because it is about files: for an imported playlist, `mode=delete` would remove
the `.m3u` and `mode=ignore` would add it to the ignore list. This route touches
no file. The tracks are untouched — being in a playlist was never where they
lived.

#### `PUT /api/playlists/{id}/entries`

Replaces the membership with exactly this list, in this order.

```json
{ "item_ids": [12, 40, 12] }
```

`204`. Reorder, insert, and remove-several are all this call: a playlist is an
ordered sequence, and every one of those edits is the caller having decided the
whole sequence. Repeats are kept — sending the same id twice is a playlist that
holds a track twice, not an error.

`400` naming the id if any item does not exist, and nothing is written.

#### `POST /api/playlists/{id}/entries`

Appends to the end, in the order given. Same body, `204`. The one edit whose
position the caller does not have to decide.

#### `DELETE /api/playlists/{id}/entries/{position}`

Removes one entry. `204`, or `404` if there is no entry there.

**By position, not by item id.** An id does not identify an entry in the one
listing that may hold the same id twice. Positions are 0-based and stay dense,
so a position is the index the client rendered; after a removal, everything
below it has shifted up by one.

#### What playlist writes require

A session, and no particular role. The admin gate exists for filesystem access
and account control; a playlist edit is neither, and the audit log records who
made it. Playlists are server-wide ([ADR 0030](adr/0030-playlists-and-m3u.md)
leaves per-user ownership undecided), so any member may edit any playlist.

Every one of these writes **locks `members`** on the playlist, which is what
stops the next scan re-importing the `.m3u` over the edit. Renaming is the
ordinary `PATCH /api/items/{id}` with a `title`.

### `GET /api/items/{id}/trailer`

The trailer for an identified item, looked up through the provider that
identified it.

```json
{ "trailer": { "site": "YouTube", "key": "abc123", "name": "Official Trailer" } }
```

`trailer` is `null` when the item has no external id, the provider has no
trailer for it, or the lookup failed — all three are the same answer to a
client, which is "do not offer a trailer button". A failed lookup is **200 with
a null**, never an error: a provider being unreachable must not turn a detail
page into an error page over something optional.

### `GET /api/enrich`

A snapshot of the enrichment worker, for the activity display.

```json
{ "running": true, "enriched": 412, "failed": 3,
  "remaining": 88, "total": 500, "updated_at": 1754870400 }
```

Poll it while `running` is true. `failed` counts items the providers could not
identify, which is information rather than an error — an unmatched file is a
review-queue entry, not a fault.

### `POST /api/update/download`

Fetches the release the last check found and stages it, without applying it.
Admin only. The download is the slow half and the restart is the disruptive
half, so they are separate calls: a client can stage an update while people are
watching and restart when nobody is.

Returns the staged version. Calling it with no update available is a no-op
rather than an error — the check may simply have gone stale.

### `POST /api/update/restart`

Finishes a staged update by restarting the server. **Admin only.** `202` once the
restart has been set in motion.

Required because a staged update had nowhere to go. LANcast applies one on the
way down, and when it runs as a Windows service nothing ever takes it down — so
"it takes effect the next time the server starts" meant never, and the only
route through was an elevated `Stop-Service`, which applied the update and left
the machine with LANcast not running at all.

A service cannot restart itself, so the server spawns a detached helper — the
same binary, `service restart` — which stops the service, **waits for the stop
to complete**, and starts it again. Renaming a running executable is permitted
on Windows, which is what lets the helper keep executing while the swap replaces
the file it was started from.

**An install that is not a service restarts itself too** (v0.6.14). It used to
be told to close LANcast and open it again, which left the user with no way to
know whether the swap had happened short of starting the server and reading the
version — the application knew, and did not say. The same trick one level down:
a detached `lancastd relaunch <pid> [args…]` waits for this process to exit
(which is when the staged files are applied, on the way down) and starts it
again **with the arguments it had**, so a tray launch comes back as a tray and a
`-data`/`-addr` launch comes back on the same directory and port. The helper
waits on the process rather than on a timer, and gives up rather than starting a
second server over one that will not stop.

Two refusals, both `412`:

- `nothing_staged` — there is no update to finish. This endpoint is not a
  general "restart the server" button; that is a bigger and more dangerous
  control, and it would want its own thinking about sessions and playback in
  flight.
- `not_a_service` — only where the host cannot relaunch either, which is now the
  narrow case rather than the ordinary one. Killing a process nothing will bring
  back is not an answer, so the caller is told to close LANcast and open it
  again.

A client finishing an update should **poll `GET /api/health` until it answers
with the new version** rather than assuming. That is the confirmation the panel
shows, and the reason the endpoint's silence is not ambiguous: the server is
expected to stop answering and then answer again as something new.

**The request often gets no response**, because the process answering it is the
one going down. A dropped connection here means the restart began.

`error` is a failed *check*; `download_error` is a failed *download*. They are
separate because they ask different things of the reader — "I could not ask"
versus "I asked, and installing it failed" — and because a download is started
by a request that returns `202` and then runs detached. Its outcome reaches no
caller, so the state is the only place a client can learn it. Without that, a
download that died is indistinguishable from one still running.

`GET` reads the last known result; the server checks on its own timer, so this
costs nothing. `POST /api/update/check` asks now, and **works whether or not the
automatic check is enabled** — someone who does not want a timer may still want
to ask once, deliberately.

- `available` is true only when `latest` is genuinely newer. A `dev` build
  cannot compare itself and never reports an update.
- `can_verify` is whether this build can check a release signature at all. False
  means automatic installation is unavailable regardless of the setting
  ([ADR 0016 amendment](adr/0016-packaging-and-distribution.md)).
- `enabled` is the `update_check` setting, so a client can tell "nothing to
  report" from "not looking".
- `error` is the last failure and is cleared by a success. It is reported rather
  than swallowed: a check that has been failing for months is otherwise
  indistinguishable from one with nothing to say.

An available update also appears in `GET /api/activity` as a task with
`kind: "update"` and `state: "available"` — not work in progress, but something
the server is waiting for someone to act on.

**What is sent:** a plain GET to the project's releases endpoint. No install
identifier, no library statistics, no version history. This is consistent with
the no-phone-home principle rather than an exception to it — the principle is
that nothing is *required* to reach the internet for the server to work, and
this is neither required nor load-bearing.

### `GET /api/coverart`

Background album-art progress.

```json
{ "available": true, "running": false, "found": 341,
  "none": 57, "failed": 0, "remaining": 0, "total": 398 }
```

Album covers come off the disk rather than from a provider (ADR 0024): the
picture embedded in a track first, then a `cover.jpg` or `folder.jpg` beside
it. Embedded wins because it travels with the record — it was attached by
whoever tagged the files and cannot be about a different album, where a loose
image in a directory can be anything.

`found` and `none` are reported separately on purpose. **An album with no cover
has not failed**, and a status that merged the two would make a library of
untagged rips look broken. `failed` means something went wrong — an unreadable
file, an image the cache could not store.

`available` reports whether *embedded* extraction can run, which needs ffmpeg.
It being false does not mean no artwork: sidecar files are read regardless, so a
library that keeps `cover.jpg` beside the music is fully covered without ffmpeg
installed.

Covers are recorded as `poster` artwork, so an album tile renders through the
same path a film poster does and clients need no new artwork kind. The
`source_url` is empty, because there is no URL — the image came off the disk.

Only albums are searched. An artist row has no directory of its own and no file
to extract from; artist images, if they ever arrive, will come from a provider.

### `POST /api/coverart/refresh`

Queues albums to be looked at again, and starts a pass. Admin only.
Optionally narrowed with `?library=`.

```json
{ "queued": 398 }
```

The counterpart to `POST /api/probe/refresh`, and needed for the same reason.
The pending queue is "not yet looked at", and an album is stamped whether or not
anything was found — otherwise an artless album would be re-examined on every
pass forever and the queue would never drain. That stamp is also what makes an
album invisible to the queue afterwards, so someone who has just added
`cover.jpg` files to a library has no other way to ask LANcast to look again.

Returns `400` for an invalid library id and `404` if no such library exists.

### Music items

A `track` carries the same fields as any other item, with three read in the
music sense (ADR 0024):

| Field | On a track |
|---|---|
| `series` | The album |
| `season` | The disc number; `0` when the file carries no disc tag |
| `episode` | The track number |
| `artist` | The track's own performer, present only on music |

Fetch an album's tracks with `?parent_id=<album>&sort=track`, which orders by
disc and then track number. **The sort is not optional**: without it the listing
is ordered by title, so a record arrives alphabetically — see the note under
`sort` above for why episodes do not have this problem and tracks do. A
track from a release with no disc tag carries `season: 0`, which sorts ahead of
any numbered disc, so an album mixing tagged and untagged discs is still
well-defined.

An **album** row carries two fields of its own, derived from its tracks on every
scan rather than written once:

| Field | On an album |
|---|---|
| `artist` | The **album artist** — the name of the artist row above it |
| `year` | The earliest year among its tracks, absent when none carries one |

Both are derived because an album is created from a grouping key and knows only
its title at that moment. A client reading `artist` on an album gets the album
artist; reading it on a track gets that track's performer. The two differing is
the compilation case, and comparing them is how a client knows whether a track's
performer is worth showing. Locked fields are never overwritten, so a corrected
year survives a rescan.

`artist` is the track's, not the album's. A compilation has one album artist and
a different performer per track; the album artist groups the record, and this
says who actually played. Both come from the file's embedded tags, which for
music outrank the filename — the tagger wrote them, where a filename was guessed
by whoever ripped the disc.

Item responses gain `duration_ms`, `width`, `height`, `video_codec`,
`video_profile`, `video_bitrate`, `audio_codec`, `audio_channels`, and
`probed_at`. The detail response also carries `streams` — the full track list,
including subtitle and alternate audio tracks. Each stream's `index` is the
absolute index `?audio=` takes.

Streams carry `pix_fmt` where the probe reported one. It is the reliable signal
for bit depth, which is what decides whether an H.264 file direct-plays. Items
probed before this field existed have it empty and fall back to matching the
profile name; re-probing fills it in.

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
selects a specific track by absolute index; `?profile=` names the client
profile (see above).

`?audio=` participates in the delivery decision rather than only in stream
mapping. Selecting a track means the decision is made about *that* track — a
file whose default track is TrueHD and whose second track is AAC direct-plays
when you ask for the second, and re-encodes when you do not. An index naming a
track that does not exist returns `400 bad_request` rather than silently
playing a different one.

`Accept-Ranges: none` — a live transcode has no length and cannot be
range-served, since bytes do not exist until ffmpeg produces them. Seeking
forward restarts the stream from a new `?t=`.

Returns `409 conflict` for a file that can be played directly (transcoding it
would be wasted CPU), `503` if ffmpeg is not installed, and `429` past the
concurrent-transcode limit.

### `GET /api/stream/{id}/hls/index.m3u8`

The same transcode as an HLS playlist with fMP4 segments, for clients that
speak HLS. Takes the same `?t=`, `?audio=` and `?profile=` parameters. Segment
URLs point back at `GET /api/stream/{id}/hls/{session}/{name}`.

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

### `GET` / `PUT /api/settings`

TMDB key, OpenSubtitles key, OMDb key (external ratings, [ADR 0019](adr/0019-external-ratings.md)),
rate limit, and the per-library NFO write toggle.

**Every key is write-only.** `GET` returns each provider's configured flag only —
`{"tmdb": {"configured": true}, "omdb": {"configured": false}, …}` — never the
value itself. Keys are stored in the config file at `0600`, never in the
database. Setting `omdb_key` on `PUT` enables the rating pass; clearing it (an
empty string) turns external ratings off again, and without it the pass never
runs and nothing is fetched.

**Diagnostics.** `debug_logging` raises the server's log level to debug. It
takes effect on the next line logged — no restart — and is persisted, because
the faults worth turning it on for are the intermittent ones and losing the
toggle on restart is how somebody reproduces a bug three times.

### `POST /api/settings/reset`

Restores the documented defaults. **Admin only.** Returns the settings, like
`GET`.

**Credentials and machine facts survive**: the password hash, the provider API
keys, the TLS certificate paths, and the ffmpeg directory. Wiping the first
would lock the operator out of their own server and wiping the others would
break metadata and HTTPS — none of which is what anybody means by "reset
settings", and none of which a reset can restore. What resets is behaviour.

### `POST /api/cache/clear`

Throws away something the server can make again. **Admin only.**

```json
{ "target": "artwork" }
```

| Target | What goes | What it costs |
|---|---|---|
| `artwork` | every cached image, original and derived | time and provider requests; artwork is blank until it is fetched again. The rows referencing those hashes are **left alone**, so an item keeps knowing which artwork it has |
| `transcode` | scratch space, and every running session with it | a few seconds of buffered video per viewer, rebuilt on the next play |

Responds `{"freed_bytes": N}`. Any other target is a `400`: everything reachable
here is recoverable **by the server itself**, and that is the boundary — nothing
here touches media, the database, accounts, or anything a person typed.

**Server rules** (v0.6.13). These five decide what a client shows and what it
may do, and they live here rather than in each client because the server owns
truth: a household with a phone, a browser and a TV must not hold three answers
to "have I watched this".

| Field | Default | Range | What it does |
|---|---|---|---|
| `watched_threshold` | `90` | 50–100 | The percentage of an item's duration past which it counts as watched. Applied on **every** `PUT /api/items/{id}/progress`, so a client that never sends `watched` still gets correct state. An item with no known duration is never marked by it |
| `continue_weeks` | `16` | 0–520 | Items untouched for this many weeks leave `GET /api/continue`. **0 means never expire**, which is a different answer from "expire now" |
| `continue_limit` | `40` | 1–100 | How many items that shelf holds. A client may ask for fewer with `?limit=`; it **cannot ask for more** |
| `allow_media_deletion` | `true` | — | When false, `DELETE /api/items/{id}?mode=delete` is **403**. `mode=ignore` is unaffected: it writes no file and deletes nothing from disk |
| `scan_interval_hours` | `0` | 0–168 | Rescan every library on a timer. **0 is off**, the default. Takes effect without a restart; a library already scanning is skipped rather than queued |

Out-of-range values are **rejected with 400**, not clamped — a client sending
`200` has a bug, and silently storing `90` hides it. The config file is also
repaired on load, because it is hand-editable and a hand-edited `0` threshold
would mark everything watched the moment it started playing.

`watched` on a progress write is **OR-ed** with the threshold, never overridden
downward: a client that fired `ended` knows something the server cannot see, and
a client claiming "not watched" at 98% has an out-of-date idea of finished.

---

## Plugins

> M4. Trust model: [ADR 0021](adr/0021-plugin-distribution-and-trust.md).
> **Admin only.** Every endpoint requires an admin session.

A plugin is distributed as a signed `.lcplugin` bundle. Two trust layers apply
independently: **provenance** (the `signer` — `first_party`, `pinned`, or
`unsigned`) and **authority** (the capability *grant*, which the manifest can
only *request*). Install is deliberately **two steps** — upload to inspect, then
grant to activate — so the capability approval is an explicit act.

### `GET /api/plugins`

Installed plugins, each showing requested vs granted capabilities.

```json
{ "plugins": [ {
  "name": "omdb", "version": "0.1.0", "kind": "rating_source",
  "signer": "first_party", "enabled": true, "digest": "101e40cd…",
  "requested": { "http": ["www.omdbapi.com"], "secrets": ["omdb_key"] },
  "granted":   { "http": ["www.omdbapi.com"], "secrets": ["omdb_key"] },
  "installed_at": 1754064000 } ] }
```

### `POST /api/plugins`

Upload a `.lcplugin` (raw bytes, up to 32 MiB). The bundle is **verified before
anything is compiled**; a tampered or unknown-key bundle is `400`. On success the
plugin is **staged disabled with an empty grant**, and the response reports what
it *requests* so the client can present the approval dialog.

```json
{ "name": "omdb", "version": "0.1.0", "kind": "rating_source",
  "signer": "unsigned", "enabled": false, "digest": "101e40cd…",
  "requested": { "http": ["www.omdbapi.com"], "secrets": ["omdb_key"] },
  "granted":   { "http": [], "secrets": [] } }
```

### `POST /api/plugins/{name}/grant`

Approve capabilities and activate. The grant **must be a subset of what the
manifest requests** (`400` otherwise) — the API cannot hand a plugin more than it
asked for. The recorded grant, not the manifest, is the effective authority, and
it takes effect immediately (the registry reloads).

```json
{ "http": ["www.omdbapi.com"], "secrets": ["omdb_key"] }
```

### `POST /api/plugins/{name}/enable` · `POST /api/plugins/{name}/disable`

Flip a plugin on or off. `204`; the registry reloads. `404` if unknown.

### `DELETE /api/plugins/{name}`

Forget a plugin and delete its unpacked files. `204`, `404` if unknown.

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

> **Not implemented.** This endpoint is specified and has no handler: theme
> music is blocked on OST identification ([ADR 0005](adr/0005-theme-music-sourcing.md)),
> so the field it depends on is never populated. It is documented here because
> the shape is decided, and a client must not build against it until this note
> goes away. Requesting it today is a 404 from the router, not the `not_found`
> described below.

Streams the resolved theme audio. Returns `not_found` when
`theme.available` is false — clients must treat that as silence, never as an
error to display.
