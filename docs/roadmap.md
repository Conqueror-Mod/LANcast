# Roadmap

Last updated: 2026-08-08 · **v0.5.0 released · M0–M4 built.** The React client executes the design
system and the client-UX backlog is closed. Observability (match, review, scan
diagnostics) and CI are in place. Transport security (TLS) and multi-user
accounts (admin/member roles) are built, and branding & splash shipped.

**Since v0.5.0, unreleased on `main`:** the music client UI is finished, LANcast
has a native window on Windows, and clients now tell the server what they can
decode. Details in the areas below; the three things worth carrying forward are
in "What the last pass taught" at the end.

**Music libraries shipped in v0.5.0** ([ADR 0024](adr/0024-music-libraries.md)),
which is the first media type past video and therefore the first real test of
the claim ADR 0002 made: that a new kind needs no new tables. It holds — music
is three new `kind` values on `media_item` related by `parent_id`, exactly as
show → season → episode already was. Metadata inverts the video rule: for a film
the filename is a guess and a provider corrects it, but a music file already
carries the answer in its tags, so tags win and the filename is the fallback.
The release was **server-side only**. Both gaps are now closed on `main`: album
artwork is extracted, and the client has an album view, a track list, an audio
mode and a docked mini-player. What ADR 0024 scoped is done; **artist images
from a provider are on the back burner** (see below), not because they are hard
but because music has had a long run and the rest of the map has waited.

**Plugin architecture (M4) is built** — the last milestone. A WebAssembly runtime
([ADR 0020](adr/0020-plugin-isolation-boundary.md)) sandboxes third-party code
behind a deny-by-default capability model, plugins register into the same
interfaces the native sources use ([ADR 0007](adr/0007-provider-and-localsource-split.md)),
and a signed-bundle install flow with a two-layer trust model — provenance
(Ed25519 signing) and authority (an explicit capability grant) — surfaces on a
Settings → Add-ons page ([ADR 0021](adr/0021-plugin-distribution-and-trust.md),
[plan](plugin-distribution-plan.md)). The contract is validated by OMDb
reimplemented as a first-party plugin that produces ratings byte-identical to the
native source. All four founding principles now hold in shipped code.

The **browse-experience backlog shipped** in three PRs
([plan](browse-experience-plan.md)): media-type-aware library views, Plex-style
multi-select filters (genre, decade, content rating) with per-library counts and
an unwatched toggle, and a ratings display with a rating sort. What remains of it
was **external ratings** (Rotten Tomatoes / Metacritic / IMDb via OMDb), specced
in [ADR 0019](adr/0019-external-ratings.md) and now **built** — leaving plugin
architecture (M4) as the last milestone.

The two early-lock Foundation decisions are now **built, not just decided**: the
data model past revision 1 ([ADR 0017](adr/0017-collections-and-multi-part-works.md),
schema at **revision 13**) and the API contract ([ADR 0018](adr/0018-api-contract-and-versioning.md)).
On top of them, **media organisation shipped end to end** — collections, the
show → season → episode hierarchy, multi-part works and serials/miniseries, a
library-kind that drives movie-vs-TV matching, Fix match that reaches TV,
retroactive re-parse on rescan, Play-all queues, and Remove (ignore or delete,
with a sidecar sweep). Theme music (blocked on OST identification) is the
remaining M3 depth. Packaging & distribution is **built** — two branded
executables, a goreleaser matrix and a Windows installer
([ADR 0016](adr/0016-packaging-and-distribution.md), [ADR 0022](adr/0022-client-and-server-executables.md)) —
and has been since v0.3.2.

A **feature backlog is captured below.** With M4 built, what remains is breadth
(finishing music, a Pictures library, more plugin kinds, more client surfaces)
rather than foundational milestones.

## Releases

| Version | Date | What shipped |
|---|---|---|
| **v0.5.0** | 2026-08-03 | Music libraries — the first media type past video, and the first test of ADR 0002's claim that a new kind needs no new tables (it holds: three `kind` values on `media_item`, schema 13). Scans eleven audio formats, reads embedded tags as the authority rather than guessing from filenames, and groups tracks into artists and albums by *album artist* so compilations stay whole. Playback profiles gained audio containers, which is what stopped a `.flac` being re-encoded to AAC to deliver a format every browser plays natively. Server-side only: no music player in the client, no album artwork yet. |
| **v0.4.3** | 2026-08-02 | The two guards that keep the server honest, both broken in ways only a real install showed. The cross-session single-instance check failed open: Windows returns the same "access denied" whether an object exists and may not be opened or the caller cannot create it, so a desktop launch never saw a server running as a service and started a second one — the mechanism behind the two-servers and, before v0.4.1, two-databases problems. And v0.4.2's service log wrote nothing, because it was paired with a console that does not exist under the service manager using a writer that gives up on the first failure. |
| **v0.4.2** | 2026-08-02 | Makes a service-run server diagnosable. It had no console and no inherited stderr, so everything it logged was discarded by the operating system — when it exited, the only record anywhere was Windows' own "terminated unexpectedly", which cannot tell a crash apart from a kill. It now writes `lancastd.log` beside the database, one rolled generation capped at 4 MB, and every exit goes through it including a refusal to start. New installs also restart three times after an unexpected exit and then stop, rather than staying down unnoticed or looping on a server that genuinely cannot start. |
| **v0.4.1** | 2026-08-02 | Windows run environment, all of it found by installing v0.4.0 and using it. Child processes no longer open console windows — the app flashed three or four on launch and one per file on a scan, because a windowsgui parent gives every ffmpeg child a visible console. The HTTPS redirect is temporary rather than permanent: browsers cached the 301 forever, so a server that later dropped back to plain HTTP was unreachable with `ERR_SSL_PROTOCOL_ERROR`. The client no longer starts a server on a second, per-user database while the service uses the machine-wide one. Separate **LANcast Client** and **LANcast Server** Start menu entries. Add-library focuses its first field. |
| **v0.4.0** | 2026-08-02 | Playback decisions rewritten after a second real-library test: the chosen audio track now drives the decision (picking one produced `-c:a copy` on undecodable audio and silent playback), named client profiles so HEVC stops forcing a re-encode, copy gated on what MP4 can actually carry, `pix_fmt`-based 10-bit detection (schema 12), and audio no longer re-encoded alongside video for free. Adds **Re-read media files** for libraries probed by an older build, and `lancastd reset-auth` for lockout recovery. Fixes: the app opening to a tray icon and no window, NFO sidecars growing on every write, shows libraries counting seasons and episodes as items, a certificate warning on a loopback-only server, and a restart prompt that could not deliver. |
| **v0.3.2** | 2026-08-02 | First published release. M0–M4 plus packaging: two executables, Windows installer, service install. Fixes from the first real-library test — ffprobe unreachable under a service (which had left every file direct-played), a grid that stopped at 120 of 1,226, volume, filenames for Fix match, and two upgrade-path bugs. |

**What real-library testing taught, worth remembering:** every serious bug in
this release was invisible rather than loud. Nothing was probed and playback
silently degraded; the grid truncated under a count claiming the full total; the
launcher read a TLS error as "server down"; an old process survived an upgrade
and held a lock. The fixes each added a way to *see* the failure — a media-tools
row in Settings, an honest "120 of 1,226", a message box instead of a silent
exit. Prefer that to a quiet fallback.

**v0.4.0 repeated it, and added one.** The audio-track bug played films with no
sound and logged nothing; an impossible mux made ffmpeg refuse to start and the
player just died; NFO sidecars grew on every write and nobody would ever have
noticed. Same shape: the failure had no voice.

The addition is about *how the bugs were found*. Two claims made from reading
the code were wrong — a re-probe described as taking "hours" turned out to take
15 seconds per 225 files, and a shows library's item count was explained as
correct arithmetic when it was plainly wrong on screen. Both were caught by
running the thing and looking at the output. Four of the release's fixes were
found while verifying something else. **Reasoning about the code predicts; only
running it against real files reports.**

**v0.4.1 is the same lesson at the next layer down.** Every bug in it was found
by installing v0.4.0 and using it as a user would — not by reading code, not by
tests, all of which passed. Console windows flashing on launch, a browser that
would not connect, an empty second database, a Start menu entry that hid which
program it ran: none of these are visible from inside the repository, and none
of them would ever fail a test. The unit of verification that catches this
class is *the installed artifact on a real desktop*, and it deserves a pass of
its own before a release is called good.

**v0.4.2 and v0.4.3 close the loop by turning it on the diagnostics.** The
service log added in v0.4.2 wrote nothing under the service manager — it was
tested in a terminal, which is the one environment it is not for. The
single-instance guard had been failing open since it was written, and was found
only because a real service happened to be running during an unrelated test.
Both are the same mistake in a new place: **the environment a check runs in is
part of the check.** A guard verified somewhere easier than production has not
been verified.

**v0.5.0 adds a different shape: a fix with two halves, one of them invisible.**
ADR 0024 named the audio-container problem precisely and pointed at
`probe.Profile`. Adding the containers there was correct and, on its own, would
have done nothing for `.m4a` or `.opus` — because decisions are made from the
*stored* extension mapped into ffprobe's vocabulary by `containerFromExtension`,
and that mapping knew only video. Profiles would have listed `ogg` while every
`.opus` file still arrived as `opus` and matched nothing. The tests would have
passed, the ADR would have been satisfied, and those files would have transcoded
forever with no stated reason. **An ADR names the decision, not the full set of
places the decision touches.** The second half was found by asking what actually
produces the value being compared, rather than trusting that the obvious place
was the only place.

## Ordering principle

**Plan an area immediately before building it, not long before.** Specifying the
plugin contract today would mean designing an extension API with no extensions
to validate it against — which is exactly how these projects calcify around the
wrong abstractions.

The exception is anything that constrains the **schema** or the **API
contract**. Those get decided early because they are expensive to retrofit. Two
are already handled in schema revision 1: `playback_state` carries a `user_id`
before multi-user exists, and `media_item` does not hardcode a media taxonomy.

## Milestones

| | Milestone | Definition of done | |
|---|---|---|---|
| M0 | Library scan | Point at a folder, get rows in a database | **done** |
| M1 | **Watch something** | Browse in a browser, click, play, seek, resume | **done** |
| M2 | Metadata | Real titles, artwork, seasons, OST identification | **done** |
| M3 | Transcoding + real client | Plays anywhere; React client executes the design | **done** |
| M4 | Extensibility | Plugin runtime with first-party plugins proving the contract | **done** |

M1 is the milestone that matters. Everything before it is scaffolding and
everything after it is depth.

## Areas

Status: **planned** · **next** · *unplanned*

### Foundation · M0–M1

| Area | Status | Note |
|---|---|---|
| Server core architecture | **built** | Go, SQLite, scan → browse → play |
| UI/UX design system | **built** | Nebula field, gold rule, keyboard model — executed by the React client, not just the tokens |
| Data model evolution and migrations | **built** | Forward-only migrations (rev 1→13); collections, hierarchy, multi-part & serial works ([ADR 0017](adr/0017-collections-and-multi-part-works.md)) |
| API contract and versioning | **built** | URL-path versioning, `/api` ≡ v1, additive-safe rule ([ADR 0018](adr/0018-api-contract-and-versioning.md)); `child_count`, `collection_id`, cross-type match |

### Metadata and artwork · M2

| Area | Status | Note |
|---|---|---|
| Provider interface | **built** | Scraper contract; first real extension point |
| Matching and confidence | **built** | Wrong-match correction; library-kind biases movie-vs-TV; Fix match reaches TV, not just film |
| Media organisation | **built** | Collections, show→season→episode, multi-part works, serials/miniseries; retroactive re-parse; Remove (ignore/delete) ([ADR 0017](adr/0017-collections-and-multi-part-works.md)) |
| Artwork pipeline | **built** | Fetch, cache, resize; fanart for detail pages; art-less children inherit the parent poster |
| External ratings | **built** | RT / Metacritic / IMDb via OMDb; `RatingSource` + `item_rating` side table + `imdb_id` ([ADR 0019](adr/0019-external-ratings.md)) |
| OST identification | *unplanned* | Feeds theme music; MusicBrainz / TheAudioDB |
| Library types beyond video | **partly built** | **Music built** server-side ([ADR 0024](adr/0024-music-libraries.md)): artist → album → track on `media_item`, no new tables — the taxonomy claim holds. Photos still *unplanned* and need their own ADR |
| Embedded tags as a source | **built** | ID3v2 / Vorbis / MP4 atoms via the probe that already runs. Authority order for a track: locked fields, tags, folder, filename — the inverse of video, because the file carries the answer |
| Album artwork | **built** | `internal/coverart`: embedded picture first, then `cover.jpg`/`folder.jpg` beside the tracks, in its own worker. Measured on the real library — 369 of 398 albums, 10.7s, no network. A directory's image is refused when the directory also holds audio that is not the album's, which is what stops a letter-bucket `folder.jpg` being worn by five unrelated records |
| Artist images | **back burner** | The placeholder is good enough to wait behind: artists **borrow** their most-substantial album's cover, flagged `inherited`, and a real image supersedes it automatically with nothing to clean up. TheAudioDB, name-keyed and opt-in, is the decided source ([ADR 0025](adr/0025-artist-images.md), accepted, unbuilt) — it was sequenced after the client UI, which is now built, so nothing blocks it except priority. Deferred deliberately: music has had a long run and this is the first item where the gap is cosmetic rather than functional |
| Album artist and year | **built** | Album rows carried a title and nothing else — 398 albums with no artist and no year, which read as three separate faults: a bare detail page, a Year sort with nothing to sort, and a track list that showed every performer because it had no album artist to compare against. Both are now derived from the tracks on every scan, and locks are respected |

### Playback and client · M3

| Area | Status | Note |
|---|---|---|
| Media probing | **built** | ffprobe; codecs, duration, tracks |
| Transcode decision tree | **built** | Direct play / remux / transcode, with reasons |
| Client capability negotiation | **built** | Clients report what they decode (`?can=`) and the server widens the profile ([plan](client-capabilities-plan.md)). `?profile=` had existed for a release and no client ever used it, so a browser that decodes HEVC in hardware was still served a full re-encode of every HEVC file — the whole of the "slow between films" complaint. Additive and widen-only, resolved once for both decision endpoints so they cannot disagree, and a claim that proves false is dropped, remembered, and retried as a conversion |
| ffmpeg pipeline and HLS | **built** | Progressive fMP4 + HLS, session lifecycle |
| Hardware acceleration | **built** | NVENC, QSV, AMF, VideoToolbox — verified by test encode |
| Subtitles | **built** | Sidecar, embedded, WebVTT, OpenSubtitles hash matching |
| React client build | **built** | React + TS + Vite; Home shelves, Browse, Detail, Player, Settings; subtitles local + online; central spatial focus controller (ADR 0004) |
| Theme music subsystem | specced | Behavior in design.md; blocked on M2 |
| Music player UI | **built** | Album view with a numbered track list, square sleeves, an audio mode in the player, and a docked mini-player so leaving the player no longer stops the record ([plan](music-client-plan.md)). Playback moved above the router to make that possible — the media element used to be a child of the `/watch` route, and a route owns its DOM |
| Branding & splash | **built** | App icons + favicon from the emblem, web manifest, and a once-per-session animated splash. Source art in `/assets` |

### Extensibility · M4

| Area | Status | Note |
|---|---|---|
| Plugin runtime and sandbox | **built** | WebAssembly via wazero, deny-by-default capabilities ([ADR 0020](adr/0020-plugin-isolation-boundary.md)); validated by OMDb-as-plugin |
| Extension point catalog | **built** | `rating_source` first; new source for an existing capability. Widening to new kinds waits for a plugin that needs it |
| Plugin distribution and trust | **built** | Signed `.lcplugin` bundles, two-layer trust (Ed25519 + capability grant), two-step install, Add-ons page ([ADR 0021](adr/0021-plugin-distribution-and-trust.md)) |
| Client surfaces: TV, mobile | *unplanned* | A restyle, if the focus model held |

### Native desktop client · ADR 0023

| Area | Status | Note |
|---|---|---|
| Stage 1 — own the window | **built, opt-in** | `LANcast-Client.exe -window` opens a WebView2 window instead of handing a URL to a browser ([plan](native-client-plan.md)). **Pure Go, `CGO_ENABLED=0`** — the ADR's assumed CGO cost was wrong, tested rather than argued, so the single-runner release matrix survives. The binding is a trimmed vendored copy with the embedded DLL and its from-memory loader removed ([provenance](../internal/webview2/PROVENANCE.md)); Microsoft's signed loader ships beside the executable |
| Certificate trust | **built** | The point of owning the window, and worse than the ADR assumed: against a LAN-bound server the web view does not warn, it fails the handshake and retries, so the app never loads. The client pins the server's public key, read from its own `cert.pem` on local disk; every other certificate is still validated |
| Flip `-window` to default | **next** | Gated on living with it rather than on code. One evening on one machine is not "survived real use" — the unproven list is in the plan: no installed artifact, no fresh profile, no machine without the evergreen runtime, no DPI or multi-monitor check |
| Stage 2 — own playback (libmpv) | *unplanned* | Deliberately not started. Its case is narrower than the ADR first made it — see the 2026-08-08 amendment: HEVC left the list |

### Cross-cutting

| Area | Status | Note |
|---|---|---|
| Users, auth, sessions | **built** | Multi-user accounts with admin/member roles (ADR 0015); per-user watch state |
| Remote access | documented | VPN or reverse proxy; see security.md |
| Security model | **built** | Auth, CSRF, throttling, loopback-until-secured |
| Transport security (TLS) | **built** | HTTPS beyond loopback; bring-your-own or self-signed cert, http→https redirect (ADR 0014) |
| Performance targets | *unplanned* | Budgets for a 40k-item library |
| Packaging and distribution | **built** | Two branded executables, goreleaser matrix, in-binary service install, signed-tag releases with a Windows installer ([ADR 0016](adr/0016-packaging-and-distribution.md), [ADR 0022](adr/0022-client-and-server-executables.md)) |
| Backup and restore | *unplanned* | Rebuild a library without a full rescan |
| Activity view | **built** | `GET /api/activity` in one shape for every worker; a nav indicator and task popover. Indeterminate where the worker genuinely cannot know its total — a scan discovers its own size, so it shows a count rather than a lying percentage |
| Observability | **built** | Match score breakdown, review queue, scan skip diagnostics, and a single-instance guard that names what is holding it — the service, its pid and its data directory, read at a privilege an unelevated caller actually has |
| Desktop lifecycle | **planned** | *Open on Windows start* and *Close to tray* ([plan](desktop-lifecycle-plan.md)). "Closed" means three different things today — a browser tab, tray Quit, and the service — and all three behave correctly, which is why it confuses. Both options are client-local; gated on the `-window` flip, because close-to-tray has no referent while the UI is a browser tab |
| Testing strategy | **built** | CI runs go test + client build + bundle-drift check; fixture libraries, no real media |
| Licensing and open-sourcing | *unplanned* | Decided before the repo goes public |

## Client UX backlog

Noted from use of the old single-file client, and largely resolved by the React
rebuild, which split the player dialog into distinct screens.

1. ~~**Separate the information screen from the player.**~~ **Done** — clicking a
   poster opens the full-bleed detail page (synopsis, cast, artwork) with a
   **Play** button; playback is its own screen.
2. ~~**Play the official trailer on the information screen.**~~ **Done** — a
   Trailer button opens a lightbox that embeds and autoplays the provider's
   trailer, on the detail page.
3. ~~**Subtitles belong to the player, not the preview.**~~ **Done** — the picker
   lives in the player, with local tracks, online search, and removal.
4. ~~**Reposition "fix match".**~~ **Done** — metadata correction is a modal on
   the detail page, with a score breakdown that explains each candidate, and a
   dedicated Review screen queues everything the matcher was unsure about.

All four are resolved; the backlog is closed.

## Feature backlog

Captured, not yet designed. Each of these is planned immediately before it is
built, not here — this section is the running list of *what*, so nothing is
lost, deliberately ahead of the *how*. Grouped for legibility; order within a
group is not priority.

### Pages and navigation

- **More branded, thematic home page** — beyond functional shelves.
- **Auto-expanding / collapsing navigation bar** — expands on hover/focus,
  collapses otherwise (the Plex behaviour).
- ~~**Movie library page** and **TV-show library page**~~ — **done**
  (Phase 1): media-type-aware browse views selected by library kind.
- **Add-ons page** — placeholder for now; add-ons themselves come later in the
  app's life (M4 territory).
- **More defined settings page** — a real structure, not a flat list.
- **Downloads page** and a **download handler** that manages downloaded content
  from any library, any library type, or any add-on.
- **Profile page** (details under Social and profiles below).
- **Bigscreen (10-foot) mode** — with a settings option to enable it at startup.

### Libraries and media types

- ~~**Wide-scope audio codec support** — MP3, FLAC, WAV.~~ — **done**: eleven
  audio formats scanned, and audio containers are first class in the playback
  profile so a FLAC is not re-encoded to deliver a format every browser plays.
- ~~**Music library.**~~ — **done**, end to end: server-side in v0.5.0, client
  UI and mini-player on `main`.
- **Photo library** with a built-in **image viewer**.
- **Live TV** — a tuner page and function.

### Metadata, ratings and discovery

- ~~**Ratings system**~~ — **done**: TMDB rating display + rating sort (Phase 3),
  and the **Metacritic / Rotten Tomatoes / IMDb** tie-in via OMDb
  ([ADR 0019](adr/0019-external-ratings.md)).
- ~~**Plex-style filter settings**, with a **total movie/show count per library
  page**~~ — **done** (Phase 2): multi-select genre/decade/content-rating
  filters, unwatched toggle, per-library counts in the nav.

### Social and profiles

- **Profile page**: watch/play/view history, Find Friends, Trending (trends
  computed per library), ratings/reviews, a viewer-stats list, profile edit,
  and profile information.
- **Watch Together** — synchronised playback across viewers.
- **Better profile manager.**

### System, operations and diagnostics

- ~~**Activity status in the UI**~~ — **built.** `GET /api/activity` answers
  "what is the server doing right now?" in one request, normalizing scan,
  enrich, probe, coverart and transcode into one shape, and the shell shows a
  pulsing indicator with a popover listing each task. The per-worker endpoints
  each answered for one worker, which meant a client wanting to show *anything*
  had to know the whole roster and poll `/api/libraries/{id}/scan` once per
  library — the capability existed and no caller could reasonably use it. A
  failed scan stays listed, because the recurring bug shape in this project is a
  failure with nowhere to appear.
- **Audit log — who changed what, and when.** Title removed, library added,
  match overridden, account created: recorded server-side with the user and the
  time. Server-side is the whole point — an audit trail a client writes is
  forgeable by the client it is auditing, so this stays where the mutation is
  authorised, not where it is requested. The absence of one is why "what
  emptied this library" was unanswerable during v0.4.x testing. Pairs with the
  question of whether identity should live in its own store rather than beside
  the library, so losing a password never opens the file holding the media.
- **Crash reporting.**
- **Debug logging** and an **internal log viewer** — the service now writes
  `lancastd.log` beside the database (v0.4.2); what is missing is reading it
  from the UI.
- **Clear cache and data** and **reset settings** actions.
- **Check for updates** with an **auto-update** toggle.
- **Desktop lifecycle — "Open on Windows start" and "Close to tray"**
  ([plan](desktop-lifecycle-plan.md)). "Closed" means three different things
  today — a browser tab, tray Quit, and the service — and all three behave
  correctly, which is why it confuses. Both options are client-local, no API and
  no schema change, and both are **gated on [ADR 0023](adr/0023-native-desktop-client.md)
  stage 1**: close-to-tray has no referent while the UI is a browser tab whose
  ✕ LANcast does not own.

### Input and control

- **Keyboard-control shortcut map and customizer** — building on the existing
  spatial focus model (ADR 0004).

### Resolved modeling question — multi-part and serial works

**Decided in [ADR 0017](adr/0017-collections-and-multi-part-works.md) and now
built end to end.** The four cases split on one axis — are the pieces
independent works or parts of one work? Independent works that continue a story
are a **collection** (many-to-many membership, a side table; members stay
top-level). Pieces of one work are **containment** via `parent_id`, with `kind`
values `part`/`chapter` and a `serial` kind for a closed, play-through-whole
story. All of it is implemented: TMDB `belongs_to_collection` ingestion, the
scanner's grouping heuristics, the client's members/parts views, Play-all, and
a library-kind that routes a miniseries to TV matching. Every motivating case
now works — Storm of the Century matches its miniseries, Toy Story's collection
groups, Baahubali is one work in two parts. Original framing kept for the
record:

- **Storm of the Century** — a Stephen King TV miniseries (one story, several
  parts).
- **Anne of Green Gables / Anne of Avonlea** — a film series that is one
  continuing story.
- **1940s Batman / Superman serials** — chaptered theatrical serials.
- **Baahubali** — a two-part film that is one work.

These do not fit "movie" or "episode of a show" cleanly, and the taxonomy is
deliberately open ([ADR 0002](adr/0002-one-wide-media-item-table.md)); this is
where that openness gets exercised.

## Dependencies that constrain ordering

- **Theme music → M2.** Needs TVDB ids and OST identification. Cannot land sooner.
- **TV client → keyboard focus model.** The roving-tabindex controller is the TV
  client's foundation. Compromise it during M3 and the TV client becomes a
  rewrite instead of a restyle.
- **Plugin contract → one full build of the core.** Deliberately last.
- **Users and auth → schema.** Already handled; can arrive late without data loss.
- **API versioning → before any third-party client exists.** Cheap now, breaking later.

## Next planning order

1. ~~Metadata and artwork (M2)~~ — **built.** See
   [metadata.md](metadata.md) and ADRs 0007–0010.
2. ~~Transcoding + React client (M3)~~ — **built.** Client executes design.md;
   theme music remains, blocked on OST identification.
3. ~~Security and remote access~~ — **transport security and multi-user
   accounts built** ([ADR 0014](adr/0014-transport-security.md), [ADR 0015](adr/0015-multi-user-accounts.md)).
4. ~~Data model past revision 1 + media organisation~~ — **built** (ADRs
   [0017](adr/0017-collections-and-multi-part-works.md)/[0018](adr/0018-api-contract-and-versioning.md)):
   collections, hierarchy, multi-part works, library-kind matching, delete/ignore.
5. ~~Browse-experience feature backlog~~ — **built** in three PRs
   ([plan](browse-experience-plan.md)): media-type library pages, Plex-style
   filters, per-library counts, ratings display.
6. ~~External ratings (RT/Metacritic/IMDb)~~ — **built**
   ([ADR 0019](adr/0019-external-ratings.md)): OMDb `RatingSource`, `item_rating`
   side table, `imdb_id` from TMDB, enrichment pass, detail display.
7. ~~Plugin architecture (M4)~~ — **built** across a runtime and a distribution
   flow ([ADR 0020](adr/0020-plugin-isolation-boundary.md), [ADR 0021](adr/0021-plugin-distribution-and-trust.md);
   [runtime plan](plugin-runtime-plan.md), [distribution plan](plugin-distribution-plan.md)):
   WASM sandbox, capability model, signed-bundle install with a two-layer trust
   model, Add-ons page, validated by OMDb-as-plugin.
8. ~~Music libraries~~ — **built server-side** and shipped in v0.5.0
   ([ADR 0024](adr/0024-music-libraries.md)): audio file types behind a
   kind-aware scan gate, embedded tags as an authoritative local source, the
   artist → album → track hierarchy, untagged-track scan diagnostics, and audio
   containers in the playback profile.
9. **Finish music.** Artwork came first — it was far smaller, it was
   server-side, and building the client grid against blank tiles would have
   meant designing it twice.
   1. ~~Album artwork~~ — **built.** Embedded cover art and
      `cover.jpg`/`folder.jpg` into the existing content-addressed cache.
   2. ~~Artist tiles~~ — **placeholder built.** Artists borrow their
      most-substantial album's cover, flagged `inherited`, until a real image
      supersedes it.
   3. ~~Music client UI.~~ **Built** ([plan](music-client-plan.md)): album view
      with a numbered track list, square sleeves, an audio mode in the player,
      and a docked mini-player. The grid artist images were waiting for now
      exists.
   4. **Artist images from TheAudioDB** — **back burner.** Unblocked and not
      next. The borrowed album cover is a placeholder good enough to wait
      behind, and it supersedes itself with nothing to clean up
      ([ADR 0025](adr/0025-artist-images.md)). This is the first music item
      whose absence is cosmetic rather than functional, which makes it the right
      place to stop.
10. **Native desktop client (ADR 0023 stage 1)** — **built, opt-in**
    ([plan](native-client-plan.md)). What remains is not code: living with
    `-window` long enough to make it the default, which then unblocks the
    desktop lifecycle options.
11. **Nothing foundational remains.** What's next is breadth, from the feature
    backlog: a Pictures library (its own ADR — ADR 0024 deferred photos
    deliberately), more client surfaces (TV/mobile), more plugin kinds as real
    plugins need them, theme music if OST identification lands, and the
    system/operations items — an activity view, an audit log, a log viewer. Each
    is planned immediately before it is built.

## What the last pass taught

Three, and they are the same shape as the ones above: the failure was invisible,
or the belief was untested.

**A stated cost went unchecked for a release.** ADR 0023 priced a webview as
"CGO in the client, and per-platform client builds", and both were wrong on
Windows — a pure-Go binding builds with `CGO_ENABLED=0`. Ten minutes of trying
it deleted two of the three reasons the decision looked expensive. The same
pass found the reverse: the certificate problem the ADR expected to *soften* was
worse than described, because a web view does not warn, it refuses. **A cost
written down and never measured decays in both directions.**

**A capability nobody exercised was the same as a capability that did not
exist.** `?profile=` shipped, `docs/api.md` said "clients that know better say
so", and no client ever said so — so every browser in the house was served the
floor, and HEVC files were re-encoded for a release. The feature was not
missing; the caller was.

**Two things owning one fact is a bug waiting for a witness.** The mini-player
regression — clicking one film and getting the previous one — was a URL sync and
a router both deciding what was playing. It presented as lag, because each
bounce started another hardware encode. The fix was deleting one of them, not
arbitrating between them.

## Amendments to schema revision 1

M2 planning surfaced two gaps in revision 1. Because M1 has not been built,
these belong **in** revision 1 rather than becoming the first migration:

- Add a `meta` table seeded with `schema_version = 1`. Without it, the first
  migration has to guess what it is migrating from.
- Make `container`, `size_bytes`, and `mtime` nullable, so M2 can create
  `media_item` rows for directories ([ADR 0010](adr/0010-shows-as-media-items.md)).
