# Roadmap

Last updated: 2026-08-10 · **v0.6.7 released · M0–M4 built.** The React client executes the design
system and the client-UX backlog is closed. Observability (match, review, scan
diagnostics), an audit log and CI are in place. Transport security (TLS) and
multi-user accounts (admin/member roles) are built, and branding & splash shipped.

All of it is released: the repository is **public under MIT**, releases are
**signed**, the client **opens its own window by default**, and the server can
**check for, download, verify and stage an update** that swaps itself in on the
way down. Nothing sits unreleased on `main`. Details in the areas below; what
the pass taught is at the end.

**Music libraries shipped in v0.5.0** ([ADR 0024](adr/0024-music-libraries.md)),
which is the first media type past video and therefore the first real test of
the claim ADR 0002 made: that a new kind needs no new tables. It holds — music
is three new `kind` values on `media_item` related by `parent_id`, exactly as
show → season → episode already was. Metadata inverts the video rule: for a film
the filename is a guess and a provider corrects it, but a music file already
carries the answer in its tags, so tags win and the filename is the fallback.
The release was **server-side only**. Both gaps closed in v0.6.0: album
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
| **v0.6.7** | 2026-08-10 | The navigation stands up. Library names ran horizontally across the top, competing with each other and with the account controls for one strip of pixels, so a fourth library made the third one shorter and a long name was truncated by the existence of its neighbours. They move to a rail down the left, one per line, counts in their own column. **It collapses to icons and expands on hover or focus** — the backlog's Plex behaviour — and expands *over* the content rather than pushing it, because a page that slides whenever the pointer crosses the left edge is harder to use than a narrow rail. Labels fade rather than being removed, so a screen reader and in-page search still find them; the glyphs are drawn in twenty lines rather than pulled from an icon font, for the reason hls.js is not vendored. Verified collapsed and expanded, with the expanded state driven by **focus** rather than hover — the keyboard path is the one that would have broken silently, since `:focus-within` is what makes the rail reachable without a mouse |
| **v0.6.6** | 2026-08-09 | A staged update finally has somewhere to go. LANcast applies an update as the server shuts down, and when it runs as a Windows service nothing ever shuts it down — so "it takes effect the next time the server starts" described a moment that never arrives, and the only route through was an elevated `Stop-Service`, which applied the update correctly and left the machine with LANcast not running at all. The swap was never at fault: on the reporting machine the new binaries were in place, the old ones set aside, the staging directory consumed. What was missing was the step from *installed* to *running*. `POST /api/update/restart` now spawns a detached helper — the same binary, `service restart` — which stops the service, **waits for the stop to complete**, and starts it again; the wait is load-bearing, because `Start` on a service still stopping fails, and it would fail after the old version had already gone. Scoped as "finish the update" rather than "restart the server", and a non-service install refuses and says to close and reopen instead, because killing a process nothing will bring back is the failure being fixed |
| **v0.6.5** | 2026-08-09 | **Pictures** ([ADR 0028](adr/0028-pictures-library.md), [plan](pictures-plan.md)) — the third media type, and the second test of ADR 0002's no-new-tables claim, which holds again: gallery → photo on `media_item`, one nullable column (`taken_at`) at schema 16. The design follows from one fact — a photo *is* its own artwork, where every other media type points at an image representing it — so thumbnails are generated into the existing content-addressed cache by their own worker, and the cache is handed a 1600px copy rather than the original, because storing what it is given would put a second copy of the library on disk. Folders become galleries because in a picture library the folder is the only grouping that means anything: the filenames are UUIDs and there is no provider to ask, so titles are stored verbatim — a name that means nothing beats a tidied version of one. The library opens on a banner cycling the library, a gallery on a banner over its photos; pressing a photo selects it into the banner rather than navigating, since a photograph has no detail page worth visiting. Expand opens a viewer that owns the keyboard, restores focus on escape, and never auto-starts its slideshow. EXIF gives orientation and capture date; **GPS is never read**, because the surest way not to leak location data is not to load it. Also fixed: every rescan of a music library had been re-recording every track and re-queueing the whole library for enrichment since v0.5.0 — found by a picture test asking a new kind an old question |
| **v0.6.4** | 2026-08-09 | The in-app updater could find a new version and never install one, found by pressing the button on the first release it could have installed. Two faults. The release lookup asked GitHub's JSON endpoint for `application/octet-stream` and got 415 — the check path had it right, so checking worked, finding the release worked, and only fetching it was impossible. And the failure had nowhere to go: the download runs detached from the request that starts it, so the error reached the log and nothing else, leaving the panel on "Downloading…" indefinitely. A download that died half an hour ago was indistinguishable from a slow one. `download_error` is now reported and rendered separately from a failed check. **The tests could not have caught the first fault** — the fake releases server accepted any `Accept` header, so a downloader that asked wrongly passed every test and failed against the only server that matters. The fake is now as strict as GitHub on that dimension, verified by reintroducing the bug and watching the suite fail with the same 415. Installed by hand, necessarily: a broken updater cannot deliver its own fix |
| **v0.6.3** | 2026-08-09 | A home page worth opening, and two libraries that stop failing quietly. Home now opens on a **spotlight** — the thing you are part-way through, full-bleed artwork behind a floating poster, with a Resume button; failing that, the newest addition. The screen gained **depth built from everything except colour** ([ADR 0027](adr/0027-depth-in-the-canonical-look.md)): shadows cast in the void colour so a raised object reads as further from the same field, artwork tinted into the nebula rather than pasted on top of it, and a backdrop that parallaxes behind the shelves. Gold is untouched, because the ring is the focus indicator and diluting it costs an accessibility affordance rather than a look. **Listening separated from watching** — Continue Listening and New Music are their own rows, since a square sleeve beside a 2:3 poster is a row with no shared baseline, and a half-played track among films reads as broken films. On the library side, a **kind mismatch stops being silent**: a music folder added as a Movies library scanned 1,592 tracks, imported none and reported "0 items · scanned", which reads as an empty folder; it now names what it ignored and why, and the library-type field no longer has a default, because the choice is permanent and anything selectable by inattention eventually will be. Fixed: the Start menu's server shortcut carried a data-directory argument that never expanded — NSIS resolves `$%VAR%` at compile time, on a Linux runner — so starting the server that way opened a second, empty database beside the install |
| **v0.6.2** | 2026-08-09 | LANcast updates itself, opens in its own window by default, and the source is public under MIT. **The first signed release** — a signature over the checksum list, verified against a key compiled into the binary, which is what makes automatic installation defensible rather than merely convenient: installing an update is a system-level process executing a downloaded binary, and without proof of origin that is a hole. Three outcomes stay distinct — signed installs automatically, unsigned is offered for a manual install, present-and-wrong is refused before anything is downloaded. The install is staged and swapped on the way down, so it is one restart with no elevation prompt and no second process. The check is on by default and is not a phone-home exception: a plain GET with no install identifier, statistics or history, switchable off, with a manual check that still works. The `-window` flip landed here too, with `-browser` as the opt-out and the installer offering both. Fixes: a database handle held open after a stop that raced startup (which is what makes an installer's file replacement fail), and an NFO edit that claimed the whole file rather than the field that changed |
| **v0.6.1** | 2026-08-09 | A day of fixes, an audit log, and desktop lifecycle controls — two of the fixes for problems that never produced an error. Scans stopped dying with `database is locked` when enrichment committed mid-scan; new films and shows are enriched again on any server with a music library, where un-enrichable rows had blocked the queue permanently (a "remaining" count of 2,198 that never moved became 7); the service stops when told rather than being judged a hang and restarted by its own recovery policy; and a sidecar is written only for an identity actually established, so a wrong title can no longer outlive the database that produced it. The **audit log** ([ADR 0026](adr/0026-audit-log.md)) records who changed what where the mutation is authorised — libraries, titles, matches, accounts, add-on trust — readable from Settings, with browsing and playback deliberately excluded. **Desktop lifecycle** shipped: close to tray and open when Windows starts, both off by default, both shown only in the LANcast window. Close-to-tray had shipped disabled on the belief that the tray and the web view fight over the message loop; they do not — Windows message queues are per thread, and the conflict existed only because both were run from one goroutine |
| **v0.6.0** | 2026-08-08 | Music becomes a client experience, LANcast gets its own window, and the server says what it is doing. The music UI v0.5.0 left unbuilt — artist and album tiles, a numbered track list in playing order, an audio mode, a docked mini-player — plus album artwork off the disk (369 of 398 albums, 10.7s, no network) and album artist/year derived from tracks. `-window` opens a WebView2 window that pins the server's own certificate, which matters because a web view does not warn on a bad certificate, it refuses to load. Clients now declare what they can decode, ending the full re-encode of every HEVC file. An activity indicator and a log viewer make background work visible. Two fixes found by using it: scans aborting with `database is locked` when enrichment committed mid-scan (SQLITE_BUSY_SNAPSHOT, which `busy_timeout` does not cover), and new items never being enriched at all on any server with a music library, because un-enrichable rows sat at the head of the queue and the worker stopped at the first unproductive batch |
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
| Library types beyond video | **built** | **Music** ([ADR 0024](adr/0024-music-libraries.md)) and **pictures** ([ADR 0028](adr/0028-pictures-library.md)) both on `media_item` with no new tables — the taxonomy claim from ADR 0002 has now survived two media types that work nothing like video. Pictures added the case nobody predicted: the file *is* its own artwork, where everything else points at an image representing it |
| Embedded tags as a source | **built** | ID3v2 / Vorbis / MP4 atoms via the probe that already runs. Authority order for a track: locked fields, tags, folder, filename — the inverse of video, because the file carries the answer |
| Album artwork | **built** | `internal/coverart`: embedded picture first, then `cover.jpg`/`folder.jpg` beside the tracks, in its own worker. Measured on the real library — 369 of 398 albums, 10.7s, no network. A directory's image is refused when the directory also holds audio that is not the album's, which is what stops a letter-bucket `folder.jpg` being worn by five unrelated records |
| Artist images | **back burner** | The placeholder is good enough to wait behind: artists **borrow** their most-substantial album's cover, flagged `inherited`, and a real image supersedes it automatically with nothing to clean up. TheAudioDB, name-keyed and opt-in, is the decided source ([ADR 0025](adr/0025-artist-images.md), accepted, unbuilt) — it was sequenced after the client UI, which is now built, so nothing blocks it except priority. Deferred deliberately: music has had a long run and this is the first item where the gap is cosmetic rather than functional |
| NFO sidecar authority | **built** | A sidecar is written only for an identity actually established — writing one for a failed match committed a guess to disk under LANcast's own name, where it outlived the database and was inherited by the next one. The marker that recognises LANcast's own sidecars is version-tagged, so a future release hashing a different field set cannot silently reclassify every sidecar as hand-edited and start trusting stale contents over live metadata. On `main`, an edit authors only the field that changed, per field, rather than claiming the whole file |
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
| Stage 1 — own the window | **built, default** | `LANcast-Client.exe` opens a WebView2 window instead of handing a URL to a browser ([plan](native-client-plan.md)). **Pure Go, `CGO_ENABLED=0`** — the ADR's assumed CGO cost was wrong, tested rather than argued, so the single-runner release matrix survives. The binding is a trimmed vendored copy with the embedded DLL and its from-memory loader removed ([provenance](../internal/webview2/PROVENANCE.md)); Microsoft's signed loader ships beside the executable |
| Certificate trust | **built** | The point of owning the window, and worse than the ADR assumed: against a LAN-bound server the web view does not warn, it fails the handshake and retries, so the app never loads. The client pins the server's public key, read from its own `cert.pem` on local disk; every other certificate is still validated |
| Flip `-window` to default | **built** | Done after living with it, not after arguing about it. The browser lost on three things a tab cannot fix: LANcast cannot say what its close button means, cannot pin the server's certificate, and gets a warning against a LAN-bound self-signed server that the window does not need. `-browser` is the opt-out, `-window` is kept as a no-op alias so existing shortcuts and the autostart run key keep working, a machine with no WebView2 runtime falls back on its own, and the installer's finish page offers both |
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
| Observability | **built, one known gap** | Match score breakdown, review queue, scan skip diagnostics — including `skipped_kind`, the count of media a library's own kind discards — and a single-instance guard that names what is holding it: the service, its pid and its data directory, read at a privilege an unelevated caller actually has. **The gap:** a show library created as a movie library still scans silently, because both kinds take the same files. See the feature backlog |
| Desktop lifecycle | **built** | *Open on Windows start* and *Close to tray* ([plan](desktop-lifecycle-plan.md)), both off by default and both shown only in the LANcast window, because a tab has no tray to reduce to and no close button LANcast owns. The Settings section states which of the three meanings of "closed" you are looking at — stop the server, or leave it running because the service owns it. Autostart records the mode you chose, so picking the browser does not silently become the window at login |
| Update checking and self-update | **built; proven as far as staging, unproven past it** | Check, download, verify, stage, and swap on the way down. **The download was broken from the day it shipped until v0.6.4**: the release lookup asked a JSON endpoint for octet-stream and GitHub answered 415, so v0.6.2 and v0.6.3 could find an update and never fetch one. Found by pressing the button, not by reading — the first release that could have been installed was the first test the code had ever had. **v0.6.5 was the first release the updater could fetch, and it got further than
before and still not all the way**: it downloaded, verified and staged
correctly — the swap applied, the binaries were replaced — and then stopped
dead, because a service never shuts down on its own and the update is applied
on the way down. v0.6.6 adds the restart. What remains untested is the whole
path in one go on an installed artifact: download, stage, restart, come back on
the new version. Two releases have now been spent finding one more link in that
chain each time, which is what a path that only executes during a release costs. What *is* proven, on the published artifacts: the signature verifies against the key compiled into the shipping binaries, and the Windows archive's digest matches the one in the signed list — signature → checksums → bytes, all three links checked rather than assumed. Signed releases first, because auto-install is a LocalSystem process executing a downloaded binary and without authenticity that is remote code execution as SYSTEM — an Ed25519 signature over `checksums.txt`, offline verification against a key compiled into the binary, and a **separate key from the plugin project key** so one compromise is not both. Three distinct states: unsigned is installable by hand only, a wrong signature is refused outright, and a build with no key refuses everything. The swap works because a running process on Windows can rename *itself*: the binary moves aside to `.old`, the staged one takes its place, and the next start is the new version — one restart, no UAC prompt, no second process. Nothing is written into the install directory before the swap; staging lives in the data directory. The check is on by default and is not a phone-home exception — a plain GET with no install identifier, no statistics, no history, and switchable off entirely |
| Audit log | **built** | Who changed what, and when, recorded server-side where the mutation is authorised ([ADR 0026](adr/0026-audit-log.md)) — libraries, deleted titles, overridden matches, accounts, add-on trust. An audit trail a client writes is forgeable by the client it is auditing. Readable from Settings, newest first, filterable by action. Browsing and playback are deliberately excluded: burying a handful of deliberate acts under a million routine ones is how a log becomes unreadable. Each entry freezes the actor's name and a sentence at the time it happened, so "who deleted this library" still answers after both the library and the account are gone |
| Testing strategy | **built** | CI runs go test + client build + bundle-drift check; fixture libraries, no real media |
| Licensing and open-sourcing | **done** | **MIT** (`LICENSE`) and **the repository is public**, which is what the update check needed: the releases API returns 404 for a private repo and cannot distinguish "no such repository" from "not yours to see", so the checker was correct and inert until the switch was flipped. Vendored code keeps its own notices and the README points at them. **The history cleanup did not happen before the repo went public and is now a judgement call rather than a scheduled one** — three `lancastd.exe~` blobs from the M3 era still sit in history (~25 MB packed, about a third of `.git`). Rewriting 352 commits and re-pointing every release tag was worth doing *once, before* the first public clone; after it, a rewrite also breaks every clone and every commit link that already exists. The honest options are to accept the weight or to pay it deliberately and announce it; `*.exe~` is gitignored, so it cannot grow either way |

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
- ~~**Auto-expanding / collapsing navigation bar**~~ — **built** in v0.6.7,
  alongside the move from a horizontal nav to a vertical rail. It expands over
  the content rather than displacing it, and on focus as well as hover.
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
  UI and mini-player in v0.6.0.
- ~~**Photo library** with a built-in **image viewer**~~ — **built** in v0.6.5 ([ADR 0028](adr/0028-pictures-library.md), [plan](pictures-plan.md)). Folders become galleries, because a filename like `openart-f81b76…_raw.jpg` says nothing and there is no provider to ask. Thumbnails run in their own worker through the existing content-addressed cache; HEIC decodes through the ffmpeg already required, because a phone backup is mostly HEIC and a wall of placeholders would be a feature that looks finished and is useless. EXIF orientation and date-taken only — **GPS deliberately unread**, since the safest way never to leak location data is never to load it.
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
- ~~**Audit log — who changed what, and when.**~~ — **built** in v0.6.1
  ([ADR 0026](adr/0026-audit-log.md)), server-side where the mutation is
  authorised, readable from Settings. The absence of one is why "what emptied
  this library" was unanswerable during v0.4.x testing. Still open beside it:
  whether identity should live in its own store rather than beside the library,
  so losing a password never opens the file holding the media.
- **A wrong library kind is only half-visible.** Kind is chosen once and is
  immutable by design — it decides which files are scanned at all and biases
  movie-vs-TV matching, so changing it later would mean a rescan re-litigating
  identity for a whole library, which is the thing the locked-fields rule exists
  to forbid. Plex takes the same position. The consequence is that choosing
  wrongly is unrecoverable except by removing and re-adding, so the mistake has
  to be **loud at the moment it happens**, and today only one of the two cases
  is. A music library created as a movie library now reports how many audio
  files its own kind discarded (`skipped_kind`), because the audio-vs-video gate
  makes that case obvious: zero items imported. **The show-vs-movie case has no
  signal at all**, because both kinds scan exactly the same files. Nothing is
  skipped, the count stays zero, the scan succeeds, and the library is quietly
  wrong in its *shape* rather than its size. Measured on the test library, the
  same folder scanned as `movie` instead of `show`:

  | | `kind=show` | `kind=movie` |
  |---|---|---|
  | shows | 3 | 2 |
  | seasons | 3 | 2 |
  | episodes | 15 | 12 |
  | stray movie / parts | — | 1 movie + 3 parts |

  One show stopped being a show: its episodes were read as a film in three
  parts — almost certainly the miniseries [ADR 0017](adr/0017-collections-and-multi-part-works.md)
  exists for. That is worse than the music case, not better. Music fails loudly
  enough to be reported within minutes; this produces a library that looks
  finished and is wrong, and would be found weeks later by someone wondering why
  a miniseries is a film. The fix is a different signal from a skip count —
  candidates are a post-scan sanity check (a `show`-kind library that produced no
  shows, or a `movie`-kind library where most files parsed as episodes) surfaced
  the same way the review queue surfaces uncertain matches.
- **Library editing, deferred to the settings redesign.** With kind immutable,
  what an Edit control would actually govern is the name — worth having, not
  worth bolting onto the current page. The settings page is due a planned
  rebuild, and this belongs to it.
- **Crash reporting.**
- ~~**Internal log viewer**~~ — **built.** `GET /api/logs` returns the tail of
  `lancastd.log` and Settings shows it, collapsed by default and never polled.
  The log had been written beside the database since v0.4.2 and could only be
  read by opening a file manager — the wrong ask for the case it serves, since
  it matters most when the server runs as a service and something is wrong. It
  says when the view is partial rather than letting a reader believe they have
  the whole file. **Debug logging** — raising the level from the UI — is still
  open.
- **Clear cache and data** and **reset settings** actions.
- ~~**Check for updates** with an **auto-update** toggle.~~ — **built,
  unreleased on `main`.** Signed releases, an update check on by default, and a
  download-verify-stage path that swaps the binary in on shutdown. What remains
  is the first release cut *with* the signing key in place, which is the only
  thing that can prove the published half end to end.
- ~~**Desktop lifecycle — "Open on Windows start" and "Close to tray"**~~ —
  **built** in v0.6.1 ([plan](desktop-lifecycle-plan.md)).

### Input and control

- **Keyboard-control shortcut map and customizer** — building on the existing
  spatial focus model (ADR 0004).
- **Pop-out player** in our own window rather than the browser's
  ([ADR 0029](adr/0029-picture-in-picture-is-our-window.md), proposed).
  Picture-in-picture hands the element to Chrome, so the window arrives with
  Chrome's chrome: our subtitles keep rendering in the parent tab while the
  picture is in the corner, a Live Caption button offers guessed transcription
  in place of the real tracks, and speed, audio track and queue disappear.
  Document PiP renders our own player instead. The clock — the fault that
  started it — was fixed without the rework, by reporting the true timeline
  through MediaSession (`72619a6`, unreleased).

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
10. ~~**Native desktop client (ADR 0023 stage 1)**~~ — **built and now the
    default** ([plan](native-client-plan.md)). Living with it settled it, which
    also unblocked and delivered the desktop lifecycle options.
11. ~~**Audit log**~~ — **built** in v0.6.1 ([ADR 0026](adr/0026-audit-log.md)).
12. ~~**Distribution trust and self-update**~~ — **shipped in v0.6.2**, which is
    the first signed release and therefore the first one that exercised the
    published half. It failed on the first attempt for a reason worth keeping:
    the release pipeline had never signed anything, so the signing step had
    never run.
13. **Nothing foundational remains.** What's next is breadth, from the feature
    backlog: a Pictures library (its own ADR — ADR 0024 deferred photos
    deliberately), more client surfaces (TV/mobile), more plugin kinds as real
    plugins need them, theme music if OST identification lands, crash reporting,
    and debug-level logging from the UI. Each is planned immediately before it
    is built.

## What the last pass taught

**A release step that has never run has never been tested, and cutting the tag
is the test.** v0.6.2 failed at signing: the artifact path was passed
positionally as `"${artifact}"` and arrived at the shell empty, because the
placeholder is substituted where it appears *inside* an argument, not when it is
the whole of one. `lcsign` refused the empty `-in` and said so, which is the
behaviour worth having — the alternative is a release that publishes unsigned
and is discovered by a user whose updater declines to install it. Two things
made the fix quick rather than a guessing game. It was reproduced locally with a
snapshot build before anything was changed, and the argv was *printed* rather
than reasoned about: `0=[] 1=[] 2=[]` ruled out quoting and ruled out a shell
difference between the runner and this machine in one line, which is where
reading the config would have sent someone first. The same pass found a second
branch that had never run — the unsigned fallback, which could not have worked
either, since goreleaser expects the signature file the block declares. **Every
branch of a release pipeline is untested until a real tag takes it.**

**A rule derived from reasoning met a library and lost.** The picture decoder
sent HEIC and HEIF to ffmpeg and nothing else, on the sound-sounding logic that
those are the formats Go cannot read. The first scan of a real library found
eight photographs that disprove it — ordinary BMPs, family pictures, that Go's
decoder rejects and ffmpeg reads without complaint. They were reported as
failures because `.bmp` was not on a list someone had written from memory. The
replacement rule needs no list: whatever the in-process decoders refuse is
offered to ffmpeg. **A list of exceptions is a claim about the world, and the
world has more cases than the person writing the list.**

**A new media type is an audit of every assumption the old ones left behind.**
Adding pictures found a detail page offering to *play* a photograph, a gallery
offering "Play all" over 779 of them, Fix match offered against a provider that
will never exist, "recently added" answering with folders, and a library opening
sorted by UUID. None of those were pictures bugs; they were places where "a leaf
is something you press play on" had been true for so long it had stopped being
written down. It also found a real one in music: every rescan re-recorded every
track, because the reinterpretation check had a default that assumed anything
unfamiliar was "other". True since v0.5.0, found by a picture test asking a new
kind an old question.

**A fake that is more permissive than the real thing tests nothing.** The
updater's download was broken from the day it shipped: the release lookup asked
GitHub's JSON endpoint for octet-stream and got 415. It had tests, and they all
passed, because the fake releases server accepted any `Accept` header. The fake
agreed with the code instead of with GitHub, so it could only ever confirm what
the code already believed. The fix was to make the fake strict on exactly that
dimension and then reintroduce the bug to watch the suite fail — a regression
test nobody has seen fail is a regression test nobody has tested. The general
form: **a test double is a claim about the real system, and an untested claim is
where the bug hides.**

**The last mile of a release pipeline only runs at the last mile.** The download
half of the updater could not run against a real release until a signed release
existed, so it did not, for two releases. The same shape as the signing step
that failed on the first tag that used it, one week earlier and one layer up.
Anything that can only execute during a release is untested until a release
executes it, and both times the cost was one extra version.

**A default on an irreversible choice is a bug with a delay on it.** The
add-library form pre-selected "Movies" in a dropdown beside a name field, and
library kind cannot be changed afterwards. A library named "Music", pointed at a
music folder, was created as a movie library — and then discarded 1,592 tracks
and reported "0 items · scanned", which reads as an empty folder. Two fixes came
out of it, and the second is the general one: the skip now has a voice, and the
field no longer has a default. **Where a choice is permanent, the interface has
to make it deliberate**; anything selectable by inattention eventually will be.

**A blocker that was never tested blocked a feature for a release.**
Close-to-tray shipped as a disabled toggle on the belief that the tray and the
web view both need the main thread's message loop. They do not: Windows message
queues are per *thread*, and the conflict existed only because both were being
run from one goroutine. The belief was plausible, written down, and wrong, and
it cost a shipped option that said "not yet available" while being one goroutine
away. Then the build of it repeated the lesson one layer down — `PostQuitMessage`
posts to the *calling* thread's queue, so Quit from the tray quit the tray and
left the window open, and the interface comment claiming it is safe from a
background thread is wrong for this backend. Neither fact was reachable by
reading; both took someone watching the screen.

**A guard written against one platform is a guard against one platform.** The
self-update staging check used `filepath.Base` to insist on a flat filename, and
`filepath.Base` is platform-dependent by design: on Linux `..\evil.exe` is one
legal filename, so it was admitted — and it becomes a path traversal the moment
that data directory is carried to Windows, into a directory the service writes
to as LocalSystem. CI found what a Windows-only suite structurally could not.
The test was right and the code was wrong, which is the good version of this.

**A failure with no voice can be under the floor as easily as in our code.** The
service-stop test failed one run in five on cleanup, always on a still-locked
`lancast.db`. Everything observable through `database/sql` was provably clean —
Opens and Closes 24 to 24, `db.Stats()` all zeros, no server goroutine alive —
and identical on passing and failing runs, which is precisely what ruled our
code out. The handle was leaked below us, in the SQLite driver; the fix was a
dependency bump, measured (5 failures in 24 before, 0 in 48 after) rather than
asserted. It also is not only a test annoyance: the installer stops a running
instance before replacing its files, and a stop is likeliest to race startup
exactly when something is restarting it.

### Carried forward from the pass before

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
