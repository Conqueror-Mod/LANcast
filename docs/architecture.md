# Architecture

> **Status:** scope froze at v0.11.0 on the way to 1.0; this was last brought
> up to date on 2026-11-16, after the whole-app review's fixes. The schema's
> revision is `CurrentSchemaVersion` in `internal/store/migrate.go`, and this
> document deliberately does not repeat the number. It describes the shape of
> the server as it stands; the ADRs say why, and [roadmap.md](roadmap.md) says
> when.

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

**`internal/serverapp`** — the server program. Parses flags and subcommands,
resolves config, opens the store, constructs the workers and API, serves, and
shuts down gracefully. Releases ship one executable built from `cmd/lancast`,
which is the app and, by subcommand (`LANcast.exe serve`, `reset-auth`, …),
this same server ([ADR 0080](adr/0080-one-executable-two-roles.md)).
`cmd/lancastd` remains as a three-line server-only entrypoint for development;
no logic lives in either `cmd`.

**`internal/config`** — resolves the data directory (platform config dir, or
`-data`), the listen address, and settings. Creates the data directory if
absent. Holds the TMDB and OpenSubtitles keys a release build carries, and the
one rule for which key is in use: the person's own, else the built-in one
([ADR 0110](adr/0110-lancast-carries-its-own-provider-keys.md)). Server-wide
switches live here too — among them `live_tv`, which is off (see
[Playback](#playback)).

**`internal/dataperm`** — gives the service's data directory permissions of its
own. As a Windows service that directory is under ProgramData, whose defaults
let every local account read the identity and TLS keys, `secrets.key`, the
database and the backups, and *create* files in any folder of it — a planted
update, restore or face worker was a way to SYSTEM that skipped UAC. The
directory is now owned by Administrators with a protected DACL: SYSTEM and
Administrators have full control, and Users may list the top folder's names
and read one file, the public certificate the desktop window pins
(`ShareCert`). It is applied by `service install`, by a switch to the service,
and checked on every service start, which leaves a folder already so alone
rather than walking tens of thousands of artwork files again. The cost is that working on
that folder by hand now needs an elevated shell.

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

**Live TV is behind a switch, and the switch is off.** Live TV is parked work
that 1.0 does not offer, so `live_tv` in `config.json` defaults to false, on
new servers and on every upgraded one, and nothing in the app turns it on.
While it is off every live TV route answers `409 live_tv_off`
(`internal/api/livetvswitch.go`) and the guide's twelve-hourly fetch does not
run. Off removes nothing: channel sources and the guide stay in the database
and come back as they were.

**`internal/mediatools`** — fetches, verifies, and installs the pinned ffmpeg
build, and records its directory so a service account finds it
([ADR 0043](adr/0043-media-tools-are-fetched-not-bundled.md),
[ADR 0048](adr/0048-media-tools-install-themselves-on-first-run.md)).

### People, sharing, and other servers

**`internal/auth`** — password verification and server-side session tokens.
bcrypt cost 12; sessions are rows keyed by the SHA-256 of a 32-byte token, so a
stolen database grants no sessions. The schema has been keyed by user since
revision 1 ([ADR 0006](adr/0006-playback-state-keyed-by-user.md)), so
multi-user was an extension rather than a rewrite
([ADR 0011](adr/0011-single-password-with-server-sessions.md),
[ADR 0015](adr/0015-multi-user-accounts.md)). Beside the password: an optional
authenticator code after it (`totp.go`,
[ADR 0100](adr/0100-a-code-after-the-password-for-whoever-wants-one.md)),
passkeys for the public name (`passkey/`,
[ADR 0082](adr/0082-a-passkey-belongs-to-the-public-name.md)). API keys for
third-party clients are handled in `internal/api` and are deliberately not
sessions ([ADR 0061](adr/0061-an-api-key-is-not-a-session.md)).

**`internal/picture`** — an account's uploaded picture
([ADR 0121](adr/0121-a-picture-of-your-own.md)). The upload is never kept: it
is decoded (JPEG, PNG or WebP, judged by the decoder, dimensions checked before
a pixel is allocated), cropped square, and re-drawn as two JPEGs of its own,
256 px and 64 px, which are what the database stores. EXIF, GPS and anything
hidden in the file go with the original. Pure and fuzzed: any bytes in give a
refusal or two valid JPEGs, never a panic.

**`internal/together`** — synchronised playback rooms. A second of drift is
acceptable for "we are watching this together"; frame accuracy is not the goal
and could not be delivered to three devices over a LAN in any case.

**`internal/remote`** — play on another of your own devices
([ADR 0117](adr/0117-play-on-another-device.md)). A *player* is a signed-in
window that has offered itself: it reports what it is playing every few
seconds, is gone after 20 s of silence, and collects the commands (play,
pause, seek, stop) another window of the same account queued for it. Keyed by
account first, so somebody else's player fails exactly as one that does not
exist. In memory and per process, like `together`: a window that is not open
is not a player, so there is nothing to keep and no schema.

**`internal/identity`** — the server's own keypair and fingerprint
([ADR 0044](adr/0044-server-identity-and-peering.md)). Only the public half and
the fingerprint are exported: LANcast writes crash reports to its own data
directory (nothing is sent anywhere), and a private key that reaches a crash
report is a key published to whoever later reads or shares that file. The key
signs — guest tickets, the peer TLS certificate, and an identity proof — but
never leaves the package as bytes. The proof
([ADR 0124](adr/0124-a-server-proves-its-identity-over-the-connection.md)) is
what a desktop client asks for when a known server's serving key changes:
`GET /api/identity/proof` signs the client's nonce together with keying
material exported from *that* TLS connection, so somebody in the middle, who
necessarily runs two connections, cannot pass the real server's signature on.
A valid proof accepts the new key with no person comparing fingerprints; an
invalid one is refused. The message layout lives once, in `proof.go`, because
the server signs it and `knownserver` verifies it.

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

**`cmd/lancast`** — `LANcast.exe`, the one executable a release ships: the
window and tray somebody double-clicks, and by subcommand the server
([ADR 0080](adr/0080-one-executable-two-roles.md)). What lives in it is the
entrypoint and the window's bindings — the frame-rate clock for visualizers
(`displayclock.go`), for one. `cmd/lancast-faces` is the face sidecar. The
other commands are tools rather than products — `cmd/lcplugin` and
`cmd/lcsign` build and sign plugin bundles; `cmd/hlsharness` exists to measure
the live pipeline against a real channel, which is how the ADR 0013
amendment's own premise was found to be false; and `cmd/segserve` and
`cmd/wv2harness`, each behind a build tag, reproduce HLS delivery and a bare
WebView2 window for problems only the desktop engine shows.

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

**`internal/publiccert`** — the opt-in public certificate: one hostname an
administrator names, issued over ACME TLS-ALPN-01 for the server's existing
serving key, and served only to clients that ask for that name
([ADR 0081](adr/0081-a-public-name-can-earn-a-public-certificate.md)). Off
until turned on; with it off, nothing here makes a request.

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
(nothing is sent anywhere), and the icon compiled into the executable.

### Watching, beyond the stream

**`internal/marker`** — finds where a film or an episode stops being itself:
where the credits begin and where a season's shared intro ends
([ADR 0054](adr/0054-a-marker-says-where-the-film-stops.md),
[ADR 0055](adr/0055-an-intro-is-what-every-episode-shares.md)). The decisions
are pure and tested against fixtures; the worker runs ffmpeg.

**`internal/preview`** — the frames over the scrub bar: a picture every ten
seconds, tiled into JPEG sheets, made once per file in the background
([ADR 0105](adr/0105-a-preview-is-a-keyframe-every-ten-seconds.md)).

**`internal/optimize`** — the rules of optimized copies, as pure functions:
what a copy is, when playback prefers it, how big it will be, and where it may
live ([ADR 0087](adr/0087-an-optimized-copy-is-converted-once.md)).

**`internal/rating`** — national certificates placed on one ladder of ages, so a
per-account ceiling can compare a BBFC 15 with a TV-14.

**`internal/mpv`** — libmpv driven through syscalls, the desktop window's own
player ([ADR 0067](adr/0067-the-desktop-client-plays-through-libmpv.md)), split
like `probe` into what the player is told and how its events are read. While
nothing is over a full-size film, the page's overlay window leaves the screen
so Windows can show the film without composing it
([ADR 0111](adr/0111-the-page-leaves-the-screen-over-a-film.md),
`internal/webview2/overlay_idle.go`).

**`internal/displaymode`** — refresh-rate matching for that player
([theater output plan](theater-output-plan.md), step 1): which mode suits a
film is pure and table-tested; changing it and putting it back, including after
a crash, is a thin Win32 layer. Only the monitor the video is on, never the
primary display.

**`internal/vsync`** — waits for one monitor's vertical blank
([ADR 0113](adr/0113-a-visualizer-keeps-the-screens-time.md)). Chromium ticks
`requestAnimationFrame` at one rate for the whole desktop, the main monitor's,
so a visualizer on a 30 Hz TV beside a 72 Hz monitor moved by 28 ms, then
42 ms, on alternate pictures. Only the native side can see the TV's own
refresh. Deliberately one call wide — open a monitor, wait, close; which
monitor and what to tell the page are `cmd/lancast/displayclock.go`'s, and the
page (`web/src/playback/frameDriver.ts`) moves to those ticks only when they
differ from rAF's rate.

**`internal/dlna`** — a DLNA/UPnP media server for televisions, browsing as a
chosen account ([ADR 0086](adr/0086-a-tv-browses-as-somebody.md)). On Windows it
binds the LAN address, not the wildcard, or SSDP hears nothing.

### Music, pictures, comics and games

**`internal/lyrics`** — words for a track from the disk they are already on: an
`.lrc` sidecar or the embedded tag. No provider, no network.

**`internal/milkdrop`** — accepts MilkDrop presets for the Now Playing
visualizer, checks them, and keeps them under the data directory
([ADR 0091](adr/0091-milkdrop-presets.md)).

**`internal/faces`** / **`internal/faceinstall`** — face grouping and photo
search, run in the `lancast-faces` sidecar
([ADR 0052](adr/0052-face-grouping-runs-in-a-native-sidecar.md),
[ADR 0060](adr/0060-semantic-photo-search-is-a-second-model-in-the-sidecar.md)),
and the on-request download of its models and runtime, which are not bundled.

**`internal/geo`** — names the town nearest a photo's GPS from a gazetteer built
into the binary ([ADR 0078](adr/0078-a-place-is-the-town-a-photo-was-taken-in.md)).

**`internal/comic`** — reads comics: a `.cbz` or `.cb7` archive's page images,
or a `.pdf` whose pages are the one picture each shows
([ADR 0101](adr/0101-a-comic-is-read-out-of-its-archive.md),
[ADR 0102](adr/0102-a-pdf-comic-is-its-pictures.md)). A `.cb7` stays open
between pages, because a solid archive's last page otherwise costs every page
before it.

**`internal/retro`** — retro games
([ADR 0073](adr/0073-a-retro-game-is-a-file-the-server-owns.md)): `romhash` and
`retrodb` identify a dump against pinned DAT files, `identify` is the worker,
`saves` keeps the server's copies, `libretro` and `host` run a core in the
desktop window, `webcores` in a browser
([ADR 0077](adr/0077-a-browser-can-play-a-game.md)), `cores` decides which
emulator core plays each console and fetches it from a pinned, checksummed
libretro release (fetched, never bundled), `remote` hands files and saves out
by ticket, and `ra` is the RetroAchievements profile
([ADR 0090](adr/0090-retroachievements-are-yours-to-connect.md)).

`cheevos` is *earning* achievements while playing
([ADR 0112](adr/0112-achievements-are-earned-through-the-server.md)): the
desktop player's binding to rcheevos, built from `third_party/rcheevos` into
`lancast-cheevos.dll` and loaded by syscall, with no cgo, as libmpv and the
cores are. rcheevos reads the game's memory in C every frame; only rare things
cross into Go. The package makes no HTTP calls of its own — every request goes
through the LANcast server, which holds the sealed RetroAchievements token,
forwards only the calls it allows, and keeps unlocks it could not deliver
until RetroAchievements answers.

**`internal/games`** — what the PC game launchers have installed on *this*
machine, read locally ([ADR 0066](adr/0066-a-game-belongs-to-the-machine-it-is-installed-on.md)).

### Running it, and keeping it safe

**`internal/backup`** — the folder backups live in: names, which ones this
build can restore, and the containment rule that stops a name in a URL becoming
a path. The snapshot itself is `store`'s
([ADR 0058](adr/0058-a-backup-is-the-database.md)).

**`internal/tasks`** — the board behind Status › Scheduled tasks: every
background job, what sets it off, when it last ran, and a way to run it now.

**`internal/history`** — the dashboard's minute-by-minute record, a ring kept 48
hours and saved beside the database
([ADR 0106](adr/0106-the-dashboard-remembers-two-days.md)).

**`internal/events`** — tells open windows that something changed. It carries
names, never data: a topic means "ask again"
([ADR 0079](adr/0079-the-server-says-when-something-changed.md)).

**`internal/webhook`** — delivers events to URLs an administrator set, signed
with each webhook's secret; nothing without one
([ADR 0097](adr/0097-a-webhook-goes-where-the-owner-points-it.md)).

**`internal/report`** — Report an issue: posts a report only when Send is
pressed, to the one webhook a release build carries
([ADR 0109](adr/0109-a-report-goes-only-when-sent.md)).

**`internal/servermode`** — moves a library between the service's folder and the
person's own, which is what switching between a service and an app actually is
([ADR 0092](adr/0092-the-library-moves-with-the-server.md)).

**`internal/contain`** — is this path inside that folder, with links followed.
Every handler that turns a row into a path asks it
([ADR 0094](adr/0094-containment-follows-links.md)). A lexical
`filepath.Abs` + `filepath.Rel` check passed `film.mkv` linking anywhere on the
machine, which is why nothing hand-rolls one. `EntryWithin` is the variant for
removing a path, where a link is judged by the folder it sits in, because
deleting a link deletes the link.

**`internal/sharedopen`** — opens a file for reading that its owner can still
delete or replace. Go's `os.Open` on Windows leaves out `FILE_SHARE_DELETE`, so
a film held by a stalled stream or a `.cb7` kept open on the comic shelf would
make Explorer refuse to delete it for as long as the handle lived.

**`internal/imagecap`** — bounds a picture's size before it is decoded. Go's
decoders allocate the whole pixel buffer from the header: a 204-byte PNG can
ask for 4 TB, and the runtime's out-of-memory is not a panic anything can
recover. One such file in a library was a server that could not stay up, so
every decode of a picture from a library, an archive or the artwork cache
reads the header first and refuses anything too large.

**`internal/diskuse`** — what LANcast keeps on disk, for Settings: one walk of
the data folder with each file counted against the part it belongs to
(database, artwork, previews, backups), and the largest unclaimed entries named.

**`internal/projectdocs`** — tests and nothing else: prose checked against the
thing it describes, as `apidoc_test.go` and `openapi_test.go` are for the API.
CLAUDE.md's layout map must name directories that exist, README's count of
decision records must match `docs/adr`, and the public list of emulator cores
must match the code.

**`internal/netguard`** — an HTTP client that will not reach the machine it runs
on or the network around it: what artwork downloads and plugins fetch through,
since both reach for URLs a provider or a plugin chose.

**`internal/secret`** — seals small secrets for the database with AES-256-GCM
under a key kept outside it.

**`internal/guestticket`** / **`internal/knownserver`** — the short-lived
credential that admits somebody from a paired server
([ADR 0046](adr/0046-remote-guests.md)), and the desktop client's record of
which servers it trusts, and by which key
([ADR 0070](adr/0070-the-desktop-client-can-trust-a-server-it-did-not-install.md)).

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

1. Look up the item for the caller — which also answers whether this account
   may see it — and the library location it was scanned under.
2. **Verify the path is still inside that location**, through
   `internal/contain` (`containedPath` in the API), which resolves links
   before it compares. The database is trusted, but a bad or hand-edited row,
   or a file that is really a link, must not become arbitrary file read
   access. This check is mandatory, applies identically on every playback
   route, and must never be optimized away.
3. Open it with `internal/sharedopen`, so a person can still delete or replace
   the file while it plays.
4. Hand off to `http.ServeContent`, which provides correct `Range` handling,
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
constant or entry per revision, and it is worth reading rather than summarising
here: a table in this file is a second source of truth that goes stale, and
this one had stopped at 33 while the code went past 90. Broadly, what has been
added since revision 1 — a revision's meaning never changes once released, so
these rows stay true; only the end of the table falls behind:

| Revisions | What they brought |
|---|---|
| 2–6 | Match state, field locks, artwork, credits, genres, provider cache, `parent_id` for show → season → episode; sessions; probe results; external subtitles; frame rate |
| 7–11 | User accounts ([ADR 0015](adr/0015-multi-user-accounts.md)); collections and multi-part works ([ADR 0017](adr/0017-collections-and-multi-part-works.md)); the ignore list; external ratings and `imdb_id` ([ADR 0019](adr/0019-external-ratings.md)); installed plugins and their capability grants ([ADR 0021](adr/0021-plugin-distribution-and-trust.md)) |
| 12–16 | `pix_fmt` (bit depth decides direct play); track artist; cover-art bookkeeping; the audit log ([ADR 0026](adr/0026-audit-log.md)); pictures ([ADR 0028](adr/0028-pictures-library.md)) |
| 17–20 | Playlist membership ([ADR 0030](adr/0030-playlists-and-m3u.md)); a library in more than one place ([ADR 0034](adr/0034-multi-root-libraries.md)); colour metadata for HDR ([ADR 0033](adr/0033-hdr-tonemapping.md)); the library shape warning |
| 21–24 | Your own rating and note; per-account activity sharing ([ADR 0035](adr/0035-who-may-see-whose-viewing.md)); live TV channels and sources; EPG ids ([ADR 0036](adr/0036-epg.md)) |
| 25–29 | Re-parse bookkeeping; undoing seasons matched by name ([ADR 0040](adr/0040-a-season-is-not-a-searchable-work.md)); peers and presence grants ([ADR 0044](adr/0044-server-identity-and-peering.md), [ADR 0045](adr/0045-live-presence-between-paired-servers.md)); the `edition` column, read from a filename's edition marker — a scan now fills it on older rows too (`internal/scan/edition.go`), but the feature built on it, [ADR 0049](adr/0049-an-edition-is-a-copy-of-a-work.md), is still *proposed* |
| 30–33 | Profile totals that survive a history reset; `watch_count`, because a boolean is wrong about how people watch things; lifetime viewings; dismissed collisions ([ADR 0042](adr/0042-two-files-one-work.md)) |
| 34–43 | Sensitive pictures ([ADR 0051](adr/0051-sensitive-content-is-obscured-until-asked-for.md)); faces, clusters and rejections ([ADR 0052](adr/0052-face-grouping-runs-in-a-native-sidecar.md)); intro and credits markers ([ADR 0054](adr/0054-a-marker-says-where-the-film-stops.md), [ADR 0055](adr/0055-an-intro-is-what-every-episode-shares.md)); music grouping keys; photo embeddings ([ADR 0060](adr/0060-semantic-photo-search-is-a-second-model-in-the-sidecar.md)); API keys ([ADR 0061](adr/0061-an-api-key-is-not-a-session.md)); tags and favourites ([ADR 0062](adr/0062-a-tag-is-a-note-to-yourself.md)) |
| 44–53 | A plugin's own credential; re-running the marker passes; a per-account rating ceiling; audio and subtitle language preferences; shared libraries and a friend's progress on the friend's server; the account's chosen picture; a watch history, one row per viewing ([ADR 0074](adr/0074-a-viewing-is-a-row.md)) |
| 54–69 | Marker re-decisions; exact and near-copy photos ([ADR 0075](adr/0075-a-duplicate-is-the-same-bytes.md)); retro games and their saves ([ADR 0073](adr/0073-a-retro-game-is-a-file-the-server-owns.md)); photo places ([ADR 0078](adr/0078-a-place-is-the-town-a-photo-was-taken-in.md)); indexes for the browse grid and genre chips |
| 70–82 | Passkeys ([ADR 0082](adr/0082-a-passkey-belongs-to-the-public-name.md)); subtitle style; the device behind each session; intro skipping ([ADR 0083](adr/0083-an-intro-skips-itself.md)); libraries kept from an account ([ADR 0084](adr/0084-a-library-can-be-kept-from-an-account.md)); the watchlist ([ADR 0085](adr/0085-a-watchlist-is-a-list-of-promises.md)); optimized copies ([ADR 0087](adr/0087-an-optimized-copy-is-converted-once.md)); a RetroAchievements account ([ADR 0090](adr/0090-retroachievements-are-yours-to-connect.md)); a collection's missing films ([ADR 0093](adr/0093-a-collection-knows-its-missing-films.md)); webhook sharing; skipping pre-roll ([ADR 0099](adr/0099-a-preroll-plays-before-a-film-from-the-start.md)); two-factor sign-in ([ADR 0100](adr/0100-a-code-after-the-password-for-whoever-wants-one.md)) |
| 83–90 | Where each person is in each comic ([ADR 0101](adr/0101-a-comic-is-read-out-of-its-archive.md)); invites ([ADR 0104](adr/0104-a-friend-is-invited-by-a-link-that-works-once.md)); scrub-bar preview bookkeeping ([ADR 0105](adr/0105-a-preview-is-a-keyframe-every-ten-seconds.md)); ReplayGain ([ADR 0108](adr/0108-music-is-levelled-by-its-own-replaygain.md)); the sealed RetroAchievements token and unlocks waiting to be sent ([ADR 0112](adr/0112-achievements-are-earned-through-the-server.md)); music genres ([ADR 0114](adr/0114-a-music-library-is-browsed-at-three-levels.md)); MusicBrainz albums ([ADR 0115](adr/0115-albums-are-identified-by-musicbrainz.md)); a book's speed ([ADR 0116](adr/0116-an-audiobook-is-its-own-library.md)) |
| 91–97 | Pinned shelves ([ADR 0119](adr/0119-homepage-2-part-two.md)); Home, reading and listening defaults per account ([ADR 0120](adr/0120-a-setting-belongs-to-what-it-describes.md)); an uploaded picture ([ADR 0121](adr/0121-a-picture-of-your-own.md)); a library's own options ([ADR 0123](adr/0123-a-library-carries-its-own-options.md)); data fixes that send rows back through a worker — music tags read again, films holding a provider's runtime probed again |

Many revisions are data fixes rather than shape: they clear a worker's stamp so
rows go through it again. Such a migration does nothing until that worker
runs, which is why the recent ones say in their comment that their worker is
kicked at startup rather than left waiting for a scan.

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
