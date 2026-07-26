# Roadmap

Last updated: 2026-07-26 · **M0–M3 built.** The React client executes the design
system; theme music (blocked on OST identification) is the remaining M3 depth.

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
| Data model evolution and migrations | *unplanned* | Strategy past revision 1 |
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
| Users, auth, sessions | **built** | Single password; multi-user still open |
| Remote access | documented | VPN or reverse proxy; see security.md |
| Security model | **built** | Auth, CSRF, throttling, loopback-until-secured |
| Transport security (TLS) | *unplanned* | The largest remaining gap |
| Performance targets | *unplanned* | Budgets for a 40k-item library |
| Packaging and distribution | *unplanned* | Windows, Linux, NAS, Pi, service install |
| Backup and restore | *unplanned* | Rebuild a library without a full rescan |
| Observability | *unplanned* | Scan diagnostics: "why did this not match?" |
| Testing strategy | *unplanned* | Fixture libraries; no real media in CI |
| Licensing and open-sourcing | *unplanned* | Decided before the repo goes public |

## Client UX backlog

Noted from use of the old single-file client, and largely resolved by the React
rebuild, which split the player dialog into distinct screens.

1. ~~**Separate the information screen from the player.**~~ **Done** — clicking a
   poster opens the full-bleed detail page (synopsis, cast, artwork) with a
   **Play** button; playback is its own screen.
2. **Play the official trailer on the information screen.** *Partial* — the
   trailer is surfaced on Detail (via the trailer endpoint) but shown as a note;
   in-page trailer playback is not built yet.
3. ~~**Subtitles belong to the player, not the preview.**~~ **Done** — the picker
   lives in the player, with local tracks, online search, and removal.
4. **Reposition "fix match".** *Open* — the metadata-correction UI is not yet
   ported to the React client at all.

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
3. **Security and remote access.** Decided before anything is exposed past
   localhost, not after.
4. **Plugin architecture (M4).** Last, informed by all of the above.

## Amendments to schema revision 1

M2 planning surfaced two gaps in revision 1. Because M1 has not been built,
these belong **in** revision 1 rather than becoming the first migration:

- Add a `meta` table seeded with `schema_version = 1`. Without it, the first
  migration has to guess what it is migrating from.
- Make `container`, `size_bytes`, and `mtime` nullable, so M2 can create
  `media_item` rows for directories ([ADR 0010](adr/0010-shows-as-media-items.md)).
