# Roadmap

Last updated: 2026-08-02 · **v0.4.0 released · M0–M4 built.** The React client executes the design
system and the client-UX backlog is closed. Observability (match, review, scan
diagnostics) and CI are in place. Transport security (TLS) and multi-user
accounts (admin/member roles) are built, and branding & splash shipped.

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
schema at **revision 11**) and the API contract ([ADR 0018](adr/0018-api-contract-and-versioning.md)).
On top of them, **media organisation shipped end to end** — collections, the
show → season → episode hierarchy, multi-part works and serials/miniseries, a
library-kind that drives movie-vs-TV matching, Fix match that reaches TV,
retroactive re-parse on rescan, Play-all queues, and Remove (ignore or delete,
with a sidecar sweep). Theme music (blocked on OST identification) is the
remaining M3 depth. Packaging & distribution is specced but deferred
([ADR 0016](adr/0016-packaging-and-distribution.md)).

A **feature backlog is captured below.** With M4 built, what remains is breadth
(more plugin kinds, more client surfaces, the deferred packaging build) rather
than foundational milestones.

## Releases

| Version | Date | What shipped |
|---|---|---|
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
| Data model evolution and migrations | **built** | Forward-only migrations (rev 1→9); collections, hierarchy, multi-part & serial works ([ADR 0017](adr/0017-collections-and-multi-part-works.md)) |
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
| Library types beyond video | *unplanned* | Music, photos — proves the taxonomy is open |

### Playback and client · M3

| Area | Status | Note |
|---|---|---|
| Media probing | **built** | ffprobe; codecs, duration, tracks |
| Transcode decision tree | **built** | Direct play / remux / transcode, with reasons |
| ffmpeg pipeline and HLS | **built** | Progressive fMP4 + HLS, session lifecycle |
| Hardware acceleration | **built** | NVENC, QSV, AMF, VideoToolbox — verified by test encode |
| Subtitles | **built** | Sidecar, embedded, WebVTT, OpenSubtitles hash matching |
| React client build | **built** | React + TS + Vite; Home shelves, Browse, Detail, Player, Settings; subtitles local + online; central spatial focus controller (ADR 0004) |
| Theme music subsystem | specced | Behavior in design.md; blocked on M2 |
| Branding & splash | **built** | App icons + favicon from the emblem, web manifest, and a once-per-session animated splash. Source art in `/assets` |

### Extensibility · M4

| Area | Status | Note |
|---|---|---|
| Plugin runtime and sandbox | **built** | WebAssembly via wazero, deny-by-default capabilities ([ADR 0020](adr/0020-plugin-isolation-boundary.md)); validated by OMDb-as-plugin |
| Extension point catalog | **built** | `rating_source` first; new source for an existing capability. Widening to new kinds waits for a plugin that needs it |
| Plugin distribution and trust | **built** | Signed `.lcplugin` bundles, two-layer trust (Ed25519 + capability grant), two-step install, Add-ons page ([ADR 0021](adr/0021-plugin-distribution-and-trust.md)) |
| Client surfaces: TV, mobile | *unplanned* | A restyle, if the focus model held |

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
| Observability | **built** | Match score breakdown, review queue, scan skip diagnostics |
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

- **Wide-scope audio codec support** — MP3, FLAC, WAV.
- **Music library.**
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

- **Activity status in the UI** — what the server is doing right now, shown in
  the interface rather than a separate window or log: items being added to a
  library, metadata being scanned, artwork fetched, files probed. The Plex
  model. The pieces already exist behind `/api/libraries/{id}/scan`,
  `/api/enrich`, and `/api/probe`; what is missing is one place in the UI that
  surfaces them continuously.
- **Crash reporting.**
- **Debug logging** and an **internal log viewer.**
- **Clear cache and data** and **reset settings** actions.
- **Check for updates** with an **auto-update** toggle.

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
8. **Nothing foundational remains.** What's next is breadth, from the feature
   backlog: more client surfaces (TV/mobile), more plugin kinds as real plugins
   need them, the deferred packaging build ([ADR 0016](adr/0016-packaging-and-distribution.md)),
   and theme music if OST identification lands. Each is planned immediately before
   it is built.

## Amendments to schema revision 1

M2 planning surfaced two gaps in revision 1. Because M1 has not been built,
these belong **in** revision 1 rather than becoming the first migration:

- Add a `meta` table seeded with `schema_version = 1`. Without it, the first
  migration has to guess what it is migrating from.
- Make `container`, `size_bytes`, and `mtime` nullable, so M2 can create
  `media_item` rows for directories ([ADR 0010](adr/0010-shows-as-media-items.md)).
