# Roadmap

Last updated: 2026-07-22 · **5 of 26 areas planned. M0, M1, and M2 built.**

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
| M3 | Transcoding + real client | Plays anywhere; React client executes the design | |
| M4 | Extensibility | Plugin runtime with first-party plugins proving the contract | |

M1 is the milestone that matters. Everything before it is scaffolding and
everything after it is depth.

## Areas

Status: **planned** · **next** · *unplanned*

### Foundation · M0–M1

| Area | Status | Note |
|---|---|---|
| Server core architecture | **built** | Go, SQLite, scan → browse → play |
| UI/UX design system | planned | Nebula field, gold rule, keyboard model — M1 client carries the tokens only |
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
| Transcode decision tree | *unplanned* | Direct play vs stream vs transcode |
| ffmpeg pipeline and HLS | *unplanned* | Segmenting, seeking, session lifecycle |
| Hardware acceleration | *unplanned* | QSV, NVENC, VAAPI capability matrix |
| Subtitles | *unplanned* | Sidecar, burn-in, styling, sourcing |
| React client build | *unplanned* | Executes the design system for real |
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
| Users, auth, sessions | *unplanned* | Schema already anticipates it |
| Remote access | *unplanned* | Self-owned; the no-phone-home promise |
| Security model | *unplanned* | Path traversal, LAN exposure, CSP |
| Performance targets | *unplanned* | Budgets for a 40k-item library |
| Packaging and distribution | *unplanned* | Windows, Linux, NAS, Pi, service install |
| Backup and restore | *unplanned* | Rebuild a library without a full rescan |
| Observability | *unplanned* | Scan diagnostics: "why did this not match?" |
| Testing strategy | *unplanned* | Fixture libraries; no real media in CI |
| Licensing and open-sourcing | *unplanned* | Decided before the repo goes public |

## Dependencies that constrain ordering

- **Theme music → M2.** Needs TVDB ids and OST identification. Cannot land sooner.
- **TV client → keyboard focus model.** The roving-tabindex controller is the TV
  client's foundation. Compromise it during M3 and the TV client becomes a
  rewrite instead of a restyle.
- **Plugin contract → one full build of the core.** Deliberately last.
- **Users and auth → schema.** Already handled; can arrive late without data loss.
- **API versioning → before any third-party client exists.** Cheap now, breaking later.

## Next planning order

1. ~~Metadata and artwork (M2)~~ — **planned.** See
   [metadata.md](metadata.md) and ADRs 0007–0010.
2. **Security and remote access.** Decided before anything is exposed past
   localhost, not after.
3. **Transcoding (M3).** The hardest engineering; plan it immediately before building.
4. **Plugin architecture (M4).** Last, informed by all of the above.

## Amendments to schema revision 1

M2 planning surfaced two gaps in revision 1. Because M1 has not been built,
these belong **in** revision 1 rather than becoming the first migration:

- Add a `meta` table seeded with `schema_version = 1`. Without it, the first
  migration has to guess what it is migrating from.
- Make `container`, `size_bytes`, and `mtime` nullable, so M2 can create
  `media_item` rows for directories ([ADR 0010](adr/0010-shows-as-media-items.md)).
