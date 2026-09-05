# Navidrome comparison study

Date: 2026-09-03 · Navidrome at ~15k stars, GPL-3.0, Go + pure-Go SQLite,
React/Material-UI web client, single binary, no external services.

This is a study, not a plan. Nothing here is decided. Where a difference is a
decision LANcast already made on purpose, it is recorded as such rather than as
a gap.

## What Navidrome actually is

A music server, and only a music server. It is a much closer relative than
Immich: **same language, same database, same single-binary posture, same
"self-hosted, no cloud" framing**. ADR 0001 and Navidrome arrived at the same
stack independently, which is mild evidence that ADR 0001 was right.

Because the platform choices match, the divergences are informative in a way
Immich's were not. Almost every difference below is a *product* decision rather
than a consequence of the runtime.

The defining one: **Navidrome's real product is Subsonic API compatibility.**
It ships a web client, but the reason people run it is that every existing
Subsonic client — Symfonium, Amperfy, substreamer, DSub, play:Sub, Feishin —
works against it on day one. It also now ships a Jellyfin-compatible endpoint
(`Jellyfin.Enabled`), which is the same bet placed a second time.

---

## Where LANcast is doing it right

**No phone-home is a real position, and theirs isn't.** Navidrome ships
`EnableInsightsCollector: true` and carries a `GATrackingID` option. Anonymous
telemetry on by default, opt-out. That is an ordinary choice for an OSS project
and it is precisely the one LANcast's four principles refuse. Worth stating
plainly because it is the cleanest example of a principle costing nothing and
buying something.

**Field-level locking has no counterpart there.** Navidrome is
tags-are-the-only-truth: the answer to "this album's year is wrong" is "fix the
tag and rescan". There is no editable, protected field. ADR 0008 and the
locked-fields integration test are strictly more forgiving, and the cost —
carrying `item_lock` — is small.

**Deny-by-default plugin capabilities.** Both use wazero. Navidrome layers
Extism on top and exposes a wide host surface: HTTP, task queues, scheduling, a
KV store, blob storage, filesystem access, library metadata, user info,
scrobble history, and **calls back into its own Subsonic API**. LANcast's ABI
grants two things (allowlisted HTTP hosts, named secrets) and supports one kind
(`rating_source`). Theirs is more useful; ours is more defensible, and ADR 0020
said which of those it was optimising for.

**An unsecured server binds loopback only.** Navidrome defaults to
`Address: 0.0.0.0`, `Port: 4533`, and relies on first-run admin creation. Our
gate is stricter and is a structural guarantee rather than a setup step.

**Configuration restraint.** Navidrome's config struct runs to roughly 150
fields, ~35 of them `Dev*` flags that are load-bearing in shipped builds
(`DevExternalScanner`, `DevScannerThreads`, `DevSelectiveWatcher`,
`DevShowArtistPage`). That is a real maintenance surface and a real support
surface. LANcast's settings are small and in the UI.

**The scan-cost problem is already solved to parity.** v0.8.54 took an
unchanged 9,054-track library from 105s to 0.5s. Navidrome's scanner skips GC,
artwork and stats behind the same atomic "nothing changed" flag. Same answer,
arrived at independently.

---

## Where Navidrome is ahead — things worth taking

### 1. A read-only Subsonic API shim

This is the finding. LANcast's music client is one React screen that has to be
built, polished and maintained alone. A Subsonic-compatible read surface over
the music library — `ping`, `getMusicFolders`, `getIndexes`, `getArtists`,
`getAlbum`, `getSong`, `search3`, `getCoverArt`, `stream`, `getPlaylists`,
`scrobble` — would hand LANcast an entire third-party mobile and desktop client
ecosystem for a bounded amount of work, on top of a data model that already
exists.

It also fits the principles: it is another client contract over the same server
truth, which is what ADR 0018 already says the HTTP API is. The care needed is
in identity — Subsonic ids are strings, ours are integers, and its auth is a
token in a query string, which sits badly beside the `SameSite=Strict` +
`Origin` rules in the security doc. That is an ADR's worth of thinking, not a
blocker.

### 2. The music data model is thinner than the rest of LANcast

`probe.Tags` reads eight fields: Title, Artist, Album, AlbumArtist, Genre,
Track, Disc, Year. Navidrome reads a mapping file and models **14 participant
roles** — composer, conductor, lyricist, arranger, producer, director, engineer,
mixer, remixer, DJ-mixer, performer (with subroles) — plus multi-value
`ARTISTS`/`ALBUMARTISTS`, sort tags, MusicBrainz IDs, the compilation flag,
grouping, mood, and disc subtitles.

The consequences of the gap are concrete:

- **Classical is unusable.** No composer means no way to browse by the name
  that actually identifies the work.
- **"The Beatles" sorts under T** for artists. `IgnoredArticles` and sort tags
  are how Navidrome fixes it; `media.SortTitle` already exists for titles and
  is the obvious place, per the one-normalizer rule.
- **A featured artist is unfindable.** ADR 0024's per-track `artist` holds the
  display string; without a multi-value split nothing can be found under the
  guest.
- **The compilation flag is not read at all**, so `dropBucketAlbums` and the
  album-artist grouping are doing by heuristic what a tag states outright.

Widening `probe.Tags` is a small change with a large surface behind it. Note
Navidrome's separator parsing (`" / "`, `" feat. "`, `"; "`) as the fallback
when the plural tags are absent, and its `Scanner.ArtistSplitExceptions` list
for the names that contain a separator legitimately.

### 3. Persistent IDs — a moved file loses its history

`media_item.path` is `UNIQUE` and is the identity. Nothing re-identifies a file
by content. So a track moved or renamed **outside** LANcast is marked missing at
the old path and inserted fresh at the new one, and its playback position, watch
count, rating and playlist membership stay attached to the dead row.
(ADR 0041's rehoming is LANcast moving the file itself, which is the other
case.)

Navidrome's answer is a **persistent ID**: a configurable hash spec
(`PID.Track`, `PID.Album`) over tags with folder/album-artist fallbacks, so
identity survives a move. Its scanner has a whole phase for it — phase 2
"processes missing files, checking for moves", including across libraries.

A cheaper version exists: when the scan finds a new path and a missing row with
the same size and mtime in the same library, that is a move — adopt the row
rather than insert. Worth confirming with an actual move against a real library
before anything is designed, since this is a claim about behaviour and the
project's own rule is to measure first.

### 4. ReplayGain

`EnableReplayGain: true` by default. LANcast has nothing. This is the difference
between a shuffle across a ripped-CD library and a shuffle across a
loudness-war library being listenable at one volume. The tags are already in the
files; it is a tag read plus a gain applied in the audio element.

### 5. Smart playlists

Navidrome's are `.nsp` JSON criteria files — field/operator/value rules with
sort and limit, refreshed on a delay (`SmartPlaylistRefreshDelay`). This sits
unusually well with ADR 0030: a smart playlist has no human-edited membership,
so the `members` lock question never arises, and the importer's "an `.m3u` seeds
a playlist and is not the playlist" rule has an exact analogue.
`docs/playlists-page-plan.md` already gestures at this; Navidrome has a shipped
format to copy from.

### 6. Declared artwork priority chains

`CoverArtPriority: "cover.*, folder.*, front.*, embedded, external"`, plus
`ArtistArtPriority`, `DiscArtPriority`, `LyricsPriority`. LANcast's rules live
in `internal/coverart` and `internal/artistart` as code. Making the chain a
declared, ordered string is cheap, makes the rule legible in one line of
documentation, and gives users an override for the folder convention they
already use. ADR 0025 spent real effort on *which* image; this is how to say the
answer out loud.

### 7. A library watcher

Navidrome watches the filesystem and imports new files without a scan being
asked for (`Scanner.WatcherWait`, `DevSelectiveWatcher`). The v0.8.54 bug was
reported as *"tracks loaded from elsewhere ... do not appear for some time"* —
the some time was the scan. Making the scan fast fixed the symptom; a watcher
removes the wait entirely. Both Navidrome's and Immich's docs warn that watchers
do not work reliably on network drives, which is exactly the case LANcast's
multi-root libraries invite, so this needs the caveat designed in rather than
discovered.

### 8. Jukebox mode

Server-side playback to an audio device attached to the server, driven by mpv
(`Jukebox.Devices`, `MPVCmdTemplate`, `Jukebox.AdminOnly: true`). LANcast's
server is frequently the machine near the speakers, and ADR 0043/0048 already
fetch media tools on first run. The interesting part is that it is a *control*
surface rather than a streaming one, which no LANcast client currently is.

### 9. Backup retention, while the backup work is open

Navidrome has `Backup.Path`, `Backup.Schedule` and `Backup.Count` — scheduled
backups with a retention count. ADR 0058 and the `backup-api` branch just built
take/restore/list/delete. Schedule plus retention is the obvious next increment
and is cheaper to design now than to retrofit onto a shipped `/api/backups`.

### 10. Smaller items, listed without argument

- **Index groups and ignored articles** — `A B C ... X-Z(XYZ) [Unknown]([)` and
  `The El La Los Las Le Les Os As O A`. The A-Z jump bar for a large library.
- **Natural sorting** (`EnableNaturalSorting`) — "Track 2" before "Track 10".
- **Per-user and per-player transcoding profiles.** Ours is per-request plus the
  ADR 0047 host cap; theirs remembers what a given player asked for.
- **Favourites separate from star ratings** (`EnableFavourites`,
  `EnableStarRating`). The Immich study reached the same finding independently;
  two studies agreeing is worth something.
- **Scrobbling to Last.fm / ListenBrainz** as a plugin capability.
  User-initiated outbound traffic, which is a different thing from telemetry,
  but it still needs an ADR to say so.
- **`MetadataAgent` and `Lyrics` plugin kinds.** Concrete second and third kinds
  for the M4 plugin surface, which currently has one.
- **`SonicSimilarity`** — acoustic similarity via a plugin (AudioMuse-AI). The
  right shape for a feature like this: outside the core, behind a capability.
- **Externalized auth** via a trusted reverse-proxy header
  (`ExtAuth.UserHeader: Remote-User`, with a trusted-sources allowlist). The
  security doc already contemplates a reverse proxy; this is the SSO story.
- **Prometheus metrics and an `inspect` endpoint** that returns exactly what the
  scanner read from a file. The latter is a debugging tool LANcast would have
  wanted during the v0.8.54 hunt.
- **Internet radio stations** as a first-class model — cheap, and adjacent to
  live TV's channel model without reopening it.
- **`Scanner.PurgeMissing`** as a stated policy (`never` by default) rather than
  a trash screen. Ours marks missing and has trash; theirs names the policy.
- **Lyrics** — already in the backlog. Their priority list
  (`.ttml,.yaml,.elrc,.lrc,.srt,.txt,embedded`) and sidecar-first ordering match
  what the roadmap already proposes, which is a useful confirmation.

---

## Things Navidrome does that we should not take

**Telemetry on by default.** Stated above; it is the one place the two projects'
principles actually conflict.

**Tags as the only truth.** It makes the scanner simple and makes a mistagged
library permanently wrong. Locking is the better answer and we already have it.

**A `Dev*` config surface in shipped builds.** Thirty-odd flags that gate real
behaviour, documented as development options. Either a flag is supported or it
should not be reachable.

**Extism as the plugin layer.** It buys PDKs for Go, Rust, Python and
TypeScript — which is genuinely the "SDK, not just an ABI" finding from the
Immich study — at the cost of a dependency between us and wazero, and a host
surface far wider than ADR 0020 wants. Take the SDK lesson; leave the layer.

**Becoming a Subsonic server in identity.** The shim is a compatibility surface
over LANcast's model. Adopting Subsonic's model — its ids, its auth, its
folder-versus-tag duality — would be the tail wagging the dog.

---

## What I would look at first

1. **Move a track on disk and rescan**, against a copy of a real library, and
   see what happens to its rating and playback position. Cheapest test here and
   the only finding that might be a live bug rather than a missing feature.
2. **Widen `probe.Tags`** — sort tags, MusicBrainz IDs, the compilation flag,
   multi-value artists, composer. Small change; most of a real music experience
   sits behind it.
3. **An ADR for a read-only Subsonic surface.** The largest return per line in
   this document, and the decision is mostly about identity and auth, both of
   which can be settled on paper.
4. **ReplayGain**, because it is nearly free and immediately audible.
5. **Backup schedule and retention** while ADR 0058's work is still open.
