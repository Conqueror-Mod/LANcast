# Roadmap

Last updated: 2026-11-17 · **v1.0.0 · scope frozen at v0.11.0, the acceptance run passed ([v1-run-plan.md](v1-run-plan.md)) · M0–M4 built.** Releases before 1.0 are in [history.md](history.md). The React client executes the design
system and the client-UX backlog is closed. Observability (match, review, scan
diagnostics), an audit log and CI are in place. Transport security (TLS) and
multi-user accounts (admin/member roles) are built, and branding & splash shipped.

All of it is released: the repository is **public under AGPL-3.0**, with a
commercial licence available (ADR 0053), releases are
**signed**, the client **opens its own window by default**, and the server can
**check for, download, verify and stage an update** that swaps itself in on the
way down. Details in the areas below; what the pass taught is at the end.

## Where it stands

*Rewritten 2026-09-30 and refreshed 2026-10-04, 2026-10-07 and 2026-10-26.
The 2026-10-26 pass found this file fifteen days and seventeen releases
behind: the Releases table stopped at v0.9.78, and three backlog entries still
listed work that had shipped as open. The release-by-release record is the
[Releases](#releases) table, and what remains is [What is left](#what-is-left).
Refreshed again 2026-11-07, after v0.10.6: the file had stopped at v0.10.3.
Refreshed 2026-11-08 for 1.0 planning, after v0.10.10: the file had stopped
at v0.10.8, and the schema, the executables and the CI rows had drifted.
Brought to v0.10.11 on 2026-11-09.*

**On main, unreleased:** nothing. Main is v0.10.11.

v0.10.11 is the latest release. Since v0.9.78:
- **v0.10.11:** Settings 3.0 complete ([plan](settings-3-plan.md),
  [ADR 0120](adr/0120-a-setting-belongs-to-what-it-describes.md)): Your account
  › Home and › Reading & listening, metadata in 33 languages
  ([ADR 0122](adr/0122-metadata-may-be-in-another-language.md)), options per
  library with right-to-left comics
  ([ADR 0123](adr/0123-a-library-carries-its-own-options.md)) and a storage
  overview; and Profile Page 2.0 ([plan](profile-2-plan.md)): your own
  picture ([ADR 0121](adr/0121-a-picture-of-your-own.md)), tabs, totals by
  kind and one History. Schema 95, one-way.
- **v0.10.10:** shelves you pin to Home from a filtered library, Suggested
  falling back to what you have been finishing, and On this day from watch
  history ([ADR 0119](adr/0119-homepage-2-part-two.md)). Schema 91, one-way.
- **v0.10.9:** Homepage 2.0 ([ADR 0118](adr/0118-the-home-page-is-a-bridge.md)):
  one Continue panel for every kind, Friends and On this day beside the
  spotlight, a background tinted from the spotlight, and New this week.
- **v0.10.8:** a Debug log that holds about 28 hours rather than 11.
- **v0.10.7:** audiobooks step 3 (a sleep timer, narrators, and filters by
  author, narrator and series), Continue Watching with a list of its own, and
  a film watched with a friend starting in seconds rather than minutes.
- **v0.10.6:** play on another of your own devices
  ([ADR 0117](adr/0117-play-on-another-device.md)), a friend's join request
  shown over a full-screen film, and friends' posters on a shared library
  served again.
- **v0.10.5:** a half-watched film in a collection returns to Home when an
  earlier one is finished, the tray opens pages at the window's own address
  (so it no longer signs the window out), and a Debug log that holds about a
  day instead of a few hours.
- **v0.10.4:** audiobooks, steps 1 and 2
  ([ADR 0116](adr/0116-an-audiobook-is-its-own-library.md)), and albums
  identified by MusicBrainz
  ([ADR 0115](adr/0115-albums-are-identified-by-musicbrainz.md)). Schema 90,
  one-way.
- **v0.10.0–v0.10.3:** frame-rate matching, uncomposed video and HDR on the
  TV, in-play RetroAchievements
  ([ADR 0112](adr/0112-achievements-are-earned-through-the-server.md)), and
  music by artist, album, track and genre
  ([ADR 0114](adr/0114-a-music-library-is-browsed-at-three-levels.md)).
- **v0.9.79:** one program, `LANcast.exe`, with the tray always present
  ([ADR 0080](adr/0080-one-executable-two-roles.md)), and a scan can be cancelled.
- **v0.9.80–v0.9.81:** collections continue the way shows do, an About tab,
  and the close button working while a film plays.
- **v0.9.82:** Settings grouped four ways, with scheduled tasks; friends see
  posters on a shared library.
- **v0.9.83:** intros skip themselves
  ([ADR 0083](adr/0083-an-intro-skips-itself.md)), authorised devices, and
  backups on a schedule.
- **v0.9.84:** a library kept from an account
  ([ADR 0084](adr/0084-a-library-can-be-kept-from-an-account.md)), the
  Watchlist ([ADR 0085](adr/0085-a-watchlist-is-a-list-of-promises.md)) and
  DLNA ([ADR 0086](adr/0086-a-tv-browses-as-somebody.md)).
- **v0.9.85:** optimized versions
  ([ADR 0087](adr/0087-an-optimized-copy-is-converted-once.md)).
- **v0.9.86:** a certificate you bring
  ([ADR 0088](adr/0088-a-certificate-you-bring-is-followed.md)) and a port that
  moves and comes back ([ADR 0089](adr/0089-the-port-moves-and-comes-back.md)).
- **v0.9.87:** a shelf for rating what you watched, picking your account at
  sign-in, and passkeys in the desktop window.
- **v0.9.88:** RetroAchievements on the profile
  ([ADR 0090](adr/0090-retroachievements-are-yours-to-connect.md)), and a paged
  game menu with fast-forward, rewind, picture and controls.
- **v0.9.89–v0.9.91:** Now Playing redesigned, and visualizers (Spectrum,
  Scope, Trails).
- **v0.9.92–v0.9.93:** MilkDrop presets
  ([ADR 0091](adr/0091-milkdrop-presets.md)), and visualizers that fit a phone.
- **v0.9.94:** a film ends on what to watch next, and running as a service is
  a choice ([ADR 0092](adr/0092-the-library-moves-with-the-server.md)).
- **v0.9.99:** loudness levelling from ReplayGain
  ([ADR 0108](adr/0108-music-is-levelled-by-its-own-replaygain.md)), RAW
  photos ([ADR 0107](adr/0107-a-raw-photo-shows-the-cameras-own-jpeg.md)),
  `.cb7` comics, Media sent and a dashboard that moves while open, and Report
  an issue as a form that sends
  ([ADR 0109](adr/0109-a-report-goes-only-when-sent.md)).
- **v0.9.98:** comics (CBZ and PDF), chapters and previews on the scrub bar,
  extras, pre-roll, two-factor sign-in, invite links, friends who need not be
  reachable, and the dashboard's history.
- **v0.9.97:** a restore reloads at once, and any session that ends forgets
  its cache.
- **v0.9.96:** restore from Settings and a backup before every update (ADR
  0058 amended), webhooks
  ([ADR 0097](adr/0097-a-webhook-goes-where-the-owner-points-it.md)), metrics
  for Prometheus ([ADR 0096](adr/0096-metrics-have-their-own-key.md)), trusted
  proxies ([ADR 0095](adr/0095-a-proxy-is-believed-by-name.md)), and a
  non-service server that restarts as the server again.
- **v0.9.95:** collections show their missing films
  ([ADR 0093](adr/0093-a-collection-knows-its-missing-films.md)), retro save
  states and touch controls in a browser, readable invites, Report an issue,
  and containment that follows links
  ([ADR 0094](adr/0094-containment-follows-links.md)).

v0.9.55, 57–59, 61 and 62 were tagged and never published; each one's changes
went out in the next published release. Audio pass Phase 2 is built: night
mode for music and for films in a browser tab ([plan](audio-pass-plan.md)).

**Watch Together across servers (federation Phase 5) is built end to end**
([plan](phase-5-room-crosses-the-boundary-plan.md)): the server half in v0.9.67,
the client in v0.9.68. It was **watched between two real machines** testing
v0.9.71, which is what found the eight-second guest lag fixed in v0.9.72. Both
machines were on one LAN, so the path across the internet (timeouts, path MTU)
is still unwatched.

**Watch Together on one server was never joinable from the app until v0.9.68.**
The join endpoint and hook existed from the first build (9d342771) and nothing
called them; the panel promised a list of open sessions that did not exist.
v0.9.68 adds it, on People and in the panel. The follower side of local Watch
Together has therefore never been watched by a person either.

**Skip intro across the TV library is finished.** The
[ADR 0055](adr/0055-an-intro-is-what-every-episode-shares.md) coverage audit (#767)
produced v0.9.65 and v0.9.66, and since v0.9.83 an intro skips itself
([ADR 0083](adr/0083-an-intro-skips-itself.md)). The six gaps left in
*Futurama* season 7 are known limits of the detector, not open work.

**Shipped but never watched working.** Each one is something the suites
cannot see: jsdom performs no layout and no media, and nothing in Go runs as
session 0.

- The **MSE live path**, which is moot while Chromium plays HLS natively, and
  parked with the rest of live TV.

**Watched on 2026-11-16**, against the installed service (ledger rows 1 and 9
of the [1.0 plan](v1-plan.md)):

- **MusicBrainz albums**, read from a copy of the live database: 2,696 albums
  matched, 85 locked by hand, 82 unmatched and 1 held for review. That 83 is
  the Review badge's count exactly. No wrong match turned up in a random 15
  matched albums. The unmatched ones are albums MusicBrainz cannot know: tags
  that file an artist's loose tracks under an album called "Singles", "Unknown
  album (12/17/2011…)", self-released 2025 tracks, a magazine compilation, and
  a misspelt artist. The one in review is "Unknown Album" by "Unknown Artist",
  which is right to hold back. Artists: 1,300 matched, 65 unmatched.
- **The denial reset row** on Settings → Display is laid out properly, and its
  summary updates when a codec is turned off.
- **The remux path**: with Matroska turned off in a browser, an H.264 + AAC
  `.mkv` played. The server's decision was `remux` ("matroska container is not
  supported, but both codecs are"), with both streams copied. With Matroska
  on, the same file plays directly.
- **Skip credits on a film the server converts**: at 480p, "Skip credits"
  appeared once the credits had started, and pressing it jumped to the end and
  the end-of-film screen.
- **The follower side of Watch Together on one server** (ledger row 6). The
  first attempt found a fault. A browser guest on an AC3 episode restarted its
  converted stream four times in 90 s. Every follower seek asked for a new
  conversion, even when the target was already inside the finished remux, and
  each new conversion waited about 25 s. After the fix (#1166), the same join
  took one remux of 22 s and then stayed in sync for over two minutes with no
  restart, and a deliberate leave and rejoin did the same.

**Watched on 2026-11-17**, the sound and couch sitting (ledger rows 2, 7 and 8),
against the installed service:

- **Audiobooks with sound**, across a chapter change, with the sleep timer's
  fade. One fault was found and fixed in the same sitting. "End of chapter"
  never stopped at a chapter that is its own file. Chromium pauses the element
  at its end, then sends the last timeupdate, then `ended`. The timer fired with
  nothing left to pause and cleared itself, and `ended` played on into the next
  chapter. The sleep menu's choices were also drawn white on white. Both were
  fixed and retested working.
- **Retro**: a pad on the TV and the touch controls on a real phone both
  passed. The phone showed a steady stutter (Kirby and the Amazing Mirror, GBA, on a Motorola Edge
  2025), which is noted and not yet diagnosed. **Game Boy, Game Boy Color and Master System are labelled**: there
  are no dumps for them in the library.
- **Now Playing** in the desktop window, and the MilkDrop presets on the phone
  (smooth, no lag), passed. **Labelled:** a pad does not drive the visualizers,
  because LANcast reads a gamepad only in the retro player, and pad navigation
  outside games is after 1.0. **Synced lyrics** are labelled too, since there
  are no `.lrc` files in the library.

**Watched on 2026-11-17**, the evening with a friend on another network (ledger
rows 4 and 5), with both servers at v0.11.3:

- **Watch Together across networks.** A film that plays directly stayed about
  0.5 s apart between the two viewers, and pause and play answered at once. A
  film converted for the guest (an AC3 episode) took 44 s to connect, against
  22 s on one network, and then stayed within 5 s with no restart. That is
  inside the 8 s a guest is allowed while the conversion runs. A phone on
  cellular joined as a third viewer, the first session with three. A deliberate
  leave and rejoin passed. **Labelled:** the friend hosting, since their
  network is not set up to be reached.
- **Roster push from another network** (ADR 0103) passed: the friend's change
  showed on this side without a refresh.
- **A friend's join request over a full-screen film** (v0.10.6) passed.
- **An invite link opened on a phone on cellular** (ADR 0104) passed.

**Watched by 2026-10-07**, in the installed build: the **HLS file fallback**
as the service (#589/#590), the **Up Next** card, launching **GOG, EA and Xbox
app** games, and **media keys** with Win+Shift+Arrow to the TV.

**Watched on 2026-10-04.** The **season pass**, which now decides episode
credits from the closing theme as well as intros, ran as the installed service.
Of 1,074 episodes, 794 were done when checked: no decode failures, and the only
two failures logged were the service being stopped mid-season by an installer.
The **Duplicates** page was checked on the real library, where it showed 35
extra copies in two sections.

**Watched on 2026-09-30.** Both ran in the desktop client with libmpv removed,
so that playback took WebView2's `<video>` path, the one these claims are
about. They ran against the installed service.

- **High 10 direct-plays.** *Deep Space Nine*, "The Nagus" (H.264 High 10,
  `yuv420p10le`): a clean picture, the clock running against the true 45:18,
  no conversion started, and no decode-health fallback after 40 seconds.
- **MKV + Opus does not remux, it direct-plays.** That is better than the
  entry expected, and it means the entry asked the wrong question. *Dreamcatcher*
  (H.264 + Opus 5.1 in Matroska) played with no ffmpeg at all, the clock
  running against the true 2:13:47. The reason is that the **`hevc` claim also
  grants the `matroska` container** (decide.go: "the matroska container it
  usually arrives in"). This client claims `hevc`, so every MKV whose codecs it
  can play is sent as-is, and WebView2 demuxes it. That held here. Picture and
  clock were checked; the sound was not heard.

  **The container grant was split off in v0.9.46 (#724).** Riding on a codec
  probe, it also made a failed MKV unrecoverable: the client withdraws the
  claims a file's codecs needed, and `hevc` was never one of them for an H.264
  file. `matroska` is now its own claim, probed as
  `video/x-matroska; codecs="avc1.64001f"`, which WebView2 answers "probably"
  (measured with `cmd/wv2harness`, which had itself been broken since 2cfc6b2).
  Still unwatched: the remux path on a client that claims no Matroska.

**Open decisions** are indexed under [Open decisions](#open-decisions). A
proposed ADR is a different queue from an unbuilt feature.

**The finding that keeps recurring:** the interesting faults are found by using
the app or measuring the live database, not by the test suites. Examples are a
codec denial that silently re-encoded every film for weeks, a muxer flag
corrupting every live channel's timestamps, and a context menu painted under
the docked player for four releases. Falling back to a transcode is correct
behaviour, so a permanently downgraded machine looks like a working one. The
suites prove wiring and arithmetic. Watching it is still the only way to know
it works.

**What exists, in one paragraph.** All four milestones are built, plus the
native desktop client, which plays through libmpv. The media types are films,
TV, music, pictures and live TV, with live TV parked. On top of that sit
federation between servers, Watch Together, installed games, plugins,
semantic photo search, retro games, DLNA, optimized versions and music
visualizers, and webhooks; and from v0.9.98 comics (CBZ and PDF), chapters and
previews on the scrub bar, extras, pre-roll, two-factor sign-in, invite links and
the dashboard's history; and by v0.10.6 in-play RetroAchievements, frame-rate
matching, music browsed by album, track and genre, albums identified by
MusicBrainz, audiobooks, and playing on another of your own devices; and by
v0.10.10 a home page built around what you are in the middle of, with shelves
you pin; and by v0.10.11 settings sorted by what they describe, metadata in
your language, and a profile with your own picture. The schema is at
**revision 95**.
The [Areas](#areas) tables hold the status of each piece, and the
[Releases](#releases) table records when each one landed. What is left is
breadth, and it is listed in the [Feature backlog](#feature-backlog).

## What is left

*Everything on this list is unbuilt or unwatched. The detail behind each line
is in the [Feature backlog](#feature-backlog) entry it names.*

**Open faults from use.** Reported by the owner 2026-11-03 and 2026-11-06,
diagnosed as far as the evidence went. The two still open each lost its
server log to Debug rotation. v0.10.5 quieted the polling, then v0.10.6's
play-on-another-device heartbeat and a router's DLNA searches filled it again,
to eleven hours by 2026-11-08; with those left out too (v0.10.8)
it holds about 28. The next step for each is to reproduce it and copy the log
at once.
- ~~**Continue Watching emptied itself.**~~ **Fixed in v0.10.7 (#1025).** Home read one mixed list of 20 and split it afterwards, so
  half-played songs took the films' places: 16 of the newest 20 rows were
  tracks on the owner's library. `/api/continue?shelf=` now gives each shelf
  a limit of its own. Passed as the installed service on 2026-11-07.
- **Music stalled loading on a phone.** No evidence survived. Reproduce, then
  read the `/api/stream` request times and the "music on deck/element" lines.
- ~~**The encoder stays held after a guest stops a film.**~~ **Not a fault.**
  Reproduced 2026-11-07: a friend's stopped film was released at 608 s idle,
  the ten-minute `IdleTimeout` that lets a pause resume. An explicit
  end-on-stop is worth building only if a faster release is wanted.
- **A sign-in at the public name was refused** (2026-11-04). The tray half was
  fixed in v0.10.5; why the window's own sign-in failed is not known, and no
  code fault was found. Next time, grep `login refused` with `method=passkey`.

**Next: the road to 1.0** ([plan](v1-plan.md)). Settings 3.0 and Profile
Page 2.0 shipped in v0.10.11. What remains before the freeze is the owner's
last notes (plan §9); then freeze scope at v0.11.0: empty or label the list below, prove the clean install, the
update from 0.9.79 and the Linux archive on real artifacts, and rewrite the
documents for a newcomer. The last item is an acceptance run of the
candidate as the installed service, and the release itself.

**Shipped from the previous Next list.**
- ~~**Audiobooks step 3**~~ — **shipped in v0.10.7**
  ([ADR 0116](adr/0116-an-audiobook-is-its-own-library.md)): a sleep timer
  (15 to 60 minutes or end of chapter, fading out), narrators read from the
  composer or narrator tag, and filters by author, narrator and series. No
  schema change. Never heard with sound or run as the service.
- **In-play RetroAchievements** — unlocks while you play (rcheevos). **Shipped
  in v0.10.2** ([ADR 0112](adr/0112-achievements-are-earned-through-the-server.md)):
  casual (softcore), in the desktop player, through the server, which holds the token
  and keeps unlocks that cannot be sent yet. Schema 87. The profile half shipped in v0.9.88.
  **Hardcore rules built and hidden** (step 5): no state loads or rewind in hardcore,
  Continue drops to casual, turning it on restarts the game. All behind
  `ra.HardcoreApproved`, which stays false until RetroAchievements approves LANcast.
  **Step 6, ready to apply:** the full user agent, [docs/privacy.md](privacy.md) and
  [docs/cores.md](cores.md) are built; [the checklist](retroachievements-application.md)
  names what is still open (leaderboard trackers, queued leaderboard entries). The
  application can go in from 2027-02-02.

**Feature backlog, not yet planned.**
- Libraries and media: **ebooks** beyond comics (EPUB),
  `.cbr` comics, and photo **bursts**. Loudness levelling from ReplayGain tags
  is built ([ADR 0108](adr/0108-music-is-levelled-by-its-own-replaygain.md));
  measuring untagged tracks is not.
- Playback: **Chromecast and AirPlay**, and **offline downloads** (the client half).
  **Play on another of your own devices** shipped in v0.10.6
  ([ADR 0117](adr/0117-play-on-another-device.md)); audiobooks, games,
  comics, photos and live channels cannot be sent yet.
- Social: **Last.fm and Trakt scrobbling**.
- Operations: **bandwidth limits**, and **granular sharing** by label or
  on a friend's server (confirm somebody needs it first).
- Security hardening: **ffmpeg under fewer privileges** than LocalSystem, an
  **update path for the pinned ffmpeg** (check before building), and **OIDC**
  with per-key API rate limits (two-factor codes are built).

**Back burner.** **Theme music**, blocked on OST identification, and
**artist images from TheAudioDB** (provider built, not wired, needs a key).
A dedicated TV or phone client is not planned; Bigscreen mode covers the
10-foot case.

**Parked.** Live TV (2026-08-29): the guide grid (ADR 0039 step 4), ADR 0013
step 6, which is now native HLS against MSE, and
[ADR 0065](adr/0065-a-channel-nobody-is-watching-stops.md), a channel nobody
is watching that keeps running.

**Built, not yet watched working.**
- **Extras** (#950), **labelled, not watched**: the real library holds no
  extras at all (checked 2026-11-16), so an MKV extra in the desktop window
  is proven by the tests alone. Chapters, pre-roll, two-factor sign-in and the
  comics reader (CBZ and PDF) all passed as the installed service on
  2026-10-27.
- From v0.9.96: trusted proxies behind a **real** Caddy or nginx, metrics in a
  real Prometheus, and webhooks into a real Home Assistant. All five were
  checked as the installed service before release (restore through
  `service restart`, the pre-update backup on a real migration, the proxy
  header simulated with curl, metrics with curl, webhooks into a local
  receiver).

## Releases

Every release from v0.1 to v0.11.3, with what shipped and what was not looked at, is in [history.md](history.md#releases). From 1.0 the GitHub release notes, written to `docs/releases/`, are the record.

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
| Data model evolution and migrations | **built** | Forward-only migrations (rev 1→95); collections, hierarchy, multi-part & serial works ([ADR 0017](adr/0017-collections-and-multi-part-works.md)) |
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
| Client capability negotiation | **built** | Clients report what they decode (`?can=`) and the server widens the profile ([plan](client-capabilities-plan.md)). `?profile=` had existed for a release and no client ever used it, so a browser that decodes HEVC in hardware was still served a full re-encode of every HEVC file — the whole of the "slow between films" complaint. Additive and widen-only, resolved once for both decision endpoints so they cannot disagree, and a claim that proves false is dropped, remembered, and retried as a conversion. **Remembered used to mean for ever**, which turned the safety net into a ratchet: one machine had written off all four claims it can make and was re-encoding every HEVC film as a result, invisibly, because falling back is correct behaviour. Denials expire after a fortnight and can be cleared from Settings → Display (`845610f`) — a denial is a fact about this machine *today*, and a driver update, a WebView2 update or a codec extension being installed cannot announce themselves here |
| 10-bit H.264 | **built** | `high10` lets a client that can decode High 10 direct-play it instead of being served a full video re-encode (`6ae4396`). Excluded from every profile before, on a comment that read as a fact about browsers and was a fact about nobody asking — Chromium answers `probably` for `avc1.6e0033`, and High 10 is most of an anime library. Unlike `hevc10` it trusts no *native* listing, since H.264 is listed by every profile: the same rule would grant High 10 to a set-top box for decoding 8-bit, and High 10 is missing from most fixed-function decoders that manage High profile fine |
| Lossless through a container rewrite | **built** | `flacmp4` and `opusmp4` let a client that can read FLAC or Opus *inside MP4* have them copied rather than re-encoded when only the container is wrong (`c969561`). Before it, rewrapping an MKV turned lossless into AAC to change a box — the one conversion here that cannot be undone. Gated on a claim rather than widened into the floor, because "can decode FLAC" and "can decode FLAC in MP4" are different questions and only the first is true of every browser. ALAC needs no claim, MP4 being its native home; Vorbis is deliberately excluded and asserted so |
| ffmpeg pipeline and HLS | **built** | Progressive fMP4 + HLS, session lifecycle |
| Hardware acceleration | **built** | NVENC, QSV, AMF, VideoToolbox — verified by test encode |
| Subtitles | **built** | Sidecar, embedded, WebVTT, OpenSubtitles hash matching |
| React client build | **built** | React + TS + Vite; Home shelves, Browse, Detail, Player, Settings; subtitles local + online; central spatial focus controller (ADR 0004) |
| Skip intro, skip credits, Up Next | **built** | Markers in their own table ([ADR 0054](adr/0054-a-marker-says-where-the-film-stops.md), [ADR 0055](adr/0055-an-intro-is-what-every-episode-shares.md)). Film credits come from black runs put to a frame gate. Episode credits come from the closing theme a season shares, checked against black runs. Intros come from audio a season shares. Every rule was tested on a held-out sample judged by eye. A visible button, never an automatic jump; Up Next counts down and then rolls on through the episode's own ending |
| Theme music subsystem | specced · **back burner** | Behavior in design.md; blocked on OST identification, which is unplanned. Deliberately parked rather than waiting: nothing else is queued behind it |
| Music player UI | **built** | Album view with a numbered track list, square sleeves, an audio mode in the player, and a docked mini-player so leaving the player no longer stops the record ([plan](music-client-plan.md)). Playback moved above the router to make that possible — the media element used to be a child of the `/watch` route, and a route owns its DOM |
| Branding & splash | **built** | App icons + favicon from the emblem, web manifest, and a once-per-session animated splash. Source art in `/assets` |

### Extensibility · M4

| Area | Status | Note |
|---|---|---|
| Plugin runtime and sandbox | **built** | WebAssembly via wazero, deny-by-default capabilities ([ADR 0020](adr/0020-plugin-isolation-boundary.md)); validated by OMDb-as-plugin |
| Extension point catalog | **built** | `rating_source` first; new source for an existing capability. Widening to new kinds waits for a plugin that needs it |
| Plugin distribution and trust | **built** | Signed `.lcplugin` bundles, two-layer trust (Ed25519 + capability grant), two-step install, Add-ons page ([ADR 0021](adr/0021-plugin-distribution-and-trust.md)) |
| Client surfaces: TV, mobile | **partly built** | The 10-foot case is covered by Bigscreen mode, one attribute and a `zoom` rather than a second client, and the focus model held. A dedicated TV or phone client is not planned |

### Native desktop client · ADR 0023

| Area | Status | Note |
|---|---|---|
| Stage 1 — own the window | **built, default** | `LANcast-Client.exe` (since v0.9.79 `LANcast.exe`, [ADR 0080](adr/0080-one-executable-two-roles.md)) opens a WebView2 window instead of handing a URL to a browser ([plan](native-client-plan.md)). **Pure Go, `CGO_ENABLED=0`** — the ADR's assumed CGO cost was wrong, tested rather than argued, so the single-runner release matrix survives. The binding is a trimmed vendored copy with the embedded DLL and its from-memory loader removed ([provenance](../internal/webview2/PROVENANCE.md)); Microsoft's signed loader ships beside the executable |
| Certificate trust | **built** | The point of owning the window, and worse than the ADR assumed: against a LAN-bound server the web view does not warn, it fails the handshake and retries, so the app never loads. The client pins the server's public key, read from its own `cert.pem` on local disk; every other certificate is still validated |
| Flip `-window` to default | **built** | Done after living with it, not after arguing about it. The browser lost on three things a tab cannot fix: LANcast cannot say what its close button means, cannot pin the server's certificate, and gets a warning against a LAN-bound self-signed server that the window does not need. `-browser` is the opt-out, `-window` is kept as a no-op alias so existing shortcuts and the autostart run key keep working, a machine with no WebView2 runtime falls back on its own, and the installer's finish page offers both |
| Installed games | **built** | Steam, Epic, Battle.net, the Xbox app, GOG and the EA app, each read from the launcher's own files in the client, with no sign-in ([ADR 0066](adr/0066-a-game-belongs-to-the-machine-it-is-installed-on.md)) |
| Stage 2 — own playback (libmpv) | **built** | Shipped in v0.9.28: the window decodes with libmpv ([ADR 0067](adr/0067-the-desktop-client-plays-through-libmpv.md)), built by LANcast itself as LGPL ([ADR 0069](adr/0069-lancast-builds-its-own-lgpl-libmpv.md)), and streams with a ticket ([ADR 0068](adr/0068-a-native-player-streams-with-a-ticket.md)). Since v0.9.29 the server no longer converts anything for the desktop client |

### Cross-cutting

| Area | Status | Note |
|---|---|---|
| Users, auth, sessions | **built** | Multi-user accounts with admin/member roles (ADR 0015); per-user watch state |
| Remote access | **built** | A VPN is still the simplest route. Beyond it: a public name with a publicly trusted certificate ([ADR 0081](adr/0081-a-public-name-can-earn-a-public-certificate.md)), passkeys ([ADR 0082](adr/0082-a-passkey-belongs-to-the-public-name.md)), trusted proxies ([ADR 0095](adr/0095-a-proxy-is-believed-by-name.md)) and invite links ([ADR 0104](adr/0104-a-friend-is-invited-by-a-link-that-works-once.md)); see security.md |
| Security model | **built** | Auth, CSRF, throttling, loopback-until-secured |
| Transport security (TLS) | **built** | HTTPS beyond loopback; bring-your-own or self-signed cert, http→https redirect (ADR 0014) |
| Performance targets | **built** | Budgets for a 40k-item library, accepted and met for the filter bar, every grid page by title, year and date added, search, Continue Watching and Recently Added; guarded on the query plan, not the clock. The Unwatched shelf was quadratic and is now 57ms against 50; a rating-sorted page 39,000 deep and the two-facet page sit at their 150ms edge. All three are recorded misses ([ADR 0057](adr/0057-what-a-forty-thousand-item-library-may-cost.md), amended 2026-10-09) |
| Packaging and distribution | **built** | One executable, `LANcast.exe`, the app or the server by how it starts ([ADR 0080](adr/0080-one-executable-two-roles.md), v0.9.79; two branded executables before that), goreleaser matrix, in-binary service install, signed-tag releases with a Windows installer ([ADR 0016](adr/0016-packaging-and-distribution.md), [ADR 0022](adr/0022-client-and-server-executables.md)) |
| Backup and restore | **built** | A backup is the database and nothing else ([ADR 0058](adr/0058-a-backup-is-the-database.md)) — `VACUUM INTO`, **441ms on a 103MB library** from a live server with no downtime, where a file copy under WAL yields a torn, *plausible*, wrong file. Taken and downloaded from Settings, restored with `lancastd restore` on the machine with the server stopped. **Restoring is deliberately not an endpoint**: a server that could restore onto itself would be replacing the file it is reading. Artwork is excluded and the cost is stated — the cache is 4.6GB against 100MB and re-fetchable. A backup carries no sessions, cleared when it is written rather than when it is restored, so a file copied to a stick cannot sign anyone in |
| Activity view | **built** | `GET /api/activity` in one shape for every worker; a nav indicator and task popover. Indeterminate where the worker genuinely cannot know its total — a scan discovers its own size, so it shows a count rather than a lying percentage |
| Observability | **built** | Match score breakdown, review queue, scan skip diagnostics — including `skipped_kind`, the count of media a library's own kind discards — and a single-instance guard that names what is holding it: the service, its pid and its data directory, read at a privilege an unelevated caller actually has. The gap once listed here, a show library created as a movie library scanning silently, closed in v0.6.27 ([libraryshape.go](../internal/store/libraryshape.go)) |
| Desktop lifecycle | **built** | *Open on Windows start* and *Close to tray* ([plan](desktop-lifecycle-plan.md)), both off by default and both shown only in the LANcast window, because a tab has no tray to reduce to and no close button LANcast owns. The Settings section states which of the three meanings of "closed" you are looking at — stop the server, or leave it running because the service owns it. Autostart records the mode you chose, so picking the browser does not silently become the window at login |
| Update checking and self-update | **built; the routine update path since v0.9.79, with no recorded run from the oldest supported version (a 1.0 check, [v1-plan.md](v1-plan.md) §3D)** | Check, download, verify, stage, and swap on the way down. **The download was broken from the day it shipped until v0.6.4**: the release lookup asked a JSON endpoint for octet-stream and GitHub answered 415, so v0.6.2 and v0.6.3 could find an update and never fetch one. Found by pressing the button, not by reading — the first release that could have been installed was the first test the code had ever had. **v0.6.5 was the first release the updater could fetch, and it got further than
before and still not all the way**: it downloaded, verified and staged
correctly — the swap applied, the binaries were replaced — and then stopped
dead, because a service never shuts down on its own and the update is applied
on the way down. v0.6.6 adds the restart. What remains untested is the whole
path in one go on an installed artifact: download, stage, restart, come back on
the new version. Two releases have now been spent finding one more link in that
chain each time, which is what a path that only executes during a release costs. What *is* proven, on the published artifacts: the signature verifies against the key compiled into the shipping binaries, and the Windows archive's digest matches the one in the signed list — signature → checksums → bytes, all three links checked rather than assumed. Signed releases first, because auto-install is a LocalSystem process executing a downloaded binary and without authenticity that is remote code execution as SYSTEM — an Ed25519 signature over `checksums.txt`, offline verification against a key compiled into the binary, and a **separate key from the plugin project key** so one compromise is not both. Three distinct states: unsigned is installable by hand only, a wrong signature is refused outright, and a build with no key refuses everything. The swap works because a running process on Windows can rename *itself*: the binary moves aside to `.old`, the staged one takes its place, and the next start is the new version — one restart, no UAC prompt, no second process. Nothing is written into the install directory before the swap; staging lives in the data directory. The check is on by default and is not a phone-home exception — a plain GET with no install identifier, no statistics, no history, and switchable off entirely |
| Audit log | **built** | Who changed what, and when, recorded server-side where the mutation is authorised ([ADR 0026](adr/0026-audit-log.md)) — libraries, deleted titles, overridden matches, accounts, add-on trust. An audit trail a client writes is forgeable by the client it is auditing. Readable from Settings, newest first, filterable by action. Browsing and playback are deliberately excluded: burying a handful of deliberate acts under a million routine ones is how a log becomes unreadable. Each entry freezes the actor's name and a sentence at the time it happened, so "who deleted this library" still answers after both the library and the account are gone |
| Testing strategy | **built** | CI runs go test, the client lint and build, the vitest client suite (about 2,000 tests, jsdom, so no layout) and a bundle-drift check; fixture libraries, no real media |
| Licensing and open-sourcing | **done** | **AGPL-3.0-or-later** (`LICENSE`) with a commercial licence available (`COMMERCIAL.md`, ADR 0053) — chosen because a media server can be run for other people without ever being *distributed*, which plain GPL does not reach. Possible at all because the copyright is undivided: 1,041 commits, one contributor. Releases up to v0.8.44 were **MIT** and remain so; a relicence is forward-only. `NOTICE` carries the attribution Apache-2.0 requires, and the settings page links to the source because section 13 asks for it. **The repository is public**, which is what the update check needed: the releases API returns 404 for a private repo and cannot distinguish "no such repository" from "not yours to see", so the checker was correct and inert until the switch was flipped. Vendored code keeps its own notices and the README points at them. **The history cleanup did not happen before the repo went public and is now a judgement call rather than a scheduled one** — three `lancastd.exe~` blobs from the M3 era still sit in history (~25 MB packed, about a third of `.git`). Rewriting 352 commits and re-pointing every release tag was worth doing *once, before* the first public clone; after it, a rewrite also breaks every clone and every commit link that already exists. The honest options are to accept the weight or to pay it deliberately and announce it; `*.exe~` is gitignored, so it cannot grow either way |

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

- **Organising a large channel list** — **built** except the deferred
  guide grid: playlist selector, groups that open, and hidden and favourite
  channels per device.
  [ADR 0039](adr/0039-organising-a-large-channel-list.md). A real server now
  carries **1,862 channels from one provider** with a second playlist beside it,
  merged onto one page with no way to ask for one and not the other; the ~60
  group chips wrap to five rows before a single channel is visible, so the
  filter row is taller than what it filters. Four changes in cost order: a
  `source_id` filter on `/api/channels` with a selector that appears only when a
  second playlist exists, groups that open rather than filter, per-device hidden
  and favourite channels, and a guide-first grid **deferred** — ADR 0036 already
  refused the grid for the schedule strip, and it would render 1,862 rows of "no
  listings" while no provider in use publishes XMLTV. Virtualising the tiles is
  named and rejected: the ask is not to be shown 1,862 tiles, not to scroll them
  faster.
- ~~**More branded, thematic home page** — beyond functional shelves.~~ —
  **built**: a masthead that greets by name and by local hour, states the size
  of the collection, and puts the libraries in reach as destinations with their
  counts. It compresses when there is a hero below it, because two full-height
  openings stacked is one too many. **No gold anywhere on it except the focus
  ring** — the brief invites decoration and decoration is exactly what would
  kill the focus signal, so the theme comes from the nebula palette and the
  letterspaced caps instead. It also gives a fresh install something to say:
  before this, a server with no watch history and no recent additions opened on
  a blank page.
- ~~**Auto-expanding / collapsing navigation bar**~~ — **built** in v0.6.7,
  alongside the move from a horizontal nav to a vertical rail. It expands over
  the content rather than displacing it, and on focus as well as hover.
- ~~**Movie library page** and **TV-show library page**~~ — **done**
  (Phase 1): media-type-aware browse views selected by library kind.
- ~~**Add-ons page**~~ — **built** as a real page at `/addons`. The rail has
  called Add-ons a destination since the shell was built and pointed it at
  `/settings?pane=addons`, which taught that Add-ons is a setting. It lists what
  is installed and says plainly why there is nothing to install yet — a plugin
  contract is a promise about the shape of the core, and making that promise
  before the core is finished is how a plugin API becomes the thing that stops
  the core changing. Installation stays in the settings pane that already does
  it properly rather than growing a second uploader to keep in step. Still admin
  only, for a narrower reason than before: `/api/plugins` is admin-gated, so a
  member would reach a page that could not tell them whether anything was
  installed — and it says *that* instead of showing them an empty list.
- ~~**More defined settings page** — a real structure, not a flat list.~~ —
  **built** in v0.6.9. Categories down the left, one pane at a time, the pane
  in the URL. Grouped by whose setting it is (server versus this device) rather
  than by subject, because that is the distinction that matters once two people
  share a server. Also added: a General pane showing the server and API version
  from `/api/health`, which no client had ever asked for, and controls for
  `rate_per_sec` and `update_check` — both accepted and validated by the API for
  as long as it has existed, and reachable until now only by hand-editing
  `config.json`.
- ~~**Downloads page** and a **download handler**~~ — **built.**
  `GET /api/items/{id}/download` serves the original as an attachment, named
  from the item's metadata rather than its path (`Arrival (2016).mkv`, and
  `Show - S02E07 - Title.mkv` for an episode, because `Pilot.mkv` collides with
  every other pilot ever made). **Never transcoded**: the transcoder exists so a
  device that cannot play a file can still watch it, and a download that quietly
  returned a re-encoded copy would be a lie about what you have. Range requests
  are honoured, so an interrupted nine-gigabyte transfer resumes. The page
  itself is deliberately **a receipt list, not a transfer manager** — once a
  download starts the browser owns it, and a progress bar over an unobservable
  transfer is a guess that reads as fact. Per device, because the phone that
  downloaded something is the phone that has the file. What is still open: an
  add-on's content, which has no route to serve until add-ons do.
- ~~**Profile page**~~ — **built** (details under Social and profiles below).
- ~~**Bigscreen (10-foot) mode** — with a settings option to enable it at
  startup.~~ — **built**, as one attribute on the document root and a `zoom` on
  `body`. Not a second client: a separate television UI is a second set of
  screens to keep in step, and every product that has built one has spent the
  years afterwards explaining why a feature exists in one and not the other. It
  is a zoom rather than a set of larger tokens because every size in this client
  is in px, so a token-by-token enlargement would mean auditing every stylesheet
  and would still miss the ones written after the audit. Applied **before the
  first paint** by four lines in `index.html`, since React cannot read
  localStorage until it has mounted and a television that flashes the desk
  layout on every load reads as an app breaking and correcting itself. The
  setting is per device — "I am ten feet away" is a fact about the room, not
  about the account — and `Ctrl+Shift+B` toggles it from anywhere, because the
  way people find it is by turning it on and wanting out. It works at all only
  because the keyboard model came first (ADR 0004): a pointer-only client would
  have needed a rewrite, and this one needed a stylesheet.

- ~~**Homepage hero as a recommendation**~~ — **built.** Four modes in
  [heroMode.ts](../web/src/lib/heroMode.ts), per device beside `bigscreen` and
  `spoilers`. The description below is what was asked for; three notes on what
  it turned into.

  **The fallback chain is the feature**, more than the modes are. Every mode
  ends with resume-then-recent, because each way of having nothing to show is
  ordinary — a new library has nothing to resume, a fully-watched one nothing to
  suggest, and a pinned item can be deleted by somebody else while you are
  looking at the page. None of those is a reason to render a hero-shaped hole.

  **Recommended is not an engine, and the honest description is the design.**
  It takes the thing you are part-way through, reads the genres off it, and
  finds something unwatched in the same library sharing one — so it can always
  say where it came from, and the spotlight renders that sentence: *Because you
  watched X*. A candidate it cannot attribute is not shown as a suggestion at
  all, which is the guard against this quietly becoming the recommender the
  no-phone-home rule exists to refuse. The seed is fetched in full because
  genres are a detail response only; that request, and the candidate search,
  fire **only** in this mode. The two default modes still cost nothing, which
  matters on the first screen of the app.

  **Pinning is a gesture on the item, not a field in Settings.** "Pin to
  homepage" is a thing you think while looking at a film, and it sets the mode
  as well as the id — a pin that stored a number and changed nothing you can see
  would be this project's favourite bug wearing a new hat. Settings shows what
  is pinned and offers the way out.

  Original entry: `pickHero` in [Home.tsx](../web/src/screens/Home.tsx) is a fixed
  rule: the first resumable item carrying fanart, else the first recently added,
  music and pictures excluded. Resume-wins is right for the common case and
  wrong for the one actually complained about — a library nobody is mid-way
  through, where the hero becomes a permanent monument to the last thing
  watched. Four modes: continue watching (today), recently added, recommended,
  and one item pinned by hand. **Per device**, in `localStorage` beside
  `bigscreen`, `tileSize` and `spoilers`, not a server setting: the hero is what
  one person sees on one screen, and the reasoning under "Bigscreen mode" above
  applies unchanged. **Recommended is the half that needs designing**, because
  LANcast has no recommender and must not grow one that phones home. The
  material is all already local — genre, decade and content rating on the item,
  `playback_state` for what got finished rather than started, and the private
  per-user ratings from [ADR 0035](adr/0035-who-may-see-whose-viewing.md). The
  honest first version is "something unwatched, from a genre you finish" and
  should be named as that rather than as an engine. Whatever the mode, **the
  fanart test still gates it**: a hero with no backdrop is a grey slab, so a mode
  with no qualifying candidate falls back to the next mode rather than rendering
  empty.

### Libraries and media types

- ~~**Playlists, and importing `.m3u`**~~ — **built** in v0.6.9
  ([ADR 0030](adr/0030-playlists-and-m3u.md), accepted). A playlist is
  `kind = 'playlist'` on `media_item` — no new *item* table, the third media
  concept to manage that — but membership needed one: `item_collection` is keyed
  `(item_id, collection_id)` and so cannot hold the same track twice, which is
  ordinary in a playlist and impossible in a collection. `playlist_entry` is
  keyed on **position** instead, which is that difference written in SQL
  (schema 17). An `.m3u` is an **import, not a mirror**: the database is the
  truth, and editing a playlist locks its membership so a rescan cannot undo the
  edit — the locked-fields rule applied to membership. Unresolvable lines are
  counted and reported rather than silently dropped, our own HLS output is
  refused, and Windows paths resolve on Linux. The repeat case forced the queue
  cursor to become a *position* rather than an id: `indexOf` always finds the
  first occurrence, so a track appearing twice used to send playback backwards
  and strand everything after it.
- ~~**Editing a playlist**~~ — **built** in v0.6.10, after v0.6.9 shipped
  playlists that could be imported and played and not changed. Five writes:
  create, add, reorder, remove an entry, delete the list. Entries are addressed
  by **position**, not by item id, for the same reason the queue cursor is — an
  id does not name a row in the one listing that may hold the same id twice —
  and a removal resequences so a position stays the index the client rendered.
  Every membership write **locks `members` before writing**, which is what stops
  the next scan re-importing the `.m3u` over the edit; a rescan against the real
  test libraries confirms it, re-importing the untouched playlist beside it and
  leaving the edited one alone, with the `.m3u` on disk byte-identical.
  Deletion is its own route rather than `DELETE /api/items/{id}`, whose modes
  are about *files*: one would delete the seeding `.m3u`, the other ignore-list
  it. The writes need a session but **no particular role** — the admin gate is
  for filesystem access and account control, and a playlist edit is neither —
  which stands until ADR 0030's open question about per-user playlists is
  decided. In the client, playlist rows gain reorder and remove-from-list
  controls (buttons, not a drag: a d-pad cannot drag), numbered by position
  rather than by the track numbers they carried on their own records, and any
  playable item gains **Add to playlist**, which appends and can create the list
  on the way.
- ~~**Wide-scope audio codec support** — MP3, FLAC, WAV.~~ — **done**: eleven
  audio formats scanned, and audio containers are first class in the playback
  profile so a FLAC is not re-encoded to deliver a format every browser plays.
- ~~**Music library.**~~ — **done**, end to end: server-side in v0.5.0, client
  UI and mini-player in v0.6.0.
- ~~**Photo library** with a built-in **image viewer**~~ — **built** in v0.6.5 ([ADR 0028](adr/0028-pictures-library.md), [plan](pictures-plan.md)). Folders become galleries, because a filename like `openart-f81b76…_raw.jpg` says nothing and there is no provider to ask. Thumbnails run in their own worker through the existing content-addressed cache; HEIC decodes through the ffmpeg already required, because a phone backup is mostly HEIC and a wall of placeholders would be a feature that looks finished and is useless. EXIF orientation and date-taken only — **GPS deliberately unread**, since the safest way never to leak location data is never to load it.
- **Live TV** — a tuner page and function. **Channels, playback and the EPG
  are built; what is not right is how the picture is paced.** A channel
  source is an M3U — from an IPTV provider or from a tuner on the network —
  and a channel is deliberately **not
  a `media_item`**: that table describes works, and a channel has no duration,
  no file, no position and no identity a provider could match, so it would have
  meant six nullable columns and a `kind` every listing must exclude
  ([ADR 0002](adr/0002-one-wide-media-item-table.md)). Channel lists are
  routinely credentialed, so **the upstream URL never reaches a client**: clients
  play through a proxy that takes a channel id rather than a URL, and for HLS the
  playlist is rewritten so segments resolve *relative to that channel's own
  base* — nothing a caller sends can change the host, which is what keeps it
  from being an open relay inside somebody's network. Refreshing **replaces**
  rather than merges, because the file carries no id worth trusting across
  versions and merging duplicates every channel each time the guess is wrong.

  **Playback now works in any browser.** `GET /api/channels/{id}/live` routes a
  channel through the ffmpeg pipeline that already produces progressive fMP4 for
  files, which is what makes an MPEG-TS channel playable in Chromium at all —
  `canPlayType("video/mp2t")` is empty, and [ADR 0013](adr/0013-transcode-pipeline.md)
  refuses to vendor hls.js. Verified end to end: a real transport stream
  decoding at 640×360 in the browser, buffered and advancing.

  Usually a **remux**, not a transcode — nearly every channel is H.264, which
  fMP4 takes as-is. Audio is treated the opposite way, and the asymmetry is the
  design: a video encode costs a core per viewer and an audio encode costs a few
  percent, so video gets the benefit of the doubt and audio does not. Audio is
  copied only when it is *known* to be AAC and re-encoded otherwise, because
  guessing wrong about audio produces a working picture with silence — the
  failure that looks like success.

  The bug that found: **AAC inside MPEG-TS carries ADTS framing MP4 refuses.**
  Without `-bsf:a aac_adtstoasc`, ffmpeg emits a valid `ftyp` box, rejects the
  first audio packet and exits — 16 KB where the fix produces 1.05 MB on the
  same source, and a browser showing one frame before stopping. The most common
  live format in existence, failing in the way hardest to attribute.

  ffmpeg's lifetime is the request's, tested against a real process: a live
  source never ends, so nothing else would ever stop it and a leak pulls a
  stream at full rate for ever.

  **The EPG is built** ([ADR 0036](adr/0036-epg.md)). A source can carry a
  second URL — an XMLTV guide, plain or gzipped — and the Live TV page shows
  what is on now and next on every tile, with a schedule under the player and a
  search that reads programme titles as well as channel names.

  Listings attach to channels by **`tvg-id` and nothing else**, and that is a
  decision to refuse a feature rather than a gap. Matching on display name is
  the obvious fallback and it attaches "BBC One" listings to "BBC One HD" with
  total confidence — a failure that is *invisible from the guide*, because the
  titles and times are all plausible and the only way to find it is to watch the
  channel. A channel with no `tvg-id` says it has no listings instead.

  The ordering constraint that fell out of it: replacing a source's channels
  cascades their listings away, so a refresh imports the channel list first and
  the guide second. The other order yields an empty guide and no error anywhere
  to explain it. Tested from both sides.

  Guides refresh themselves every twelve hours, unlike library scans, which are
  opt-in. The asymmetry is the point: an unrefreshed guide does not go blank, it
  goes *wrong* — it is the only thing here that decays into a falsehood by being
  left alone.

  **Still not built:** hardware tuners (HDHomeRun and friends — no device to
  build against), and recording. **Recording is on the back burner** — it is the
  piece that turns a channel list into something you own, and it is also the one
  that needs somewhere to put the files and a decision about what a recording
  *is* once it lands in a library, which is a bigger question than the feature
  looks.

- **~~The rest of the launchers — GOG and EA~~ — built.** **Xbox** followed on 2026-10-04,
  read from the `.GamingRoot` file on each drive and each game's
  `MicrosoftGame.config` and `appxmanifest.xml`. It is launched by application
  user model id through `shell:AppsFolder`, the same route the Start menu takes.
  The package family name is computed from the manifest, and it was checked
  against the one Windows reports for a real install: Minecraft for Windows in
  `D:\XboxGames`. **GOG and EA followed on 2026-10-06**, read from GOG's registry keys and
  `goggame-<id>.info`, and from EA's uninstall entries and
  `installerdata.xml`. Each is started the way its own shortcut starts it:
  GOG through Galaxy, EA by the game's executable. Steam **shipped in
  v0.9.19** under
  [ADR 0066](adr/0066-a-game-belongs-to-the-machine-it-is-installed-on.md), which
  settled the model for all of them: a local read in the desktop client, no
  sign-in, no server table, off by default, and the launcher's own files as the
  only source of truth. **Epic and Battle.net followed in v0.9.27**
  ([internal/games](../internal/games/)). This entry said Epic was deferred
  until the artwork question was answered, and that went stale on the day it
  shipped. The answer was the one the entry had already named as honest:
  **lettered placeholders**, the same ones a Steam game without artwork uses,
  because Epic keeps nothing on disk that can be tied to a game, and a guessed
  image is worse than a letter.
  What follows is the original capture, kept because
  it is the reasoning that produced the ADR. It was the first
  backlog item that **broke the model rather than extending it**, which is why it
  needed an ADR before it needed a design.
  [ADR 0002](adr/0002-one-wide-media-item-table.md) claims a new media type is a
  new `kind` value and no migration, and that claim has now survived music,
  pictures and photos. It does **not** survive this one, and the reason is not
  the schema — it is *server owns truth, clients are thin*. Every kind so far is
  a file on the server, which the server can read, probe, transcode and stream to
  anything on the LAN. **A game is installed on the client's machine**, cannot be
  streamed, and is unlaunchable from every other device in the house. Put it in
  `media_item` and every listing starts lying: it surfaces in search, in Recently
  Added, and on a phone that can never run it. The alternative is a machine
  identity on the row plus an exclusion in every listing that already exists —
  which is precisely the six-nullable-columns argument ADR 0002 used to keep
  **channels** out of `media_item`. Channels are the precedent, and they got
  their own table.

  The mechanics are the easy half and cost nothing in principle: Steam is
  `libraryfolders.vdf` plus `appmanifest_*.acf` under `steamapps`, Epic is a
  folder of JSON manifests — both local file reads, **no API key and no
  network**, so *no phone-home* holds without an argument. Launching is a URI
  handler (`steam://rungameid/…`, `com.epicgames.launcher://apps/…`). Artwork is
  the one part that wants a provider, and providers are the thing this codebase
  is built out of, so that part fits.

  **The scope question is what the ADR must answer first:** is LANcast a media
  server that also launches games, or is this a big-picture front end that
  belongs behind a plugin? The plugin runtime exists and a games source is a
  plausible first genuinely third-party plugin — except that a deny-by-default
  WASM sandbox ([ADR 0020](adr/0020-plugin-isolation-boundary.md)) **cannot
  launch a local executable**, because launching local executables is the exact
  capability it was built to refuse. Granting it a "run this URI" capability
  reopens that door for every plugin that follows. That tension is the decision,
  and nothing should be built until it is made.

- **Retro games (emulated ROMs)** — **built**: all three stages, released in
  v0.9.69, with the core download, the BIOS check, rating by hand and the
  pad-driven menu following in v0.9.70.
  [ADR 0073](adr/0073-a-retro-game-is-a-file-the-server-owns.md) and its
  amendments hold the decisions; [the build plan](retro-games-plan.md) opens
  with where it stands. Played on real cores in the window: NES, SNES, GBA,
  Mega Drive, N64 and PlayStation. Not yet played: Game Boy, Game Boy Color
  and Master System, for want of dumps. **A browser plays them too** since
  v0.9.76 ([ADR 0077](adr/0077-a-browser-can-play-a-game.md)). The server
  fetches libretro's web cores, and the browser plays NES, the Game Boy
  family, Master System and Mega Drive, with the game's own save shared with
  the desktop app. In a browser so far: NES, GBA and Mega Drive. SNES, N64 and
  PlayStation stay desktop-only, SNES because its only web core forbids
  commercial use. **Built since, in v0.9.95:** numbered states in a browser,
  each saying which player made it (#928), and on-screen controls for phones
  (#929). In-play achievements are built (ADR 0112, PR #989). **Still open:**
  Game Boy, Game Boy Color and Master System played on real dumps.

  Original entry, kept for the reasoning: the request was to play old
  console games, N64 first, inside LANcast without keeping a separate emulator
  front end up to date. **A ROM is not an installed game.** ADR 0066 kept PC
  games off the server because they live on one PC and cannot be served. A ROM
  is a file the server can hash, identify and hand to a client, which is what
  every kind already is. So it is `kind = 'rom'` on `media_item`, in a `retro`
  library, with a nullable `platform` column, and ADR 0002's claim holds again.
  Identity comes from the **file's hash** checked against libretro-database's
  DAT files, which works offline, and the N64 byte-order normalisation is a
  pure function. The player is a **libretro host inside the desktop client**,
  loaded through `syscall` the way libmpv is. **Cores are fetched, never
  bundled**, because the N64 cores are GPL and several popular others are
  non-commercial, and neither fits a commercial build. Saves and save states
  live **on the server, per user**, so a game continues on another PC. Three
  stages: **(1)** the ROM library, **(2)** the player on framebuffer systems
  (GBA first, then NES, SNES and Genesis), **(3)** N64. N64 has a stage of its
  own because its cores need an **OpenGL context owned by the host**, which is
  most of the risk and can only be proven on real hardware. Rough size: two to
  three months on a traditional estimate. ROM libraries are **not shared with
  paired servers** by default. Five questions were open (where people play,
  which systems, which controller, whether saves sync across machines, and
  whether a RetroArch launcher is wanted as a stop-gap); all five were answered
  in the ADR before stage 1.

- ~~**Lyrics**~~ — **built**, and so is the rest of music below: gapless in
  v0.9.73 and crossfade in v0.9.74.
  [internal/lyrics](../internal/lyrics/lyrics.go) parses LRC,
  `GET /api/items/{id}/lyrics` resolves **sidecar first then the embedded tag**,
  and the audio player grows a panel that follows the song. No provider and no
  network, exactly as the entry below asked.

  Three things the build settled that the entry did not:

  **The album is the case the discovery rule exists for.** Subtitle discovery
  can use a file named only for its language, because it first works out
  whether a video is the only one in its folder. Twelve tracks share a folder
  and every one is a different song, so a lyric file that does not name its
  track belongs to nothing that can be worked out — the stem must match, and
  that is the whole rule. `lyrics.lrc` sitting in an album folder is matched to
  nothing rather than to everything.

  **`synced` is reported rather than inferred.** "Every line at zero" is also
  what a one-line synced file looks like, and a player deciding which to draw
  must not have to guess. An unsynced file is still shown, with the panel saying
  the words are not timed — plenty of `.lrc` files are somebody's
  copy-and-paste, and words that do not scroll beat no words.

  **The embedded tag cost a new probe call, deliberately.** `probe` discards
  `LYRICS` on purpose and that stays right; `ReadRawTags` is the other half of
  that decision rather than a reversal — a caller that wants exactly the blob
  asks for it, once, when somebody has opened the panel, and nothing is stored.

  Gapless and crossfade, which the entry below also asked for, shipped in
  v0.9.73 ([plan](gapless-plan.md)) and v0.9.74.

  Original entry: [probe](../internal/probe/probe.go) throws
  the `LYRICS` tag away on purpose — a multi-kilobyte blob has no business in a
  struct that answers "can this client play this file", and that stays true. So
  lyrics are a **separate read**, not a probe field: embedded `USLT`/`LYRICS`
  from the tags, and `.lrc` sidecars beside the track. **Sidecar first**, exactly
  as subtitles resolve, and for the same reason — the sidecar is the one a person
  can fix. It is also the only one of the two that carries **timestamps**, which
  is the whole feature: unsynced lyrics are a text file, synced lyrics follow the
  song. No provider and no network for a first version; everything needed is
  already on disk. Alongside it, the music player still owes **gapless and
  crossfade** — a live album with a gap between tracks is a broken record, and
  that is a decoder-scheduling problem in the client rather than anything the
  server can fix.
- **Photos need more than a grid.** **Built so far:** people (face grouping and
  naming, in a native sidecar, [ADR 0052](adr/0052-face-grouping-runs-in-a-native-sidecar.md)),
  semantic search ([ADR 0060](adr/0060-semantic-photo-search-is-a-second-model-in-the-sidecar.md)),
  the Timeline, exact duplicates and near copies (v0.9.63), and **places**
  (v0.9.74, [ADR 0078](adr/0078-a-place-is-the-town-a-photo-was-taken-in.md)).
  **RAW is built** (2026-10-28,
  [ADR 0107](adr/0107-a-raw-photo-shows-the-cameras-own-jpeg.md)): a RAW shows
  the full-size JPEG the camera stored inside it, never developed, and a RAW+JPEG
  pair shows once. **Still open:** bursts, described below. The original entry
  follows, starting with people. Face grouping is the
  feature Google Photos used to justify reading every family album ever uploaded,
  which makes doing it **entirely on the box** the sharpest available statement
  of the difference. The model and the worker are the large part; the **naming
  UI is the actual product**, because a cluster of unnamed faces is a curiosity
  and a named one is how you find a photo. It follows the existing worker shape
  (scan → enrich → probe) and must be re-runnable, with the same rule the rest of
  the project holds: **a name a person typed is an edit and is locked**, and a
  re-cluster may not overwrite it.

  The smaller photo gaps are worth naming while we are here.
  [exif.go](../internal/photo/exif.go) reads **two tags** — orientation and
  capture time — by deliberate refusal of a general EXIF dependency. That refusal
  was about scope rather than privacy, so it is revisitable, but it should be
  revisited *consciously*: a places/map view needs GPS parsing, and GPS is the
  one EXIF field that is a privacy decision rather than a parsing chore.
  **Decided 2026-10-04: GPS may be read.** A map still needs map imagery, and
  fetching it from a public map service would break the no-phone-home
  principle. So the places view is either a list grouped by place with no map,
  or a map drawn from imagery the owner hosts. That question is open.

  **Exact duplicate detection is built** (v0.9.53,
  [ADR 0075](adr/0075-a-duplicate-is-the-same-bytes.md)): the same bytes, each
  copy shown with its album, and nothing picks which copy to keep. **Near copies
  are built too** (2026-10-06): the same picture resized or re-saved, measured
  first. Neither the search embedding nor a perceptual hash works alone, because
  bursts sit as close as re-saves. Together with the capture time they selected
  31 extra copies on a real library, none of them a different picture. They are
  shown apart from exact duplicates, with the largest version marked. **Bursts**
  (picking the best of a run) are unbuilt. **RAW is built** (see above).

  **Date-grouped browsing is built** — a Timeline view on a picture library,
  grouped by EXIF capture time rather than by folder, with each month fetched
  only when it is opened. Verified against the reporting library before it was
  designed rather than after: 3,469 of 3,676 photographs carry a capture time
  (94.4%), spanning 2006 to 2026, so the column is real rather than inert — the
  check that `media_item.edition` taught this project to run first. The other
  207 are their own bucket. Marked folders are excluded from it entirely
  (ADR 0051, amended). Face grouping, the large piece when this was written,
  has since been built (ADR 0052).
- **Audiobooks** — steps 1 and 2 shipped in v0.10.4 (books from folders, a grid, and playing);
  step 3 (sleep timer, narrators, filters) shipped in v0.10.7; **accepted** as
  [ADR 0116](adr/0116-an-audiobook-is-its-own-library.md) (2026-11-03), its own
  library kind, with the owner's nine answers recorded in it. The test library
  is 12 Star Trek books, read before step 1 was built. Checking
  the code for it corrected this entry: resume by position already works for
  any audio, chapters are already read from a file on demand (#949), and both
  players already carry a playback rate. What is missing is a place for books,
  a model that is not artist → album → track, and a player with chapters, speed
  per book, 15/30 skips and a sleep timer. An `.m4b` is **not indexed at all**
  today: the scanner's audio list lacks it, so a book dropped into a music
  library does not appear, rather than appearing as one long track.
- **Ebooks** — not built. A library kind for EPUB and a reader in the client.
  Three things carry the weight. The **renderer**: a reflowable EPUB reader
  would be the largest third-party library the client carries, so it gets the
  hls.js treatment (vendored, pinned, its own chunk) and its own ADR. **Reading
  position**: a location in reflowable text is not a millisecond, so it does not
  fit `playback_state` as it stands. **Metadata**: the OPF inside the file names
  title, author and series, and should be read before any provider is asked.
  ~~Comics (CBZ/CBR) are the cheap first step~~ — **comics are built** (#953,
  [ADR 0101](adr/0101-a-comic-is-read-out-of-its-archive.md)): a `comics`
  library of `.cbz` files, pages read out of the archive, the first page as the
  cover, and a reader that keeps your place in its own table — the "a page is
  not a millisecond" question this entry raised, answered for comics. **PDF**
  comics followed ([ADR 0102](adr/0102-a-pdf-comic-is-its-pictures.md)): each page
  is served as the one picture it shows, with no renderer. **`.cb7`** followed
  (2026-10-28, ADR 0101 amended), read with the 7-Zip reader already in the
  build and kept open between pages, because a solid archive's last page
  otherwise costs every page before it. `.cbr` (RAR) is not read: no reader
  in the build; a scan counts them.
- ~~**Extras on the detail page**~~ — **built** (#950,
  [ADR 0098](adr/0098-extras-are-found-not-imported.md)): a row on a film's or a
  show's page, found on demand from the files ADR 0038 already recognised and
  never imported, so no extra can reach a grid or a count. A local trailer
  replaces the online Trailer button. Original entry: trailers, featurettes,
  behind-the-scenes and
  deleted scenes, played rather than skipped. The files are already recognised:
  `media.IsExtra` finds them by the folder and suffix conventions Plex and Kodi
  document, the scan leaves them out, and library settings reports how many
  ([ADR 0038](adr/0038-extras-are-not-works.md)). That ADR calls exclusion the
  honest first step, says it is reversible, and names what playing them needs —
  a kind, a place on the detail page, and a decision about whether a trailer
  plays through the same routes. This entry is that follow-up. It must not undo
  the reason 0038 exists: an extra never appears as a film, in the grid or in a
  count. The online Trailer button stays for films with no local trailer.
- ~~**Pre-roll (custom cinema intros)**~~ — **built** (#951,
  [ADR 0099](adr/0099-a-preroll-plays-before-a-film-from-the-start.md)): a folder
  set in Settings, one clip at random, before a film started from the beginning
  only, never recorded, skippable, and each account can turn it off for itself.
  It plays at the player's entrance, so neither engine changed. Original entry:
  a clip the owner chooses,
  played before a film; Plex's version is a list of paths with one picked at
  random. Four questions decide it: server-wide or per library; **never on a
  resume**, since a pre-roll before minute 47 is only an obstacle; it must not
  touch playback state or count as watching anything; and it plays in the
  player, so both the browser and the desktop's mpv player need it (ADR 0067).

### Metadata, ratings and discovery

- ~~**Cast search, beyond the crash**~~ — **built** in v0.8.18
  ([items.go](../internal/api/items.go),
  [browsefilter.go](../internal/store/browsefilter.go)). Matching is a
  substring rather than prefix-or-word, LIKE's own wildcards are escaped, and
  library 0 searches every library. The description below is what was wrong.
  Searching people by name *and* role
  answered `datatype mismatch` and an empty list for every query, because
  `SearchCast` assigned its where clause instead of appending and dropped the
  role condition while keeping its argument. The picker always scopes to a
  role, so that was every keystroke anyone typed into it — which is why it read
  as a thin feature rather than a broken one, and why both existing tests
  passed: one searched with no role, the other filtered with no query. Fixed,
  with a test for the combination. **What remains is the actual feature.**
  Matching is `name LIKE 'q%' OR name LIKE '% q%'`, so it finds a first name or
  a surname and nothing else — no substring, no initials, no accent folding,
  and "de niro" does not find "Robert De Niro" typed as "deniro". It is also
  scoped to **one library**, so there is no way to ask "everything Harrison Ford
  is in" across films and shows at once, which is the question people actually
  have. Both are real work rather than a one-character fix, and neither should
  reuse a second normalizer — `clean` and `SortTitle` in `internal/media` are
  the ones that exist.

- ~~**Actor images beside the names**~~ — **built** in v0.8.18. `thumb_hash` is
  written by [enrich.go](../internal/enrich/enrich.go) from TMDB's
  `profile_path`, fetched once per person and enforced by the database rather
  than the caller, and drawn by [CastRow.tsx](../web/src/screens/CastRow.tsx)
  with initials as the designed fallback. The description below is the gap it
  closed. A detail page listed its cast as text, and
  the storage for the pictures is already there and empty: `person.thumb_hash`
  has been in the schema since the credits work and **nothing in the codebase
  writes it or reads it**. TMDB returns `profile_path` on the same credits
  request the names already come from, so the fetch is not a new provider call
  — it is the artwork pipeline pointed at a table it does not yet cover.
  [ADR 0025](adr/0025-artist-images.md) is the precedent and the warning: it
  exists because artist images needed their own decisions about *which* image
  is canonical and what happens when there is none. A face has the same
  problem and one more, because a missing headshot is far more common than a
  missing album cover — so the empty state is the design, not an afterthought.


- ~~**Ratings system**~~ — **done**: TMDB rating display + rating sort (Phase 3),
  and the **Metacritic / Rotten Tomatoes / IMDb** tie-in via OMDb
  ([ADR 0019](adr/0019-external-ratings.md)).
- ~~**Plex-style filter settings**, with a **total movie/show count per library
  page**~~ — **done** (Phase 2): multi-select genre/decade/content-rating
  filters, unwatched toggle, per-library counts in the nav.

### Social and profiles

- ~~**Resetting watched and listened history**~~ — **built** in v0.8.18, and
  given a user interface in v0.8.19, which is when it became reachable
  ([history.go](../internal/api/history.go),
  [historyreset.go](../internal/store/historyreset.go)). Three scopes, audited
  like removing a library, and priced before it is performed so the
  confirmation names a number. The description below is why it needed three.
  There was no way to undo a viewing
  record beyond one title at a time, and the request is usually one of three
  quite different things: *forget this show* (a shared account watched
  something for somebody else), *forget everything* (handing the server on, or
  starting a rewatch), or *forget the last hour* (the film that autoplayed while
  nobody was in the room — see "Are you still watching" under Input and
  control). Those want different controls and only the middle one is a single
  button. Two constraints are already decided and must hold. Playback state is
  keyed per user ([ADR 0006](adr/0006-playback-state-keyed-by-user.md)), so a
  reset is per account and never global, and an administrator clearing their own
  history must not touch anybody else's. And it is **destructive and
  irreversible**, which puts it in the same class as removing a library: it
  belongs in the audit log ([ADR 0026](adr/0026-audit-log.md)), and it needs the
  inline confirmation the re-probe row uses rather than a modal, since a modal
  overstates a thing people will do deliberately. Worth deciding at the same
  time whether "watched" and "in progress" clear together — they are one table
  and two meanings, and a person asking to forget a finished show rarely means
  "and lose my place in the one I am half way through".


- ~~**Profile page**~~ — **built.** The last part landed after v0.9.42: the
  profile now shows **your favourites** (a shelf, then a full list at
  `/profile/favourites`), **your ratings** with score and review (the first use
  of `GET /api/profile/ratings`, which had been specified and never called), and
  **your tags**, each opening everything that carries it across libraries at
  `/profile/tags/{id}`. Favourites and tags reach any level when asked without
  `library_id`, because a song or an episode is favourited as often as an artist
  or a show. All three are private to the account (ADR 0062). Public reviews stay
  unbuilt, which is a product decision and not a gap. What follows is the entry
  as it stood when **the history half was built** and the rest deliberately
  was not. `GET /api/profile` answers identity, history and totals in one request,
  derived from `playback_state`, which has held those answers since v0.4 and had
  never been asked. The stated cost of no new table: one row per item per user
  means the *last* time each thing was played, not every time — a history, not a
  log of sittings, and the page says so. `watched_ms` counts time **spent**, not
  runtime owned, because summing the duration of everything opened reports
  eleven hours for eleven films abandoned in their first minute. Missing items
  stay listed: "what happened to the film I watched last week" is a question
  about history, and a lost drive should not lose the answer. **Find Friends,
  Trending, ratings/reviews and viewer stats are not stubbed** — two of them need
  a decision about who may see whose viewing that nobody has made, and a page of
  scaffolding promising four features is worse than three true numbers, because
  the scaffolding is what people plan around. **Find Friends and viewer stats
  are now built too, because the decision they waited on has been made:**
  [ADR 0035](adr/0035-who-may-see-whose-viewing.md) settles that viewing is
  private by default and shared only by an explicit per-account opt-in. Four
  deferrals across three passes was enough — each feature built around the
  question had encoded an assumption about the answer, and the assumptions were
  not obviously compatible. "Find Friends" became a **People** page, which is
  the honest name on a household server: there is no directory to search and no
  second server to federate with, so it lists the accounts already here. It says
  who has chosen *not* to share rather than showing an empty list, because "has
  not shared" and "watches nothing" are different statements. The switch lives
  in the account's own settings and there is **no administrator route that could
  set it** — a switch somebody else can flip is not consent. **Ratings and
  reviews are also built, and only the private half**: your rating is yours, stored per user,
  shown to you, and aggregated for nobody. The routes carry no user id at all,
  so a leak cannot be introduced by forgetting a filter. Turning private
  verdicts into visible ones changes what people are willing to write, which
  makes it a decision about the product rather than a flag — and it stays
  unmade. Scores are out of ten so a half-star interface needs no migration and
  so the scale matches the provider ratings it sits beside; withdrawing a rating
  is deliberately distinct from scoring something 1.
- ~~**Trending (trends computed per library)**~~ — **built**, from
  `playback_state` and no new table. It counts **accounts, not plays**, because
  that table holds one row per item per user — so the API also reports how many
  accounts contributed, and the client names the shelf from that: "Trending in
  Films" with several, "Recently Played in Films" with one. Calling one
  person's history a trend would be a small lie told on the home page every
  day, which is the kind that survives longest because nobody bothers to argue
  with it. `finishers` is reported beside `viewers` because a title many people
  start and nobody finishes is a different fact from one everybody finished.
  Not admin-gated and it names no accounts: which titles are popular is a fact
  about a shared library, who watched them is a fact about a person.
- ~~**Watch Together** — synchronised playback across viewers.~~ — **built.**
  The design question is where the truth lives, and principle one answers it:
  the server owns it. A room holds what is playing, the position, whether it is
  paused and who is in it; clients converge on that. The alternative — every
  client broadcasting its own position — makes the last writer win, and on a
  lossy connection that is whoever lagged worst.

  **In memory, no schema.** A room means nothing after a restart; persisting one
  would resurrect a film nobody is watching. **Polling, not sockets**: nothing
  else in this stack streams, and a socket layer for one feature is the
  dependency argument [ADR 0013](adr/0013-transcode-pipeline.md) settled. A
  second of drift is fine for "we are watching this together"; frame accuracy
  was never the goal.

  **One host drives**, and the host leaving *ends* the room rather than promoting
  somebody — promotion sounds generous and is worse, because the film keeps
  playing in three houses under a driver nobody chose. Rooms drop members who
  stop polling, because **nobody presses leave, they close the laptop**.

  Two things the build taught. The sweep that drops absent members has to record
  the caller *before* it runs, or a host polling exactly on the interval is
  judged absent and takes down their own room for being on time. And a
  follower's correction has to allow for the time since the host reported —
  otherwise every seek lands one poll-interval behind and never catches up,
  including the case where two machines' clocks disagree and the naive sum seeks
  backwards forever.
- ~~**Better profile manager.**~~ — **built**, as two surfaces separated by
  authority: your own display name lives in Account, while renaming anyone else
  and changing roles lives in Users. A rename **keeps the account id**, so watch
  history, ratings and playlist membership follow silently — that is what makes
  it a rename rather than a replacement. Promotion and demotion are one button
  because they are one decision with two directions, offered for yourself as
  well: the rule that protects the install is "not the last admin", not "not
  you". That refusal lives in the store, inside a transaction with the count,
  because two admins demoting each other at the same moment is a race a
  handler check loses — and the prize for losing it is a server nobody can
  administer without `reset-auth` on the machine itself.

- ~~**Managed profiles with a content-rating ceiling**~~ — **built**, schema 47.
  A ceiling on the account row, an administrator-only control in Settings →
  Users, and enforcement in the listing **and in playback authorisation**. Four
  notes on what the build decided, since the entry below records only what was
  approved.

  **There was no ordering to enforce, so one had to be built.**
  `content_rating` is whatever string a provider or an NFO wrote, drawn from a
  dozen national systems that do not sort next to each other.
  [internal/rating](../internal/rating/rating.go) maps the labels libraries
  actually carry onto a ladder of **ages**, because that is the one thing every
  system is really saying — BBFC 15 and TV-14 land a year apart, where pairing
  the lists by index would put R beside 15 counting one way and 18 counting the
  other. It is deliberately coarse and is not a classification authority.

  **An episode is judged by its show.** Episodes almost never carry a
  certificate; their shows do. Without inheritance a ceiling hides every episode
  in the library and leaves the films, which is not a limit anybody asked for —
  it is how a feature gets switched off and called broken. Own rating, then
  parent, then grandparent, which is the same shape as the resolved `sensitive`
  flag ([ADR 0051](adr/0051-sensitive-content.md)).

  **The column it stands on is empty, and nobody checked.** Measured on the
  live library the day it shipped: **`content_rating` is NULL on all 19,460
  items** — 0 of 1,208 films and 0 of 13 shows carry one. So setting any ceiling
  on an account today hides that account's **entire video library**, because
  what is unrated is blocked. The feature is not merely unverified on real data;
  it is **unusable until something populates the column**.

  This is `media_item.edition` again — inert for the same reason, recorded in
  this document, and the exact check the Timeline entry above says this project
  learned to run *first*: that one verified 94.4% of photographs carried a
  capture time **before** the view was designed. Nobody ran it here. Enrichment
  has a `ContentRating` field and a writer for it, so the gap is a provider
  pass that never asks TMDB for a certification, not a schema problem.

  **The provider half is now built.** TMDB carries certifications and was never
  asked for them: `append_to_response` already fetched credits and keywords on
  the same request, so `release_dates` (films) and `content_ratings`
  (television) ride along at **no extra request**. Measured before the rule was
  chosen — of thirty films sampled at random from the live library, **thirty
  carried a US certificate and thirty a GB one**, so the country order decides
  the label rather than whether there is one. US leads, GB is the fallback, and
  a country nobody in the house reads is deliberately **not** borrowed: an FSK
  on a detail page is a surprise, and the ladder places every system anyway. A
  country setting is the obvious next step and is not that change.

  Verified end to end through the shipping client against the live API rather
  than against a fixture: The Fifth Element PG-13, Fight Club R, Toy Story G,
  South Park TV-MA, Friends TV-14 — every one of them placeable on the ladder.

  **The backfill is a deliberate manual step.** Existing rows keep their empty
  column until something re-enriches them, and *Refresh metadata* on a library
  already does exactly that with its cost shown first. A migration that
  re-queued 1,221 titles on everybody's next start would spend somebody else's
  API budget to fix this library.

  **Music and photographs are exempt**, which is the correction the build
  needed and did not get from reading the rule. A track and a photograph carry
  no certificate and never will, so "unrated, therefore blocked" meant a child
  account could not see a single song or a single photograph — a lockout rather
  than a limit. Found by asking what the rule does to a music library; every
  test written for the rule passed the whole time it was doing this.

  **What is still unrated is blocked** where a certificate was possible, and
  that is the uncomfortable half.
  Letting it through puts the hole exactly where the unlabelled sits — home
  video, anything a provider never matched, most of what somebody added by hand.
  A limit that stops at the catalogued and waves the rest past is a filter that
  looks like a limit. The cost is that a restricted account sees less than the
  household expects, which is visible, complainable-about, and fixed by rating
  the item; the other failure is invisible.

  **`GetItem` is the chokepoint**, which is what makes this hold rather than
  being remembered in a dozen handlers: stream, transcode, download and the
  detail page all turn an id into an item through it, so a blocked item answers
  **404 — not 403** — everywhere. The two must be indistinguishable, or walking
  the ids becomes a way to enumerate what a household is keeping from somebody.

  One error the store's own tests caught, worth recording because the reasoning
  sounded right: an id naming no account was refused as a stale or forged
  session. An unsecured loopback server has no accounts and reads every request
  as `LocalUserID`, so that emptied the whole library for the configuration
  meant to work out of the box. Deciding whether a session is real is the
  session layer's job; this one answers what a ceiling permits.

  **Three shapes, not two.** `ListItems` carries the predicate, `GetItem` is
  the chokepoint for anything that becomes bytes, and the third is a listing
  that builds its own SQL and hands back rows — a shelf, a collection's
  members, a container's children. `PermittedItems` covers those in one query
  with the *same* predicate, applied where every list becomes a response, so it
  is idempotent on the page `ListItems` already filtered. A restricted account
  shown a title it cannot open is worse off than one not shown it: the tile
  names precisely what the household is keeping from them. Original entry:

  approved, and mostly
  assembly: [ADR 0015](adr/0015-multi-user-accounts.md) already gives accounts
  and roles, and `content_rating` already flows through
  [items.go](../internal/api/items.go). What matters is *where it is enforced*.
  **Server-side, in the queries and in playback authorisation** — a client-side
  hide is a suggestion, and this API serves files; an account that can be talked
  into a stream by a hand-written request has no ceiling at all. Library
  visibility belongs in the same place.

  It also needs one thing said out loud so it does not read as a contradiction of
  [ADR 0035](adr/0035-who-may-see-whose-viewing.md), which holds that a switch
  somebody else can flip is not consent. **This one is deliberately the
  opposite**: a rating ceiling is set *for* an account by an administrator and
  cannot be cleared by the account itself, because a limit you can lift is not a
  limit. The distinction is that ADR 0035 governs what others learn about you and
  this governs what a household allows — and the two rules must not be
  generalised into each other.
- ~~**A local Wrapped**~~ — **built**, as *Your year* on the profile page.
  `GET /api/profile/year` and a section beneath the lifetime totals, computed
  from `playback_state` with no new table and no request leaving the machine.

  **The constraint below decided the whole design, and it is on screen rather
  than in a comment.** One row per item per user means a year is *the titles
  whose last play fell in it* — a film watched in January and again in December
  belongs to December, and the January sitting cannot be recovered. So
  `watched_ms` counts **one viewing of each title**, not `watch_count`
  viewings: the tally is real and the dates of those viewings are not, and
  multiplying would attribute every rewatch to the year of the most recent one.
  The figure is deliberately low. A number that is missing is easier to
  disbelieve than one that grew on its own, and on a page that is believed by
  default that asymmetry is the design.

  What it says instead of a top list: titles, finished against put down, time
  spent, a bar for each of the twelve months — **including the empty ones**, or
  the chart lies about the shape of the year — a breakdown by kind, how many
  libraries were touched, and the first and last things played. `partial` marks
  a year still running, because "your 2025" and "your 2025 so far" are a summary
  and a claim.

  Year boundaries are **local**, in SQLite, the same way the photo timeline
  buckets: eight in the evening on New Year's Eve belongs to the year they were
  in when they watched it, and in any US timezone the UTC date is already
  January. Your own year and nobody else's — there is deliberately no route
  taking another account's id (ADR 0035). Original entry:

  Everything Spotify computes about you in a datacentre,
  computed on your own machine, from data that has never left it — and the
  novelty is precisely that it is *not* a marketing artefact, so nothing about it
  gets uploaded or shared unless ADR 0035's opt-in says so. The material is
  `playback_state`, already there since v0.4.

  One constraint decides whether this is charming or a lie, and it is the same
  one the Profile page already documents: **`playback_state` holds one row per
  item per user — the *last* time each thing was played.** So "your top film,
  seven times" is not answerable and must not be implied. What *is* honest is
  time spent, breadth, what you finished versus abandoned, and the shape of a
  year. This is also the exact constraint the history question below turns on.
- ~~**Open question — a real history, Trakt- or Sonarr-shaped.**~~ — **decided
  and built** ([ADR 0074](adr/0074-a-viewing-is-a-row.md), schema revision 53):
  both a log and an export. A `viewing` row is written each time a film or an
  episode is *finished*, on the same edge that moves `watch_count`. Music is not
  logged, and rows are kept until the account's history reset clears them. What
  was watched is copied onto the row so it survives the item being deleted.
  Existing finished titles were seeded with one row each, marked `estimated`,
  and earlier rewatches were not invented. The profile links to a Watch history
  page with CSV and Trakt-shaped downloads, which LANcast sends nowhere. The
  question as it was first captured follows. It was a
  *question*, not a feature, because the interesting part is a cost decision
  nobody has made. Everything above reads `playback_state`, which is a **state**
  table: one row per item per user, overwritten on every play. It can answer
  "where was I" perfectly and can never answer "how many times" or "what did I
  watch on a Tuesday in March", because the previous answer is gone the moment
  you press play again. A genuine history is therefore **an append-only event
  log, and a second source of truth** — a row per sitting, growing without
  bound, which is a different kind of object from anything in this schema and
  brings retention, size and migration questions with it. That is the weight.
  Nothing should be built until the scope is settled, and the first thing to
  settle is whether the ask is a *log* (every sitting, for its own sake) or an
  *exporter* (get my data out, in a format something else reads). Those are very
  different jobs and only one of them is heavy.
- **Last.fm and Trakt scrobbling** — not built. The Watch history page already
  exports a Trakt-shaped file (ADR 0074) and sends it nowhere. Scrobbling is the
  live version: each finished film or episode to Trakt, each track to Last.fm.
  Outbound to a service the person signs in to, per account, off by default —
  the same no-phone-home test as webhooks. Music would need a listening log,
  since ADR 0074 deliberately does not record tracks.

### System, operations and diagnostics

- ~~**Containment that resolves symlinks**~~ — **built**
  ([ADR 0094](adr/0094-containment-follows-links.md)). The check every
  row-to-path handler makes was lexical, and the scanner imported a symlinked
  file like any other, so `film.mkv` linking to any file on the machine was
  served by a service running as LocalSystem (outside review, confirmed
  2026-10-25). `internal/contain` now resolves root and target before
  comparing, and walks the path itself because `EvalSymlinks` fails beneath a
  Windows junction. The scanner skips links that leave their location and says
  so in the scan's issues. The survey found two handlers with worse gaps —
  external subtitles had no check at all, lyrics sidecars a lexical one — and
  both are held to it now.

- ~~**Developer tools in the desktop window**~~ — **built** in v0.8.18, and
  **it did not work until v0.9.6.** The switch is on Settings, off by default,
  honest that it takes effect at the next launch — the browser arguments are
  read when the web view environment is created and there is no supported way to
  add one to a running environment. What shipped was half of it. Opening the
  inspector needs `--auto-open-devtools-for-tabs` *and*
  `AreDevToolsEnabled`, and the vendored web view was told `false` for the
  second one unconditionally: the pane opened, and a moment later the API
  switched devtools off underneath it. That is a window with nothing in it, and
  it was written down in v0.8.49's notes as "a devtools pane that renders blank
  in this WebView2 build" — a defect attributed to the runtime by a window that
  was disabling the thing it had just asked to open. Nothing failed and nothing
  logged, because both halves did exactly what they were told. The default
  context menu now travels with the switch, since right-click → Inspect is the
  only way back once the pane is closed: the UI owns the keyboard model
  ([ADR 0004](adr/0004-keyboard-focus-model.md)) so F12 never arrives. The wiring has
  tests now, which is the half that had none — the same shape as the close
  handler this package installed one line too late and never called.


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
- ~~**A wrong library kind is only half-visible**~~ — **built** in v0.6.27
  ([libraryshape.go](../internal/store/libraryshape.go)). The description below
  is the half that was missing. Kind is chosen once and is
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

  **Built**, as both of those candidates plus a third. It is a *verdict* rather
  than a count, because "1 movie, 3 parts, 0 shows" is not something a person
  should have to interpret at the end of a scan; the sentence and its remedy are
  written server-side and rendered as given, which also removed a sentence the
  client used to assemble for the one case it could see. Thresholds are
  deliberately forgiving — one show is enough to be a shows library, three
  episode-shaped names in a film library is a box set, and a library under five
  items is not judged — because a check that cries wolf is a check that gets
  ignored, which is worse than no check. It runs only on a **successful** scan:
  reporting "your TV library has no shows in it" because a drive vanished
  halfway through would be a false alarm about a permanent mistake.

  Two things the build taught, both found by running it rather than by
  reasoning. The verdict has to be **stored** (schema 20): it began on live scan
  progress, which dies with the process, so a library scanned on Tuesday looked
  fine on Wednesday — the wrong lifetime for a warning about a property that
  cannot be changed. And **withdrawing** it is the subtle half: the episode
  count is gathered during the walk, so a rescan that changes nothing produces
  no evidence and no verdict, which is not the same as a clean bill of health.
  As first written, any rescan silently erased a standing warning.
- ~~**Library editing, deferred to the settings redesign.**~~ — **built** in
  v0.6.16, and it governs more than the name: the *path* is editable too, which
  is the drive-letter case. Repointing rewrites every item path under the old
  root in one transaction, so a moved library keeps its matches, artwork, watch
  positions and playlist membership rather than being deleted and re-added.
  Kind remains immutable, for the reasons above.
- ~~**Crash reporting.**~~ — **built**, local only. A panic used to unwind
  through `net/http`, which closes the connection without a response: the client
  saw a network error and the operator saw nothing unless they happened to be
  reading the log at that moment — the recurring failure shape in this project,
  a fault with nowhere to appear. Now the request answers `500` with the
  ordinary error envelope and the panic becomes a numbered report, listed in
  Settings → Logs. Reports record the **route pattern** rather than the URL,
  because `GET /api/items/{id}` is what somebody fixes. They are JSON files in
  the data directory rather than database rows, since the crash most worth
  having is the one where the database was the thing going wrong; the newest 50
  are kept, because a crash loop writes the same stack a thousand times and the
  first one is the informative one. `http.ErrAbortHandler` is excluded — a
  deliberate connection drop is not a crash. **Nothing is sent anywhere**, and
  that is the point: "we do not phone home, except for crash reports" is how
  every product that phones home began.
- ~~**Internal log viewer**~~ — **built.** `GET /api/logs` returns the tail of
  `lancastd.log` and Settings shows it, collapsed by default and never polled.
  The log had been written beside the database since v0.4.2 and could only be
  read by opening a file manager — the wrong ask for the case it serves, since
  it matters most when the server runs as a service and something is wrong. It
  says when the view is partial rather than letting a reader believe they have
  the whole file. ~~**Debug logging** — raising the level from the UI~~ —
  **built**: a toggle on the Logs pane that takes effect on the next line
  logged, no restart, and survives one, because the faults worth turning it on
  for are the intermittent ones.
- ~~**Clear cache and data** and **reset settings** actions.~~ — **built**,
  with the boundary stated rather than assumed: every action must be
  recoverable *by the server itself*. Cached artwork re-downloads, transcode
  scratch is rebuilt on the next play, settings return to documented defaults —
  and the password hash, provider keys, certificate paths and ffmpeg location
  survive a reset, because a reset cannot restore them and losing the first
  would lock the operator out of their own server.
- ~~**Check for updates** with an **auto-update** toggle.~~ — **built,
  shipped in v0.6.2.** Signed releases, an update check on by default, and a
  download-verify-stage path that swaps the binary in on shutdown. What remains
  is the first release cut *with* the signing key in place, which is the only
  thing that can prove the published half end to end.
- ~~**Desktop lifecycle — "Open on Windows start" and "Close to tray"**~~ —
  **built** in v0.6.1 ([plan](desktop-lifecycle-plan.md)).

- ~~**Multiple displays — which screen the window opens on, and remembering it.**~~
  — **built** in v0.8.56 and made to actually work in v0.8.57, where it had
  shipped as dead code: the handler that records the position was installed
  *after* the webview had been constructed with the original callback, which
  takes it by value, so the restore half worked perfectly on nothing. The
  placement rules were pure and fifteen tests passed throughout, because the
  fault was in the wiring that reaches them.

  Of the three things below, the first and third are answered as written. **DPI
  is handled by its consequence rather than by reading a DPI**: a size that will
  not fit its new screen is shrunk, and an absurd remembered size is refused, so
  the 4K-to-1080p case lands somewhere sensible without the code knowing why. If
  that ever proves insufficient it will look like a window that is the wrong
  *size* rather than in the wrong *place*, which is the cheaper failure.

  The original entry, kept because it is what the build was measured against:

  [desktopprefs](../internal/desktopprefs/prefs.go) is already the right home and
  says why in its own doc comment: per user, per machine, a small JSON file the
  server never learns about. "Which of my three monitors" is that kind of fact
  exactly — it is about a desk, not an account, and putting it in `/api/settings`
  would move one household member's window onto another's screen. The Win32 half
  also already exists: `MonitorFromWindow`, `GetMonitorInfoW` and `SetWindowPos`
  are in [fullscreen_windows.go](../internal/clientwindow/fullscreen_windows.go),
  which is monitor-aware on purpose, so this is new preference plumbing rather
  than new platform work.

  Three things make it more than storing a rectangle:

  **A remembered monitor stops existing.** A laptop is undocked, and coordinates
  saved against a monitor that is gone restore the window off-screen — running,
  painting, focusable, and invisible. This is the whole reason to store a
  monitor *identity* (device name) alongside the rect and treat the rect as
  advisory: validate against the monitor set present at startup and fall back to
  primary. A window nobody can see is worse than a window in the wrong place.

  **DPI is not carried by the coordinates.** A size remembered on a 4K panel and
  restored onto 1080p is not the same window, and WebView2 scales inside it.

  **"Open on" and "fullscreen on" are one question, not two.** Fullscreen
  already targets the monitor the window is on, which is the correct behaviour
  and should stay — so this is one setting, and a second one would be a way for
  the two to disagree.
- ~~**Webhooks**~~ — **built** (#944, [ADR 0097](adr/0097-a-webhook-goes-where-the-owner-points-it.md)):
  Server › Webhooks, administrator-only and none by default. `playback.started`
  and `playback.stopped` (only for accounts that turned it on themselves, in
  Privacy, and carrying only the work's title), `scan.finished` and
  `backup.taken`, each POST signed with HMAC-SHA256, three tries, no redirects
  followed, the last outcome and a Test button on each. The live-updates stream
  was not used as the source after all: its events are contentless topic names
  made for a screen that refetches. Original entry: a URL the owner sets, which receives a POST when
  something happens: a film starts or stops, an item is added, a scan finishes.
  Plex's are what Home Assistant automations and Discord bots are built on. One
  thing makes this cheaper than it looks: the live-updates event stream
  ([ADR 0079](adr/0079-the-server-says-when-something-changed.md)) already names the server's events, so a
  webhook is another subscriber, not a second event system. Three things make it
  careful. **It is outbound traffic** under a no-phone-home principle, and it is
  consistent with that only because the owner types the destination and nothing
  is configured by default — the ADR should say so in those words.
  **Administrator-only**, because a URL the server fetches is a way to make the
  server reach into the LAN. **Playback events name an account and a title**,
  which ADR 0035 treats as that person's business, so whose viewing a webhook
  may report is a consent question before it is an HTTP one.
- **A server dashboard** (Plex Dash) — **history is built**
  ([ADR 0106](adr/0106-the-dashboard-remembers-two-days.md)): Settings ›
  Dashboard shows people watching, conversions, whole-machine CPU, memory,
  requests and server errors, a reading a minute for 48 hours, kept through
  restarts in a file rather than a table. **Media sent followed** (2026-10-28):
  bytes counted on the media routes only, never the event stream, drawn as
  Mbit/s and exported as `lancast_http_media_bytes_total`. Original entry: partly built. Settings → Activity
  already shows what the server is doing *now*: its sessions, idle-first and
  administrator-only (v0.9.22, `GET /api/transcodes`). What Plex Dash adds is
  **history**, and a view that works on a phone: streams and bandwidth over
  time, who watched what, server load. Who watched what already exists as the
  `viewing` log ([ADR 0074](adr/0074-a-viewing-is-a-row.md)). The new weight is
  sampling the server itself — bandwidth and CPU over time means a table of
  samples with retention and size limits. Any per-account view stays
  administrator-only, for the reason `/api/transcodes` is.
- **Bandwidth limits** — not built as a server setting. What exists is per
  device: the quality ceiling the client keeps in its own storage (v0.6.23),
  plus the session cap. Neither lets the owner say *remote streams may use at
  most N Mbps* or *the server may upload at most N in total*, which matters now
  that remote reach is real (the public name, federation). Plex offers a cap per
  remote stream, a total upload cap, and LAN addresses treated as unlimited. The
  per-stream cap belongs **in the play decision** as one more input, so it
  stays testable from fixtures like every other rule in `probe`. The total cap
  is the heavier half: it is admission control across sessions, which decides
  what happens to the stream that would cross the line.
- **Granular sharing restrictions** — **mostly built**; recorded here so it is
  not rebuilt from Plex's list. Every account can carry a rating ceiling
  (schema 47) and a narrowed set of libraries
  ([ADR 0084](adr/0084-a-library-can-be-kept-from-an-account.md)), both enforced
  in the store at the `GetItem` chokepoint. Two things Plex has beyond that.
  **Restricting by label** — allow or exclude titles carrying a tag. LANcast's
  tags are private to the person who wrote them
  ([ADR 0062](adr/0062-a-tag-is-a-note-to-yourself.md)), so they cannot serve;
  this would need a household label, which is a new concept. **Limits on a
  friend's server** rather than on a local account: a share is a whole-library
  grant ([ADR 0071](adr/0071-a-shared-library-is-a-standing-grant.md)).
  Confirm that somebody needs either before designing it.
- ~~**Docs that contradict the code**~~ — **done** 2026-10-26: README,
  `security.md` and `architecture.md` now describe what is built (status
  v0.9.94, 92 ADRs, `LANcast.exe reset-auth`, ACME, passkeys, the custom
  certificate, per-library visibility, session sign-out, crash reports stated
  as local). The recurring check is `internal/projectdocs/readme_test.go`: the status line and ADR count must match the repo. Found by an outside review (2026-10-25)
  that read the repository the way a stranger would, and was misled by it. The
  README's status line says v0.8.34 and "50 decision records" (there are 92);
  `security.md` gives the recovery command as `lancastd reset-auth`, which no
  longer exists as a binary since ADR 0080, says there is **no ACME** although
  ADR 0081 shipped it, and lists per-library visibility as the oldest open item
  although ADR 0084 built it; `security.md` and `architecture.md` both say the
  project "ships crash reporting", which reads as telemetry when `crashlog` is a
  local record. The cost is not tidiness: a reviewer filed three missing
  features that exist because the docs said so. Worth a recurring check, the way
  `apidoc_test.go` guards the API contract.
- ~~**Trusted proxies for the login throttle**~~ — **built** (#941,
  [ADR 0095](adr/0095-a-proxy-is-believed-by-name.md)): Server › Network ›
  Trusted proxies, empty by default; `X-Forwarded-For` read right to left from
  those addresses only, by rewriting `RemoteAddr` at the edge. It was worse than
  this entry said: behind a same-machine proxy the sign-in page's account
  picker was shown to the internet too. Original entry: `InsideNetwork`
  exempts loopback and private callers from the per-account backoff, and reads
  only the remote address, correctly refusing forwarded headers. The
  consequence, which `security.md` states, is that behind a reverse proxy on the
  same machine **every caller is inside** and the per-account wait never
  applies. The fix the review suggests is the standard one: an explicit list of
  proxy addresses whose `X-Forwarded-For` is honoured, empty by default, with
  `ClientKey` and `InsideNetwork` both reading through it. Now that ADR 0081
  makes the server publicly reachable without a proxy this matters less than it
  did, but the docs still recommend a proxy.
- **ffmpeg runs with the service's privileges** — not built. The service runs
  as LocalSystem, and ffmpeg and ffprobe parse whatever files are in a library,
  which is the largest attack surface LANcast has. ADR 0043 accepts the parser
  risk; it does not discuss the account. Options, cheapest first: launch the
  media tools with a restricted token, or run the whole service as a virtual
  account (`NT SERVICE\lancastd`). The second is larger than it sounds — several
  comments and the certificate and port-change code assume LocalSystem — and
  must be verified as the service, per CLAUDE.md.
- **An update path for the pinned ffmpeg** — needs checking before it needs
  building. A bump is a code change and ships with a release (ADR 0043), which
  is the right model for a CVE. What is not obvious from the code is whether an
  existing install takes the new build: the tools are located by whether the
  file exists, not by version. If a bump does not reach machines that already
  have the old one, a security fix to ffmpeg never arrives.
- ~~**A snapshot before every migration**~~ — **built** (#940): at startup,
  when a migration is pending on a database that existed, as
  `…-schema<N>-premigrate.db`, the newest three kept apart from the schedule's
  count. Original entry: Backups exist, by hand and
  on a schedule (`VACUUM INTO`, under a second on 100 MB), and migrations are
  forward-only by design — so the file that would undo a bad one is the one
  nobody takes automatically. Take one just before `migrate` changes the schema
  version, named so it is recognisable, and swept by the same retention.
- ~~**Restore from the interface**~~ — **built** (#942), as this entry
  guessed: Restore stages the backup and the server restarts itself, after
  backing up what it had; the next start applies it, once. Original entry:
  Backups can be listed, taken and
  downloaded in Settings, but restoring is the `restore` subcommand on the
  machine. Some of that is right — a restore replaces the database the running
  server holds open — so the real shape is probably "stage this backup and
  restart", which the server can do to itself.
- ~~**A metrics endpoint**~~ — **built** (#943,
  [ADR 0096](adr/0096-metrics-have-their-own-key.md)): `GET /api/metrics`,
  readable by an administrator or by a metrics token that opens that route and
  nothing else, so API keys stay non-admin. Original entry: Prometheus-format counters (streams,
  transcodes, scan durations, request errors) for people who already run
  Grafana. Administrator- or loopback-only, and the same data the server
  dashboard entry above would draw on.
- **Sign-in beyond passwords and passkeys** — **two-factor codes are built**
  (#952, [ADR 0100](adr/0100-a-code-after-the-password-for-whoever-wants-one.md)):
  each account opts in, an administrator can only remove it, ten recovery
  codes, and the secret sealed beside the database. OIDC is what is left. The
  review asked for 2FA or OIDC. Passkeys (ADR 0082) already give phishing-resistant sign-in, so
  TOTP is the smaller remaining gap; OIDC (Authelia, Authentik, Keycloak) is for
  households that already run an identity provider, and is outbound only to the
  one the owner names. Rate limits beyond login — per-key limits on the API —
  belong with this.

### Input and control

- ~~**"Are you still watching"**~~ — **built** in v0.8.18, made visible in
  v0.8.19 and made reachable in v0.8.25
  ([stillWatching.ts](../web/src/playback/stillWatching.ts)). It counts
  *automatic advances* rather than interactions — the obvious rule punishes
  the person watching a two-hour film properly — needs a count and a clock
  together, and asks *before* advancing rather than after. The description
  below is the case for it. Nothing stopped playback when nobody was there.
  Autoplay walks a season, each episode marks itself watched at the end, and a
  server left running overnight has both spent hours of transcode and rewritten
  the one piece of state people actually care about — where they had got to. The
  idle reaper is not this: it kills a session nobody is *reading*, and an
  unattended stream is being read perfectly well. Two things make it more than a
  convenience here. Progress is per user and load-bearing
  ([ADR 0006](adr/0006-playback-state-keyed-by-user.md)), so silently marking
  six episodes watched is data loss dressed as a feature, and it is the thing
  the history-reset item under Social and profiles would then be needed to
  undo. And every unattended stream holds one of a **default ceiling of three**
  transcode slots, so one forgotten television costs a third of the server's
  capacity to serve nobody. The rule wants deciding rather than copying: after N
  consecutive unattended items, or N hours, and *what counts as attention* —
  Plex uses the absence of any interaction, which punishes anyone watching a
  film without touching the remote. Whatever it is, it must stop **before**
  writing the next progress record, or it has already done the damage it exists
  to prevent.


- ~~**Keyboard-control shortcut map and customizer**~~ — **built** on the
  existing spatial focus model (ADR 0004). The map was already in one place so
  the overlay and the settings pane could not disagree; the rows became
  *bindings* rather than being replaced by them, so everything that rendered the
  old shape renders the new one. Rebinding is by **capture** — press the key you
  want — because a text field can be given a key that does not exist, and
  because capture is also how the map ends up correct for a remote, which emits
  whatever it emits. Three rules make it safe to hand somebody this control:
  only **overrides** are stored, so a binding added in a later version still
  arrives instead of being invisible to everyone who ever opened the pane;
  Escape and the arrows are **fixed**, since a map that can strand you on a page
  you cannot leave is a trap rather than a customizer; and a key already in use
  is refused **by name** rather than silently taken. The motivating case is
  ordinary: `[` and `]` for subtitle tracks are one key on a US layout and a dead
  key on several European ones, and a shortcut you cannot physically press is
  not a shortcut.
- ~~**Pop-out player** in our own window rather than the browser's~~ —
  **built** in v0.6.27 ([ADR 0029](adr/0029-picture-in-picture-is-our-window.md),
  [popout.ts](../web/src/playback/popout.ts)). The acceptance test the ADR
  asked for is [crossDocumentMove.test.tsx](../web/src/playback/crossDocumentMove.test.tsx),
  which holds both the safe shape and the shape that breaks. The description
  below is why the browser's own window was not enough. Picture-in-picture
  hands the element to Chrome, so the window
  arrives with Chrome's chrome: our subtitles keep rendering in the parent tab
  while the picture is in the corner, a Live Caption button offers guessed
  transcription in place of the real tracks, and speed, audio track and queue
  disappear. Document PiP renders our own player instead. The clock — the fault
  that started it — was fixed without the rework, by reporting the true timeline
  through MediaSession (`72619a6`, shipped in v0.6.8). First piece of work is the
  acceptance test the ADR asks for, before any feature code: proving the media
  element survives an imperative cross-document move under React re-render.

- ~~**Skip intro and skip credits**~~ — **both built**, and an **Up Next** card
  on top of them.

  - **Skip credits on films** shipped in v0.9.49 (#736): each black-run
    candidate is checked against the frames that follow it, and only text on
    black passes. Early markers fell from 14 of 80 to 1, checked by eye on a
    held-out sample.
  - **Skip credits on episodes** shipped in v0.9.50 (#737, #738). The film check
    was wrong for television, so an episode's credits come from the closing
    theme its season shares, checked against the black run: 0 early on 40
    held-out episodes, against 3.
  - **Intro fixes** shipped in v0.9.51 (#739): ten minutes fingerprinted rather
    than seven (47 Star Trek intros had been cut off mid-theme), and a studio
    ident at 0:00 is no longer an intro.
  - **Up Next** (#740): when an episode's credits begin, a card names what
    follows and counts down ten seconds, rolling on through the episode's own
    ending, so watched, auto play and the still-watching prompt all still apply.
  - **The credits marker is the watched threshold for episodes** (v0.9.56,
    #750, ADR 0054 decision 4). The gentle form, the earlier of the marker and
    90%, changed 40 of 1,260 markers, too few to matter. The strong form
    replaces 90%, and was taken only after late markers were measured (#749).
    Films keep 90%.
  - **Coverage, in progress.** A library audit (#767) listed the seasons with
    episodes lacking an intro. v0.9.65 (an ident that decided nothing) and
    v0.9.66 (a season with two openings) closed the first of them. The rest is
    being finished in its own session.

  The rest of this entry is the history that led there, kept for its reasoning.

  [ADR 0054](adr/0054-a-marker-says-where-the-film-stops.md) gated the button on
  a check nobody had done: *"the rule is consistent, not right. Nobody has yet
  watched a film and written down where its credits begin."* So it was done —
  forty films, one frame each thirty seconds past the detected marker, where
  credits should plainly be rolling. **About one in five was still in the film**,
  mid-scene, with ten minutes left to run. A button that drops somebody out of
  the third act one time in five is worse than no button, which is exactly what
  the ADR gated against. The number is the useful part: the rule now has
  something to be improved against rather than a shrug, and the ADR can move off
  *proposed* once it is.

  Intros come from the other detector — audio fingerprints compared across a
  season — and every one checked landed inside a title sequence. Its known
  weakness is the *end*, occasionally a few seconds early on a long title
  sequence, which is a benign failure where the credits one is not: you see the
  last bar of a theme rather than losing the third act. **The end is not padded**,
  because padding trades that for clipping the first line of the episode.

  The original entry follows. Its opening claim — that nothing in the codebase
  knows where an intro is — stopped being true when the fingerprint worker
  shipped, and the rest of its reasoning is what the build followed.

  Nothing in the codebase knows where an intro is: [probe](../internal/probe/)
  reads streams, not chapters, and
  the `chapter` hits elsewhere in the tree are the multi-part serial work of
  [ADR 0017](adr/0017-collections-and-multi-part-works.md), which is unrelated.
  Two sources, and they are not alternatives so much as a cheap one and a real
  one. **Container chapter markers** are free, exact and honest — and present
  only when whoever made the file bothered, which on a real library is a
  minority. **Audio fingerprint comparison across a season** is what actually
  works: the intro is the passage every episode has in common, which makes it
  findable without knowing anything about the show. It is also the expensive one,
  and it is the reason this needs a decision rather than a design.

  Three rules to hold whichever way that goes:

  **Skip ranges are derived data, not item fields.** They are computed and
  recomputable, so they belong in their own table that can be dropped and rebuilt
  — with one exception that follows from the rest of this project: if a person
  *corrects* a range, that is an edit, and **locked fields apply**. A re-detect
  must not re-litigate a human's correction, exactly as a rescan must not
  re-litigate identity or playlist membership.

  **Detection runs in its own worker**, like probing and for the same reason
  ([ADR 0012](adr/0012-probe-before-transcode.md)): keep process execution split
  from the analysis, so the ranges are testable against fixtures rather than
  requiring a season of real episodes and a working ffmpeg.

  **Never skip without a visible affordance.** A button that appears, not a jump
  that happens. An automatic skip that is a few seconds wrong is indistinguishable
  from a broken file, and the first thing it will eat is a cold open.

- ~~**The desktop client connects to a server on another machine**~~ — **built**
  in v0.9.29 ([ADR 0070](adr/0070-the-desktop-client-can-trust-a-server-it-did-not-install.md),
  accepted as amended; the pin is over the server's identity key, kept per
  server in `internal/knownserver`). What follows is the original capture. A second person in the household has an account on the server and
  no way to reach it from their own PC: their copy of the client opens their
  own empty server, and the shared library is simply not in the app.

  A browser reaches it today and that is not the answer. A browser cannot play
  through libmpv, so the server converts for that viewer — undoing exactly the
  work [ADR 0067](adr/0067-the-desktop-client-plays-through-libmpv.md) did, for
  the one person whose files are most likely to need it. The household's second
  viewer would cost more server CPU than its owner, permanently.

  **The blocker is trust, not a missing screen.** `serverCertPin()` reads the
  pin out of the *local* data directory, so there is nothing on this disk to
  pin for a server on another machine. The decision the ADR makes is
  trust-on-first-use with the fingerprint on screen, and a **changed pin
  refused** rather than clicked through — strict because the pin is over the
  public key, so neither certificate rotation nor a new network interface
  changes it, and a pin that has changed means the key really was replaced.

  Nothing on the server moves: the window sits on the server's own origin, so
  the cookie is first-party and the `Origin`/`Host` check matches. No endpoint,
  no API change.

- ~~**The audio pass — loudness leveling, a dialogue-clarity knob, and an
  equaliser**~~ — **built**, across music, shows and films. The most-felt gap on this page:
  whispered dialogue and deafening explosions are the standing complaint about
  watching anything at home, and **no commercial service offers the control at
  all**.

  **Phase 1 shipped in v0.9.45 (#720, #722):** night mode and dialogue
  boost for films and episodes in the desktop client. The design below did not
  survive contact with [ADR 0067](adr/0067-the-desktop-client-plays-through-libmpv.md):
  it was written for a Web Audio graph on the media element, and the desktop
  plays films through libmpv, which is not an element. So there are now two
  engines behind one preference ([plan](audio-pass-plan.md)). On the desktop,
  mpv's own `af` chain is built in Go from numbers, and surround stays surround
  by construction. Music and browser tabs keep the element, and the design below
  still applies to them as Phase 2. Phase 2 also has two traps this entry did
  not list: `setSinkId` stops routing once audio goes through an
  `AudioContext`, and `createMediaElementSource` works once per element.
  Every transition was read from `mpv.log` in the installed service, and both
  controls were **listened to** on action, a whisper-quiet scene and a stereo
  track, and judged clearly worth having. **Phase 2 is built**: night mode
  on Web Audio for music (measured offline and then listened to; a vocals
  control was tried and removed), and for films in a browser tab (v0.9.67,
  #775), offered only where what reaches the element is mono or stereo, so a
  directly played 5.1 soundtrack is never folded down without saying so.
  Dialogue boost stays the desktop's. **Phase 3, the equaliser, is built for
  music** (v0.9.74, #818): presets and five bands. Films keep night mode and
  dialogue boost, by choice. Loudness levelling across a library is built
  from ReplayGain tags (2026-10-28,
  [ADR 0108](adr/0108-music-is-levelled-by-its-own-replaygain.md)), off by
  default, per device.

  The original entry follows.

  The obvious implementation is the wrong one. ffmpeg has `loudnorm`, `compand`
  and `equalizer`, and reaching for them costs twice: a filter **forces a
  transcode on a file that would have direct-played**, and it bakes one person's
  curve into a stream a household shares. **The client is the right home** — a
  Web Audio graph on the media element, `BiquadFilterNode`s for the bands and a
  `DynamicsCompressorNode` for night mode. Per device, no server cost, and it
  works *with* direct play rather than defeating it. There is no `AudioContext`
  in the client today, so this is new ground rather than an extension.

  **The trap is surround, and it is real.**
  `createMediaElementSource` reroutes the element's output through the graph,
  and the graph's destination is stereo — so **a 5.1 file that was direct-playing
  silently becomes two channels the moment somebody touches the EQ**. This
  codebase already has the vocabulary for it: `MaxAudioChannels` in
  [decide.go](../internal/probe/decide.go) exists to *force* a downmix, and doing
  the same thing by accident is the quiet kind of wrong this roadmap keeps a
  section about. So the graph engages only where the output is already stereo, or
  the trade is stated plainly at the switch. It is never silent.

  Two smaller rules. **Presets are per content type** — one flat curve shared by
  a symphony, a sitcom and a war film is what makes people turn an equaliser off
  and never return. And the graph must **survive the cross-document move** that
  [ADR 0029](adr/0029-picture-in-picture-is-our-window.md) needs, which the
  acceptance test named there should now cover an `AudioContext` as well as the
  element. Watch Together raises no question here: the curve is per device, so
  there is nothing to synchronise.
- ~~**Subtitle appearance — font, size, colour, background, position**~~ —
  **built.** Colour, size and position shipped in v0.8.18 on the playback
  settings panel; the **typeface** and the fix below are v0.9.6.

  Two of the five named here were never going to be controls. **Background is
  deliberately not offered**: it stays a dark box because that is what makes a
  subtitle readable over a bright scene, and a switch that could turn it off
  would let somebody make their subtitles invisible over exactly the shots that
  need them most. **Font is a generic family, not a name** — a name is a bet
  that the machine has that face, and no font is bundled, because a
  high-legibility face designed for dyslexia is a licence and bundle-size
  decision that a dropdown does not get to make on its own.

  And what shipped did not reach the **pop-out player** for three releases. The
  preferences were written as custom properties on the page's `documentElement`,
  and the pop-out ([ADR 0029](adr/0029-picture-in-picture-is-our-window.md)) is a
  second document with a root of its own: `copyStyles` carries the stylesheets
  across, an inline style on an element is not a stylesheet, and every
  `var(--cue-…, fallback)` resolved to its fallback. Subtitles at the shipped
  defaults whatever anyone had chosen, with nothing failing anywhere — the third
  time this project has shipped a feature whose halves were each correct and
  never joined.
- ~~**Chapters and preview thumbnails on the scrub bar**~~ — **both built**.
  Chapters (#949): ffprobe's chapters, cached per file, as ticks on the scrub
  bar with the name on hover, and Page Up/Down to skip between them. Previews
  ([ADR 0105](adr/0105-a-preview-is-a-keyframe-every-ten-seconds.md)): a
  keyframe every 10 s, tiled into JPEG sheets by a background pass that is off
  until turned on, about 40 s and 2 MB per film, shown above the pointer.
  Original entry: two halves
  of one control. **Chapters** are already in most MKVs and MP4s; `probe` does
  not ask ffprobe for them, and once it does the scrub bar can mark them and
  skip to them. That half is cheap. **Preview thumbnails** (trickplay) are the
  large one, already on the "genuinely large" list from the Plex settings
  triage: a frame every few seconds for every title is an enrichment pass with
  real CPU and disk cost, and it needs the cost shown first, as *Refresh
  metadata* does.
- **Chromecast and AirPlay** — not built. DLNA is (#872), so a TV that speaks
  DLNA can already pull from LANcast. Chromecast needs a sender in the client
  and a receiver on the device; Google's default receiver is loaded by the
  Chromecast itself, so LANcast contacts nothing, but a custom receiver would
  need registering with Google. AirPlay has no official server-side
  specification and is the heavier of the two. Each is its own ADR.
- **Play on another of your own devices** — shipped in v0.10.6
  ([ADR 0117](adr/0117-play-on-another-device.md)), proven phone to desktop
  as the installed service. The owner's "screen share
  / casting" item, read on 2026-11-07 as Plex's "play on": one LANcast window
  sends a film or a song to another window signed in as the same account, at
  the moment it was at, and controls it from a remote panel. In memory, no
  schema; commands are announced by the event stream (`remote`) and fetched,
  never pushed as payloads. The desktop window offers itself by default and a
  browser or phone opts in, because a browser will not start sound untapped.
  Never across accounts and never across servers.
- **Offline downloads** — partly built. `GET /api/items/{id}/download` already
  hands over the original file. What is missing is the client half: keeping a
  copy on a device, playing it with no server, and reporting the progress when
  the device is back. That needs a client with its own storage, so it waits on
  a phone or TV client rather than on the server.

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

- **Theme music → M2.** Needs TVDB ids and OST identification. Cannot land
  sooner, and is now **on the back burner** by choice as well as by dependency:
  OST identification is unplanned, and theme music is the only feature whose
  absence nobody has reported.
- **TV client → keyboard focus model.** The roving-tabindex controller is the TV
  client's foundation. Compromise it during M3 and the TV client becomes a
  rewrite instead of a restyle.
- **Plugin contract → one full build of the core.** Deliberately last.
- **Users and auth → schema.** Already handled; can arrive late without data loss.
- ~~**API versioning → before any third-party client exists.**~~ — **built**,
  and deliberately not as a URL rewrite. [ADR 0018](adr/0018-api-contract-and-versioning.md)
  has stated the policy since M3 and nothing enforced it. Now every `/api`
  response carries `X-LANcast-API-Version`, a client may send the same header to
  assert what it was built against, and a version this build cannot serve is
  refused by name with `unsupported_api_version`. That refusal is the valuable
  half: without it a mismatch surfaces as a field that is mysteriously absent
  three screens later, and the report that arrives is "the library page is
  blank". Moving every route under a version prefix would have broken the
  existing client today to buy a property nobody is using yet, and ADR 0018
  already promises `/api` never changes meaning — the same guarantee at no cost.

## Open decisions

*One ADR is `proposed` and it is not waiting on code. They are waiting on
somebody deciding, which is a different queue and was not written down anywhere
— so it is written down here. A third, 0042, was accepted and built on
2026-08-25 and is kept in the table struck through, because what a decision
unblocked is worth more than a tidy list.*

| ADR | Proposed | The question | What it holds up |
|---|---|---|---|
| [0013 amendment](adr/0013-transcode-pipeline.md) | 2026-08-23 | Adopt **MSE for live TV and only live TV**, vendoring hls.js as pinned, reviewable source | **The question changed on 2026-08-29 and now needs deciding again.** Steps 4 and 5 are built, but the gate on step 6 cannot be run as written: the MSE path is unreachable on this client, because **Chromium plays HLS natively** and takes step 5's branch every time. Measured — a bare element handed Apple's fMP4 playlist with no library reached `readyState 4`, `480x270`, 12.0s buffered, frames decoded. That contradicts the premise the amendment rests on, and the reason is narrow and real: Chromium cannot demux **MPEG-TS**, which most IPTV *sources* are, and this server does not serve those — `Args` sets `-hls_segment_type fmp4`, which is exactly what Chromium plays. The rule was true about the sources and got generalised to the output, and the output is ours to choose. So the comparison worth making is now **native HLS against MSE**, not either against progressive, and it wants a Firefox — the likely real audience for MSE is a browser with `MediaSource` and no native HLS, which is reasoning rather than a reading |
| ~~[0048](adr/0048-media-tools-install-themselves-on-first-run.md)~~ | 2026-08-27 | Should the server **fetch ffmpeg on first run**, having said so, instead of waiting to be asked | **Accepted 2026-08-28, narrower than proposed, and the principle survives.** The tools are a ticked option on the setup form rather than an automatic fetch: pressing *Create account* fetches them, unticking is one click. That serves the inattentive user — the one the ADR was written for — exactly as well, and the traffic follows a button somebody pressed, so no-phone-home keeps its third job and **README is unchanged**. The exception the ADR argued should be written into the principle turned out not to be needed. **Built in v0.8.24** (#408, with #409 saying where the missing tools bite). This row said "not yet built" for a month after it shipped |
| ~~[0060]~~(adr/0060-semantic-photo-search-is-a-second-model-in-the-sidecar.md) | 2026-09-04 | ~~Should **semantic photo search** be built, and as a second model in the existing native sidecar rather than as a plugin~~ | The one library that cannot be searched. A photograph's title is `DSC_0042`, so `?q=` answers nothing there, and the timeline, folders and people are the only ways in. **It corrects an answer this project had half-adopted**: the Immich study said semantic search should prove a plugin SDK, and the sandbox exports three host functions — log, http, secret — so the only plugin-shaped version is the one that uploads photographs to somebody else's server. Waiting on a decision, and on a model whose *weights* permit commercial use — the trap ADR 0052's first draft fell into. **Accepted 2026-09-04.** OpenAI's own CLIP weights declare no licence at all and are out; LAION's are MIT with an inherited out-of-scope *caveat* rather than a licence term; SigLIP is Apache-2.0 and is the documented fallback. **OpenCLIP ViT-B/32** chosen, because the cleaner licence has the harder tokenizer — SentencePiece against a BPE this project can write itself |
| ~~[0061]~~(adr/0061-an-api-key-is-not-a-session.md) | 2026-09-06 | ~~Should a **long-lived API key** authenticate a third-party client, and should it be **refused admin** even when its owner is an administrator | The spec shipped in v0.9.0 describes 141 operations a third party can now read, and leaves them nothing to authenticate with but a session cookie obtained by storing somebody's password. The part that needs deciding is not the table — it is whether a credential that lives unattended in a config file may add a library, which is arbitrary filesystem read at a path the request chooses. This says no, and says a CLI that needs otherwise should arrive as its own decision rather than as a quiet flag. Also decides that changing a password does **not** revoke keys, which is a documented exception to the rule that it logs everything out.~~ **Accepted 2026-09-06**, admin restriction as drawn |
| ~~[0062]~~(adr/0062-a-tag-is-a-note-to-yourself.md) | 2026-09-07 | ~~Should **tags be private to each account**, should they stay out of NFO sidecars, and what carries a favourite now that gold cannot | The Immich study's #5. Genres are somebody else's vocabulary and a refresh overwrites them; there is no TMDB genre for *needs a better copy*. **Two of the study's claims did not survive.** It called tags a locked-field problem — true only while a tag can arrive from something other than a person, and with sidecars off the table nothing competes, so a lock would be guarding a writer that does not exist. And the first draft of this ADR made tags **shared**, reasoning from the model's shape: genres and playlists are shared, so organisation is shared. That reasoning is about a library, and a tag is about the person writing it — *needs a better copy*, *watch before the sequel*, *do not delete* are a picture of what somebody is thinking, and all of it would have been visible to the house because a table happened to be shaped that way. Corrected before any rows existed. The consequence worth the ADR is that the **vocabulary** is per-account too: a shared name table leaks the names, and privacy that depends on every query remembering to filter is a habit rather than a boundary.~~ **Accepted 2026-09-07**, private per account |
| [0065](adr/0065-a-channel-nobody-is-watching-stops.md) | 2026-09-08 | **A channel nobody is watching keeps running, and by the fifth one the server refuses to play anything.** Found by driving the app: two minutes of ordinary channel surfing, and `refused a channel: at the session ceiling running=3 max=3` three times — the slots held by channels already left. Three minutes after leaving Live TV entirely, two ffmpeg were still alive and one still writing segments at ~120 KB/s, pulling a provider stream at full rate for nobody. Nothing is leaking: everything works as written. Moving live TV to an HLS playlist removed the only signal the server had — the progressive path tied ffmpeg to the request, and a poll of a playlist is not a lifetime, which `context.WithoutCancel` correctly recognises. The replacement is `IdleTimeout`, and **that constant is a film's number**: its own comment records being raised to ten minutes "so a paused film keeps its ffmpeg". A paused film costs nothing while it waits and is expensive to resume; an abandoned channel costs full-rate bandwidth for ten minutes and is not a thing anybody resumes into — coming back to live means joining at *now*. One constant answering two questions, tuned for the one where waiting is cheap. **The visible failure is not the bandwidth** but being told your own server is busy while alone in the house. Proposed: the client says when it stops watching — sent as a beacon, because a fetch during unload is routinely cancelled and a stop that only arrives when the tab survives misses the case it exists for — **and** a separate short idle timeout as the backstop, derived rather than picked (a viewer polls every `SegmentSeconds`, so thirty seconds is about five missed polls). Both, because a timeout alone is what exists now and an explicit stop alone trusts a client that may never speak again. `MaxSessions` is deliberately **not** raised: the refusals happened because the sessions were not real. And the same test-shaped hole as the playlist bug found hours earlier — `TestLiveStopsFFmpegWhenTheClientGoes`, whose comment calls this "the property this feature lives or dies on", exercises the **progressive** endpoint, the one path that can satisfy it by construction, while the HLS path ships untested |
| ~~[0064]~~(adr/0064-what-the-plugin-abi-promises.md) | 2026-09-08 | ~~**What does the plugin ABI promise?** The contract is written down for outsiders now, which is what makes the question askable — before a contract is published, "what do we promise" is a question nobody is entitled to ask; after, the answer is either written or improvised, and improvised means discovered by somebody else's plugin breaking. What existed was one line, an exact-match check, and **that is not a versioning policy but the absence of one**: under it every change was breaking, including ones that could not possibly break a plugin, so the contract could only evolve by flag day — and the pressure that creates is to never evolve it, or to change it without bumping, which is worse because then the number lies.~~ **Accepted 2026-09-08.** Additive-safe within a version, mirroring [ADR 0018](adr/0018-api-contract-and-versioning.md) for the same reason with the difference that the rule must hold in *both* directions, since here the host both sends and receives code we do not control. The host runs the current ABI **and the one before it**, for at least one release: a self-hosted operator cannot be upgraded in lockstep with anything, and without a window the ordering is impossible — nobody can publish a build for an ABI that has not shipped, so a hard cutover strands every plugin on day one by construction. One version deep, not everything ever shipped, because carrying five readers is what ADR 0063 refused under another name. **ABI 1 stays exempt**, recorded explicitly so nobody later reads the rule, sees 1 refused, and files it as a bug. Two obligations on the guest make the additive rule safe and are contract rather than folklore: ignore unknown fields, tolerate absent ones. The decision **obliged two code changes and said so while proposing them**: `MinABIVersion` turns the window into a constant, because a policy living only in a document is one the next bump forgets; and `Plugin.HasExport` makes an optional export genuinely additive, since `Call` errors on a missing one and a host calling a newly-added function would otherwise fail every plugin built before it — writing that rule without the change would have been a policy that lies, and the next author to follow it would have broken every installed plugin. Reading the exports at compile time also paid for itself immediately: a module is now checked against its kind **at load**, so a provider exporting no `search` is refused where somebody is reading the error instead of installing cleanly and failing at the first call with a line that reaches a log |
| ~~[0063]~~(adr/0063-a-provider-is-the-second-plugin-shape.md) | 2026-09-08 | ~~Should a **metadata provider be the second plugin kind**, before the plugin contract is documented and published | The Immich study's #2, and the runtime is further along than that study assumed: a guest SDK, a first-party OMDb plugin holding no key and no socket, an equivalence test against the native implementation, signed bundles and an Add-ons page all exist. What does not is a second extension point — and `m4-plugin-scope.md` asserts that a boundary carrying OMDb can carry a metadata provider, which was an assumption and never a result. **Trying it found two holes.** The ABI cannot say "it went wrong": empty and failed are the same answer, survivable for ratings and not for a provider, where "no candidates" means *unmatched* and gets written down. And `Record.Artwork` is a list of URLs the host fetches with **no allowlist check** — correct while only first-party code fills it, and a server-side request forgery the moment a plugin does, because the host fetches the URL and serves the bytes back as the item's poster. The capability model stops a plugin reaching the network; this is the host reaching it on the plugin's behalf, through a field nobody thought of as a capability. Both are the kind of thing a third party would have found after publication, which is the whole argument for a second shape first.~~ **Accepted 2026-09-08.** The second hole was split out and fixed first, and splitting it turned up something worse: the allowlist matches a *hostname*, and a hostname is not a destination — a plugin whose granted domain resolves to 127.0.0.1 could already reach the server, with no provider and no artwork field involved. Live for the rating plugins that exist today, fixed in `internal/netguard` |
| ~~[0039](adr/0039-organising-a-large-channel-list.md)~~ | 2026-08-17 | ~~How to make **1,862 channels** usable~~ | **Accepted 2026-09-30.** Steps 1–3 are built: a playlist selector that appears once there are two playlists, groups as sections that open, and hidden and favourite channels per device. Step 4, a guide grid, stays deferred until a provider publishes XMLTV |
| ~~[0042](adr/0042-two-files-one-work.md)~~ | 2026-08-17 | ~~What happens when **two files claim one work**~~ | **Accepted 2026-08-25 and built.** The collision report exists, the edition marker is kept, and nothing merges, ranks or deletes. This no longer blocks the [ADR 0041](adr/0041-a-misplaced-file-is-corrected-on-disk.md) parser fix |

0042 was the one with a real dependency, and it is **accepted and built**: the
parser fix it was holding up can now land, because the thirteen duplicate pairs
it would otherwise create silently are reported instead. The two that remain are
ceilings rather than blockages — nothing is waiting on either.

The standing rule 0013's amendment argued with is in
[CLAUDE.md](../CLAUDE.md): this build ships no unaudited third-party player, and
progressive fMP4 is the default precisely so it does not have to. **That
amendment was accepted on 2026-08-27** and the rule was satisfied rather than
relaxed — hls.js is vendored as a bundle reproduced here and verified
byte-identical to the published one, reviewed and signed. Steps 4 and 5 are
built and MSE sits behind a device setting that defaults to off.

**Step 6 is not merely ungated now; it is the wrong question.** It flips the
default to MSE and deletes the compensation code, and it was waiting on somebody
watching a real channel play. Somebody has, twice — and both times what played
was **step 5's native-HLS path**, because Chromium takes it every time. The
amendment's premise, that a browser needs a third-party library to play HLS at
all, is true of MPEG-TS sources and false of this server's own fMP4 output.

Two things follow, and neither is a decision this file should make. The
comparison worth running is native HLS against MSE. And **the sentence in
CLAUDE.md is now too broad** — it is right about what this build refuses to
ship and wrong about what a browser can do, and a rule that is wrong in its
reasoning gets ignored eventually even where it is right.

Recorded rather than acted on, because unvendoring a package that was reproduced
byte-identically, reviewed and signed is a decision with a cost, and the reading
that prompted it is one afternoon old.

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
   4. **Artist images from TheAudioDB** — **provider built, not yet wired to a
      worker.** The lookup, the matching rules and the refusals are implemented
      and tested against fixtures; what it does **not** have is a live
      verification, because TheAudioDB needs an API key and none was added. The
      rule worth keeping is that a **near miss is refused**: a search endpoint
      returns neighbours, and taking the first row is how "Sun" gets a
      photograph of "Sunn O)))" — a wrong face is worse than the borrowed album
      sleeve it would replace, and refusing leaves that placeholder in place.
      Originally recorded as: **back burner, unblocked and not next.** The borrowed album cover is a placeholder good enough to wait
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
    backlog: more client surfaces (TV/mobile), more plugin kinds as real
    plugins need them. **Theme music is on the back burner** — it stays
    blocked on OST identification and is not waiting for a slot. Each is
    planned immediately before it is built.

    ~~Ahead of any of it: **Live TV plays at the wrong speed**~~ — **diagnosed
    and fixed**, and almost every word of the old entry here was wrong, which
    is why it is worth replacing rather than deleting.

    It was never a timestamp question and never the server. `-fflags +genpts`,
    the standing suspect since v0.6.46, is **exonerated** — reproduced from the
    command line at last, twice, the second time against a real channel: mean
    packet step 0.033s against a declared 30fps, largest gap 0.17s, zero holes
    over 0.25s, audio monotonic. The server hands over 29.9 frames per
    wall-second and audio at 46.9 packets per second against the 46.9 AAC-LC at
    48kHz requires, across twenty channels.

    **The fault was the client pausing itself.** `hold()` was wired to
    `waiting`, which on a progressive stream means "I cannot render the next
    frame right now" and fires at fragment boundaries — about **0.84 times a
    second** on a real channel — with no relation to how much media is in hand.
    Measured with devtools attached: **28.1% of wall time paused, while holding
    a median of 131 seconds of buffered media**, dragging playback to 0.76×.
    The buffering a viewer saw every few seconds was not the symptom of a
    drought, it *was* the mechanism. The buffer decides now, not the event:
    0.6% paused after, and the one hold left had 7 seconds in hand.

    Two calibrations came out of measuring **our own endpoint** rather than the
    provider, and both have since been superseded by fixing the cause instead.
    The head start was 8s against a drought measured at 5s upstream; at the
    endpoint the silences reached **9,850ms**, so it went to 12s. Then the
    server stopped relaying the silences and it came back **down to 3s** — the
    two cushions add up and a viewer waits for the sum. Drift is closed by
    playing 10% faster, **never by seeking**: the live endpoint is an unbounded
    chunked response with no `Accept-Ranges`, so a seek that misses strands the
    element, observed as a channel holding 22 seconds of media at `0:00` that
    would not restart.

    **The server-side fix is built** (v0.8.5). This entry previously called it
    "unbuilt" and named it the real answer, and that was right on both counts:
    `internal/livebuf` reads ahead and hands the stream out at its own rate,
    turning a p99 gap of **5,326ms** into **152ms** with **zero** silences over
    a second, at the price of 12.4s to first byte paid once. It paces by bytes
    at the observed rate rather than by decoding timestamps, so it stays a byte
    pipe that cannot corrupt a container or disagree with the muxer about what a
    second is.

    Two follow-ons have shipped since. The player now says **"Catching up"**
    whenever it is running above 1.0x (v0.8.6) — a 10% correction is audible,
    and silently applying it turns the fix into somebody else's bug report,
    which is exactly what happened. And **hold and resume no longer share a
    threshold** (v0.8.7): a channel near the boundary held at 2.9s, resumed at
    3.0s, lost it within three seconds and held again, so resuming now waits for
    5s where starting still waits for 3.

    **Then the "no known open fault" claim turned out to be wrong.** This
    section said exactly that, and driving two channels in the app found a
    defect within minutes: one froze at `0:01` for eighty seconds while ffmpeg
    stayed healthy and went on producing bytes for three more minutes. The
    cause was ours and it was not the channel — `frag_every_frame`, chosen
    deliberately in [ADR 0013](adr/0013-transcode-pipeline.md) so a long-GOP
    channel would not wait for its first keyframe, **corrupted the timestamps
    of every live channel it produced**. Measured against a fixed 40s capture
    so it was the flag and not the stream: 2,192 ffmpeg warnings and duplicate
    DTS with it, zero without, and a browser demuxer requires DTS to increase
    strictly. `231043c` moves the fragment interval off the frame and onto the
    clock (`-frag_duration 200000`), which keeps the reason the flag was chosen
    and drops the damage, at 0.9% more bytes. Verified **as the installed
    service**, per the session-0 rule: audio sync corrected and the channel
    plays.

    Three things are worth keeping from how it was found, because none of them
    was the test suite. The symptom users would report is *audio drift*, not a
    freeze. The server log said `ffmpeg reported errors` **on every normal
    channel stop**, which sent two separate diagnoses down the wrong path
    before anyone read it properly. And the reason it survived is that nothing
    failed: ffmpeg exits fine, the picture simply stops.

    **Where it stands.** The two v0.8.5–v0.8.7 follow-ons still hold: the player
    says **"Catching up"** whenever it is above 1.0x (v0.8.6), because a 10%
    correction is audible and applying it silently turns the fix into somebody
    else's bug report — which is exactly what happened; and **hold and resume
    stopped sharing a threshold** (v0.8.7), which had a channel near the
    boundary holding at 2.9s, resuming at 3.0s, losing it within three seconds
    and holding again. Two things are still true rather than fixed. **"Far too
    fast" was never reproduced** and is not claimed; if it is real it is a
    separate fault and needs a fresh report. And every client-side constant is
    still *compensation* — the reason
    [ADR 0013](adr/0013-transcode-pipeline.md) carries an amendment arguing for
    MSE on the live path and only there, with hls.js vendored as pinned,
    reviewable source. That decision is now **accepted in principle** (2026-08-27)
    and **gated on its step 2**: the amendment rests on the server already
    producing HLS a real player can consume, and that has never been
    demonstrated. Until a throwaway harness plays a live channel end to end,
    no dependency is taken and the standing rule that this build ships no
    unaudited third-party player is untouched. **That harness has now been run** (2026-08-27) and the
    answer is no: over 60 seconds ffmpeg wrote nine good segments and **never
    wrote a playlist**, so there was nothing for a player to fetch. A control
    changing one flag — `-hls_playlist_type vod` to `event` — produced the
    playlist at 7.0s, so the cause is not in doubt. `Manager.Live` hard-codes
    progressive output and no channel has ever taken the HLS path, so the
    amendment's premise that the server needs no work was **wrong**. Building a
    live HLS output was the next Live TV task and **is now built** (#391): the
    playlist type follows the source, `event` for a channel, and a session is
    shared between viewers. **Step 3 is amended rather than done.** Vendoring
    hls.js *as source* turned out to mean 54,255 lines to review and a
    993-package build toolchain — more third-party exposure, not less, and a
    review nobody would honestly perform. The term is now **reproduce the
    bundle**: build from the pinned commit, confirm byte-identity with the
    published artefact, vendor what you built. Verified on 1.7.1 — three of
    four bundles byte-identical, the fourth identical modulo Windows line
    endings. **The dependency is now taken** (#393): hls.js
    1.7.1 is vendored as a bundle reproduced here and verified byte-identical to
    the published one, reviewed and signed. **Steps 4 and 5 are built** (#394) —
    MSE behind a device setting that defaults to off, with native-HLS browsers
    given the playlist directly and any device that cannot do MSE falling back
    rather than failing. **Step 6 is gated**: the default does not flip and
    nothing is deleted until MSE has been watched playing a real channel in a
    real browser, against a server containing #391. jsdom performs no media, so
    the 443-test client suite proves the wiring and cannot prove playback. The v0.8.7 hysteresis fix was step one and the only step that does not
    depend on the outcome.

    And the lesson, which cost an hour: the activity panel called every ffmpeg
    session "Transcoding for playback" whatever it was doing, so a channel being
    *copied* announced itself as a transcode and sent this investigation after
    an encoder that was never running, on the evidence of a badge, while the log
    two lines away said `video=copy`.

    ~~Also ahead of the breadth work: **the right-click menus are half a
    feature**~~ — **closed in v0.8.6, and the layout gap closed in v0.8.11.**
    Every poster answers a right-click, containers included, and the three
    capabilities that existed but were each reachable from exactly one screen
    (add to playlist, remove from library, start over) are on the tiles that own
    them.

    The layout half is worth keeping because it took five releases and was
    invisible the whole time. The v0.8.6 entry recorded that jsdom performs no
    layout, so `PointMenu`'s edge-flip had never been watched carrying a
    seven-item list. It was worse than mis-placement: a menu near an edge is
    clamped back on screen to `innerHeight - height - 8`, which is exactly where
    the docked player sits, and it was painted underneath it at `z-index: 60`
    against 120 and 121. **The clamp did not permit the collision, it produced
    it** — the tiles most likely to need repositioning were the ones guaranteed
    to be covered afterwards. It reported as "no context menu opens near
    boundaries", because that is what being covered looks like from the front.

    Two things generalise. **The suite cannot see paint**, so every assertion
    about that menu passed in both states for four releases; it was found by
    somebody using the app and confirmed with `elementFromPoint`, not by a test.
    And the numbers now live in `tokens.css` as a named ladder because they were
    four values in four files expressing one relationship — **the comment
    directly above them already recorded the same class of failure one level
    down**, where the docked strip grew and the picture's clearance did not
    follow, and it happened again anyway.

14. **Codec breadth, now that the denial ratchet is gone.** Asking "what is
    still unsupported" turned up a list in three tiers, and only one of them is
    worth building for the current client.

    ~~**Reachable now.** A **10-bit H.264** claim~~ — **built** (`6ae4396`).
    `high10`, named for the profile rather than the bit depth because "High 10"
    belongs to H.264 alone where HEVC's is Main 10, so it cannot be misread as
    covering both the way `hevc` once was. High 10 is most of an anime library
    and had been a full video re-encode every time, on the reasoning that it
    "is not supported by browsers" — which was read for years as a fact about
    browsers and turned out to be a fact about nobody having asked: Chromium
    answers `probably` for `avc1.6e0033`.

    It differs from `hevc10` in one way on purpose. `hevc10` trusts a profile
    that lists HEVC *natively*, because `tv` and `safari` are device classes
    known to decode Main 10 in hardware; `high10` trusts no native listing,
    because H.264 is listed by every profile including the browser floor, and
    the same rule would hand High 10 to a set-top box on the strength of it
    decoding 8-bit. High 10 is absent from most fixed-function decoders that
    manage High profile perfectly.

    Safe to add only because a denial now expires; before `845610f` it would
    have become the fifth claim written off for ever. **Watched end to end on
    2026-09-30**: a *Deep Space Nine* episode in High 10 direct-played in
    WebView2 with no conversion and no decode-health fallback (see
    [Where it stands](#where-it-stands)).

    **Not reachable, and the reason is the client rather than the server.**
    MPEG-2, MPEG-4 part 2, VC-1, TrueHD and DTS all answer empty in Chromium,
    so a claim for any of them would never fire. They are **TV-client work**,
    listed here so the next person does not re-derive it: the server already
    handles them, and `tv` already lists most of them.

    **The measurement lesson, which cost an hour.** A synthetic `canPlayType`
    sweep in ordinary Chromium **does not predict WebView2** — it answers empty
    for `ac3` and `eac3`, and WebView2 direct-plays E-AC-3 quite happily, Edge
    carrying different codec licensing. The only probe that counts is one run
    in the shipping client, and the cheapest way to run it is to play a file
    with that codec and read the decision reason out of `lancastd.log`.

15. ~~**ffmpeg on first run is a decision before it is any code.**~~ **Decided:**
    [ADR 0048](adr/0048-media-tools-install-themselves-on-first-run.md) was
    accepted on 2026-08-28 in a narrower form. The tools are a ticked option at
    setup rather than an unasked fetch (see [Open decisions](#open-decisions)).
    The original entry follows. The intent is
    to install it automatically rather than waiting to be asked, which
    [ADR 0043](adr/0043-media-tools-are-fetched-not-bundled.md) deliberately did
    not do. That needs an **amendment written and accepted first**: fetching
    ~80MB from a third party on first start, unasked, is what the no-phone-home
    principle in [README.md](../README.md) exists to prevent, and that principle
    is not negotiable without an ADR. The install button itself already exists
    and works; what is proposed is removing the asking, which is exactly the
    part the rule is about.

## What the passes taught

The lessons of each review pass before 1.0 are in [history.md](history.md#what-the-last-pass-taught).

## Amendments to schema revision 1

M2 planning surfaced two gaps in revision 1. Because M1 has not been built,
these belong **in** revision 1 rather than becoming the first migration:

- Add a `meta` table seeded with `schema_version = 1`. Without it, the first
  migration has to guess what it is migrating from.
- Make `container`, `size_bytes`, and `mtime` nullable, so M2 can create
  `media_item` rows for directories ([ADR 0010](adr/0010-shows-as-media-items.md)).
