# ADR 0030 — Playlists, and what an `.m3u` on disk means

Date: 2026-08-11 · Status: accepted

## Context

LANcast has no playlists. It has queues — an album, a season, an artist's
discography, a whole music library — but a queue is built at the moment you
press play and is gone when you stop. There is nothing you can name, keep, add
to next week, and press again.

Two separate things are being asked for, and conflating them is the main way
this decision can go wrong:

1. **A playlist as a thing LANcast owns.** You make it, name it, reorder it, and
   it lives in the database.
2. **An `.m3u` file on disk.** A text file of paths, produced by every music
   player of the last twenty-five years, sitting in the library next to the
   media. The test library has them; so does anyone's.

They meet at an obvious question — should LANcast read the `.m3u` files it
finds? — and that question hides a second one that matters more: *if it does,
which copy is the truth when they disagree?*

## Decision

### A playlist is an item, not a new table

`kind = 'playlist'` on `media_item`, with membership in the existing
`item_collection` join. This is the third media concept to land without a new
table ([ADR 0002](0002-one-wide-media-item-table.md)), after music
([ADR 0024](0024-music-libraries.md)) and pictures
([ADR 0028](0028-pictures-library.md)).

The fit is close to exact. `item_collection` is already
`(item_id, collection_id, ord)` — an *ordered many-to-many*, which is precisely
what a playlist is and precisely why collections needed a side table in the
first place ([ADR 0017](0017-collections-and-multi-part-works.md)). A playlist
gets the browse, artwork, progress and API plumbing every other item has, for
free.

### One exception, and it needs a schema change

**`item_collection` cannot hold the same track twice.** Its primary key is
`(item_id, collection_id)`, which is correct for a collection — a film belongs
to a franchise once — and wrong for a playlist, where putting the same song in
twice is legitimate and common. A "best of" with a reprise, a DJ set, a workout
list with one track at the start and the end: all ordinary, all currently
impossible to represent.

So membership needs a key that admits repeats:

```sql
-- schema revision 17
CREATE TABLE playlist_entry (
    playlist_id INTEGER NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    item_id     INTEGER NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    ord         INTEGER NOT NULL,
    PRIMARY KEY (playlist_id, ord)
);
CREATE INDEX idx_playlist_entry_item ON playlist_entry(item_id);
```

Keyed on position rather than on membership, which is the difference between the
two concepts stated in SQL. `ON DELETE CASCADE` on `item_id` means deleting a
track removes it from every playlist it is in, which is the honest behaviour:
the alternative is a playlist that silently plays eleven of its twelve entries.

This is a new table, and [ADR 0002](0002-one-wide-media-item-table.md)'s claim
is about *kinds* not needing new tables, not about no table ever existing —
`item_collection`, `credit` and `item_genre` are the same long-tail case. The
claim survives: a playlist is still a `media_item`.

### An `.m3u` on disk is an import, not a mirror

**The database is the source of truth. A scanned `.m3u` seeds a playlist once
and is not watched afterwards.**

The alternative — treat the file as truth and write back on every edit — was
rejected for three reasons:

- **It cannot round-trip.** An `.m3u` is a list of paths. A LANcast playlist
  will grow things a path cannot express: who made it, when, whether it is
  shared. Writing back would silently discard them, or invent a dialect of
  `.m3u` that only LANcast reads.
- **It writes to the media directory.** The project already treats that as a
  serious act: `write_nfo` is a setting, defaulted off in development, precisely
  because writing next to someone's media is not something to do casually.
- **It is the NFO argument again**, and [ADR 0009](0009-nfo-round-trip-safety.md)
  already settled the shape of it: read willingly, write deliberately.

Re-scanning does not re-import. A playlist imported once and then edited in
LANcast must not have the edit undone by a scan — that is the *locked fields*
rule (CLAUDE.md) applied to membership, and the same failure it exists to
prevent: a rescan reconciling *files* must never re-litigate *decisions*.

### A missing path is recorded, not dropped

`.m3u` files reference tracks that may not be in the library — a different rip,
a file since moved, a path from another machine.

An entry that cannot be resolved is **counted and reported, not silently
skipped**. The import says "imported 47 of 52 tracks; 5 not found in this
library", and the five are listed. Silently importing 47 produces a playlist
that looks complete and is not, and nobody ever finds out which five are
missing.

This is the same argument as the kind-mismatch diagnostic in v0.6.3: a scan that
imported nothing and reported "0 items · scanned" read as an empty folder. What
was ignored, and why, is the useful half of the message.

## Consequences

**Good — playlists are browsable the day they exist.** A `playlist` kind gets
the grid, the detail page, artwork, `?queue=`, Play all and the API's
`parent_id`/membership conventions without new screens.

**Good — the ordering problem is already solved.** `ord` on the join, and
`sort=track` on the API, are how an album already comes back in the order it
plays.

**Good — no new provider, no network.** Unlike almost everything else on the
roadmap, this needs nothing external. It is entirely local, which makes it a
good candidate for a session that must not depend on a rate-limited API.

**Cost — a new table and a migration**, hence this ADR. Schema revision 17.

**Cost — two write paths into membership** (the importer and the user), which is
the seam where a rescan could clobber an edit. The permanent test that protects
locked fields should be extended to cover playlist membership, or this will be
rediscovered the hard way.

**Cost — the m3u dialect zoo.** Extended M3U (`#EXTINF`), plain M3U, absolute
paths, relative paths, Windows separators in a file read on Linux, latin-1 bytes
in a file that claims to be UTF-8. The parser must be pure and table-driven for
exactly this reason — every one of those is a fixture, and none of them needs a
disk or a database to test. Same discipline as `probe.ParseJSON`.

**Cost — `.m3u8` is ambiguous.** It is both "UTF-8 M3U" and the extension HLS
uses for its playlists, and LANcast *serves* HLS ([ADR 0013](0013-transcode-pipeline.md)).
A scanner that imports every `.m3u8` it finds would try to import a transcode
playlist out of its own cache directory. Import must be restricted to library
roots and must reject anything containing `#EXT-X-` tags, which is HLS's
unambiguous marker.

## What this does not decide

Whether playlists are per-user or shared. Every account currently sees the same
library, and playback progress is already keyed by user
([ADR 0006](0006-playback-state-keyed-by-user.md)) — so the machinery for "yours"
exists, but "my playlists versus the server's playlists" is a product question
about what LANcast is for, and it wants deciding awake. **Until it is decided,
playlists are server-wide**, which is the smaller claim and the one that can be
narrowed later without breaking a shared playlist that someone came to rely on.

Smart playlists (rules rather than members) are out of scope and want their own
decision; a rule-backed playlist is not an ordered list of ids and does not fit
this table.

## Revisit when

Playlists need to be per-user, smart playlists are wanted, or someone asks
LANcast to write `.m3u` back to disk — at which point the round-trip argument
above is the thing to re-read first.
