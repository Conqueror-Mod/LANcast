# Settings: what is missing, and how to reorganise it

Status: proposal, 2026-10-12, revised the same day with Chris's decisions and
his own list (§6, §7). Nothing here is built yet. It answers two
questions Chris asked: what settings LANcast lacks, and how Settings should be
reorganised to hold them. He named Network, Scheduled Tasks, Transcode,
Troubleshooting, Languages and Dashboard as categories he expects.

## 1. What Settings holds today

There are 21 panes in two groups.

**Server (administrators):** General · Libraries · Pictures · Retro games ·
Metadata · Playback · Users · Other servers · Live TV · Add-ons · Updates ·
Backup · Activity · Logs

**This device:** Account · This app · PC Games · Display · Keyboard · About

Every server setting in `config.Settings` has a control except one,
`audit_retention_days`. What is missing is not hidden toggles. It is whole
areas that have no settings at all, plus several panes that each hold three
unrelated things.

### Where things are hard to find today

| Problem | Example |
|---|---|
| **One pane, unrelated things** | *Playback* holds the hardware encoder, the conversion limit and the quality ceiling (transcoding), next to the watched threshold and Continue Watching rules (watch state). |
| | *Libraries* holds the scan schedule and the artwork cache limit, which are a schedule and storage. |
| | *General* holds the public certificate (networking) beside version and licence (about). |
| **Personal things under "This device"** | *Account* is per person, not per device. It holds the display name, password, passkeys, API keys, language preferences and sharing. |
| **Player preferences only inside the player** | Subtitle size, font, colour and position, the audio pass (night mode, dialogue, EQ), autoplay, crossfade and per-device quality exist only in the player's own panel. They cannot be set before pressing Play. |
| **Diagnostics in three places** | Logs (server log, debug logging, maintenance actions), Activity, and codec-failure resets under Playback. |
| **Schedules invisible** | The guide refresh (12 h), maintenance (daily), update check (24 h) and the detection and identification workers run on fixed timers. Only the library scan schedule shows anywhere. |

## 2. Proposed structure

Three groups. That keeps the scope of every setting obvious: the server's (it
changes things for everyone), your account's (it follows you to any device),
or this device's (it stays on this screen).

### Server (administrators)

| Pane | Holds | Comes from |
|---|---|---|
| **Dashboard** *(new, the admin's landing pane)* | Health at a glance: version and update status; who is watching and what is being converted; running scans and workers; storage use; recent warnings; how the server is reachable. | New, built mostly from existing routes (`/api/transcodes`, `/api/activity`, `/api/health`). |
| **General** | Server name *(new)*, identity fingerprints, updates. | General, Updates |
| **Network** *(new)* | Listen port, LAN access status, certificates (self-signed fingerprint, a custom certificate, the public certificate), local networks. | General (public certificate), config-file-only settings |
| **Users & access** | Accounts, roles and content ceilings; sign-in policy; everyone's active sessions. | Users |
| **Libraries** | The libraries and per-library options (§3), deletion and trash rules. | Libraries |
| **Metadata** | Provider keys, request rate, certification country, enrichment, NFO writing, metadata language and region *(new)*. | Metadata |
| **Transcoding** *(new, split from Playback)* | Encoder, conversions at once, quality ceiling, media tools, re-read media files, scratch location and size, HDR tone-mapping, audio and subtitle conversion policy. | Playback, Logs |
| **Playback & progress** | Watched threshold, Continue Watching rules, intro and credit detection, the still-watching prompt. | Playback |
| **Live TV** · **Pictures** · **Retro games** · **Other servers** · **Add-ons** | Unchanged. | — |
| **Scheduled tasks** *(new)* | Every background job in one table: schedule, last run, next run, Run now. | Libraries (scan schedule), fixed timers |
| **Backup & storage** | Backup, a backup schedule *(new)*, artwork cache limit, transcode scratch, database size. | Backup, Libraries, Logs |
| **Troubleshooting** *(new)* | Server log, debug logging, the audit log and its retention, codec-failure resets, clear caches, reset settings, a diagnostics bundle *(new)*. | Logs, Activity, Playback |

*Activity* folds into the Dashboard. It is the same information, and the
Dashboard is where an administrator looks first.

### Your account (everyone)

| Pane | Holds |
|---|---|
| **Profile** | Display name. |
| **Security** | Password, passkeys, API keys, **your sessions, with sign out** *(new)*. |
| **Languages & subtitles** | Preferred audio and subtitle language and the subtitle mode, which exist today under Account. Plus subtitle *style* (size, font, colour, position), which is per device today and could follow the person. |
| **Privacy** | Sharing what you watch, visibility to other servers, clearing watch history. |

### This device

| Pane | Holds |
|---|---|
| **Player** *(new as a pane)* | Everything in the player's panel, settable before pressing Play: quality on this device, autoplay, crossfade, the audio pass, audio output, subtitle style if it stays per device, the playback codec list. |
| **Display & home** | Homepage spotlight, spoilers. |
| **Keyboard** · **This app** · **PC Games** | Unchanged. |
| **About** | Unchanged. |

**Languages, as a category.** Chris named it. The proposal splits it by
scope:
- **Metadata language** is a server decision, in Metadata.
- **Your audio and subtitle languages** are an account decision, in Languages
  & subtitles.

One pane mixing both is how a person ends up changing everyone's metadata
while setting their own subtitles. **The interface language** is not a
setting LANcast can offer yet: the client has no translations. That would be
its own project.

## 3. Settings that are missing

Priority: **P1** is a gap a person hits; **P2** is clearly worth having;
**P3** is nice. *Server work* means new storage or routes. *ADR* means it
changes a rule in CLAUDE.md or a decision record and needs one.

### Network

| Setting | Priority | Notes |
|---|---|---|
| Listen port | P1 | Today a command-line flag baked into the service at install. Changing it means rewriting the service's command line and restarting. **Server work.** The loopback rule (no password means 127.0.0.1 only) must survive unchanged. |
| LAN access, shown as a status | P1 | "Reachable on your network at https://…:8080", or why not (no password, a restart owed). Read-only: the gate is not a toggle, by rule. |
| Custom TLS certificate | P2 | `tls_cert_file` and `tls_key_file` exist in config with no control. |
| Local networks | P2 | Which addresses count as home, for future rules (for example, quality caps for remote viewers). **Server work.** |
| Reachability check | P3 | Only from the LAN side. Asking an outside service to probe the server would be phone-home. |

### Users & access

| Setting | Priority | Notes |
|---|---|---|
| Active sessions, per account and server-wide, with sign out | P1 | Sessions are server-side already; nothing lists them. **Server work** (list and revoke routes). |
| Sign-in policy: login backoff, session lifetime, passkey-only accounts | P2 | Backoff (#838) and the 30-day lifetime are fixed in code. |
| Per-user library access | P2 | Today every account sees every library (ceilings only). Plex and Jellyfin both offer this. **Server work, ADR**: it is a new access rule on every listing. |
| Per-user transcoding limits (quality cap, may convert) | P3 | **Server work.** |

### Libraries (per library)

| Setting | Priority | Notes |
|---|---|---|
| Metadata language and region per library | P2 | A Japanese anime library beside an English one. |
| Exclude patterns (sample files, extras folders) | P2 | Filename rules belong in `internal/media` (CLAUDE.md), so this is a list that rule reads. |
| Per-library scan schedule and NFO writing | P3 | Global today. |
| Intro and credit detection per library | P3 | Off for music videos or stand-up. |
| Show on Home / include in search | P3 | |

### Metadata

| Setting | Priority | Notes |
|---|---|---|
| Metadata language and region | P1 | TMDB answers in a language and region; LANcast does not choose. Titles and overviews follow it. |
| Artwork language preference | P2 | Posters in your language first, then textless. |
| Provider order and per-provider on/off | P3 | |

### Transcoding

| Setting | Priority | Notes |
|---|---|---|
| Transcode scratch location and size cap | P1 | Scratch sits in the data directory. A large 4K conversion on a small system drive is the realistic failure. |
| HDR tone-mapping on or off, and its method | P2 | Tone-mapping on the CPU was the 4K stutter. Choosing it beats discovering it. |
| Audio: allow surround to pass through, or downmix to stereo | P2 | |
| Subtitles: burn in image subtitles, or refuse | P2 | |
| Throttle conversions ahead of the viewer | P3 | Saves CPU on long films. |
| Remote quality cap | P3 | Depends on *Local networks*. |

### Playback & progress

| Setting | Priority | Notes |
|---|---|---|
| Still-watching prompt: after how many episodes, or off | P2 | Fixed at three today. |
| Skip intro and credits automatically | P2 | Today a button only, deliberately (ADR 0054). An opt-in, off by default, would not change that default. |
| Continue Watching includes collections' next films | P3 | Built on in 0.9.80; a switch for anyone who does not want it. |

### Scheduled tasks

One table, read-only at first, with **Run now** for each. The jobs are:
- library scan;
- metadata refresh;
- intro and credit detection;
- face grouping;
- ROM identification;
- guide refresh;
- maintenance (prune and vacuum);
- update check;
- backup.

Editable schedules come second. Only the scan has one today. P1 for the
table, P2 for editing.

### Backup & storage

| Setting | Priority | Notes |
|---|---|---|
| Backup schedule and how many to keep | P1 | Backups are manual only. A database holding every correction by hand deserves a schedule. **Server work.** |
| Storage overview | P2 | Database, artwork cache, scratch, backups, with sizes. |

### Troubleshooting

| Setting | Priority | Notes |
|---|---|---|
| Audit log viewer and retention | P2 | `audit_retention_days` exists with no control. |
| Diagnostics bundle | P2 | One download: the logs, the settings with secrets removed, versions, the probe of a chosen file. It goes to the person, not anywhere else. |
| Self-tests | P3 | Media tools present, encoder proven, write access to each library. |

### Your account

| Setting | Priority | Notes |
|---|---|---|
| Your sessions, with sign out | P1 | The same work as the server-wide list. |
| Subtitle style follows you | P3 | Per device today. |

### This device

| Setting | Priority | Notes |
|---|---|---|
| Player preferences in Settings | P1 | Exist today, only inside the player. |

### Dashboard

New. It needs no new settings, only a view: who is watching (`/api/transcodes`
and presence), running work (`/api/activity`), version and update, storage,
recent warnings from the log, reachability. P1, because it is the pane an
administrator should land on.

## 4. Order of work

Each step is one or more small PRs and leaves Settings usable.

1. **Restructure, moving nothing new.**
   - Three groups; Dashboard first for admins.
   - Split Playback into Transcoding and Playback & progress.
   - Move Account out of This device into Your account.
   - Fold Activity into a first Dashboard; fold Logs into Troubleshooting.
   - Old `?pane=` links keep working through redirects. The tray's "Update
     libraries" uses one.
2. **The player's preferences in Settings** (This device › Player). Client
   only.
3. **Scheduled tasks**, read-only with Run now.
4. **Sessions** (yours and everyone's) and **backup schedule**. Server work.
5. **Metadata language and region**, and **transcode scratch location and
   cap**. Server work.
6. **Network pane**: port, LAN status, custom certificate. Server work,
   carefully, around the loopback rule.
7. P2 items, then P3, as Chris chooses.

## 5. Decisions for Chris

1. **Three groups** (Server · Your account · This device), or keep two with
   Account under the server group? The proposal is three.
2. **Dashboard as the admin's landing pane**, replacing General as the first
   thing seen?
3. **Subtitle style:** follow the person (account) or stay per device? TVs and
   phones often want different sizes, which argues for per device with an
   account default.
4. **Per-user library access** is the largest item here (an access rule on
   every listing, an ADR). In scope, or parked?
5. **Automatic intro skip:** offer it, off by default, or keep the button as
   the only way, as ADR 0054 decided?
6. Anything from Chris's own list that is not here.

## 6. Decisions taken (2026-10-12)

1. **Groups:** at least three; four is fine, two is too few. The revision in
   §7 uses four: Status, Server, Your account, This device.
2. **Dashboard is the first pane an administrator sees.**
3. **Subtitle style is per account.** It follows the person to every device,
   alongside their audio and subtitle languages.
4. **Per-user library access:** Chris wants it explained before deciding.
   Still open.
5. **Intro skip happens automatically by default** (confirmed). That amends ADR 0054
   decision 3, which made skipping a button only; it needs its own ADR.
   - **What stays from 0054:** the skip happens only where the player would
     have offered the button, so only for markers that pass the detector's
     trust rules.
   - **What changes:** a per-account switch turns it off.
   - **What it costs:** the measured risk 0054 records, a wrong skip landing
     mid-scene, becomes a jump the viewer did not ask for, so the threshold
     deserves a second look before shipping.
   - Credits keep the button; skipping credits automatically would end
     films early.

## 7. Chris's list, mapped

| Chris's item | Where it lands | New? |
|---|---|---|
| **Authorized Devices** | Your account › Security: every device signed in as you, by name, browser or app, and last seen, each with **Sign out**. Server-wide under Users & access for an administrator. | New. Server work: sessions record a device name and last-seen time (nullable columns). |
| **Watchlist** | A feature first: a per-account list of things to watch later, with a Home shelf. Its settings sit in Display & home (show the shelf) and Privacy (whether friends' servers may see it). | New feature; needs an ADR (it is a new kind of per-user list, like favourites, ADR 0062). |
| **Scheduled tasks** | Status › Scheduled tasks. | In §2. |
| **DLNA** | Server › DLNA: on or off, which libraries, as which account, the name TVs see. | New feature; needs an ADR (below). |
| **Transcode** | Server › Transcoding. | In §2. |
| **Network** | Server › Network: this machine and the LAN. | In §2. |
| **Troubleshooting** | Status › Troubleshooting. | In §2. |
| **Privacy** | Your account › Privacy, plus a server-side section: audit-log retention, and what friends' servers are told. | In §2, widened. |
| **Quality** | Server › Quality: the quality ceiling for everyone, a separate ceiling for viewers away from home, per-account caps. This device › Player keeps the quality *this* device asks for. | Split out of Transcoding. Remote and per-account caps are server work. |
| **Debug** | Status › Debug: debug logging, ffmpeg's own logs, the statistics overlay default, the desktop app's developer tools. Kept apart from Troubleshooting, which is for fixing a problem rather than looking inside. | Mostly existing settings, moved. |
| **Player** | This device › Player. | In §2. |
| **Conversions** | **Optimized versions** (confirmed): convert a film once, ahead of time, into a copy a phone or a slow link can play directly, instead of converting live every time. Status › Conversions lists them and what is converting now. Server › Transcoding holds the rules: which presets, where copies live, how much space they may use. | New feature; needs an ADR (below). |
| **Remote Access** | Server › Remote access, separate from Network: the public name and certificate, port 443, whether the server answers from outside, what friends need. Network stays about this machine and the LAN. | Public certificate moves here from General; the rest is new. |

### DLNA, briefly

DLNA lets a TV, console or speaker browse and play the library with no
LANcast app at all. Two things make it a decision rather than a toggle.

- **It has no sign-in.** Anything on the network may browse. LANcast's rule
  is that every request is someone's. So DLNA would answer **as one chosen
  account**, with that account's ceiling and visible libraries, and only on
  the LAN. It would be off by default.
- **It announces the server.** Devices discover it by broadcasting on the
  network. That stays on the LAN and is not phone-home, but it is the first
  thing LANcast would do unasked.

### Optimized versions, briefly

- **What it is:** a stored, pre-converted copy of a title (for example, 1080p
  H.264 at 8 Mbit/s, or 720p for phones). The player's decision chooses it
  when it plays directly where the original would need converting live.
- **Why it needs an ADR:** it is a second file per title the server owns,
  so it touches several existing rules.
  - Storage, and its own size cap.
  - The direct-play, remux or transcode decision (`internal/probe`), which
    gains a third source to choose from.
  - Scans: a copy is not a library file, and must never be mistaken for one.
  - Deletion: a copy goes when its original does, and "Remove" says so.
- **How copies get made:** chosen per title, per collection or per library,
  then converted by the scheduled-tasks runner with the same encoder the live
  path proved at startup. Per CLAUDE.md, this is verified as the installed
  service, not from a shell.

### The four groups, revised

- **Status:** Dashboard · Scheduled tasks · Conversions · Troubleshooting ·
  Debug
- **Server:**
  - General · Network · Remote access · Users & access
  - Libraries · Metadata · Transcoding · Quality · Playback & progress
  - Live TV · Pictures · Retro games · Other servers · DLNA · Add-ons
  - Backup & storage
- **Your account:** Profile · Security (with Authorized devices) · Languages
  & subtitles (with subtitle style) · Privacy · Watchlist
- **This device:** Player · Display & home · Keyboard · This app · PC Games ·
  About

Activity folds into the Dashboard, and Logs into Troubleshooting and Debug, as
in §2.

