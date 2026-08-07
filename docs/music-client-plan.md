# Music client UI — plan

Roadmap step 9.3, the last of the music arc. [ADR 0024](adr/0024-music-libraries.md)
shipped music server-side in v0.5.0 and scoped the client deliberately small:
**album view and a track list that plays.** This plan says what that costs in
the client, and what has to be fixed in the server first.

Everything below is a variation on show → season → episode, which is already
built. The work is not inventing a third container depth — it is noticing the
handful of places where the video assumption is baked in and would quietly
produce something wrong rather than something broken.

## What already works

- The API returns artists, albums and tracks as `media_item` rows related by
  `parent_id`. `GET /api/items?parent_id=` walks the hierarchy.
- Album artwork is in the content-addressed cache; artists borrow their
  most-substantial album's cover, flagged `inherited` ([ADR 0025](adr/0025-artist-images.md)).
- A track is decided, streamed, resumed and progress-tracked by the same
  endpoints a film uses. Common audio formats direct-play.
- `isContainer` in `lib/kind.ts` falls through to `child_count > 0`, so artists
  and albums already register as containers without being named.

So a music library *renders* today. It renders wrong in four specific ways, and
one of them is a server bug.

## Server: tracks come back in the wrong order

**Confirmed by running it, not by reading it.** `ListItems` orders by
`sort_title, season, episode`. Episodes survive that because the scanner sets an
episode's `sort_title` to its *series*, so every episode of a show ties and the
order falls through to `season, episode`. A track keeps its **own** title as its
sort title — `internal/scan/scanner.go` does this on purpose — so tracks never
tie, and the fallthrough never happens.

A three-track album seeded with tracks 1/2/3 titled Zebra/Mango/Apple comes back
in exactly reverse order:

```
position 0: track 3 "Apple"
position 1: track 2 "Mango"
position 2: track 1 "Zebra"
```

The comment at `internal/scan/scanner.go:524` asserts the opposite — "A track is
the opposite: it sorts within its album by number, so it keeps its own sort
title." That is the intent; the `ORDER BY` does not implement it. Nothing caught
it because nothing has ever listed an album's tracks. This is the same shape as
the v0.5.0 lesson already in the roadmap: **the decision was named in one place
and had to be honoured in another**, and the untouched place was the one that
produced the value.

**Fix: an explicit `sort=track`** — `ORDER BY season, episode, sort_title` — and
`useChildren` passes it when the parent is an album.

Rejected alternatives, for the record:

- *Reordering the default to `season, episode, sort_title` globally.* Movies are
  unaffected (their season and episode are NULL and tie), but a cross-show
  episode listing would interleave every show's season 1 before any season 2.
  A silent regression in the video path to fix an audio one.
- *Zero-padding a track's `sort_title` with its number.* Fixes ordering by
  corrupting the field that title search and title sort both read, and requires
  a rescan to take effect on existing libraries.

`sort` is an existing dimension with an existing test surface; adding a value is
additive and safe under [ADR 0018](adr/0018-api-contract-and-versioning.md).
`docs/api.md` is updated in the same commit — that is a contract change.

## Client: four places the video assumption shows

### 1. Square art in a 2:3 frame

`PosterTile.css` hardcodes `aspect-ratio: 2 / 3`. Album covers are square, and
an artist wearing a borrowed album cover is square too. In a 2:3 frame,
`object-fit: cover` crops a third of every record sleeve away — the tile still
*looks* fine, which is why this needs stating before it ships.

A `shape` derived from `kind` (album and artist → square), applied as a CSS
modifier on `.poster-tile__art`. The cache needs no change: variants are resized
by width and preserve aspect, so a square source already returns square.

### 2. `lib/kind.ts` does not know the music nouns

`CONTAINER_KINDS` omits `artist` and `album` — they work only by the
`child_count` fallback, which means an album with no tracks yet reads as a
playable leaf. `childLabel` and `containerNoun` have no music cases, so an
artist tile reads "4 items" instead of "4 albums" and an album's section header
reads "Contents" instead of "Tracks". Add `artist`/`album` to the kind set and
`album`/`track` to both label functions.

### 3. An album is a track list, not a poster grid

`Detail.tsx` renders every container's children as a grid of `PosterTile`s. For
a season of episodes that is right. For an album it is wrong twice: twelve
identical copies of the same cover, and no track numbers or durations.

A new `components/TrackList.tsx` — numbered rows, title, per-track artist when
it differs from the album artist (which is exactly the compilation case ADR 0024
grouped by album artist to preserve), duration, and a play affordance per row.
`Detail.tsx` branches on `item.kind === "album"` and renders it instead of the
grid. An artist keeps the grid, now square.

Rows go through `useFocusable` like every other interactive element, so the
spatial focus model ([ADR 0004](adr/0004-spatial-focus-model.md)) covers a track
list without a second navigation idea.

**Play all already exists** and needs nothing: the container path builds a
`?queue=` of playable children and the player advances through it. A track row
plays the album from that track by passing the same queue.

### 4. Three actions that do not apply to music

On the detail page, gated off for music kinds with a reason, not silently:

- **Fix match.** There is no music provider (ADR 0024 defers MusicBrainz), so
  Fix match on an album searches TMDB for a record and offers films. Hidden for
  `artist`, `album` and `track` — the same reasoning that already hides it for a
  season: identity here is structural, assembled from tags, not a match waiting
  to be corrected.
- **Remove**, on an artist or an album. Those rows are invented by the scanner
  and swept when empty by `DeleteEmptyMusicContainers`. Removing one is the
  collection case, which is already not offered. A **track** keeps Remove — it
  is a real file.
- **Trailer** disappears on its own (no trailer, no button). No work.

## Player: an audio mode

`Player.tsx` is a video surface with video chrome. A track plays through it
correctly today and shows a black rectangle, a fullscreen button and a subtitle
menu.

Keep the single `<video>` element. It plays audio natively, and every piece of
machinery around it — decision fetch, resume, progress persistence, queue
advance, volume memory, transcode seeking — is format-agnostic and would have to
be duplicated to gain nothing. **One media element, one set of handlers**; the
divergence is presentational and stays in the chrome.

When `item.kind === "track"`: the video surface is replaced by the album cover
over the nebula field, the title line becomes title / artist / album, and the
fullscreen and subtitle controls are dropped. Scrubber, transport, volume and
the queue stay exactly as they are.

### The mini-player is in scope after all

Originally this plan deferred a persistent mini-player, citing ADR 0024's "a
music player UI beyond the minimum". **Chris has asked for it as part of the
player work**, and the reason is sound enough to overrule the deferral: leaving
the player screen currently *stops the music*. That is fine for a film, which
you watch and finish, and wrong for a record, which you put on and then go and
do something else — browsing the library while listening is the ordinary case,
not an advanced one.

So: **going back from the player leaves a mini-player docked bottom-right**,
Plex's behaviour. Cover art, title and artist, play/pause, and a way back into
the full screen. Playback continues across the navigation.

This is the one part of the music work that pushes on the existing
architecture rather than following it, and it is worth naming before it is
built. The player owns a `<video>` element inside a route. A mini-player that
survives leaving that route means the element can no longer live there — it
has to be hoisted above the router, or the media element unmounts and the audio
stops. That is a real change to where playback state lives, and it will touch
video as much as music.

Two consequences to decide when building it, not now:

- **Does a film get one too?** The mechanism is identical and the answer is
  probably yes eventually, but a docked video thumbnail is a different design
  question from a docked record sleeve.
- **Does this deserve an ADR?** Hoisting the media element out of the route is
  a structural decision of the kind [ADR 0004](adr/0004-spatial-focus-model.md)
  covers for focus. If it turns out to reshape how the client holds playback
  state, it should be written down rather than discovered later in a diff.

Still **not** in scope: shuffle, repeat, gapless, playlists, lyrics.

## Browse configuration

`libraryConfig.ts` gains a `MUSIC` entry: search placeholder, and sorts of
Title / Year / Recently added. **Rating is dropped** — there is no music rating
source, so offering the sort promises an ordering the data cannot produce.

The facet row degrades correctly without work: facets return only values
actually present, so genre and content rating render empty for a tag-only music
library and the chips do not appear. Worth knowing rather than debugging later.

## Shipping order

Four PRs, each independently reviewable and each leaving the tree working.

| | Change | Why this order |
|---|---|---|
| 1 | `sort=track` + `docs/api.md` + store test | A confirmed bug, server-side, no client dependency. Everything after it would otherwise be built against wrong data. |
| 2 | `kind.ts` nouns, square tiles, `libraryConfig` | Makes the artist and album grids read correctly using screens that already exist. Visible on its own. |
| 3 | `TrackList` + album detail + action gating | The album view proper. Depends on 1 for order and 2 for the tiles above it. |
| 4 | Player audio mode + docked mini-player | Last because a track already plays. The audio chrome is presentation and easy to judge once there is a track list to reach it from; the mini-player is not — it moves the media element out of the route, so it is the one piece here that changes shared structure. Worth splitting into two commits, and possibly two PRs. |

## Before claiming done

`go test ./...` and `go build ./...`, plus `npm run build` in `web/` with the
committed `internal/web/dist` updated in the same commit.

Then the part tests cannot do: **run it against `TEST MUSIC LIBRARY`** and play
a record end to end. The roadmap's own recurring lesson is that reasoning about
this code predicts and only running it reports — and every failure mode in this
plan is a quiet one. A cropped sleeve, an album in alphabetical order and a
subtitle button on a song all render without erroring. Specifically worth
looking at: a **compilation** (per-track artists, one album artist) and a
**multi-disc release** (disc ordering ahead of track ordering), because those
are the two cases the ordering fix and the track list are each most likely to
get subtly wrong.
