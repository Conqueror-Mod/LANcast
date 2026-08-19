# Season page — plan

A season currently renders as a wall of blank 2:3 tiles with a title under each,
and a single **Play all**. It is the least finished screen in the app, and the
reason is not that artwork is missing. It is that a season is being drawn with
the movie grid, and an episode is not a poster.

This is the same mistake the music arc already corrected one media type over —
[music-client-plan.md](music-client-plan.md) §3, *"An album is a track list, not
a poster grid"*. The season page is the last screen still assuming a leaf is
something with a spine.

## What already works

- **Episodes exist, ordered correctly.** `EpisodesOf` returns season, episode,
  then row id, and `NextEpisodeFor` uses the same order — so "next" and "the
  list" agree by construction (v0.7.0).
- **Per-episode play and queueing.** The container path already builds a queue
  of playable children; a row can play the season from that episode by handing
  over the same queue, exactly as a track row does.
- **Watched state and progress are recorded** per user in `playback_state`, and
  attached to items by `AttachProgress`. Nothing on this screen reads them.
- **The row pattern itself.** `TrackList` is a list of rows with numbers, a
  title block, a duration and a play affordance, wired through `useFocusable` so
  spatial navigation ([ADR 0004](adr/0004-spatial-focus-model.md)) covers it. A
  season list is that component's shape with a still on the left.

## 1. The stills are already being fetched, and thrown away

`tmdb.go` maps every episode's `still_path` onto the record:

```go
rec.Artwork = []meta.ArtRef{{Kind: meta.ArtThumb, URL: imageURL(e.StillPath)}}
```

Nothing downstream consumes `meta.ArtThumb`. The artwork cache stores posters
and backdrops; the thumb is dropped on the floor on every enrichment pass.

So the imagery for this screen already arrives, for free, from work the server
is doing anyway. **Generating scene thumbnails with ffmpeg is the fallback, not
the plan** — it costs a decode per episode, it picks its frame blind (a black
frame, a title card, the credits), and the result is worse than the one a human
chose. Worth keeping in mind for libraries with no provider match, and not
before.

Work: persist `ArtThumb` through the enrichment path into the existing
content-addressed cache, and serve it the way posters are served. The cache is
content-addressed already, so an episode still costs nothing new architecturally.

## 2. Structure: a list, not a grid

Each episode is one **wide row**:

```
┌────────────┐  4 · Love's Labours Lost in Space          22m · 1999-04-13
│   still    │  Leela sets out to rescue her old flame…          ★ 7.6
│  16:9      │  ▓▓▓▓▓▓▓▓░░░░░░░░░░░░  8m left
└────────────┘
```

- **Still on the left**, 16:9, at the size the provider gives (they are small —
  300px wide is typical, which is exactly right for a row and hopeless for a
  hero).
- **Number and title** on one line. The number leads because it is how anybody
  refers to an episode.
- **Runtime and air date** on the same line, right-aligned; rating if present.
- **Synopsis** on the next line, clamped to two lines. This is the thing the
  grid has no room for and the reason a season page exists at all.
- **A progress bar** only when there is progress, so an untouched season is not
  a wall of empty bars.

Rows go through `useFocusable` like every other interactive element rather than
inventing a second navigation idea.

Why a list rather than a 16:9 grid: a 26-episode season is a long scroll either
way, and only the list has room for a synopsis. The grid buys nothing back
except density nobody asked for.

## 3. The fallback is typographic, not a placeholder image

When there is no still — no provider match, an unenriched library, a show TMDB
has never heard of — the row does **not** render a grey rectangle or the show's
poster shrunk into a wide frame. It renders the episode number, large, in the
space the still would occupy.

That is a deliberate state rather than a degraded one. A missing image drawn as
a missing image reads as broken; a number drawn as a number reads as a design.
It also means the whole screen is shippable before any artwork work lands, and
that a library with no internet access looks finished rather than empty.

Gold stays out of it. The number is text, not a focus signal —
[design.md](design.md)'s rule holds: gold means *where you are* and nothing else.

## 4. What each row can do

- **Play** — plays that episode.
- **Play from here** — queues that episode and everything after it in the
  season. The queue already exists; this is the same list sliced.
- **Mark watched / unwatched** — the state is per user and already stored, and
  a season page is where somebody wants to correct it after watching something
  elsewhere.

The season header keeps **Play all** and gains **Continue**, matching the show
page so the two do not disagree about what a season offers.

## 5. Spoilers are a decision, not a default

A still and a synopsis for an episode you have not reached is a spoiler, and
this screen is where that lands hardest — the next unwatched episode is the one
you are most likely to be looking at.

Three defensible answers:

1. **Show everything.** Honest, simplest, and wrong for anybody watching a
   thriller.
2. **Hide the synopsis of unwatched episodes**, keep the still.
3. **Hide both**, replacing the still with the typographic state from §3, which
   already exists for the no-artwork case.

Proposed: **2 by default, with a setting for 3.** The still alone rarely gives a
plot away; a synopsis routinely does, because it is written as a summary rather
than a tease. Doing nothing here is itself a choice, and the wrong one to make
by accident.

## Data required

| Piece | Source | Status |
| --- | --- | --- |
| Episode still | TMDB `still_path` → `ArtThumb` | fetched, discarded — §1 |
| Synopsis | TMDB `overview` | stored |
| Runtime | probe `duration_ms` | stored |
| Air date | TMDB `air_date` → `released_at` | stored |
| Rating | TMDB `vote_average` | stored |
| Watched / progress | `playback_state` | stored, unread on this screen |

Only the first needs new plumbing. The rest is a rendering job over data the
server already has, which is why this screen can improve substantially in one
pass.

## Shipping order

1. **The row layout, with the typographic fallback.** No new data. The page
   stops looking unfinished immediately, and every subsequent step is additive.
2. **Watched state, progress and per-episode actions.** Reads what is already
   stored.
3. **Persist episode stills** through enrichment, and the rows fill in.
4. **The spoiler rule**, once there is something to hide.

Explicitly not in scope: extracting frames with ffmpeg, hover previews, and a
season-level hero image. The first is a fallback for libraries without a
provider and should be judged on its own; the other two are polish on a screen
that does not yet do its job.

## Open questions

- **Specials (season 0).** They sort first, which puts them before the pilot.
  Right for some shows, wrong for most. Worth a rule, and the show page's
  Continue already has the same question.
- **A season with one episode**, or a show with no seasons at all — the loose
  episodes `shapecheck` allows. The list handles it; the header wording may not.
- **Whether the season page survives at all** once the show page has Continue.
  It might reasonably become a filter on one long episode list rather than its
  own screen. Not proposed here, but worth knowing it is a question before
  investing in the layout twice.
