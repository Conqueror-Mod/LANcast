# ADR 0115 — Albums are identified by MusicBrainz, and the tags still win

Date: 2026-11-03 · Status: **accepted** · Related: [ADR 0024](0024-music-libraries.md), [ADR 0007](0007-provider-and-localsource-split.md), [ADR 0008](0008-field-level-locking.md), [ADR 0114](0114-a-music-library-is-browsed-at-three-levels.md)

## Context

[ADR 0024](0024-music-libraries.md) deferred any remote music provider: "tags
cover the overwhelming majority of a personal library". The real library shows
that it was right about the tags. A random sample of 120 tracks:

- artist and title on 120;
- album artist on 118;
- album on 117;
- a date on 111;
- a genre on 103.

What a film or a show has and an album does not is an **identity**. A film is a
TMDB id. That id is what lets the Fix match screen correct one, lets a
collection list the films you don't own, and lets a rating source join on
it. An album is only the strings in its files. Two releases of the same
record with slightly different album tags are two albums. And nothing outside
LANcast can be told which record you mean, which is what scrobbling to
Last.fm, an artist's discography and "albums you don't have" all need.

Three measurements on the real library (2,900 albums, 9,945 tracks) shape the design.

1. **No file carries a MusicBrainz id.** Four hundred random tracks were
   probed (391 MP3, 5 FLAC, 3 M4A, 1 Ogg): none has a `MUSICBRAINZ_*` or
   AcoustID tag. The probe does see custom frames ("release type", "ripping
   tool"), so this is a real absence, not a blind reader. The files came from
   Windows Media Player and Google Play, not a MusicBrainz tagger. **Matching
   is a search**, not an id lookup.
2. **1,798 of the 2,900 albums hold one track.** These are purchases and singles
   filed under the album they came from. Matching on album title and album
   artist still identifies the record. Matching on track count would reject
   nearly all of them.
3. **The gaps are small.**
   - 295 albums have no year;
   - 940 have no genre;
   - 88 have no artwork.

   Filling them is a benefit, not the reason.

MusicBrainz is the source because it is the open catalogue the rest of the
music world keys on (Last.fm, ListenBrainz, Picard, Lidarr). It needs no key.
Its core data (artists, release groups, releases, dates) is
**CC0**. Its rules for clients are:

- **one request per second per IP**, on average;
- a **User-Agent** that names the application and a contact.

Above that rate every request gets a 503 until the rate drops.

## Decision

### What is matched: albums, and artists through them

- **An album is matched to a MusicBrainz release group**, the record, not a
  release (one pressing). The editions of one album differ in track lists and
  barcodes, and nothing in a one-track "album" can tell them apart. A release
  group is what a person means by "that album".
- **An artist is identified through its albums, not by searching its name.**
  "Nirvana" is several artists, and a name search cannot tell which. Once its
  albums are matched, the artist takes the MusicBrainz artist most of its
  matched albums credit. With no matched album, it stays unmatched, which is
  honest.
- **Tracks are not matched.** That would be 9,945 searches at one a second,
  about 2¾ hours, for an identity nothing uses yet. Recording ids belong to the
  scrobbling decision, which needs them.

### How it is matched

- Search `release-group` by **album title and album artist** (`releasegroup:`
  and `artist:` in MusicBrainz's query syntax), and score each candidate
  with the existing machinery (`meta.Rank`, ADR 0007):
  - title through the one normalizer (`media.SortTitle`, built on `clean`);
  - the artist credit through the same normalizer;
  - year against the release group's first release date;
  - **an artist that disagrees caps the score below review.** "Greatest
    Hits" by the wrong artist is the music version of matching an obscure
    film to a blockbuster.
- The thresholds are the ones films use (0.85 applied silently, 0.55 applied
  and flagged). **Before any UI is built, the matcher is run over a sample of
  the real library and the split is reported.** If the review band holds
  hundreds of albums, the scoring is wrong, and the Review screen must not
  find that out.
- `album` and `artist` leave `unmatchableKinds`, the list whose comment already
  says "the day a music provider exists this one line changes". `track` stays
  on it.
- **One worker, one limiter, one request per second**, shared with nothing
  else, backing off on a 503. The first pass is roughly two requests per
  album (a search, then a fetch of the chosen group with its artist credit,
  type and genres) plus one per artist. On this library that is a little over
  two hours, in the background. Later passes only see new albums.
- The User-Agent is `LANcast/<version> ( https://github.com/Conqueror-Mod/LANcast )`.
  It is the project's address, never the server owner's, so no person is
  identified to MusicBrainz.

### What a match changes: identity first, then only gaps

**The tags still win.** In the merge (ADR 0008) a file's tags are a local
source, and local sources outrank providers for every field. MusicBrainz
cannot rename an album or move it to another artist, and it does not override
a genre or a year a file states. That is the same position ADR 0114 took on
genres: the tag is the owner's statement.

A match adds:

- **the release group id and artist id**, kept as the item's provider and
  external id, as a film keeps its TMDB id;
- **a release type**: Album, EP, Single, Compilation, Live or Soundtrack, from
  MusicBrainz's primary and secondary types. This is new information no tag
  here carries. It becomes a filter on the Albums level ("show me albums, not
  singles"). That one is a nullable column (schema 89), and nothing else in the
  schema changes;
- **a year** where the album has none, from the first release date;
- **genres** where the album has none. Genres on a release group are
  MusicBrainz users' tag votes, which are **CC BY-NC-SA, not CC0**. LANcast
  does not redistribute them: each server fetches them for its own owner's
  library. The album page credits MusicBrainz wherever they are shown;
- **cover art** where the album has none, from the Cover Art Archive (front
  image of the release group). Asked for only for those albums: 88 here.

Locked fields are never touched, and a `locked` match is never re-scored, as
for every other kind. **Fix match works on albums and artists:** the existing
`/candidates` and `/match` endpoints learn the two kinds. The dialog searches
MusicBrainz by album title and artist, as it searches TMDB for a film.

### Switched on like films are

Music matching is on by default, as TMDB is for films. What it sends:

- album titles and artist names to `musicbrainz.org`;
- release group ids to `coverartarchive.org`.

[docs/privacy.md](../privacy.md) gains both under "What is on unless you turn
it off", in the same commit as the code. A library setting turns it off.
**Existing music libraries start matching on upgrade**, and genres from
MusicBrainz fill albums that have none, with credit shown. Both were approved
by the owner on 2026-11-03. Off by default would have been an identity
nobody discovers.

## The thing that is easy to get wrong

**Letting a confident wrong match overwrite a right tag.** A film library
tolerates a wrong match because the file name is weak evidence. Here the
tags are strong evidence and MusicBrainz is the guess. Every rule above follows
from that: identity is added beside the tags, fields are filled only where
empty, and an artist disagreement sinks the score instead of being outvoted by
the title.

The second trap is **the rate limit as a correctness problem.** A 503 is not
"no match". An album that got a 503 is retried later, never written down as
unmatched. Otherwise a busy hour at MusicBrainz quietly marks half a library
"no match found".

## Consequences

- **Built in steps**, each its own PR:
  1. the provider (`internal/meta/musicbrainz`): client, limiter, User-Agent,
     pure parsing tested against recorded responses, and a measurement run
     over a sample of the real library;
  2. enrichment of albums and artists, and the release type column (schema 89);
  3. Fix match and the release-type filter in the client;
  4. Cover Art Archive for albums with no art.
- The first pass is long (about two hours here), and the activity readout
  must say so instead of looking stuck.
- **A commercial licensee** (COMMERCIAL.md) who offers a hosted service
  fetches CC BY-NC-SA genres on its users' behalf. That is between the
  licensee and MusicBrainz, and COMMERCIAL.md should say so.
- With identity in place, follow-on work needs no new matching:
  - **an artist's albums you don't have**, as a collection lists films you
    don't own (ADR 0093);
  - **scrobbling**, which needs recording ids, matched then;
  - artist images by MusicBrainz id, from a source still to be chosen.

## Not in this ADR

- Matching tracks or recordings, and AcoustID fingerprinting. Nothing here is
  untagged, so fingerprints would answer a question this library does not
  ask.
- Rewriting file tags. LANcast does not write into music files, and matching
  does not change that.
- Artist biographies and images. MusicBrainz has neither. TheAudioDB has both
  and needs a key; that is its own decision.
