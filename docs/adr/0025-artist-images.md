# ADR 0025 — Artist images from TheAudioDB

Date: 2026-08-03 · Status: accepted

## Context

Album artwork is solved and solved locally: the picture embedded in a track, or
a `cover.jpg` beside it ([ADR 0024](0024-music-libraries.md)). Measured against
a real 398-album library, that covers 369 of them with no network at all.

Artists are the one container with nothing to source an image from. An album has
a picture in its files; an artist has neither a file nor a directory of its own.
The obvious local fix — read `artist.jpg` from the artist folder, the way Kodi
does — was tried against the real library and does not work. The images that sit
in an artist folder turn out to be a media player's per-album art cache
(`AlbumArtSmall.jpg`, `AlbumArt_{GUID}_Large.jpg`, one pair per album) rather
than a photograph of anyone. Reading those would put an album sleeve on the
artist tile *while looking like it had found something better*, which is the
worse of the two failures.

So artists currently **borrow** a poster from their most-substantial album, at
read time, flagged `inherited` in the API. That is honest and it is a
placeholder by construction. Every artist in the library wears one of its own
album sleeves.

Doing better requires a remote source. ADR 0024 deferred this with "when
untagged rips prove to be a real problem, that ADR gets written then". This is
that ADR, though it lands somewhere other than where 0024 guessed.

### What the sources actually offer

Checked rather than assumed, because this area has a lot of folklore:

- **Last.fm** is the obvious name and it is dead for this purpose. `artist.getInfo`
  returns a placeholder star image at every size; Navidrome carries a commit
  titled "ignore artist placeholder image".
- **fanart.tv** is the quality answer — purpose-built artist thumbs, backgrounds
  and logos. It keys on **MusicBrainz IDs**, so it is not one integration but
  two, and MusicBrainz is capped at one request per second with a required
  contactable User-Agent.
- **TheAudioDB** looks up **by artist name**, needing no MusicBrainz at all. The
  free tier is the public key `123` at 30 requests per minute.
- **Deezer and Spotify** carry good images and terms that assert ownership of
  all content including artist photographs, granting developers "no right upon
  the Content". Caching those into a permanent content-addressed store is not a
  defensible reading of that.
- **MusicBrainz** alone has no images.

## Decision

**TheAudioDB, keyed by artist name, as a narrow image source — opt-in, off by
default, native before it is ever a plugin.**

### Why the cheap one over the good one

fanart.tv almost certainly returns better and more consistent artwork. It also
costs a MusicBrainz integration, an identity-resolution step, a one-request-per-
second crawl, and a project API key — to improve a **placeholder that already
exists and is not embarrassing**. Artists today wear their own album's cover,
which is wrong in a defensible way rather than blank.

TheAudioDB removes the MBID dependency entirely, which is the single largest
piece of work in the alternative. If its coverage disappoints against a real
library, fanart.tv is the upgrade path and MusicBrainz is the thing to build
then — measured, rather than guessed at now.

### The shape: a narrow source, not a Provider

```go
type ArtistImageSource interface {
    ID() string
    ArtistImages(ctx context.Context, name string) ([]ArtRef, error)
}
```

Deliberately not a `Provider`, for the reason `RatingSource` is not one
([ADR 0019](0019-external-ratings.md)): it cannot search for identity, and
forcing `Search`/`Fetch` on it would mean faking a confidence-scored candidate
from a service that has no opinion about what an artist is. The identity — the
album-artist string the tags already agreed on — is resolved before this is ever
called.

Images land in the existing content-addressed cache and are recorded as `poster`
for the tile and `fanart` for the detail background. **No new artwork kinds, and
no new fallback logic**: the moment an artist owns a poster, the borrowed-album
fallback stops applying by itself, because it only ever fired for artists with
nothing of their own.

### Opt-in has to be explicit, and that is new

Every remote source LANcast has is gated by a secret the user had to go and get.
No TMDB key, no enrichment; no OMDb key, no ratings; and in each case "nothing
leaves the machine" is a consequence of the key being absent rather than a
separate check.

**TheAudioDB breaks that pattern, because its free key is the public literal
`123`.** A key that ships in the binary is not a gate. Baking it in and running
would make this the first thing in LANcast that phones home without the operator
having done anything, and the no-phone-home principle is not negotiable without
an ADR — which is what this paragraph is.

So the gate is an explicit setting, default off:

```go
ArtistImages bool `json:"artist_images"`
```

Off, the pass never runs and no packet leaves. On, the operator has said yes to
this specific service by name. A key field is offered alongside it for anyone
with a Patreon key who wants the higher limits, and empty means the public tier.

### Rate, cache, and cost

30 requests per minute against 206 artists is about seven minutes, once,
in the background — the same shape as the cover-art worker, which does 398
albums in eleven seconds. Responses go through the existing `provider_cache`
table, so a rescan costs nothing and a re-run after a failure re-asks only for
what is missing.

### Native first

OMDb was built native ([ADR 0019](0019-external-ratings.md)) and only later
reimplemented as a plugin to prove the plugin contract
([ADR 0020](0020-plugin-isolation-boundary.md)). The same order applies here,
for the same reason: a plugin extension point designed against no working
implementation is an abstraction guessing at its own requirements.

That said, this is the strongest candidate yet for the **first new plugin kind**.
`internal/plugin/discover.go` wires only `rating_source` today, and the roadmap
says widening waits for a plugin that needs it. An image source is a second
narrow, identity-resolved, network-bound source with exactly the shape the first
one had — which is the evidence a second kind is worth defining, once this one
works.

### Not in this ADR

- **MusicBrainz, MBIDs, and fanart.tv.** The upgrade path, not the first step.
- **Artist biographies.** TheAudioDB returns them. Text is a different merge
  problem to images — it collides with locked fields and with NFO — and it
  deserves its own decision rather than riding along.
- **Album art from a remote source.** Local already covers 369 of 398. The 29
  that found nothing are mostly folders shared between several albums, which a
  remote source keyed on album title would not obviously fix.
- **Lyrics, ReplayGain, gapless.** Still deferred, as in ADR 0024.

## Consequences

**Good — one integration, no identity resolution.** Name in, image URLs out.
The album-artist string is already the thing the grouping agreed on, so there is
no second notion of who an artist is to keep consistent with the first.

**Good — it degrades to what exists now.** Every failure mode — off, rate
limited, no match, service down — lands on the borrowed album cover rather than
on a blank tile. Nothing regresses if this never runs.

**Good — nothing to clean up if it is removed.** The inherited fallback is
computed at read time and was built to be superseded, so deleting this feature
returns the grid to exactly its current state with no migration.

**Cost — the first explicit network opt-in.** Until now "did you supply a key"
answered "may we call out". A boolean that means the same thing is a second
mechanism for one rule, and a second mechanism is a second thing to get wrong.
It has to be as visible in Settings as the keys are.

**Cost — licensing is unstated, and silence is not permission.** TheAudioDB's
free API documentation sets out no licensing or attribution terms for its
imagery at all. That is materially weaker than the Creative Commons position of
Wikidata/Commons, and weaker than a stated restriction, because an unstated
position can be asserted later. Accepted here because the images are cached
locally on a self-hosted server, never redistributed, and behind an off-by-
default switch — and because the whole feature is removable without trace. It is
recorded as a known risk rather than resolved.

**Cost — a name is a weaker key than an id.** See below.

## The thing that is easy to get wrong

**Treating an artist name as an identity.** It is not one. Several distinct
bands share a name, and a name-keyed lookup cannot tell them apart — this is
precisely what an MBID exists to solve, and precisely what was traded away to
avoid the MusicBrainz integration.

The consequence is that a **wrong artist photograph is a real possible outcome**,
and it is worse than no photograph, for the same reason five albums sharing one
`Folder.jpg` was worse than five blank tiles: a confidently wrong image reads as
a broken server, where a missing one reads as missing. So a result is taken only
on an exact normalized name match, using the single normalizer in
`internal/media/parse.go` and no fuzzy fallback. A near miss is not a match, and
the borrowed album cover is a perfectly good place to land.

**Forgetting the gate, because nothing forces you to remember it.** Every other
source fails closed: no key, no calls, and the code cannot accidentally omit
that check because there is nothing to call with. Here the code will work
perfectly if the opt-in check is left out — it will simply phone home for every
user on first scan. That failure is silent, which is the class of bug this
project keeps writing down.

## Revisit if

- **Coverage disappoints against a real library.** The measurement to take is
  the same one taken for album art: how many of 206 artists get an image, and
  how many are visibly the wrong person. Poor numbers make fanart.tv and the
  MusicBrainz integration worth their cost.
- **Name collisions turn out to be common** rather than theoretical, which is
  the same trigger by a different route.
- **TheAudioDB states licensing terms**, in either direction. Explicit
  permission removes the recorded risk; an explicit restriction removes the
  provider.
- **A second image source wants this shape**, which is when
  `artist_image_source` should become the first new plugin kind rather than a
  native-only interface.
