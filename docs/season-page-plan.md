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

## 1. The stills were already there — this section was wrong

Written as "fetched and thrown away", on the strength of a grep that missed the
place they are read. **Corrected against the running library:**

```
artwork kinds stored:  fanart 1819 · poster 8055 · thumb 993
Futurama S01E01 … S01E04:  thumb, all four, hashes present
```

The whole path already worked. `tmdb.go` maps `still_path` to `meta.ArtThumb`,
`storeArtwork` persists any kind it is handed, and both `ItemArtwork` and
`AttachArtwork` map `thumb` onto the item's artwork — which the API serialises
like every other image. Nothing needed building.

What was actually missing was two lines in the client: the `Artwork` type had no
`thumb` field, so nothing could read it, and the poster grid asked for
`artwork.poster` — which an episode does not have. **That is why the tiles were
blank.** Not missing data: a screen asking for the wrong image.

And one bug of this plan's own making, found the same way. The row's first
version put the artwork **hash** straight into `src`, where every other screen
passes it through `artworkURL`. A hash is truthy, so the row took the image
branch and rendered a broken image on all 993 episodes that had a still —
strictly worse than the number it was supposed to fall back to. Fixed with the
step-2 work.

**Generating scene thumbnails with ffmpeg remains the fallback, not the plan** —
it costs a decode per episode, picks its frame blind (a black frame, a title
card, the credits), and produces something worse than the frame a human chose.
For libraries with no provider match, and not before.

The lesson worth keeping: a grep across `internal/` for a constant found the
producer and not the consumers, and I wrote a section of a plan on it. Checking
the database took thirty seconds and disagreed.

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
| Episode still | TMDB `still_path` → `ArtThumb` | **stored and served all along** — §1 |
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
   Deliberately first even though §7 is unsettled: the row is the same row
   whether it sits on a season page or under a season selector, so building it
   commits to neither answer and none of the work is lost.
2. **Watched state, progress and per-episode actions.** Reads what is already
   stored.
3. ~~**Persist episode stills** through enrichment~~ — **not needed.** They were
   already stored and served; the client simply never read them, and then read
   them wrongly. See §1. What remains under this heading is only the ffmpeg
   fallback for libraries with no provider match, which is a separate decision
   rather than a step of this plan.
4. **The spoiler rule**, now that there is something to hide — §5. This is next.

Explicitly not in scope: extracting frames with ffmpeg, hover previews, and a
season-level hero image. The first is a fallback for libraries without a
provider and should be judged on its own; the other two are polish on a screen
that does not yet do its job.

## 6. Specials go last, and are never "next"

Season 0 sorts first today, which puts a Christmas special and a
behind-the-scenes reel in front of the pilot. **Specials go at the end**, after
every numbered season.

The corollary matters more than the ordering, and it is the half that is easy to
miss: **Continue must never land on a special.** They are not part of the line
somebody is working through, so a special sitting unwatched between two seasons
must not become the next episode, and finishing the last numbered episode of a
show must report it as finished rather than routing into the extras.

So there are two orderings, and they are worth naming as different rather than
discovering it later:

| Question | Specials |
| --- | --- |
| What is in this show, in order? | included, last |
| Play all / Randomize | included, last |
| What is **next** (Continue)? | **excluded** |
| Is the show finished? | judged on numbered seasons only |

In SQL that is `ORDER BY (season = 0), season, episode, id` for the listings and
a `season > 0` restriction inside `NextEpisodeFor`. A special is still playable
and still counts as watched when watched; it simply never volunteers itself.

Open: a show that is *only* specials — a one-off, a concert film filed as a show
— would leave Continue with nothing to offer. Probably: fall back to season 0
when there is no numbered season at all.

## 7. Whether a season is a screen

The season page exists because the data has seasons in it, not because anybody
decided a season needed a route. Now that the show page has Continue, the
question is live, and it is worth answering before this layout is built twice.

Five arrangements, with what each actually costs:

**A · Season pages, as now.** Show → season → episodes. Familiar, and it matches
the shape of the data. But a season page is a screen whose entire content is
"the episodes of one season" — a filter wearing a route — and it puts two clicks
between a show and the thing you want to watch.

**B · One episode list with a season selector.** The Netflix arrangement: the
show page holds one list and a control switches which season fills it. Seasons
stop being screens and become a filter, which is what they are. One click from
show to episode. The selector has to survive seventeen seasons without becoming
a wall, so it is a dropdown at that size rather than tabs.

**C · Continuous list, season headings.** Every episode in one scroll with
sticky headings. Lovely for a three-season show and unusable for a
seventeen-season one, where "take me to season 9" becomes a scroll rather than a
choice. Better as a mode than as the only arrangement.

**D · A shelf per season.** Horizontal rows, one per season, the way the home
screen already works. Reads well and browses badly: a synopsis has nowhere to
live, and a horizontal scroll inside a vertical one is the interaction people
misfire on most.

**E · B, with the season in the URL.** The selector filters, and the choice lives
in the query string exactly as the browse filters do — `?season=2`. Deep links
and bookmarks keep working, Back returns to the season you were in rather than
the first one, and the rule this project already follows holds: *every control
lives in the URL, so a filtered view is linkable and survives reload.*

**Proposed: E.** It is B's structure with this project's own rule about state
applied, and it removes a screen rather than redesigning one. Season rows stay
in the database — they are real, the scanner builds them, `parent_id` still
means what it meant — they simply stop having a page of their own.

Two things E has to get right to be worth doing:

- **The selector must be reachable by the focus model**
  ([ADR 0004](adr/0004-spatial-focus-model.md)). A dropdown is the control most
  likely to be built mouse-first and found unusable from a remote later. Tabs
  for a handful of seasons, a list for many, both focusable.
- **What "no season chosen" shows.** Defensible: the season holding the next
  unwatched episode, which is where somebody wants to be — the answer Continue
  already gives, applied to the list rather than to playback.

## Open questions

- **A season with one episode**, or a show with no seasons at all — the loose
  episodes `shapecheck` allows. The list handles it; the header wording may not.
- **A show whose only content is specials**, per §6.
- **Whether E's selector replaces the season route or merely bypasses it.**
  Keeping `/item/{seasonID}` working costs nothing and keeps old links alive;
  removing it is tidier. Worth deciding once rather than drifting into.
