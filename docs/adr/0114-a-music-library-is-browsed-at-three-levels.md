# ADR 0114 — A music library is browsed at three levels, and its genres come from the files

Date: 2026-11-02 · Status: **accepted** · Related: [ADR 0024](0024-music-libraries.md), [ADR 0018](0018-api-contract-and-versioning.md)

## Context

A music library offered no genre filter and no year filter. Both were
asked for, and both looked like missing controls. They were missing data.

- **Genre.** `probe.Tags.Genre` was read from every file and thrown away.
  Nothing in a music library had a genre, so the facets had none to offer.
- **Year.** Albums and tracks have years: a track's from its tag, an album's
  as its earliest track's (`FillAlbumMetadata`). The browse grid shows the
  library's top level, which is artists, and the facets describe the top
  level, as they should, because a filter must never offer a value the grid
  cannot find. An artist has no year, so the year and decade filters hid
  themselves on every music library.

Faking an artist year to fill the gap would make the filter lie: "1994" on an
artist who has records in four decades is a value nobody can reason about.

## Decision

**A music library can be shown as Artists, Albums or Tracks.** The switch is
`?show=albums|tracks` in the URL, and Artists, the old grid, is the default.
At the Albums or Tracks level the grid lists that kind across the whole
library (`/api/items?kind=…`, which already lifted the top-level default), and
the facets describe the same kind (`/api/libraries/{id}/facets?kind=album|track`).
Each level has its own sorts: Year comes back on albums and tracks, and the
duration sorts appear on tracks. Neither is offered on artists, where they
would tie every row.

The facets' `kind` is a **closed set**, `album` or `track`. It is spelled into
SQL at seven places inside FILTER clauses and correlated subqueries, so it is
checked against the set and never passed through. Anything else is a 400.

Switching level keeps genre, search and unwatched, and drops year, decade, the
A–Z letter and any sort the new level does not have. A filter carried across
with no chip to show it would empty the grid with nothing on screen to say why.

**Genres come from the tags.** One tag can hold several. ID3v2.4 separates
them with NUL, and most taggers write `; `. A `/` is left alone: "Drum/Bass" is
one genre to the people who tag it that way. A tag's genre joins an existing
genre of the same name **in any case**: taggers disagree about case, and two
chips reading "Hip-Hop" and "hip-hop" would split one genre. An empty tag
leaves a genre alone, as an empty title does. An edited genre is locked
(`genres`, the field a film's genre edit already uses), and a rescan never
overwrites it.

**Albums and artists carry the union of what is under them,** re-derived on
each scan the way an album's year is (`RollUpMusicGenres`), so a retag reaches
the album on the next pass. An album or artist whose genres somebody edited is
locked and left alone. That is what lets the Artists view filter by genre too:
an artist is in Jazz if anything of theirs is.

## Consequences

- Schema **88** clears the tag-group keys of every track, so the next scan
  re-reads every file's tags and picks the genres up (the revision 86
  pattern). Until a scan runs, the genre filter has nothing to offer. It is
  one-way: an older server will not open the database afterwards.
- A track tile opens the track's page, as it does from search. A tracks view
  laid out as rows would read better at 9,000 tracks; it is not this decision,
  and the tile grid pages in as it scrolls.
- **Combined genres stay as tagged.** On the first real library (9,945
  tracks, 119 genres), many tags are lists joined by something other than `;`:
  `Indie / Alternative` on 836 tracks, `Alternative Rock / Indie Rock /
  Post-Grunge`, `Alternative\Rock`, `Modern Metal, Power Metal`, and a
  tagger's leftover `//folk`. Splitting on `/`, `\` and `,` was offered and
  declined (2026-11-02): the tag is the owner's statement, and a chip reading
  exactly what the file says is something they can fix in their tagger. Do not
  re-propose the split without a new reason.
- Genres are whatever the files say. Matching albums against a database
  (MusicBrainz) would give them an authority the tags do not have. That is a
  separate decision with its own ADR, and nothing here forecloses it.
