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

### Stating and checking a version

`X-LANcast-API-Version` works in both directions.

**On every `/api` response** the server states the contract it served, so a
client can log or assert it without a second call to `/health`.

**On a request it is an optional assertion.** Send it to say which contract the
client was built against. If this server cannot serve that version the request
is refused immediately:

```json
{ "error": { "code": "unsupported_api_version",
             "message": "this server speaks API version 1; the request asked for 2" } }
```

`400`, not `406`: the request is malformed with respect to this server, and a
client built for v2 cannot fix it by renegotiating. Switch on the `code`.

Omitting the header means "whatever you have", which is what every existing
client sends and must keep working - this is an opt-in assertion, not a
requirement. A header that is not a whole number is a `400 bad_request` rather
than being ignored: silently serving a malformed assertion hides the fault at
the exact moment somebody is looking for it.

The refusal is the point. Without it, a client expecting a contract this server
does not speak discovers the mismatch as a field that is mysteriously absent
three screens later, and the report that arrives is "the library page is blank".

`GET /api/health` also reports `api_versions`, every revision this build can
serve - a list, because a future v2 server keeps answering v1 for at least one
release, so "which versions do you speak" is a different question from "which
one am I getting".

**This is deliberately not a URL-space rewrite.** Moving every route under a version prefix would break the existing client today to buy a property nobody is
using yet, and ADR 0018 already promises `/api` never changes meaning - the same
guarantee at no cost.

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
| `GET /api/auth/status` | `{configured, authenticated, lan_enabled, restart_required, can_convert, user?}`; `user` carries `sharing` |
| `POST /api/auth/setup` | `{username, password, install_media_tools?}` → creates the first admin; only while unconfigured. Answers `media_tools_installing` |
| `POST /api/auth/login` | `{username, password}` → session cookie. Throttled per IP |

**`can_convert` says whether ffmpeg is present**, and it is reported to every
caller rather than only to administrators
([ADR 0048](adr/0048-media-tools-install-themselves-on-first-run.md)).

The install button is admin-only, correctly. The *fact* is not a secret, and the
person most affected is a member who cannot open Settings at all. A client must
use this to explain the failure **before** attempting playback: a `<video>`
element handed a refused request reports a bare error with no status, so a
client that waits to be told cannot be told, and the viewer sees a black
rectangle instead of a reason.

A client older than the server, or a server older than the client, may not see
this field. **Absent must read as capable** — assuming otherwise puts a warning
in front of somebody whose playback works.

**`install_media_tools` is the one place the server may fetch anything on its
own initiative, and it is why the field is optional rather than a boolean with a
default** ([ADR 0048](adr/0048-media-tools-install-themselves-on-first-run.md)).

Sending `true` starts a one-off download of ffmpeg — around 160MB, pinned URL,
checksum verified — in the background. The response reports
`media_tools_installing`, and progress is polled from `GET /api/media-tools`.

**Absent means no.** Absent and `false` are deliberately different: absent means
the caller never saw the question, which is true of an older client and of any
script. Only a client that displayed what would be downloaded, from where, how
large and under which licence may send `true` — that disclosure *is* the
consent, and it is what stands in for the admin gate that cannot exist before
any account does.

Setup does not wait for the download, and a fetch that cannot start never fails
the request: the account is created either way, and the server is fully usable
without ffmpeg — it simply cannot convert. Nothing is fetched when ffmpeg is
already present.
| `POST /api/auth/logout` | Ends this session |
| `POST /api/auth/password` | `{current_password, new_password}`; changes **your own** password and revokes **your** sessions |

When a session is active, `status`, `setup`, and `login` include
`user: {id, name, role}`. A wrong username and a wrong password are reported
identically as `401 unauthorized`.

`GET /api/auth/status` additionally carries **`user.sharing`** and
**`user.visible_to_peers`** — this account's own ADR 0035 activity-sharing
choice, and whether it appears in the roster handed to paired servers. It is here because there is nowhere else
it could be: `GET /api/people` excludes the caller by design, so a client had no
way to read back a setting it could write. It reports only the caller's own
value, and is absent (rather than false) if the server could not read it.

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

### Server identity

| Route | Purpose |
|---|---|
| `GET /api/identity` | `{fingerprint, fingerprint_display, name}` — who this server is |

This server's own cryptographic identity
([ADR 0044](adr/0044-server-identity-and-peering.md)): an Ed25519 keypair
generated on first run and stable for the life of the data directory. The
private half never leaves the server — not through this route, a log line, or a
crash report.

`fingerprint` is canonical: SHA-256 over the raw public key, base32, uppercase
and unpadded — **52 characters**, never truncated. It is what any comparison is
made against.

`fingerprint_display` is the same value with a separator every four characters,
so a client shows the readable form without inventing its own grouping. Two
clients disagreeing about where the separators go is how two screens end up
disagreeing about whether a fingerprint matched. Never parse the display form;
strip separators, spaces, colons and case to recover the canonical one.

`name` is what a peer would see this server called. Today it is the machine's
hostname.

**This route reports an identity and grants nothing.** There is no peer, no
pairing and no access decision behind it. It is session-gated because the
fingerprint travels to the people who need it *out of band*, in an invite — a
route anybody could read would be a directory of one, which is exactly what
ADR 0044 declines to build.

### Peers

| Route | Purpose |
|---|---|
| `GET /api/peers` | `{peers: [...]}` — the servers this one has been introduced to. **Admin** |
| `POST /api/peers` | `{invite}` → adds a peer from a pasted invite. **Admin** |
| `GET /api/peers/invite` | `{invite, fingerprint, fingerprint_display, name, addrs}` — this server's own invite, to hand out. **Admin** |
| `DELETE /api/peers/{fingerprint}` | Un-pairs. **Admin** |
| `PUT /api/profile/peer-visibility` | `{visible}` — whether **your** account appears in the roster handed to peers |

Pairing is administrative and granting is not, which is why the first four are
admin-gated and the last is not. Adding a peer opens a network relationship for
the whole server — the same class of operational power as adding a library, so
it is gated on the server rather than hidden in the client
([ADR 0015](adr/0015-multi-user-accounts.md)). Choosing to appear in a roster is
one account's own decision about itself, and there is deliberately no
admin-facing version of it: a switch somebody else can flip is not consent.

A peer carries `fingerprint` and `fingerprint_display` for the same reason
`/api/identity` does — one is compared, the other is read. `state` is `added`
until the far side is confirmed to hold us too, and only the transport can move
it to `paired`: **accepting an invite is not a pairing**
([ADR 0044](adr/0044-server-identity-and-peering.md) §3, introduction is
mutual). `last_seen` is 0 for a peer that has never answered, which is a
different statement from one that answered three days ago.

`DELETE` accepts the fingerprint in either form. It is the revocation
mechanism: the peer's addresses and roster go with it through the schema's
cascade, and in later phases so does every grant naming one of its people
([ADR 0046](adr/0046-remote-guests.md)).

`POST /api/peers` refuses this server's own invite with `self`, and refuses a
damaged one with `bad_invite` and a message written for the person holding the
paste. `GET /api/peers/invite` answers `409 not_reachable` on a server with no
address another machine could reach — it cannot introduce itself.

**A pairing permits nothing.** It records that two servers know who each other
are, and every later capability is granted separately.

### Presence

| Route | Purpose |
|---|---|
| `GET /api/people/peers` | `{peers: [...]}` — paired servers, the people on them, whether **you** have granted each of them presence, and what they are doing |
| `PUT /api/people/peers/{fingerprint}/{person}/presence` | `{on}` — grant or revoke *your* presence to one named person |
| `DELETE /api/presence` | Playback stopped; drop the caller's live presence now |
| `GET /api/federation/presence?person={id}` | **Peer-to-peer.** Answers what that peer's person may see. Not a session route — see below |
| `GET /api/federation/roster` | **Peer-to-peer.** The accounts here that have opted into being listed |

Presence is a **third disclosure category** and no existing opt-in widens into
it ([ADR 0045](adr/0045-live-presence-between-paired-servers.md) §1): agreeing
to publish what you have finished is not agreeing to be watched in real time.
It is off by default, and there is no migration in which anybody starts being
visible.

A grant **names a person**, never a server, so the route carries both a
fingerprint and a person id. Granting is self-service and reads the caller's id
from their session: there is no route that accepts a subject, and therefore no
way for an administrator to grant presence on somebody's behalf (§6). The
person must already be in that peer's roster, which is itself a per-account
opt-in — an account that has not opted in cannot be named by anybody's grant,
in either direction, and the schema enforces it rather than a handler.

Each person in `GET /api/people/peers` carries `granted`, and then either
`shares: false`, or `shares: true` with `online` and `watching`. Those are three
different statements — *has not shared with you*, *offline*, and *online and
idle* — and a client must not collapse them, for the reason the People page
already states about `Not sharing`: a choice and an absence are not the same
thing.

`watching` is **the work, by title, or empty**. Never an episode
("Cowboy Bebop", not "Cowboy Bebop S01E02"), never music or photographs, and
never a position — §3 bounds the disclosure exhaustively and the payload carries
nothing else. Presence is **never persisted**: there is no history, no "last
seen watching", and no route that could answer either. Revocation takes effect
on the next poll, mid-film.

`GET /api/federation/presence` is the only route in this contract not
authenticated by a session. Its caller is a server, and it is authenticated by
the **mutual-TLS pin** ([ADR 0044](adr/0044-server-identity-and-peering.md) §4):
the connection must present the identity key already recorded for that peer, and
a request arriving without a peer certificate is refused. Which *person* is
asking is the calling server's word, on the same basis a pairing already rests
on. Peer connections are told apart from browsers by an ALPN marker in the
ClientHello, so they share the ordinary port and no browser is ever asked for a
certificate.

Fetching a peer's roster is also what establishes that a pairing is **mutual**:
this server only reaches that handler for a fingerprint the far side already
holds, so a successful call proves both sides hold each other, and it is what
moves a peer from `added` to `paired`
([ADR 0044](adr/0044-server-identity-and-peering.md) §3). Until it succeeds a
peer stays `added` — accepting an invite is not a pairing. A roster is stored
wholesale, so somebody who turns their visibility off disappears from it on the
next refresh and every grant naming them cascades away.

### Roles

Every account is `admin` or `member` ([ADR 0015](adr/0015-multi-user-accounts.md)).

- **admin** — everything, including the management surfaces below.
- **member** — browse, play, and their own watch state. A member calling an
  admin-only endpoint gets `403 forbidden`.

Admin-only endpoints: `GET /api/browse`; `POST` and `DELETE` on `/api/libraries`, library
`scan`, `refresh` and `reparse`; item metadata mutation (`PATCH /api/items/{id}`, lock
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
    "roots": [
      { "id": 1, "library_id": 1, "path": "D:/Media/Films",
        "created_at": 1753142400, "item_count": 380 },
      { "id": 4, "library_id": 1, "path": "E:/Family",
        "created_at": 1755100000, "item_count": 32 }
    ],
    "created_at": 1753142400, "scanned_at": 1753228800, "item_count": 412 }
]
```

**A library can live in more than one place** ([ADR 0034](adr/0034-multi-root-libraries.md)).
`roots` is the full list; `path` is the **first** of them, kept so clients that
predate multi-root libraries keep working, and superseded by `roots` for
anything new. On a single-location library the two say the same thing, which is
every library that existed before this.

`item_count` on a root is what would be removed with it. `item_count` on the
library is the whole library, and the two are not comparable: the library's
excludes containers and honours the browse rules, a root's is a plain row count.

`media_count` is the number of **files** in the library — songs, photos, films,
episodes — as against `item_count`, which is tiles. The two differ wherever a
library groups its media: a music library of 1,171 artists holds tens of
thousands of songs, and a picture library of 67 galleries holds thousands of
photographs. Both are true and they answer different questions, which is why
both are reported rather than one being redefined; our own client shows
`media_count` for music and picture libraries and `item_count` for the rest,
because a library's unit is the thing it is *of*.

**The library's `item_count` is what the browse grid shows**, and that is a
promise rather than a description. It counts top-level, present rows and
excludes the kinds that *group* items rather than being them — `collection` and
`playlist`, the same set the grid passes as `exclude_kind`. When it did not, a
library's sidebar read 1,381 beside a grid that said 1,211, the difference being
exactly its 170 collections; the music sidebar read 1,177 against a grid of
1,171, exactly its 6 imported `.m3u` playlists. A client showing both numbers
should be able to show them side by side without explaining the gap.

### `POST /api/libraries`

`kind` is one of `movie`, `show`, `music`, `picture`, `other`. Every path must
exist and be a directory; all are validated **before** anything is inserted, so
a typo in the third location does not leave a half-made library behind.

```json
{ "name": "Films", "kind": "movie", "path": "D:/Media/Films" }
```

```json
{ "name": "Films", "kind": "movie", "roots": ["D:/Media/Films", "E:/Family"] }
```

`path` and `roots` mean the same thing; `path` is a single-element `roots`. Both
together is accepted rather than refused, with `path` first, so a client
migrating between the two forms produces the same library either way. Additive
under [ADR 0018](adr/0018-api-contract-and-versioning.md).

Returns `201` with the created library. Returns `conflict` if any path is
already registered, or overlaps a location that is — see the overlap rule below.

**Creating a library starts a scan of it.** The `201` is written first and the
scan runs in the background, so poll `GET /api/libraries/{id}/scan` for its
progress exactly as for one started by hand. A library that could not begin
scanning is still created and still returns `201` — the failure is logged, not
returned, because turning a successful create into an error would leave the
caller believing nothing happened while a library sits on disk.

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

**Changing `path` moves the library's *first* location.** With several, the
per-location endpoints below are the precise form.

---

## Library locations

Where a library's files live. **Admin only**, except the listing.

Adding a location is filesystem access at a path the caller chooses — the same
capability `POST /api/libraries` has — so it carries the same gate. Listing is
not gated, because those paths are already in `GET /api/libraries`, which any
signed-in user can read.

### `GET /api/libraries/{id}/roots`

```json
[ { "id": 1, "library_id": 1, "path": "D:/Media/Films",
    "created_at": 1753142400, "item_count": 380 } ]
```

Oldest first, so the first entry is the one `library.path` reports.

### `POST /api/libraries/{id}/roots`

```json
{ "path": "E:/Family" }
```

Returns `201` with the created location.

**Locations may not overlap.** A path that is the same directory as an existing
location, or contains one, or sits inside one — in *any* library — is `409`.
Nesting has no good answer at scan time: files under the inner location are
walked by both passes, `media_item.path` is unique so the second write fights
the first, and the item's recorded location ends up decided by scan ordering,
which is what every containment check resolves against. Compared by path
component rather than string prefix, so `/mnt/films` and `/mnt/films2` are
unrelated; case-insensitively on Windows, where `D:\Media` and `d:\media` are
one directory.

### `PATCH /api/libraries/{id}/roots/{rootID}`

```json
{ "path": "F:/Family" }
```

Moves one location, carrying its contents with it — the drive-letter case, per
location. Every item path under the old location is rewritten in a single
transaction along with the ignore list, nothing is deleted and nothing is marked
missing, and a rescan afterwards reconciles as it always does. A path that does
not exist is refused **before** anything is rewritten. Overlap is refused as
above.

A location belonging to a different library is `404`, not `403`: the caller has
no business knowing it exists.

### `DELETE /api/libraries/{id}/roots/{rootID}`

Removes a location **and every item scanned under it**. `204` on success.

This deletes where an unreachable drive marks missing, and the difference is
deliberate. "Scanning marks missing, never deletes" governs what the server may
*infer* — a scan deduces absence from not finding a file, and that deduction is
wrong when a drive is merely unplugged. This is not a deduction; it is an
administrator saying the location is no longer part of the library, which is the
same class of act as deleting the library.

**The last location cannot be removed** (`409`). A library with none cannot be
scanned, resolved or repointed; delete the library instead. Use the `item_count`
on the location to say what goes before asking.

### `DELETE /api/libraries/{id}`

Forgets a library: its rows and, by `ON DELETE CASCADE`, its items, playback
state, and subtitles. **Never deletes media from disk** — LANcast only ever
stored paths, so this is "stop tracking this folder", not a destroy. `204` on
success, `404` if unknown, `409` if a scan is running for it.

### `POST /api/libraries/scan`

Starts a scan of **every** library. Admin only. Always `202`.

```json
{ "started": [ { "library_id": 1, "state": "running", "started_at": 1753228800 } ],
  "busy": [ 3 ] }
```

Libraries already scanning are listed in `busy` rather than refused, and the
rest still start — asking for everything while two of five are mid-scan should
start the other three, not fail because the request could not be carried out in
full. This is also what the rescan timer does, which skips a busy library and
never queues behind it.

Never `409`, unlike the single-library form below: there the caller named one
library and a conflict is the whole answer, where here the body says which
libraries did what.

A library is also scanned automatically when it is created, so this is for
"check everything for new media" rather than for setup.

### `POST /api/libraries/{id}/scan`

Starts an asynchronous scan and returns `202` immediately.

```json
{ "library_id": 1, "state": "running", "started_at": 1753228800 }
```

If a scan is already running for that library the status is `409` and the body
is **the running scan's progress, in the same shape as the `202`** — not the
`{ "error": … }` envelope every other failure uses. That is deliberate and worth
stating plainly, because a client parsing it as an error finds no code and no
message: the useful answer to "start a scan" when one is already going is *how
far that one has got*, and this endpoint gives it. Branch on the status, not on
the body. Scans are never queued.

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
  "skipped_kind": 0, "skipped_extras": 14,
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

`skipped_extras` counts trailers, featurettes, deleted scenes and sample files
left out of a video library ([ADR 0038](adr/0038-extras-are-not-works.md)). Also
not part of `skipped`, and for the same reason: nothing failed, those files are
simply not works. It is reported because it is the *entire* explanation for a
count that disagrees with another server's — a library holding 1,192 films and
189 extras used to report 1,381, with nothing anywhere to say where the
difference came from.

The rule is conventional (the Plex and Kodi layout) with one condition worth
knowing: a folder named `Trailers` or `Shorts` sitting **directly** inside a
library root is a category somebody keeps on purpose, not a film's extras, and
is imported normally. An extras folder must have a film folder above it.

An extra that an older build already imported is **marked missing** on the next
scan rather than deleted — scanning marks missing, never deletes — so the counts
correct themselves while the rows stay recoverable.

`episodes_in_movie_library` is the other half of that warning, and the half that
is easy to miss. A music library created as a movie library reports an empty
library, which is loud. A **shows** library created as a movie library imports
everything and looks fine: every episode becomes a film, loose in the grid with
no series and no seasons, and nothing says why. Counted, never corrected — the
parse is right and the *library* is wrong, and a scan that changed the kind
would be re-litigating identity for a whole library, which the locked-fields
rule forbids. Kind cannot be changed afterwards, so being loud at the moment it
happens is the only defence there is.

`shape_warning` is the **verdict** those counts feed, present only when a
finished scan produced something that does not look like the kind it was
scanned as:

```json
{ "shape_warning": {
    "code": "episodes_in_movie_library",
    "message": "This library was created for films, but 12 of 16 files are named like TV episodes...",
    "remedy": "If this is a TV library, remove it and add it again as TV Shows..." } }
```

Three codes today — `no_shows_in_show_library`, `episodes_in_movie_library`,
`everything_skipped_for_kind` — and the set is open, so a client renders its own
wording for a code it knows and falls back to `message` for one it does not.

`remedy` is separate from `message` because it is the part that is hard to hear:
kind is immutable, so the only fix is to remove the library and add it again.
Saying that plainly beats implying a settings toggle exists.

It is a verdict rather than a measurement because "1 movie, 3 parts, 0 shows" is
not something a person should have to interpret at the end of a scan. It is
computed from a census of what the library actually *holds*, which is the only
thing that can see the show-versus-movie case: nothing is skipped there, so no
skip count can ever fire.

**Only on a successful scan.** A failed scan produced a partial library by
definition, and reporting "your TV library has no shows in it" because a drive
went away halfway through would be a false alarm about a permanent mistake.

**Stored on the library row** (schema 20) as well as reported in live progress,
and also returned by `GET /api/libraries`. Progress is in memory and dies with
the process, which gave a warning about an unchangeable property a lifetime of
"until the server restarts" — a library scanned on Tuesday looked fine on
Wednesday. The next clean scan clears it, because a warning that outlives its
condition is worse than none.

Thresholds are deliberately forgiving — a shows library with one show in it is
doing its job, a film library with three episode-shaped names is a box set, and
a library under five items is not judged at all. A check that cries wolf is a
check that gets ignored, which is worse than no check.

### `GET /api/libraries/{id}/trending`

What this library's people have been playing in the last thirty days.

```json
{ "items": [ { "item": { "id": 87, "title": "Arrival", ... },
               "viewers": 3, "finishers": 1, "last_at": 1755200000 } ],
  "contributors": 3, "window_days": 30 }
```

`?limit=` defaults to 12 (max 50). Ranked by `viewers` descending, then by most
recent activity — the tie-break is not decoration: without it a page of items
that all have one viewer returns in whatever order SQLite chooses, and the shelf
reshuffles itself on every refresh.

`viewers` counts **accounts, not plays.** `playback_state` holds one row per
item per user, so this is how many people have played something recently rather
than how many times it has been played.

`contributors` is why that is safe to expose. With one account every count is 1
and the list is honestly "recently played", not a trend — so the client is given
what it needs to say the true thing instead of being handed a list that calls
itself trending regardless. A number meaning different things at different
scales carries its scale with it.

`finishers` is reported beside `viewers` because a title many people start and
nobody finishes is a different fact from one everybody finished, and a single
popularity number destroys the difference.

Containers — shows, seasons, artists, albums, galleries, playlists — are
excluded. A season is not a thing anybody played; it is where the episodes live.

**Not admin-gated, and it names no accounts.** Which titles are popular is a
fact about a shared library; who watched them is a fact about a person, and this
endpoint deliberately cannot answer the second.

### `GET` / `PUT` / `DELETE /api/items/{id}/rating`

**Your** rating of an item, and an optional note about why.

```json
{ "rating": { "item_id": 87, "score": 8,
              "review": "better than I remembered", "updated_at": 1755200000 } }
```

`PUT` takes `{ "score": 1-10, "review": "..." }`; a score outside that range is
`400`. `DELETE` withdraws it, which is **not** the same as scoring something 1 -
"I have not rated this" and "I rated this badly" are different statements, and
an interface that cannot say the first is one people stop trusting with the
second. `GET` answers `{ "rating": null }` when you have not rated it, rather
than `404`: the item exists and your verdict does not.

**A rating is private to the account that wrote it.** There is no household
average, no count of how many people rated something, and no route that returns
somebody else's score. The roadmap holds ratings back alongside viewer stats
because both wait on a decision about who may see whose viewing; this makes the
smaller half of that decision and leaves the rest unmade. Turning private
verdicts into visible ones changes what people are willing to write, so it is a
decision about the product rather than a flag to flip.

That is also why these routes carry no user id: whose rating is always the
caller's, which makes it impossible to leak one by forgetting a filter.

Distinct from `rating` on the item (TMDB's opinion) and from the external
ratings of [ADR 0019](adr/0019-external-ratings.md) (IMDb's and Rotten
Tomatoes'). Three numbers about one film is one too many to leave unlabelled, so
they are never merged into a single field.

Scores are out of **ten**, not five: a half-star interface then needs no
migration, and the provider ratings this sits beside are already out of ten.

### `GET /api/profile/ratings`

Everything you have rated, most recent first. `?limit=` defaults to 50 (max
200). Same privacy rule as above - it is your list, and there is no route to
anybody else's.

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

The filter values a library's browse view offers — only values actually present,
so a chosen filter never yields an empty grid. Genres and content ratings are
sorted; decades, years and resolutions are widest/newest-first.

```json
{ "genres": ["Comedy", "Drama", "Science Fiction"], "decades": [2010, 1990],
  "content_ratings": ["PG", "PG-13", "R"], "has_watched": true,
  "collections": [{ "id": 7, "name": "A Franchise", "members": 4 }],
  "max_rating": 8.4,
  "years": [2019, 2003, 1994],
  "resolutions": [{ "key": "uhd", "label": "4K", "min_width": 3000, "max_width": 0 },
                  { "key": "hd1080", "label": "1080p", "min_width": 1700, "max_width": 2999 }],
  "has_in_progress": true, "has_unmatched": false }
```

`years` is offered **alongside** `decades`, not instead of it: a decade is how
you browse and a year is how you find. A library spanning a century has too many
years for a row of chips and exactly the right number for a searchable list.

`resolutions` are **buckets over the probed width**, not a stored field —
nothing in the database says "4K". Bucketed on width because height is what
varies: a 2.39:1 film at 4K is 3840×1608 and a 16:9 one is 3840×2160, heights
550px apart, and a height rule files every scope film a tier too low. The
boundaries sit below the nominal widths for the same reason — real 1080p is
often 1912 after cropping. A file with no width has **not been probed** and is
absent from every tier rather than counted as SD.

`collections` lists the library's collections most-populated first, with member
counts — the number that separates a franchise from a two-film pairing when
picking one from a list.

`max_rating` is the highest rating present, so a client offers only thresholds
that can match: a library topping out at 8.4 has no business showing a 9+ filter
guaranteed to return nothing.

`has_in_progress` and `has_unmatched` follow the `has_watched` rule: a status
toggle is offered only when it has something to remove.

### `GET /api/libraries/{id}/cast`

The people credited in one library, for the Cast filter's type-ahead. `q` is a
prefix-or-word match, so `vance` finds Ada Vance and `ada v` finds her too;
`limit` defaults to 50 and caps at 200.

```json
{ "people": [{ "id": 12, "name": "Ada Vance", "role": "actor", "items": 9 }] }
```

`role` scopes the search to one side of the camera (`actor`, `director`).
Unvalidated on purpose: an unknown role matches nobody and returns an empty
list, which is the truthful answer — rejecting it would turn a filter nobody can
satisfy into an error page.

`id` is repeatable and resolves specific people **instead of** searching, which
is what lets a filter pill render a name. Filter state lives in the URL, so a
bookmarked `?person=12` arrives with an id and nothing else, and a pill reading
"person 12" is not a filter anybody can read. Answers in the order asked for, so
pills do not reorder between reloads; an id with no row is skipped rather than
returned blank.

A search endpoint rather than another array on `/facets`, because the two differ
by three orders of magnitude: a library has a dozen genres and thousands of
credited people, and shipping all of them on every browse load would be a
megabyte of JSON populating a control most visits never open. Ordered by how
much of the library each person is in, then by name — a total order, so a list
re-fetched as you type cannot appear to shuffle itself.

`has_watched` is true when the calling user has finished at least one top-level
item in the library, so the client offers the unwatched-only toggle only when it
would actually remove something rather than being a silent no-op. `genres`,
`decades`, and `content_ratings` are always present, empty when nothing applies.

---

## Items


**`{id}` may be `0`, which searches every library.** "Everything this person is
in" does not stop at the boundary between films and television, and those are
separate libraries — so with every route requiring one id, the question people
actually have could not be asked. A library id of 0 cannot exist, so it is free
to mean all of them and no existing caller changes.

Name matching is a **substring**: "niro" finds Robert De Niro. It was
prefix-or-word, which found only the start of a first name or a surname.
LIKE's own wildcards are escaped, so a typed `%` searches for a percent sign
rather than matching everybody.
### `GET /api/items`

| Parameter | Meaning |
|---|---|
| `library_id` | Restrict to one library |
| `q` | Free text over title and series. **`library_id` is optional here** — omitting it searches every library, which is what the client's global search does. A search that made you name the library first would ask you to know where a thing is before looking for it |
| `initial` | The A–Z rail: one letter, or `#` for titles starting with anything that is not a Latin letter. Matches on `sort_title`, case-insensitively. A **filter, not a scroll offset** — the grid pages in as you scroll, so "jump to S" cannot mean "scroll to a row that has not loaded". `GET /api/libraries/{id}/facets` returns `initials`, the letters actually present, so a client never offers one that finds nothing |
| `exclude_kind` | Drops kinds from the listing, **comma-separated**. The browse grid passes `collection,playlist`: both group items rather than being them, and a tile beside its own members made a curated shelf read as an unsorted one. Each has its own page (`kind=collection`, `kind=playlist`). A single value still means what it always did — the parameter grew a list without changing the old contract. It was one kind for a while, and the second had nowhere to go: every `.m3u` a scene release ships stood in the *artist* grid beside the artists whose tracks were on it |
| `kind` | `movie`, `episode`, `show`, `season`, `serial`, `part`, `chapter`, `collection`, `artist`, `album`, `track`, `gallery`, `photo`, `playlist`, `other`. **An open set** ([ADR 0018](adr/0018-api-contract-and-versioning.md)) — new kinds arrive without a major version, so a client with an exhaustive switch is relying on a guarantee it does not have |
| `parent_id` | Return the children of one item — a show's episodes, a work's parts |
| `collection_id` | Return a collection's members (many-to-many; not `parent_id`) |
| `playlist_id` | Return a playlist's entries **in playing order** ([ADR 0030](adr/0030-playlists-and-m3u.md)). The only listing that may repeat an item id — see below |
| `q` | Case-insensitive substring match on title and series |
| `genre` | Restrict to items carrying this exact genre name. **Repeatable** — `genre=A&genre=B` matches either |
| `decade` | Restrict to a decade — `1990` means 1990–1999. **Repeatable**; a non-numeric value is `400` |
| `content_rating` | Restrict to this exact content rating (PG, R, TV-MA…). **Repeatable** |
| `year` | Restrict to this exact release year. **Repeatable**; a non-numeric value is `400` |
| `resolution` | Restrict to a resolution tier — `uhd`, `hd1080`, `hd720`, `sd`. **Repeatable**. An **unrecognised key is ignored rather than rejected**: these arrive from bookmarked query strings, and a renamed tier should widen the grid back rather than break the page |
| `person` | Restrict to items this person is credited on, **in any role**. **Repeatable**; ids come from `/cast`, and a non-numeric value is `400` — an id is machine-generated, so a malformed one means the caller is confused, and widening to the whole library would look like the person matched everything |
| `actor` / `director` | The same filter scoped to one credit role. **Repeatable**. "Who is in this" and "who made this" are different questions, and `person` answers both without saying which was meant — somebody looking for what Eastwood *directed* does not want what he only acted in. A person who does both matches under both, once in each |
| `status` | `in_progress` (started, not finished) or `unmatched` (no provider claimed it). **Single-valued**, because the two cannot usefully be combined |
| `collection` | Restrict to members of a collection. **Repeatable**. Reads the membership table, not `parent_id` — a film belongs to a franchise without being inside it ([ADR 0017](adr/0017-collections-and-multi-part-works.md)) |
| `min_rating` | Rated at least this highly, out of ten. **Unrated items are excluded, not sunk**: a film with no rating is not a film rated zero, and sweeping them to the bottom would quietly hide the unmatched half of a library behind a control that says nothing about matching. An unparseable value widens rather than `400`s |
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
a collection). A collection is listed only when it groups **at least two present
members**: a provider supplies a franchise even when the library holds a single
film from it, and a collection of one is just a duplicate tile of that film with
a "Play all" button.

That rule applies to **every listing**, not only the top-level grid. It was once
a property of the grid, which meant `?kind=collection` — the collections page,
and not a top-level query — was the one listing that showed the singletons
everything else refused: a Hitman Collection containing Hitman, an Aquaman
Collection containing Aquaman, a hundred more. Fetching a collection's own
members with `?collection_id=` is unaffected; the rule hides a collection from
listings *of* collections, never what is inside one.

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
  "progress": { "position_ms": 1284000, "watched": false, "watch_count": 2 } } ] }
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

### `GET /api/profile`

Who the caller is, what they have watched, and the totals behind it — in one
request, because a page needing identity, statistics and a list should not
discover that from three round trips and three loading states.

```json
{ "user": { "id": "u_3f9", "name": "Chris", "admin": true, "secured": true },
  "stats": { "started": 214, "finished": 168,
             "watched_ms": 913_400_000, "first_at": 1739000000 },
  "history": [ { "item": { "id": 87, "title": "Arrival", ... },
                 "position_ms": 1284000, "watched": false,
                 "played_at": 1755200000 } ],
  "has_more": true }
```

`?limit=` defaults to 50 (max 200) and `?offset=` pages. `has_more` says whether
the history was cut short, so a client can offer the next page rather than infer
it from a full-looking one.

**History is derived from `playback_state`, not from a log of plays.** There is
one row per item per user, so this is the *last* time each item was played and
not every time it was. That is a stated limit rather than a hidden one: a
per-play log is a second record of the same fact, free to disagree with the
first, and nothing has yet asked for one.

Items that are `missing` are included. "What happened to the film I watched last
week" is a question about history, and a library that lost a drive should not
lose the answer to it.

`watched_ms` is time *spent*, not runtime owned: a finished item counts its
duration, an unfinished one counts how far in you got. Summing the duration of
everything touched would report eleven hours for eleven films abandoned in their
first minute.

`secured` is `false` on an unconfigured loopback server, where there is no
account and the history belongs to the migrated `local` id. The client says so
rather than inventing a person.

The caller's own profile only. There is no per-user variant of this route: "what has
everyone been watching" needs an answer to who may see it before it needs a
route.

### `PATCH /api/profile`

Changes your own display name. `{ "name": "Chris" }`, 60 characters or fewer,
no control characters. `409 duplicate` if the name is taken.

The account **id does not change**, which is what makes this a rename rather
than a replacement: sessions, watch history, ratings and playlist membership all
hang off the id and follow silently.

`409 no_account` on an unconfigured loopback server, where there is no account
to edit.

### `GET /api/people`

The other accounts on this server. "Find Friends" on a self-hosted household
server means the people already on it — there is no directory to search and no
second server to federate with.

```json
{ "people": [ { "id": "u_3f9", "name": "Sam", "role": "member",
                "sharing": true, "watched": 41, "joined_at": 1739000000 } ] }
```

The caller is excluded from their own list. `sharing` is reported even when
false, so a page can say "has not shared" rather than showing an empty list that
reads as "watches nothing" — those are different statements and a page that
cannot tell them apart accuses the private of being inactive.

`watched` is zero unless that person shares. A count is still a fact about a
person.

### `GET /api/people/{id}/activity`

What one person has published — **finished titles only**, newest first.
`?limit=` defaults to 20 (max 100).

**Returns an empty list, not a `403`, for somebody who has not opted in.** A
`403` confirms there is something being withheld; what somebody watched is
private and so is how much of it there is. From outside, "has not shared" and
"has watched nothing" are the same answer, deliberately.

Governed by [ADR 0035](adr/0035-who-may-see-whose-viewing.md): viewing is
private by default and shared only by an explicit per-account opt-in. Resume
positions are never shared — where somebody stopped is a different and more
intrusive fact than what they watched — and ratings and reviews are never shared
at all.

### `PUT /api/profile/sharing`

`{ "share": true }` — the caller's own decision, and only ever the caller's.

**There is deliberately no administrator variant.** An administrator may run the
server; a switch somebody else can flip on your behalf is not consent. Audited,
because it changes who can see something about a person.

Turning it off is **retroactive**: past activity stops being visible along with
future. A switch that cannot take back what it gave is not a switch.

### `GET /api/profile/history`

`?scope=all|finished|unfinished` and an optional `?under={item_id}` — how many
playback records a reset **would** remove. Removes nothing.

It exists so the confirmation can name a number. A person who expected to clear
one show and is told four hundred has learned something while it is still free,
and a number is what makes an irreversible action reviewable rather than a
shrug.

### `DELETE /api/profile/history`

The same parameters, performed. Answers `{ "removed": n, "scope": "..." }`.

**The account is the session's and there is no user id to supply.** Playback
state is keyed by user ([ADR 0006](adr/0006-playback-state-keyed-by-user.md))
so that one person's viewing is their own, and an administrator clearing their
own history must not be able to reach into anybody else's — so the endpoint
offers no way to name a victim. This is the same reasoning as the sharing
switch above: running the server is not consent on somebody else's behalf.

Three scopes because "reset my history" means three different things.
`playback_state` is one table carrying two meanings, and somebody forgetting a
show they finished rarely means "and lose my place in the one I am half way
through". `under` narrows to an item and everything beneath it, recursively, so
forgetting a show is one call rather than a client walking its episodes.

Audited ([ADR 0026](adr/0026-audit-log.md)): destructive and irreversible puts
it in the same class as removing a library. The entry records the scope and the
count, not the rows — "forgot 412 finished items" is what answers the question a
month later, and a list of ids for things that may since have been deleted is
not.

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

**The request body does not carry `watch_count`, and clients must not try to
set it.** The server maintains it, and it moves on the *transition* from
unfinished to finished rather than on the level: a player posting `watched:
true` every five seconds through the credits records one viewing, not twelve.

Starting something again is what makes the next viewing countable, and nothing
has to announce it — an early position posts as not watched, which returns the
row to unfinished, and reaching the end counts again. So a rewatch is counted
without any client doing anything differently.

**Marking a title unwatched leaves the count alone.** "Put this back on my
list" is not a claim never to have seen it, so a response may carry
`"watched": false` with a non-zero `watch_count`. A client showing the count
must not infer it from the flag in either direction.

Counts are per account, like the rest of `progress`. They begin at 1 for
anything already finished when the server upgraded to schema revision 31 —
history that predates the column cannot be recovered, and one is the honest
minimum rather than a guess.

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

### `GET /api/items/{id}/continue`

Where a show should resume for the calling user. **Never cached** — the response
carries `Cache-Control: no-store`, and that is the feature rather than a
precaution.

```json
{ "episode": { "id": 412, "season": 2, "episode": 5, "progress": { "position_ms": 0 } },
  "resume": false, "exhausted": false }
```

The rule, in order:

1. **An episode in progress wins**, most recently touched first — that is what
   was being watched, whatever the numbering says. `resume` is true and the
   episode carries its saved position.
2. Otherwise, **the first unwatched episode after the furthest one watched**.
   Deliberately *not* "the earliest unwatched": skip episode 5, watch through 13,
   and earliest-unwatched sends you back to 5 on every press. Progress through a
   series only moves forward.
3. Nothing watched: the first episode.
4. Everything watched: `exhausted`, with no `episode`, so a client offers to
   start again rather than silently replaying the finale.

Progress is per user, so one person finishing a season does not move anybody
else's place in it.

The no-store header is doing real work: a proxy or browser holding this answer
for thirty seconds reproduces exactly the bug the rule exists to avoid — press
continue, land on an episode already watched — and does it intermittently, which
is the hardest kind to believe.

### `GET /api/items/{id}/episodes`

A show's episodes in playing order — season, then episode, then row id so the
order is total. Progress is attached, so a list can show what has been watched
without a request per episode.

Behind **Play** and **Randomize**, which are this list handed over in order or
with the player's own shuffle turned on. Ordered identically to `/continue`, so
"next" and "the queue" cannot disagree about what follows what. Also `no-store`:
this is what Randomize queues, and a cached copy would shuffle episodes whose
watched flags are out of date.

A route of its own rather than `/api/items?parent_id=`, because episodes hang
off seasons rather than off the show — the obvious call returns the seasons, and
every client would otherwise reimplement the walk and get the loose-episode case
wrong.

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

### `GET /api/items/{id}/download`

The item's original file, as an attachment.

Same bytes as `GET /api/stream/{id}`, and a different intent. A stream is for a
player and every browser treats it as one; this carries
`Content-Disposition: attachment` with a filename built from the item's
metadata rather than from its path — `Arrival (2016).mkv`, or
`Storm of the Century - S02E07 - Pilot.mkv` for an episode, because `Pilot.mkv`
collides with every other pilot ever made. The name is given in both the quoted
`filename=` form and the RFC 5987 `filename*=UTF-8''` form, so a title outside
ASCII survives.

**Never transcoded.** The transcoder exists so a device that cannot play a file
can still watch it; a download that quietly returned a re-encoded copy would be
a lie about what you have, and the one operation where the original matters most
is the one that takes it off the server.

Range requests are honoured, so an interrupted transfer resumes — which is the
case a nine-gigabyte file is in.

`404` if the item has no file or its path does not resolve inside the location
it was scanned under; `503` if the file is missing from disk.

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

`target_height` and `target_video_bitrate` are present only on a decision whose
`video_action` is `encode` **and** which a quality ceiling actually constrained.
They are what the re-encode will come in under. A copy carries neither: nothing
re-encodes, so no ceiling reached a pixel, and reporting one would have a client
believe a cap applied that did not.

`tonemap_hdr` is present and `true` when the source is HDR — PQ or HLG by its
transfer function — and the re-encode will convert it to SDR (ADR 0033). Like
the targets above it appears only on `video_action: "encode"`: a copy delivers
the source's own video bytes, which are HDR and are correctly described as such,
so there is nothing to convert and nothing to re-label.

A client should not read this as "the output is HDR". It means the opposite: the
delivered stream is BT.709 SDR and is tagged as BT.709 throughout. Whether the
conversion itself runs depends on the ffmpeg build on the server — the CPU
tonemap needs `zscale`, which is not present in every build — and a server that
cannot convert still labels the output consistently rather than emitting a file
whose colour tags disagree with each other.

Takes the same `?profile=`, `?audio=`, `?max_height=` and `?max_bitrate=`
parameters as the stream endpoints, and echoes the resolved profile back. Call
it with the parameters you intend to stream with: an explanation of a decision
the server would not actually make sends you looking in the wrong place.
`?audio=` naming a track that does not exist returns `400 bad_request`.

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
| `hevc` | HEVC video **at 8 bits**, and the `matroska` container it usually arrives in |
| `hevc10` | permission for **10-bit** HEVC (Main 10). Adds no codec of its own |
| `ac3`, `eac3`, `dts` | that audio codec |
| `matroska` | the container alone, for a client with a real demuxer |
| `high10` | permission for **10-bit H.264** (High 10). Adds no codec of its own |
| `flacmp4` | permission to **carry FLAC inside MP4**. Adds no codec of its own |
| `opusmp4` | permission to **carry Opus inside MP4**. Adds no codec of its own |

`hevc` and `hevc10` are separate because they are separate questions and the
answers differ: a browser can answer "probably" for Main profile and still
decode Main 10 badly. That was found on a real film — direct-played with perfect
audio and a stuttering picture, from a client that had probed
`hvc1.1.6` (8-bit) and been read as covering Main 10 too. A client should probe
`hvc1.2.4.L120.B0` separately and send `hevc10` only if the engine answers for
it; without it, 10-bit HEVC is transcoded.

`high10` is `hevc10`'s counterpart one codec along, with one deliberate
difference. `hevc10` trusts a profile that lists HEVC *natively* — `tv` and
`safari` are device classes known to decode Main 10 in hardware — but `high10`
trusts no native listing at all, because H.264 is listed by every profile
including the browser floor. Applying the same rule would hand High 10 to a
set-top box on the strength of it decoding 8-bit H.264, and High 10 is absent
from most fixed-function decoders that manage High profile perfectly. So every
client asks, or the file is transcoded.

Probe `video/mp4; codecs="avc1.6e0033"` and send `high10` only if the engine
answers for it. It is named for the profile rather than the bit depth because
"High 10" belongs to H.264 alone, so it cannot be misread as covering HEVC.

`flacmp4` and `opusmp4` are the same shape of question one layer down: not
"can you decode FLAC" — every browser in the floor already can, which is why a
`.flac` file direct-plays — but "can you decode it *in an MP4*". Those differ.
FLAC in fragmented MP4 is legal by spec and not universally decodable, so a file
whose only fault is its container had its audio re-encoded, turning lossless
into AAC to change a box.

A client should probe `audio/mp4; codecs="flac"` and `audio/mp4; codecs="opus"`
separately — they are two engine answers, and one does not licence the other —
and send each only if the engine answers for it. Without the claim the audio is
re-encoded, which is what happened for every such file before these existed.

ALAC needs no claim: MP4 is its native home, so only whether the client decodes
it at all is ever in question, and the profile already says.

The distinction applies **only to claims**. A profile that lists HEVC natively —
`tv`, `safari` — is a device class known to decode Main 10 in hardware, and
demanding a claim from it would re-encode HDR for the clients that handle it
best.

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

#### `?max_height=` and `?max_bitrate=` — the quality ceiling

A limit on what the client is willing to receive, applied **on top of** the
profile and whatever `?can=` widened it to:

```
GET /api/items/87/playback?max_height=720&max_bitrate=4000000
```

| Parameter | Unit | Meaning |
| --- | --- | --- |
| `max_height` | pixels | Video taller than this is scaled down to it |
| `max_bitrate` | **bits** per second | Video above this rate is re-encoded under it |

Bits per second, not kilobits — the same unit `Profile.MaxVideoBitRate` uses
internally. Absent, zero, or unparseable means no ceiling.

**It only ever narrows,** which is the mirror image of `?can=` and the reason
the two are separate parameters rather than one. A ceiling can force an encode
that would not otherwise have happened; it can never talk the server into
direct-playing something the client cannot decode. Where a named profile carries
its own ceiling, the lower of the two wins — asking for `max_height=1080`
against a profile capped at 720p does not raise it.

**A ceiling is not a target.** A file already under it is untouched: no upscale,
and no rate control that could only ever be slack. Asking for 1080p on a 480p
file direct-plays exactly as it would with no parameter at all.

**Send it to every endpoint that decides, or none** — the same rule as `?can=`,
for the same reason. A seek that drops the ceiling asks `/transcode` about an
uncapped stream and gets `409 conflict` on a file the ceiling was the only
reason to touch.

Absent, it behaves exactly as before this existed, so it is additive under
[ADR 0018](adr/0018-api-contract-and-versioning.md).

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

### `GET /api/media-tools` · `POST /api/media-tools/install` · `POST /api/media-tools/install/cancel`

Fetching ffmpeg and ffprobe from inside the app ([ADR 0043](adr/0043-media-tools-are-fetched-not-bundled.md)).
**Admin only**, and for a stronger reason than most admin gates here: this makes
the server download a binary and then execute it.

**There is no URL parameter.** The build is pinned in `internal/mediatools` with
a SHA-256 checked before anything is unpacked. A server that fetched an address
the caller chose would be the server-side request forgery the channel-source and
guide endpoints already refuse, and here the payload is an executable rather than
a playlist. A version bump is therefore a code change, deliberately: a server
following a "latest" pointer is one whose playback behaviour changes without a
LANcast release.

Nothing here runs automatically — not on first start, not when a probe fails. A
media server that contacts the internet without being asked has broken
*no phone-home*.

`GET` reports the current state, whether or not an install is running:

```json
{ "running": true, "stage": "downloading", "bytes_done": 41943040,
  "bytes_total": 168274317,
  "probe_available": false, "transcode_available": false,
  "directory": "C:\ProgramData\LANcast\tools",
  "available_source": { "version": "8.1.2 (n8.1.2-44-g7c533d0f86, win64 gpl static)",
                        "licence": "GPL v3", "licence_url": "https://www.gnu.org/licenses/gpl-3.0.html",
                        "size_bytes": 168274317, "url": "https://github.com/BtbN/..." } }
```

`available_source` is what a caller is consenting to, returned *before* they
consent: a download the user cannot identify is not consent. It is absent where
there is nothing pinned for the platform.

`stage` is `downloading`, `verifying` or `installing`. A finished install adds
`finished_at`; a failed one adds `error`.

`POST /install` answers `202` and runs in the background — 160MB held open on a
request would make a client timeout look like a failed install. `409` if one is
already running; **`501`** where no build is pinned for the platform, whose
message names the package manager instead, because Linux and macOS install
ffmpeg better than this code will and put it somewhere the lookup already
searches.

`POST /install/cancel` stops it and answers `409` when nothing is running. A
cancelled install leaves nothing behind, and a partial one reports as **absent**
rather than as installed: `ffprobe` is moved into place last, and the tools are
detected by looking for `ffprobe`.

On success the running server picks the tools up **without a restart** — the
directory goes on the process PATH, the transcode manager re-resolves its binary,
and the location is persisted the way the service config already records one.

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
  ],
  "staged": "0.8.2"
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
- A `transcode` task titles itself **"Remuxing for playback"** when the streams
  are being copied into a different container and **"Transcoding for playback"**
  only when something is genuinely re-encoded. The two differ by an order of
  magnitude in cost — a remux is a few percent of one core — and reporting both
  as a transcode overstates what the server is doing. The underlying flag is
  `encoding` on `GET /api/transcode`.
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

### `GET` / `DELETE /api/crashes`

Recovered panics, newest first. **Admin only** — a stack trace names source
paths and may name a route the caller cannot reach.

```json
{ "crashes": [ { "id": "20260815-142233.481-004", "at": 1755268953481,
  "kind": "panic", "where": "GET /api/items/{id}",
  "value": "assignment to entry in nil map",
  "stack": "goroutine 42 [running]:
...", "version": "0.6.26" } ] }
```

`where` is the **route pattern**, not the URL: `GET /api/items/{id}` is what
somebody fixes, while the URL it came from invites the belief that one particular item is
special.

Without this a panic unwinds through `net/http`, which closes the connection
without a response — the client sees a network error and the operator sees
nothing unless they happen to be reading the log at the time. The request now
answers `500` with the ordinary error envelope, and the panic becomes a numbered
report.

Reports are JSON files in `crashes/` under the data directory, not database
rows: the crash most worth having is the one where the database was the thing
going wrong. The newest 50 are kept — a crash loop produces the same stack a
thousand times, and the first one is the informative one.

`DELETE` removes them all and answers `204`. Nothing is ever sent anywhere;
LANcast does not phone home, and "except for crash reports" is how every product
that does began.

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

`/api/activity` also carries **`staged`** at the top level, present only when a
version is downloaded, verified and waiting to be applied on the next restart.
It is stated there rather than left to be read out of a task id because it is
the difference between "restarting finishes this" and "restarting will change
nothing", and the desktop client's stale-window banner is shown to *everyone*
where this endpoint's admin-only sibling is not. Absent means nothing is
waiting.

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

**A client must render the refusals.** Both are handed to a `<video>` element as
a failed request, which the element reports as a bare `error` with no status and
nothing to display — so a player that does not act on them shows a spinner for
ever, which is indistinguishable from converting slowly. Retrying is not a
recovery: the same request under a narrower profile is the same request. Both
are also written to the server log now, the `429` carrying how many sessions are
running against the ceiling, since that number is what separates "the limit is
working" from "sessions are leaking".

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

### `GET` / `POST /api/together`

Watch-together sessions: several people playing the same thing at the same
position.

```json
{ "sessions": [ { "id": "k3f9q2xw7m", "item_id": 87, "host_id": "u_3f9",
  "position_ms": 1284000, "paused": false, "updated_at": 1755200000,
  "members": [ { "user_id": "u_3f9", "name": "Chris", "host": true,
                 "last_seen": 1755200000 } ],
  "created_at": 1755199000 } ] }
```

`POST` opens a room around `item_id` (with an optional `position_ms`) and makes
the caller its host; `201` with the session. A room around an item that does not
exist is refused, because everybody who joined would sit looking at a player
that cannot load with nothing to say why.

**The server owns the truth** - what is playing, where it is, whether it is
paused - and clients converge on it. The alternative, each client broadcasting
its own position, makes the last writer win, and on a lossy connection that is
whoever lagged worst.

**Live state, no schema.** A session means nothing after a restart; persisting
one would resurrect a film nobody is watching and invite a client to rejoin a
room whose members went home hours ago.

**Polling, not sockets.** Nothing else in this stack streams, and a socket layer
for one feature is the dependency argument [ADR 0013](adr/0013-transcode-pipeline.md)
settled. A second of drift is acceptable for "we are watching this together";
frame accuracy is not the goal and could not be delivered to three devices over
a LAN anyway.

Any session may use these routes - no particular role. Watching something with
the people you live with is not an administrative act.

### `POST /api/together/{id}/join`

Adds the caller. Rejoining is not an error and does not duplicate anybody: a
refresh, a dropped connection and a second tab all arrive here.

### `GET` / `PUT` / `DELETE /api/together/{id}`

`GET` is the follower's whole synchronisation input, and doubles as the signal
that this member is still present - **nobody presses "leave", they close the
laptop**, so a room drops members who stop polling for 90 seconds and closes
when the host goes quiet.

`PUT` is the host reporting `position_ms` and `paused`. **Only the host** -
`403 forbidden` for anyone else. Two people scrubbing the same film is not
synchronised playback, it is a fight, and the loser cannot tell it from a bug.

`DELETE` leaves. **The host leaving ends the room** rather than promoting
somebody: promotion sounds generous and is worse, because the film keeps playing
in three houses under a driver nobody chose and the person who started it cannot
stop what they began. `404 not_found` once a room has ended, which is the
client's cue to stop following.

`updated_at` is when the host last reported, so a follower can work out how far
the film has moved since - without it every poll would land one interval behind
and never catch up.

### `GET /api/channels`

Live TV channels, in the order their source listed them. `?source_id=` filters
to one source.

```json
{ "channels": [ { "id": 12, "source_id": 1, "name": "Channel One",
                  "logo_url": "https://logos.example/one.png",
                  "group": "UK", "position": 0 } ] }
```

**A channel is not a `media_item`**, and that is a modelling decision rather
than a convenience. Every column on that table describes a *work* — a title a
provider could match, a duration, a file, a position you stopped at — and a
channel has none of them. It is a name, a logo and a URL whose contents differ
every time you look. [ADR 0002](adr/0002-one-wide-media-item-table.md) chose one
wide table for things that are works; this is the case that is not one.

**The upstream URL is never serialised.** Channel lists are routinely
credentialed — a token in the path, a password in the query — so publishing it
to every browser on the LAN would publish the subscription.

Source order is preserved rather than sorted: it is the order somebody curated,
and alphabetical is not an improvement on the order the channels sit in on a
remote control.

### `GET /api/channels/{id}/stream`

Relays a channel through the server. Clients play this; they never see the
provider's address.

**This proxy cannot be pointed anywhere.** It takes a channel id, not a URL. For
HLS, the playlist is rewritten so every segment comes back through the same
route, and `?path=` is resolved *relative to that channel's own base* — an
absolute reference, or one that would change host, is `400`. There is no
parameter that changes the destination, which is what keeps this from being an
open relay inside somebody's network.

**This route does not transcode.** It relays the provider's bytes untouched,
which plays in Safari and nowhere else: most IPTV channels are HLS carrying
MPEG-TS, and Chromium decodes neither — `canPlayType("video/mp2t")` answers with
an empty string, and [ADR 0013](adr/0013-transcode-pipeline.md) refuses to
vendor hls.js.

Use `GET /api/channels/{id}/live` for a channel a browser can play. This route
remains for Safari, for a client with its own demuxer, and for a source already
in a browser-friendly format.

### `GET /api/channels/{id}/live`

The same channel, converted for the browser: **fragmented MP4**, produced by the
ffmpeg pipeline the file path already uses.

**Usually not a transcode.** Nearly every channel is H.264 video with AAC audio,
which fMP4 accepts as-is — so ffmpeg rewrites the container and copies both
streams, costing a few percent of a core rather than a whole one. That is what
makes this affordable per viewer, and the copy-or-encode decision comes from the
same `probe.Decide` the file path uses rather than from a second set of codec
rules that would eventually disagree with the first.

The channel is **probed first**, briefly (6 seconds), and a failed probe falls
through to copying rather than refusing. Knowing the codecs matters: an AC-3
channel needs its audio re-encoded, and copying it produces a working picture
with silence — the worst failure available, because it looks like it nearly
worked.

**ffmpeg stops when the request ends.** A live source never finishes, so nothing
else would ever stop it: a leaked session does not idle, it pulls a stream at
full rate for ever for somebody who closed the tab. The process lifetime is tied
to the HTTP request.

`503 no_ffmpeg` when ffmpeg is not installed — named, because the fix is
installing it. `503 busy` when the server is already running its maximum number
of concurrent streams; live sessions count against the same ceiling as file
transcodes, since they are the same kind of process on the same machine.

**`502 channel_unavailable` when the source produces nothing**, with a message
saying why: `the channel's source is gone (HTTP 404) — the provider's list may be
out of date`.

The status is only sent because **the header is not written until the first byte
of video exists**. Committing to `200` first meant a dead source produced an
empty but successful video stream, and the browser reported
`DEMUXER_ERROR_COULD_NOT_OPEN` — so a stale entry in a provider's list read as a
broken application. A list of 1,862 channels will contain dead ones; that is
ordinary, and it should say so.

Once one byte has been sent there is a stream, and any later failure ends the
connection rather than changing the status — an interruption of something that
was working is a different event, and no status code can be sent by then anyway.

**The message never contains the upstream URL.** ffmpeg writes the full URL into
its stderr, and channel URLs are routinely credentialed; only a classification
derived from that text is returned, never the text itself.

Not cacheable, and no `Content-Length`: this is a stream with no end, and a
cache holding "the channel" would serve one viewer's minute to everybody who
asked afterwards.

**Still not built:** hardware tuners (HDHomeRun and friends). The EPG is built —
see `GET /api/guide` below.

**Known limit:** `EXT-X-KEY` and `EXT-X-MAP` URIs are left pointing upstream
rather than half-rewritten. A client that can reach the provider still plays;
one that cannot will fail on an encrypted stream. Stated rather than guessed at.

### `GET /api/channels/{id}/hls/index.m3u8`

The same channel as an **HLS playlist** with fMP4 segments, rather than one
progressive response.

It exists because a progressive stream gives a client no control surface: a bare
element cannot say how much media it is holding, cannot tell being starved from
being stuck, and cannot be seeked on a response that has no ranges. Those three
gaps are the subject of the [ADR 0013](adr/0013-transcode-pipeline.md) live-TV
amendment, and a playlist answers all of them for any client that can consume
one.

**The playlist is an `EVENT` playlist, and that is a claim about the source
rather than a preference.** `VOD` says the stream is complete and whole, which
is true of a film and false of a channel — and the cost of saying it is not
cosmetic. Emitted as VOD, ffmpeg defers the playlist entirely: measured against
a real channel, nine good segments were written over 60 seconds and
`index.m3u8` never appeared at all. The media was fine and undiscoverable.

`EVENT` keeps every segment listed, so a viewer who paused can still reach what
they missed, at the cost of a playlist and a segment directory that grow for as
long as the session lives. That is bounded by the session — an idle channel is
reaped and its directory goes with it — but a channel genuinely watched for a
day is a real disk cost and is not yet solved.

**One session is shared between viewers**, unlike the progressive path. Segments
are files, so a second viewer of the same channel costs an HTTP handler rather
than another ffmpeg, which on a bounded session ceiling is the difference
between a channel two people can watch and one they cannot. There is no offset
in the key: a channel has one position, now.

**ffmpeg is *not* stopped when the request ends**, which is the opposite of
`/live` and deliberate. There, the request is the stream and a closed tab must
kill the process. Here the request is one poll of a playlist among many, and
tying the encode to it would kill the channel between the playlist and its first
segment. The session ends on idle instead.

Segment URLs are rewritten to `/api/channels/{id}/hls/{session}/{name}`, so the
provider's address never reaches a client — the same rule the rest of the
channel routes keep.

`503 no_ffmpeg` when ffmpeg is not installed. `503 busy` at the session ceiling.
`503 unavailable` if no playlist appears within 30 seconds.

**No browser in this build consumes it.** hls.js is deliberately not vendored,
so this is in the same position the file HLS endpoints have been in since M3:
the output exists for a client that can use it. Shipping it does not take the
dependency decision.

### `GET /api/channels/{id}/hls/{session}/{name}`

One segment, or the `init.mp4`, from a live session's directory. Identical
handling to the file HLS segment route, including that the name is validated
rather than trusted before it becomes a filesystem path.

### `GET` / `POST /api/channel-sources`

Channel lists and where they came from. **Admin only** — and for a stronger
reason than most admin gates here: adding a source makes *the server* fetch a
URL the caller chose, which is server-side request forgery in miniature.

```json
{ "sources": [ { "id": 1, "name": "Provider", "url": "https://…/list.m3u",
                 "created_at": 1755200000, "refreshed_at": 1755290000,
                 "channel_count": 612 } ] }
```

`POST` takes `{ "name": …, "url": … }` and imports immediately, because a source
with no channels is indistinguishable from a broken one and the moment somebody
adds it is the moment they are watching to see whether it worked. A source whose
import fails is **kept**, with `import_error` in the response: the URL may be
right and the provider down, and deleting it would mean retyping it.

`epg_url` is optional and is an **XMLTV** document, plain or gzipped. It is
imported in the same request, *after* the channel list, and a guide that fails
is reported as `epg_error` — separately from `import_error`, because a working
channel list with no schedule is a usable Live TV and conflating the two makes
the channels look broken. The response carries `programs`, the number of
listings stored, which is not the number in the file: a guide covers channels a
source does not carry and those are dropped rather than stored against nothing.

**Refused guide URLs are the same set as refused playlist URLs.** A guide URL is
fetched by this server exactly as a playlist URL is, so it gets the identical
check on both `POST` and `PATCH`. A door closed on one route and open on another
is not closed.

**Refused URLs:** anything that is not `http`/`https`, and **this server's own
address**. Other loopback addresses are deliberately allowed — a tvheadend or a
local transcoder on the same machine is one of the most ordinary sources this
feature has, and banning loopback outright would make Live TV useless on the
setup it suits best. What needs protecting is one origin, not one interface.

### `POST /api/channel-sources/{id}/refresh` · `DELETE /api/channel-sources/{id}`

Re-imports, or removes along with its channels. **Admin only.**

A refresh **replaces** rather than merges. A channel list is a snapshot
published by somebody else, not a collection curated here, and the file carries
no id worth trusting across versions — `tvg-id` is optional and frequently
absent — so merging means guessing at identity and duplicating every channel
each time the guess is wrong. The replacement runs in one transaction, or a
refresh would leave a window where the old channels are gone and the new ones
have not arrived.

A refresh imports **channels first and the guide second**, and that ordering is
load-bearing rather than incidental: replacing channels deletes their rows, and
`epg_program.channel_id` cascades, so a guide imported first is deleted moments
later by the channel import — producing an empty guide with no error anywhere to
explain it.

`502 upstream` when the provider cannot be reached or does not return a
playlist. A guide failure is never a `502`: the channel list succeeded, and the
guide is reported in `epg_error`.

### `GET /api/guide`

What is on now and next, for every channel that has listings. Not admin-gated —
what is on television tonight is not a secret from the household.

```json
{ "at": 1755290400,
  "channels": {
    "12": { "now":  { "id": 91, "channel_id": 12,
                      "start_at": 1755288000, "stop_at": 1755291600,
                      "title": "The News", "description": "What happened.",
                      "category": "News", "season": null, "episode": null,
                      "icon_url": null },
            "next": { "id": 92, "…": "…" } } } }
```

Keyed by channel id so a client can render a channel grid without joining
anything — it already holds the channels. `at` is the instant the answer
describes, and clients should draw progress from it rather than from their own
clock, which may be skewed.

**A channel with no listings is absent, not present and empty.** That is what
lets a client tell "this channel has no guide" from "nothing is on", and those
are different sentences.

`?at=` takes a unix timestamp and answers as of then, for a client that wants
the guide at a time other than now.

**Listings attach to channels by `tvg-id`, and by nothing else.** A channel
whose `#EXTINF` carries no `tvg-id` never appears here. Matching on display name
instead would confidently attach "BBC One" listings to "BBC One HD" and be wrong
in a way nobody could see from the guide, so it is refused
([ADR 0036](adr/0036-epg.md)).

### `GET /api/channels/{id}/guide`

One channel's schedule.

```json
{ "programs": [ { "id": 91, "channel_id": 12, "start_at": 1755288000,
                  "stop_at": 1755291600, "title": "The News",
                  "description": null, "category": "News",
                  "season": null, "episode": null, "icon_url": null } ] }
```

`?from=` is a unix timestamp defaulting to now, `?hours=` a window defaulting to
12 and capped at 336 (a fortnight — more than any guide publishes). Programmes
are returned if they *overlap* the window rather than fall inside it, because the
programme that started before the window opened is the one being watched, and a
schedule that omits it begins with a hole.

`400 bad_request` on a malformed timestamp or an out-of-range window; `404` when
there is no such channel.

**Guides refresh by themselves, every twelve hours**, and expired listings are
pruned a day after they end. This is the one thing in LANcast that goes wrong by
*doing nothing*: an unrefreshed guide does not go blank, it goes wrong, and says
last Tuesday's programme is on now. A library that is not rescanned still lists
the films it listed yesterday, which is why scanning is opt-in and this is not.

### `GET /api/review?library_id=`

The review queue: items in `review` or `unmatched` state, with the parsed
filename alongside the proposed match so the two can be compared directly.

**Seasons are excluded.** A season has no identity of its own — its name is
"Season 1", a position within a show rather than the name of a work — so a
provider search for it fails at 0% on every season in the library, for ever. A
real TV library listed 55 of them, each offering a Fix button leading to a
search that cannot succeed. Shows are still listed: a show's title is a real
title, and a wrong match on one is worth correcting.

### `PUT /api/items/{id}/poster`

Choose which of a collection's films it wears. **Admin only** — there is one
poster and everybody sees it, so this is shared state rather than a per-viewer
preference, and it matches every other write that changes what the library looks
like to everyone.

```json
{ "from_item_id": 41 }
```

`from_item_id` of **0 clears the override** and returns the collection to the
default. Returns the updated item.

A collection with no artwork of its own borrows its **earliest** film's poster,
read-time and flagged `"inherited": true`
([ADR 0025](adr/0025-artist-images.md)'s pattern). That is right for almost every
franchise and wrong for some — a Marvel Cinematic Universe wearing Iron Man
(2008) is defensible and is not necessarily what somebody who has looked at it
wants. This is the disagreement.

It is a **selection, not a copy**: artwork is content-addressed and shared, so
the collection points at the film's existing image. Nothing is downloaded, and
the two pictures cannot drift apart.

**It locks.** Setting a poster this way writes an `artwork` field lock
([ADR 0008](adr/0008-field-level-locking.md)), because storing any new image
deselects every poster row before selecting its own — so a provider refresh
would otherwise replace the choice, and a choice a refresh can undo is not a
choice. Clearing removes the lock, so the
default resumes and improves with it: a franchise whose first film arrives later
starts wearing it again.

**400** when the item is not a collection, when `from_item_id` is not one of its
members, or when that member has no poster of its own. A non-member is refused
rather than silently ignored: the id arrives from a client, and that is the
boundary where a bad one would become "any item's poster on any collection".

### `GET /api/collisions?library_id=&compare=`

Works claimed by more than one row. **Admin only**, because it is the one
response in this API that returns `path` — the whole value of the report is being able to go
and look at the two files, and a collision the reader cannot locate on disk is a
notification rather than a report.

```json
{ "collisions": [
  { "provider": "tmdb", "external_id": "324857", "same_size": true,
    "members": [
      { "id": 41, "title": "Spider-Verse", "path": "W:/Films/…/Spider-Verse (2018).mkv",
        "size_bytes": 2832374353, "library_id": 1, "missing": false },
      { "id": 88, "title": "Spider-Verse", "path": "W:/Films/…/Spider-Verse (Alternate Cut) (2018).mkv",
        "edition": "Alternate Cut", "size_bytes": 2832374353,
        "library_id": 1, "missing": false } ] } ] }
```

**The work key is `(provider, external_id, season, episode)`**, and the last two
are not decoration. Every episode of a show carries the **show's**
`external_id` — that is how an episode's provider identity works, a show id plus
a position — so keying on the pair alone reports every multi-episode show as one
enormous collision. On a real library that was 999 episode rows against 86
genuine film ones. A film has no season or episode and groups as you would
expect; two files of the *same* episode collide, which the pair alone could
never detect.

**LANcast reports the collision and does not resolve it**
([ADR 0042](adr/0042-two-files-one-work.md)). There is no merge, no ranking, no
"keep the best copy", and no delete — not as a missing feature but as the
decision. A shared provider id is evidence that *something* wants a human, not
that anything is duplicated: on the library this was built against, thirteen
pairs shared one and **two were not duplicates at all** — a film split across two
discs, and a 1989 film wearing a 2022 film's identity from a stale `.nfo`.

`edition` is the marker the filename claimed, verbatim, or absent. It is a
**label, never a grouping key**: the file that motivated the decision called
itself an alternate cut and was byte-for-byte the theatrical copy.

**Duration is deliberately not reported.** `duration_ms` is overwritten with the
provider's runtime on match, so two rows sharing an id always report identical
durations whatever the files hold — including the misfile above, where one film
is 126 minutes and the other 177. What is real: `path`, `size_bytes`, and
comparing the bytes.

`same_size` is free and always present. Equal sizes make a copy likely; unequal
sizes rule one out, which is the more useful answer and needs no I/O.

`compare=<external_id>` opts one collision into a byte comparison, adding
`fingerprint` per member and `same_bytes` to the collision. The fingerprint is
**sampled** — the size plus three 1 MB windows at head, middle and tail — so
`same_bytes` means *identical so far as sampled* and never *identical*. That
trade is defensible only because nothing acts on it. A member that could not be
read carries `"unreadable": true` and no fingerprint, and the collision then
omits `same_bytes` entirely: a file that cannot be opened is an absence of
evidence, not a different file.

Comparison is opt-in per collision because sampling three windows of a 14.6 GB
file is cheap next to reading it and expensive next to nothing — a report is
opened far more often than any one row in it is investigated.

### `POST /api/items/{id}/refresh` · `POST /api/libraries/{id}/refresh`

Re-fetch metadata, honoring all field locks. Returns `202`.

### `POST /api/libraries/{id}/reparse`

Re-runs the filename heuristics over a library's uncertain rows and requeues the
ones whose guess changed. Admin only. Returns `200`.

```json
{ "examined": 140, "changed": 130 }
```

**Distinct from `refresh`, and the distinction is the point.** Refresh asks the
provider the same question again; this corrects the question. A film whose year
lived only in its folder name was searched with no year at all, and no number of
refreshes would have changed that answer.

Scope is deliberately narrow:

- Only `review` and `unmatched` rows are touched. A `matched` row's title came
  from a provider, which is better evidence than any filename.
- `locked` and `local` rows are never offered — a locked identity is not
  re-litigated, and a local one is what the user already said this is.
- Field locks are honored **individually**: an item whose title a person
  corrected still has its year re-parsed.
- An empty guess never clears a populated field.

**A row is re-parsed once.** Each row examined is stamped, whether or not it
changed, and stamped rows are not offered again — that is what makes a second
call free, reporting `{"examined": 0, "changed": 0}`.

The stamp is load-bearing rather than an optimization. Enrichment writes the
provider's answer back over the guess for any row that stays uncertain, so
"never re-parsed" and "re-parsed a minute ago" both disagree with their
filename and cannot be told apart by comparing titles. Without the stamp every
call rewrote the same rows and asked the provider the same question again —
measured on a real library, 32 rows flipping back and forth on every press.

`?force=true` re-offers rows that have already been re-parsed. It exists for the
one thing the stamp cannot see: the filename heuristics themselves improving, so
rows parsed under the old rules deserve another pass.

`404` when there is no such library.

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
| `audit_retention_days` | `90` | 0–3650 | Audit events older than this are deleted by a daily pass. **0 means keep for ever**, the same shape of answer `continue_weeks` gives — not "delete now". Takes effect without a restart. Cached provider responses are dropped after **7 days** regardless: that is a cache, every entry refetches, and it is not covered by this setting. Changing this value makes a pass due on the next check rather than a day later — a stamp records the policy it ran under, so shortening a window takes effect promptly instead of looking broken |

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
