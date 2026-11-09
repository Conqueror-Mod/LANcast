# Settings 3.0: a place for everything LANcast learned to do since

Status: accepted, 2026-11-08. Nothing here is built. It is written while
v0.10.10 (Homepage 2.0, part two) is being finished, so that Settings 3.0 can
start on the release after it. Chris's decisions are in §7; each took the
recommendation.

The first settings plan ([settings-plan.md](settings-plan.md), "Settings
2.0") asked what was missing and how to reorganise around it. Every step of it
shipped, from v0.9.82 to v0.9.86. This plan asks a different question: **what
has LANcast become in the four weeks since, and does Settings still describe
it?**

## 1. What changed since Settings 2.0

Since 2026-10-13 LANcast gained, among others: audiobooks (ADR 0116), comics
(ADR 0101, 0102), music levels, genres and MusicBrainz albums (ADR 0114, 0115),
ReplayGain (0108), RetroAchievements in play (0112), play on another device
(0117), scrub-bar previews (0105), pre-roll (0099), extras (0098), chapters,
webhooks (0097), metrics (0096), two-factor sign-in (0100), friend invites by
link (0104), the dashboard's history (0106) and Homepage 2.0 (0118, 0119).

Settings kept up with the **server** half of that well. Webhooks,
Visualizers, Conversions, DLNA, Pre-roll, Two-factor and Metrics all have a
pane or a row. It did not keep up with the **person** half: the new kinds of
media arrived with their own preferences and those preferences have nowhere to
live.

Today: 4 groups, 32 panes, about 130 searchable settings, and
`web/src/screens/Settings.tsx` at 4,824 lines holding nearly every pane.

## 2. What is wrong now

### 2a. The newer kinds have no home in Settings

| Kind | What exists | Where it lives | The gap |
|---|---|---|---|
| **Home** (0118, 0119) | Pinned shelves (up to 12), Spotlight mode and pin | Shelves are pinned on Home itself (server, per account, `home_shelf`). Spotlight mode is in This device › Display & home, stored in `localStorage` (`lancast:hero-mode`, `lancast:hero-pinned`). | Half of Home is per account and half is per device, for no stated reason. There is no one place to see, reorder or remove shelves; ADR 0119 already promised hiding built-in shelves "later, same table". |
| **Audiobooks** (0116) | Speed, sleep timer | Speed is per book, set in the book bar. The sleep timer is per session. | No default speed for someone who listens at 1.5× to everything. No default sleep timer. |
| **Comics** (0101) | One page at a time, fitted to the screen | Reader only | No reading direction (manga reads right to left), no two-page spreads, no fit choice. None of it is settable anywhere. |
| **Retro games** (0073, 0112) | RetroAchievements connection, rewind | RetroAchievements is on **Profile**, not Settings (ADR 0112 step 6 asked that it be findable; it is linked from Settings › Retro, which is an admin pane). Rewind is in `localStorage`. | A non-admin looking for "achievements" in Settings finds nothing they can open. |
| **Music** (0108, 0114, 0115) | ReplayGain, crossfade, EQ, Now Playing, visualizers | This device › Player | Present, but buried — see 2b. |
| **Play on another device** (0117) | Let my other devices play here, this device's name | This device › Player | Present, but not a player preference. It is about this device being a *target*. |

### 2b. This device › Player holds four subjects

Player has 16 settings: quality, dialogue boost, night mode, autoplay, intro
skip, pre-roll, EQ, crossfade, loudness, Now Playing, calm visualizers, the
photosensitivity warning, theater output, frame-rate matching, and the two
remote-play rows. That is video, music, the theater display, and remote play
in one column. It is the same fault Settings 2.0 fixed in the server's
Playback pane ("one pane, unrelated things"), arrived again on the device side.

### 2c. Per device or per account is decided case by case

Settings 2.0 drew the line clearly — "your account follows you, this device
stays on this screen" — and decided subtitle style is per account. Since then
each feature chose for itself. Spotlight is per device; pinned shelves are per
account. Book speed is per book; comic place is per account. The spoiler
switch and tile size are per device.

Some of these are right. A TV and a phone want different tile sizes. But
nobody wrote down the rule, so there is no way to tell which choices are
deliberate.

### 2d. Settings 2.0 items that were never built

These were in the 2.0 plan's §3 and no field for them exists in
`config.Settings` today:

| Item | 2.0 priority | Note |
|---|---|---|
| **Metadata language and region** | P1 | The only P1 from 2.0 left. TMDB is asked for a fixed `en-US` (`internal/meta/tmdb/tmdb.go`), stated on purpose and tied to the interface having no translations. Choosing it is a change of that decision, not a missing switch. |
| Per-library options: language, exclude patterns, detection on/off | P2–P3 | A "Manga" or "Anime" library beside an English one is now a real case (comics, 2a). |
| Local networks, and a quality cap for viewers away from home | P2–P3 | Friends now watch over the internet (v0.9.37 onward), which makes this less hypothetical than it was. |
| Per-account transcoding caps | P3 | |
| Sign-in policy: session lifetime, backoff | P2 | Fixed in code. |
| Diagnostics bundle | P2 | "Report an issue" (0109) covers part of this. |
| Storage overview | P2 | Sizes of the database, artwork, previews, optimized copies, backups, in one place. Previews (0105) and optimized copies (0087) both arrived since and both use real space. |
| Self-tests | P3 | Media tools present, encoder proven, write access per library. |
| Editable schedules beyond the scan and backup | P2 | |

### 2e. The file is the bottleneck

`Settings.tsx` is 4,824 lines with most panes inside it. Chris runs several
sessions at once against this repository, and two of them touching Settings
means a conflict in one file. Every step below is cheaper and safer after it
is split into one file per pane. `settingsIndex.ts` stays as it is — its test
already proves every label still exists.

### 2f. Not yet looked at

Nobody has walked Settings at phone width or in Bigscreen mode since the 2.0
restructure. jsdom cannot see layout, so the test suite cannot say. Step 0
below does this before anything is redesigned.

## 3. The rule, written down

Proposed, for an ADR at step 2 so that features stop choosing for themselves:

1. **Server** — changes things for everyone. Admin only.
2. **Your account** — a fact about *you*: what you like, what you have
   connected, what you allow. Follows you to every device.
3. **This device** — a fact about *the screen and speakers in front of you*:
   size, distance, output, bandwidth. Stays here.
4. **A device setting may have an account default.** Where both exist, the
   device's own value wins, and the pane says "Using your account's default"
   until it is changed. This is how subtitle style already behaves in spirit,
   made explicit.
5. **A preference about a piece of media belongs to the media**, not the
   person: reading direction is a property of a manga library, not of a reader.

Applying it to 2c:

| Setting | Today | By the rule |
|---|---|---|
| Spotlight mode and pin | Device | **Account** (what you want to see first is about you) |
| Pinned shelves | Account | Account (unchanged) |
| Tile size, Bigscreen | Device | Device (unchanged — about the screen) |
| Spoilers on a season page | Device | **Account** (about you) |
| Book speed | Per book | Per book, **with an account default** |
| Comic reading direction | — | **Per library**, override per series |
| Retro rewind | Device | Device (it costs memory on this machine) |

## 4. Proposed structure

The four groups stay — they were decided in 2.0 and they work. What changes
is inside Your account and This device.

### Your account

| Pane | Holds | New? |
|---|---|---|
| Profile | Display name | — |
| **Home** | Your shelves, in order, with remove and reorder; hide built-in shelves (ADR 0119's "later"); Spotlight mode; spoilers. | New pane; Spotlight and spoilers move here and become per account. |
| Security | Password, passkeys, two-factor, API keys, authorized devices | — |
| Languages & subtitles | Audio and subtitle languages, subtitle style | — |
| **Reading & listening** | Default audiobook speed, default sleep timer, comic layout defaults (single page or spreads, fit). | New. |
| **Connected accounts** | RetroAchievements (moved from Profile), and any later per-person service. | New pane; nothing new behind it. |
| Privacy | Sharing, peers, webhooks consent | — |

### This device

| Pane | Holds | From |
|---|---|---|
| **Video** | Quality on this device, dialogue boost, night mode, autoplay, skip intros, pre-roll. | Player |
| **Music & Now Playing** | EQ, crossfade, loudness, Now Playing, calm visualizers, photosensitivity warning. | Player |
| **Theater output** *(desktop only)* | Theater output, frame-rate matching, HDR. | Player |
| **Remote play** | Let my other devices play here, this device's name. | Player |
| Display | Bigscreen, tile size, improved live TV playback. | Display & home, minus what moved to Home |
| Retro games | Rewind, and controller mapping when it exists. | New pane, from `localStorage` |
| Keyboard · This app · PC Games · About | Unchanged. | — |

### Server

Mostly unchanged. Three additions, all from 2d:

- **Metadata** gains language and region.
- **Libraries** gains a per-library page: language, reading direction (comics),
  exclude patterns, detection on/off. Exclude patterns are read by
  `internal/media`, which remains the only place filenames are interpreted.
- **Backup & storage** gains the storage overview.

## 5. Order of work

Each step is one PR or a small stack, and leaves Settings working.

0. **Look first.** Drive Settings at desktop, phone (375px) and Bigscreen;
   record what breaks. No code. Its findings may add a step.
1. **Split `Settings.tsx` into one file per pane.** No visible change; every
   existing test passes untouched. Old `?pane=` links keep working.
2. **The rule (ADR) and the device split.** Player becomes Video, Music & Now
   Playing, Theater output and Remote play. `?pane=player` redirects to Video.
   Client only.
3. **Your account › Home.** Shelf order, remove, hide built-ins; Spotlight and
   spoilers move to the account. Server work (a column or two on the per-user
   prefs, and the built-in shelves join `home_shelf`'s table as ADR 0119
   planned). A one-time carry-over reads the old `localStorage` value so
   nobody's Spotlight resets.
4. **Reading & listening, and Connected accounts.** Book speed default,
   sleep-timer default, comic layout; RetroAchievements moves. Server work for
   the defaults.
5. **Metadata language and region.** Server work; a refresh in the new
   language is offered, never forced, and never touches a locked field.
6. **Per-library options**, starting with comic reading direction and
   metadata language. Schema change, so an ADR.
7. **Storage overview.** Read-only.

**Parked** (§7, decision 5): local networks, a quality cap for viewers away
from home, per-account transcoding caps, sign-in policy, the diagnostics
bundle, self-tests and editable schedules. Each comes back when someone needs
it, not on a timetable.

Steps 1–2 are client only and can follow v0.10.10 straight away. Step 3 is
the natural next release, because it finishes what Homepage 2.0 started.

## 6. Decisions for Chris

1. **The rule in §3** — especially point 4 (a device setting may have an
   account default) and point 5 (media preferences belong to the media).
2. **Spotlight and spoilers move to the account.** Anyone with Spotlight set
   differently on two devices would end up with one value.
3. **Split Player four ways**, or two (Video · Music), keeping theater and
   remote play inside Video?
4. **Comic reading direction per library**, or per series only?
5. **Which 2d items to carry.** The recommendation is metadata language,
   per-library options and storage overview now; park local networks and
   remote caps until a remote viewer actually needs one.
6. **Your own list.** Settings 2.0 got better from it — what is on it this
   time?

## 7. Decisions taken (2026-11-08)

1. **The rule in §3 is adopted, all five points**, including account
   defaults for device settings (point 4) and media preferences belonging to
   the media (point 5). It is written up as an ADR at step 2.
2. **Spotlight and spoilers move to the account.** Step 3's carry-over reads
   the old `localStorage` value once, so nobody's setting resets. Where two
   devices disagree, the first device to carry over wins.
3. **Player splits four ways:** Video · Music & Now Playing · Theater output ·
   Remote play.
4. **Comic reading direction is per library, with a per-series override.**
5. **Build** metadata language and region, per-library options, and the
   storage overview. **Park** the rest of §2d (listed under §5). Metadata
   language reverses the fixed `en-US` in `internal/meta/tmdb`, so step 5
   needs its own ADR saying why the metadata language may now differ from
   the interface's.
6. **Chris's list:** nothing for now. It can be added to this section later.

## 8. Step 0: what Settings looks like on screen (2026-11-08)

Walked against the live server, with this plan's step 2 build (#1045) served
by the dev client, signed in as an administrator. All 32 panes were opened at
each size; one page script measured them, and screenshots covered the panes
step 2 created. Read-only: nothing on the server was changed.

**What is fine at all three sizes:**
- Every pane draws.
- Nothing spills sideways at 1440, 375 or 1920 wide.
- No row collapses to zero height.
- Theater output is absent in a browser, as designed.
- In Bigscreen, the bottom of the nav can still be reached by scrolling.

**Found:**

1. **On a phone, the nav buries the pane.** At 375 × 812, the nav wraps into a
   block of 32 links, 467 px tall. The pane starts 624 px down, and the bottom
   tab bar covers part of what is left. Tapping a pane does not scroll to it,
   so a person sees about a row and a half of what they chose. This predates
   Settings 3.0; step 2 adds three links to the block.
   *Proposed fix:* below the phone breakpoint, show the list first and the
   pane second, with Back, as phone settings apps do. Client only.
2. **On a TV, a remote cannot move through Settings.** Neither the nav nor any
   pane control registers with the focus controller (ADR 0004); 24 other
   components do. Arrow keys therefore do nothing in Settings, with real key
   presses or synthetic ones. Only Tab moves focus, and a remote has no Tab.
   This predates Settings 3.0.
   *Proposed fix:* register the nav items and the pane's controls as
   focusables, scoped to the nav and the pane, so left and right move
   between the two. Client only.
3. **Two pane headings do not match their nav names.** *Music & Now Playing*
   opens on a section called "Music", and *Remote play* on "Play on another
   device". Small; fold into whichever PR next touches those panes.

**Not seen:** Theater output inside the desktop window, which needs the
native player. This session could not drive that window.

**Suggested order:** 1 and 2 before step 3. Both are about reaching settings
at all, which matters more than where any one of them lives. Each is one PR.

## 9. Step 3 as built

- **Server.** Revision 92 adds three nullable columns to `user`
  (`home_spotlight`, `home_spotlight_item`, `spoilers`); a null has never been
  chosen. A hidden built-in shelf is a `home_shelf` row with `builtin` set to
  its key (`new`, `rate`, `watchlist`, `unwatched:<library id>`), as ADR 0119
  planned; an Unwatched row names its library, so it goes when the library
  does. Pinned-shelf reads already skip `builtin` rows, so a hidden shelf
  never counts against the twelve. `GET`/`PUT /api/profile/home`.
- **Client.** `lib/homePrefs.ts` holds the query and the save. The spotlight
  and spoiler hooks keep their names and read the account, with the device's
  old value standing in until the account answers. `HomePrefsCarryOver`, in
  the signed-in shell, sends a device value only for a field the account has
  never set and then forgets the device copy; a failed save keeps it to try
  again.
- **Settings.** *Your account › Home* holds the spotlight, spoilers, a switch
  per built-in shelf and the pinned shelves (up, down, unpin). *This device ›
  Display & home* is *Display* again.
- **Home.** A shelf turned off is not drawn, and Watchlist and Rate what
  you've watched are not fetched. Unwatched shelves keep their library names
  while two exist, shown or not.

## 10. Step 4 as built

- **Server.** Revision 93 adds four nullable columns to `user`
  (`book_speed`, `book_sleep`, `comic_layout`, `comic_fit`); null is "never
  chosen". `GET`/`PUT /api/profile/reading`. The values are the ones the
  players offer and nothing else: a speed the book player's menu cannot show
  is refused.
- **Audiobooks.** A book with no speed of its own starts at the account's
  default; the default is not written to the book, so changing it moves every
  book nobody has set. The default sleep timer is set when a book starts and
  no timer is already counting.
- **Comics.** The reader learned two facing pages (the cover alone, then each
  pair as printed, one page on a screen taller than it is wide) and fitting to
  the width (up and down scroll; Space and Page Down read down the page and
  turn at its foot). Left to right only: reading direction is step 6. There is
  no switch inside the reader yet; the layout is chosen in Settings.
- **Connected accounts.** Connecting, disconnecting and earning while playing
  moved from the profile to *Your account › Connected accounts*. The profile
  keeps the points, unlocks and progress, with a Manage link, and sends
  somebody not yet connected to Settings.

## 11. Step 5 as built

ADR 0122 is the decision; this is what landed.

- **One server setting**, `metadata_language`, default `en-US`, chosen from a
  list the server serves (33 language-region tags). Per library is step 6.
  "Region" turned out to be the age-rating country, which already existed and
  is left where it was.
- **TMDB asks in it everywhere.** The language is part of the cache key, so
  an answer in one language is never served for another; English keeps the
  key it always had, so an upgrade keeps its cache. Where the chosen language
  has no translation, the English title or synopsis fills the gap.
- **English filenames keep matching.** A search in German returns "Der Pate"
  for `The Godfather (1972).mkv`. Every name a result has is scored: the
  translated title, the original-language title, and the English title from a
  second search made only when the language is not English.
- **Trailers** ask for the chosen language, then English, then untagged.
- **The refresh is offered, never started.** A new scope, `language`, takes
  the by-id path `settled` uses, widened to matched titles: nothing is
  searched or re-scored, match states are written back unchanged, and field
  locks hold. Titles in review are left out, since their id is a suggestion.
  After the language is saved, *Server › Metadata* shows each film and show
  library with its count and its own Refresh button, and a Not now.

## 12. Step 6 as built

ADR 0123 is the decision; schema 95.

- **Two nullable columns on `library`.** `metadata_language` (film and show
  libraries; null follows the server's) and `reading_direction` (comics; null
  is left to right). `PATCH /api/libraries/{id}` sets them, and an option a
  library cannot use is a 400 that changes nothing.
- **A series override**, `comic_series_direction`, keyed by library and
  series name (the comic's folder). Set by an admin from the reader's bar,
  through `PUT /api/items/{id}/series-direction`, so a client can only name
  a series that exists.
- **The server resolves the direction.** The pages answer carries
  `direction` and `direction_from` (series, library, default).
- **Right to left** turns the reader round: left arrow, left tap, swipe right
  and the left button go forward; a spread puts its earlier page on the right;
  the progress bar fills from the right. Space still goes forward.
- **The library's language rides the request context** into TMDB, for
  enrichment, Fix match, both by-id refreshes and trailers. The provider
  interface is unchanged.
- **The library editor** (Settings › Libraries › Edit) gains an Options
  section with whichever applies, and changing the language offers that
  library's refresh.

## 13. Step 7 as built

- **`GET /api/storage`**, admin only and read-only. One walk of the data
  folder counts every file once against its part (database, artwork,
  previews, optimized, backups, conversion scratch, subtitles, models, game
  saves, tools and downloads, logs, other), plus the optimized-copy folder on
  its own when it is on another drive, plus the drive's free space.
- **Never inside the request.** A GET answers with the last measurement and
  starts a new one in the background when it is missing or over ten minutes
  old; `?fresh=1` measures now. One at a time. On the machine it was built
  for, 50,000 files and 13.2 GB took 2.3 seconds.
- **"Other" names its five largest entries**, which is how it turned up two
  old database copies and two pre-revision-54 folders, 157 MB together, that
  nothing in LANcast ever removes.
- **Settings › Backup & storage** shows each part with its size, a neutral
  bar for its share, and whether it can be had again: artwork and previews
  are rebuilt, game saves are not. Nothing on it deletes.
