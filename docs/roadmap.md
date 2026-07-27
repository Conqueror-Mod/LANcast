# Roadmap

Last updated: 2026-07-26 · **M0–M3 built.** The React client executes the design
system and the client-UX backlog is closed. Observability (match, review, scan
diagnostics) and CI are in place. Transport security (TLS) and multi-user
accounts (admin/member roles) are built, and branding & splash shipped. Theme
music (blocked on OST identification) is the remaining M3 depth. Packaging &
distribution is specced but deferred ([ADR 0016](adr/0016-packaging-and-distribution.md)).
A **feature backlog is captured below**; **plugin architecture (M4)** remains
the last milestone.

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
| M4 | Extensibility | Plugin runtime with first-party plugins proving the contract | |

M1 is the milestone that matters. Everything before it is scaffolding and
everything after it is depth.

## Areas

Status: **planned** · **next** · *unplanned*

### Foundation · M0–M1

| Area | Status | Note |
|---|---|---|
| Server core architecture | **built** | Go, SQLite, scan → browse → play |
| UI/UX design system | **built** | Nebula field, gold rule, keyboard model — executed by the React client, not just the tokens |
| Data model evolution and migrations | **planned** | Forward-only migrations (rev 1→8); multi-part & serial works decided ([ADR 0017](adr/0017-collections-and-multi-part-works.md)) |
| API contract and versioning | *unplanned* | Decide before any third-party client |

### Metadata and artwork · M2

| Area | Status | Note |
|---|---|---|
| Provider interface | **built** | Scraper contract; first real extension point |
| Matching and confidence | **built** | Wrong-match correction — the actual pain point |
| Artwork pipeline | **built** | Fetch, cache, resize; fanart for detail pages |
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
| Plugin runtime and sandbox | *unplanned* | WASM or subprocess RPC |
| Extension point catalog | *unplanned* | What is extensible — and what never will be |
| Plugin distribution and trust | *unplanned* | Install, update, trust; Kodi's failure mode |
| Client surfaces: TV, mobile | *unplanned* | A restyle, if the focus model held |

### Cross-cutting

| Area | Status | Note |
|---|---|---|
| Users, auth, sessions | **built** | Multi-user accounts with admin/member roles (ADR 0015); per-user watch state |
| Remote access | documented | VPN or reverse proxy; see security.md |
| Security model | **built** | Auth, CSRF, throttling, loopback-until-secured |
| Transport security (TLS) | **built** | HTTPS beyond loopback; bring-your-own or self-signed cert, http→https redirect (ADR 0014) |
| Performance targets | *unplanned* | Budgets for a 40k-item library |
| Packaging and distribution | specced · deferred | One binary per platform, goreleaser matrix, in-binary service install ([ADR 0016](adr/0016-packaging-and-distribution.md)); build deferred |
| Backup and restore | *unplanned* | Rebuild a library without a full rescan |
| Observability | **built** | Match score breakdown, review queue, scan skip diagnostics |
| Testing strategy | planned | CI runs go test + client build + bundle-drift check; fixture libraries, no real media |
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
- **Movie library page** and **TV-show library page** — media-type-specific
  browse views, not one generic grid.
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

- **Ratings system** that also ties to Metacritic / Rotten Tomatoes.
- **Plex-style filter settings**, with a **total movie/show count per library
  page**.

### Social and profiles

- **Profile page**: watch/play/view history, Find Friends, Trending (trends
  computed per library), ratings/reviews, a viewer-stats list, profile edit,
  and profile information.
- **Watch Together** — synchronised playback across viewers.
- **Better profile manager.**

### System, operations and diagnostics

- **Crash reporting.**
- **Debug logging** and an **internal log viewer.**
- **Clear cache and data** and **reset settings** actions.
- **Check for updates** with an **auto-update** toggle.

### Input and control

- **Keyboard-control shortcut map and customizer** — building on the existing
  spatial focus model (ADR 0004).

### Resolved modeling question — multi-part and serial works

**Decided in [ADR 0017](adr/0017-collections-and-multi-part-works.md).** The
four cases split on one axis — are the pieces independent works or parts of one
work? Independent works that continue a story are a **collection** (many-to-many
membership, a side table; members stay top-level). Pieces of one work are
**containment** via `parent_id`, with new `kind` values `part`/`chapter` and a
`serial` kind for a closed, play-through-whole story. Schema landed in revision
8; provider ingestion is deferred to M2-provider depth. Original framing kept
below for the record. Motivating cases:

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
3. ~~Security and remote access~~ — **transport security built** ([ADR 0014](adr/0014-transport-security.md));
   multi-user accounts and session-management UI remain, tracked in
   [security.md](security.md).
4. **Plugin architecture (M4).** Last, informed by all of the above.

## Amendments to schema revision 1

M2 planning surfaced two gaps in revision 1. Because M1 has not been built,
these belong **in** revision 1 rather than becoming the first migration:

- Add a `meta` table seeded with `schema_version = 1`. Without it, the first
  migration has to guess what it is migrating from.
- Make `container`, `size_bytes`, and `mtime` nullable, so M2 can create
  `media_item` rows for directories ([ADR 0010](adr/0010-shows-as-media-items.md)).
