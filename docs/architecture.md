# Architecture

> **Status:** M0–M4 are built and released. Schema is at revision 33. This
> document describes the shape of the server as it stands; the ADRs say why,
> and [roadmap.md](roadmap.md) says when.

## Shape

LANcast is a single Go binary that owns a SQLite database and serves an HTTP
API. Clients are separate and thin. There is no daemon fleet, no message
broker, no external cache — a media server for a household does not need
distributed systems, and pretending otherwise is how self-hosted software
becomes unmaintainable.

```
                         ┌───────────────────────────────┐
   LANcast window ──────▶│  internal/api    HTTP layer   │
   browser               │  (internal/auth gates it)     │
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
        │ artwork, coverart,  │      │ transcode, subtitle    │
        │ artistart, probe    │      │                        │
        └─────────────────────┘      └────────────────────────┘
```

Every background worker and the playback path read and write through `store`;
nothing bypasses it.

## Packages

### The core path

**`cmd/lancastd`** — the server `main`. Parses flags, resolves config, opens the
store, constructs the workers and API, serves, and shuts down gracefully. It
contains wiring and nothing else; no logic lives here.

**`internal/config`** — resolves the data directory (platform config dir, or
`-data`), the listen address, and settings. Creates the data directory if
absent.

**`internal/store`** — the only package that speaks SQL. Owns `schema.sql`,
which is embedded and applied at open; every statement is `IF NOT EXISTS`, so
open is idempotent. Exposes typed methods (`CreateLibrary`, `UpsertItem`,
`ListItems`, `SaveProgress`, …) rather than a `*sql.DB`. This boundary is what
lets the storage layer change without a rewrite everywhere else.

**`internal/media`** — pure functions turning a path into a best guess:
`Parse(root, path) Info`, plus `IsVideo` and `SortTitle`. No I/O, no database,
fully unit-testable. Deliberately isolated: real metadata providers overwrite
these fields, and that must not require touching the scanner. It is also the
project's **one normalizer** — anything matching on titles reuses `clean` and
`SortTitle` from here.

**`internal/scan`** — walks a library's roots, decides what changed, and
reconciles the database against the filesystem.

**`internal/api`** — HTTP handlers over `net/http` with Go 1.22 pattern routing.
No third-party router; the standard library covers `GET /api/items/{id}`.

**`internal/web`** — embedded client assets via `embed.FS`, so a deployed
LANcast is genuinely one file. The React source lives in `web/` and is built
into `internal/web/dist`, which is committed.

### Metadata and artwork

**`internal/meta`** — the provider contract and the merge engine, and the
project's first real extension point. Holds `Provider` (searchable remote
sources) and `LocalSource` (path-based sidecar readers) as separate interfaces,
a `Registry`, confidence scoring, and field-level precedence resolution.
Everything downstream consumes a normalized `Record` and never learns which
source produced it. Subpackages `nfo/` and `tmdb/` are implementations behind
those interfaces; the plugin runtime registers into the same registry. See
[metadata.md](metadata.md).

**`internal/enrich`** — the worker that fills in metadata and artwork after a
scan. Scanning and enriching are separate phases on purpose: a large first scan
populates the grid from filenames in seconds while metadata fills in behind it.

**`internal/artwork`** — content-addressed image cache. The SHA-256 of the
source bytes is the image's identity, so a backdrop shared by several items is
stored once. Generates derived sizes on demand and is fully rebuildable:
deleting the cache directory must heal on next access.

**`internal/coverart`** — album artwork, in its own worker: embedded picture
first, then `cover.jpg`/`folder.jpg` beside the tracks. A directory's image is
refused when the directory also holds audio that is not the album's.

**`internal/artistart`** — artist images from TheAudioDB, name-keyed and opt-in
([ADR 0025](adr/0025-artist-images.md)). Until one arrives, an artist borrows
its most-substantial album's cover, flagged `inherited`.

**`internal/photo`** — picture decoding and thumbnailing. Everything except
HEIC is decoded in-process against fixtures, so the format table is testable in
milliseconds with no ffmpeg installed and no photos on disk.

### Playback

**`internal/probe`** — wraps ffprobe, persists results, and exposes `Decide()`,
which returns direct play, remux, or transcode *with a stated reason*.
`ParseJSON` is pure, so the decision rules are tested against fixtures with no
ffmpeg installed and no media on disk. Runs in its own worker, not inside
enrichment. See [ADR 0012](adr/0012-probe-before-transcode.md).

**`internal/transcode`** — runs ffmpeg to deliver media a client cannot play
directly. Argument construction is separated from process execution, so the
command line — where the subtle mistakes live — is testable without spawning
anything. One session machinery produces both a segmented output for files
([ADR 0050](adr/0050-a-converted-file-is-delivered-as-segments.md)) and the
live path. Hardware encoding is selected by a real test encode at startup, and
the decode method is named rather than left to ffmpeg's `auto` — see the
session-0 section of [CLAUDE.md](../CLAUDE.md), which exists because `auto`
cost a release.

**`internal/subtitle`** — discovery, extraction, and conversion. Browsers render
exactly one subtitle format, WebVTT; everything here exists to get text into
that format or to say clearly why it cannot. Includes OpenSubtitles search with
hash-first matching.

**`internal/playlist`** — a pure `.m3u` parser and a separate importer. Relative
paths, Windows separators read on Linux, a BOM, bytes that are not UTF-8: each
is a fixture needing no disk, no database, and no scanner
([ADR 0030](adr/0030-playlists-and-m3u.md)).

**`internal/livetv`** / **`internal/livebuf`** — channel sources, the EPG, and
the live buffer. The channel-list parser is deliberately not the playlist
parser: making one serve both would mean a mode flag deciding whether the
attributes matter, which is two parsers wearing one name.

**`internal/mediatools`** — fetches, verifies, and installs the pinned ffmpeg
build, and records its directory so a service account finds it
([ADR 0043](adr/0043-media-tools-are-fetched-not-bundled.md),
[ADR 0048](adr/0048-media-tools-install-themselves-on-first-run.md)).

### People, sharing, and other servers

**`internal/auth`** — password verification and server-side session tokens.
bcrypt cost 12 in `config.json` at `0600`; sessions are rows keyed by the
SHA-256 of a 32-byte token, so a stolen database grants no sessions. The schema
has been keyed by user since revision 1 ([ADR 0006](adr/0006-playback-state-keyed-by-user.md)),
so multi-user was an extension rather than a rewrite
([ADR 0011](adr/0011-single-password-with-server-sessions.md),
[ADR 0015](adr/0015-multi-user-accounts.md)).

**`internal/together`** — synchronised playback rooms. A second of drift is
acceptable for "we are watching this together"; frame accuracy is not the goal
and could not be delivered to three devices over a LAN in any case.

**`internal/identity`** — the server's own keypair and fingerprint
([ADR 0044](adr/0044-server-identity-and-peering.md)). Only the public half and
the fingerprint are exported: this project ships crash reporting, and a private
key that reaches a crash report is a key published to whoever reads it.

**`internal/peer`** — pairing between servers. Parsing an invite is not pairing;
pairing exists when each side has added the other, because a relationship one
party can create alone is one that can be created *at* you.

**`internal/presence`** — who is watching what, right now, between paired
servers ([ADR 0045](adr/0045-live-presence-between-paired-servers.md)). Never
written down: there is no history, and deliberately no "last seen watching".

**`internal/plugin`** — the WebAssembly runtime (wazero) and the isolation
boundary. A plugin cannot reach the filesystem, the network, secrets, or the
database. It returns data; the host owns all persistence
([ADR 0020](adr/0020-plugin-isolation-boundary.md),
[ADR 0021](adr/0021-plugin-distribution-and-trust.md)).

### The desktop and the executable

**`cmd/lancast`** — the client `main`: `LANcast-Client`, the launcher somebody
double-clicks. The other commands are tools rather than products —
`cmd/lcplugin` and `cmd/lcsign` build and sign plugin bundles, and
`cmd/hlsharness` exists to measure the live pipeline against a real channel,
which is how the ADR 0013 amendment's own premise was found to be false.

**`internal/clientwindow`** / **`internal/webview2`** — the WebView2 window the
client opens by default ([ADR 0023](adr/0023-native-desktop-client.md)). Pure
Go, `CGO_ENABLED=0`; the binding is a trimmed vendored copy with the embedded
DLL and its from-memory loader removed ([provenance](../internal/webview2/PROVENANCE.md)).

**`internal/certpin`** — pins **one** public key, read from the server's own key
material on local disk. Every other certificate is validated normally. This is
what lets the window talk to a LAN-bound self-signed server without a warning
it cannot resolve.

**`internal/tlscert`** — generates and persists the self-signed certificate, so
trust granted on first use survives a restart
([ADR 0014](adr/0014-transport-security.md)).

**`internal/service`**, **`internal/autostart`**, **`internal/singleton`**,
**`internal/raise`**, **`internal/childproc`**, **`internal/desktop`**,
**`internal/desktopprefs`** — the platform seams: service install and control,
the Windows run key, the one-instance-per-machine guard, raising the running
window instead of starting a second copy, launching a console program without a
console window, and the per-user preferences the server never learns about
([ADR 0022](adr/0022-client-and-server-executables.md),
[desktop-lifecycle-plan.md](desktop-lifecycle-plan.md)).

**`internal/update`**, **`internal/selfupdate`**, **`internal/release`** —
checking for, verifying, and staging an update. Failure is designed to be
inert: everything is verified before anything is moved, and a move that fails
puts the original back. The release key is separate from the plugin project key
on purpose — provenance for releases and provenance for plugins are different
trust domains.

**`internal/applog`**, **`internal/crashlog`**, **`internal/branding`** — the
log file a service writes because it has no console, the local crash record
(nothing is sent anywhere), and the icon compiled into both executables.

## Request lifecycle

1. `net/http` matches the method-and-pattern route.
2. Handler decodes and validates input. Validation happens here, not in `store`.
3. Handler calls one or more typed `store` methods.
4. Handler encodes JSON, or streams bytes for media.

Errors return a consistent JSON shape (see [api.md](api.md)). Handlers never
return raw SQL errors to clients. State-changing methods are origin-checked in
addition to `SameSite=Strict` — CSRF is defended twice and both halves stay.

## Streaming lifecycle

`GET /api/stream/{id}` is the one handler that turns database content into
filesystem access, so it carries an extra step:

1. Look up the item and its owning library.
2. Resolve the item path with `filepath.Abs`.
3. **Verify the resolved path is still inside the owning library root.** The
   database is trusted, but a bad or hand-edited row must not become arbitrary
   file read access. This check is mandatory, applies identically on every
   playback route, and must never be optimized away.
4. Hand off to `http.ServeFile`, which provides correct `Range` handling,
   `If-Modified-Since`, and seeking for free.

`/stream` is direct play — the file is served as-is. The decision step lives
beside it rather than inside it: the client asks
`GET /api/items/{id}/playback` for the probe's verdict and then picks its
source. Anything that is not direct play goes through a transcode session. A
direct-play guess that fails falls back to conversion once rather than showing
a black rectangle, and a client capability claim that proves false is dropped,
remembered for a fortnight, and retried.

## Scan lifecycle

1. Acquire the per-library scan lock. One scan per library at a time; a second
   request returns the in-progress status rather than queueing or racing.
2. Check each root reads. A root that does not is **skipped**, and
   reconciliation is per-root, so an unmounted drive marks nothing missing. A
   scan whose every root is unreadable fails rather than reporting success.
3. `filepath.WalkDir` from each readable root.
4. For each file the library's kind accepts, compare size and mtime against the
   stored row. Unchanged files are skipped without re-parsing — this is what
   makes rescans cheap on a large library. `scanned_at` is written only after a
   reconcile completes, so an interrupted scan does the full pass rather than
   trusting a half-built hierarchy.
5. Changed or new files go through `media.Parse` and are upserted, keyed on the
   unique `path` column.
6. Track every path seen. Anything under a readable root that was not seen is
   marked `missing = 1` — **never deleted.** A temporarily unmounted drive must
   not destroy library data, watch history, or user edits.
7. Progress is reported over a channel so the API can surface live scan state.

A scan reconciles *files*. It does not re-litigate *identity*: locked fields
and a `locked` match state are never touched, and a playlist carrying a
`members` lock is never re-imported.

## Data model

Revision 1 lives in `internal/store/schema.sql`: `meta`, `library`,
`media_item`, `playback_state`.

Three choices there are deliberate and documented as ADRs rather than left to be
rediscovered:

- `media_item` is **one wide table** with nullable `series`/`season`/`episode`
  rather than separate movie and episode tables — see
  [ADR 0002](adr/0002-one-wide-media-item-table.md). Music, pictures, and
  playlists have since all landed on it with no new item table, which is that
  claim surviving three media types that work nothing like video.
- `playback_state` is **keyed by `user_id`** even though M1 was single-user —
  see [ADR 0006](adr/0006-playback-state-keyed-by-user.md).
- File columns (`container`, `size_bytes`, `mtime`) are **nullable**, because
  `media_item` holds rows that are directories rather than files — see
  [ADR 0010](adr/0010-shows-as-media-items.md).

The `meta` table carries `schema_version`. It exists from revision 1
specifically so the first migration does not have to guess what it is migrating
from. Migrations run in order inside a transaction, gated on that value, and are
forward-only — there are deliberately no down migrations.

**`internal/store/migrate.go` is the authoritative list**, one commented
constant per revision, and it is worth reading rather than summarising here: at
33 revisions a table in this file is a second source of truth that goes stale.
Broadly, what has been added since revision 1:

| Revisions | What they brought |
|---|---|
| 2–6 | Match state, field locks, artwork, credits, genres, provider cache, `parent_id` for show → season → episode; sessions; probe results; external subtitles; frame rate |
| 7–11 | User accounts ([ADR 0015](adr/0015-multi-user-accounts.md)); collections and multi-part works ([ADR 0017](adr/0017-collections-and-multi-part-works.md)); the ignore list; external ratings and `imdb_id` ([ADR 0019](adr/0019-external-ratings.md)); installed plugins and their capability grants ([ADR 0021](adr/0021-plugin-distribution-and-trust.md)) |
| 12–16 | `pix_fmt` (bit depth decides direct play); track artist; cover-art bookkeeping; the audit log ([ADR 0026](adr/0026-audit-log.md)); pictures ([ADR 0028](adr/0028-pictures-library.md)) |
| 17–20 | Playlist membership ([ADR 0030](adr/0030-playlists-and-m3u.md)); a library in more than one place ([ADR 0034](adr/0034-multi-root-libraries.md)); colour metadata for HDR ([ADR 0033](adr/0033-hdr-tonemapping.md)); the library shape warning |
| 21–24 | Your own rating and note; per-account activity sharing ([ADR 0035](adr/0035-who-may-see-whose-viewing.md)); live TV channels and sources; EPG ids ([ADR 0036](adr/0036-epg.md)) |
| 25–29 | Re-parse bookkeeping; undoing seasons matched by name ([ADR 0040](adr/0040-a-season-is-not-a-searchable-work.md)); peers and presence grants ([ADR 0044](adr/0044-server-identity-and-peering.md), [ADR 0045](adr/0045-live-presence-between-paired-servers.md)); the `edition` column, which is so far a placeholder — [ADR 0049](adr/0049-an-edition-is-a-copy-of-a-work.md) is *proposed*, nothing populates it, and it is NULL on every row |
| 30–33 | Profile totals that survive a history reset; `watch_count`, because a boolean is wrong about how people watch things; lifetime viewings; dismissed collisions ([ADR 0042](adr/0042-two-files-one-work.md)) |

`CurrentSchemaVersion` is the single source of truth for where the chain ends,
and `internal/store` has a test asserting a freshly-created database matches it —
so `schema.sql` and the migration chain cannot silently diverge.

## What is deliberately absent

No ORM, no dependency injection framework, no service mesh, no Redis, no
separate job queue. Each of these would add operational surface to software
whose whole promise is that it runs unattended on a NAS for years. When one
becomes genuinely necessary, it gets an ADR.

**hls.js is not on the default path.** For films and episodes there is no
third-party player library involved at all, which is the part of
[ADR 0013](adr/0013-transcode-pipeline.md) that has never been reopened —
[ADR 0050](adr/0050-a-converted-file-is-delivered-as-segments.md) changed the
file path to segments and says so explicitly. hls.js **is** vendored, for live
TV only, behind a setting that is off by default, and it was taken on the
project's own terms rather than npm's: built from a pinned commit and proven
byte-identical to what upstream published, then vendored as the artefacts that
build produced. Loading it is its own chunk, so the main bundle does not carry
it. See [web/vendor/hls.js](../web/vendor/hls.js).

ffmpeg and ffprobe are external binaries rather than linked libraries — the one
deliberate process dependency, which is also why parsing and argument
construction are kept pure and separately testable. They are fetched on an
affirmative act by a present human, never bundled and never on a schedule.
