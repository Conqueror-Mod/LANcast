# ADR 0010 — Shows and seasons are `media_item` rows

Date: 2026-07-22 · Status: accepted

## Context

In schema revision 1 an episode carries a `series` text column. That is enough
to group episodes under a heading, and not enough for anything M2 needs: a show
has its own poster, fanart, overview, rating, cast, first-air date, and — per
[ADR 0005](0005-theme-music-sourcing.md) — its own theme music. A string cannot
hold any of that.

Shows need to be real entities. The obvious way to make them real is a `series`
table, with `season` alongside it. That directly contradicts
[ADR 0002](0002-one-wide-media-item-table.md), which exists to keep the media
taxonomy out of the schema so that library types can eventually be
plugin-defined.

So this is a test of whether ADR 0002 was a good decision or merely a
convenient one.

## Decision

A show is a `media_item` with `kind = 'show'`, whose `path` is the show
directory and whose file-specific columns are null. A season is the same with
`kind = 'season'`. Episodes reference their parent:

```sql
ALTER TABLE media_item ADD COLUMN parent_id INTEGER REFERENCES media_item(id);
```

The hierarchy lives in data, not in table structure.

This requires `container`, `size_bytes`, and `mtime` to become nullable — folded
into revision 1 rather than migrated, since M1 has not shipped.

## Consequences

**Good.** Everything built for items works for shows for free, with no
per-type branching: artwork ([the pipeline](../metadata.md)), field-level locking
([ADR 0008](0008-field-level-locking.md)), match state and confidence, theme
music, and the `GET /api/items/{id}` shape. A show is not a special case
anywhere in the codebase. Under a separate `series` table, every one of those
features would need a parallel implementation — and each parallel
implementation is a place for the two to drift.

**Good.** ADR 0002 pays off here rather than straining. Adding "album → track"
or "photo album → photo" later needs no new tables at all: new `kind` values and
`parent_id`. That is precisely the extensibility the wide table was chosen for.

**Good.** One recursive relationship instead of a fixed three-level hierarchy.
Nothing in the schema asserts that media is exactly show → season → episode, so
a media type with a different depth is expressible.

**Cost.** File-specific columns become nullable, weakening a real constraint —
nothing at the schema level now stops a movie row having a null `size_bytes`.
Mitigated by all writes going through typed `store` methods, per `CLAUDE.md`.
This is the same trade ADR 0002 already accepted, extended.

**Cost.** Queries must filter by `kind` to avoid returning shows as if they were
playable files. `GET /api/items` defaults to playable kinds unless a caller asks
otherwise. Getting this wrong shows folders in a poster grid, so it needs a test.

**Cost.** `parent_id` is a self-referencing FK, which makes deletion order and
recursive queries slightly more delicate. `ON DELETE CASCADE` handles removing a
show and its episodes together; depth is bounded in practice.

## Revisit if

A media type appears whose hierarchy genuinely cannot be expressed as parent and
child rows. The response would be a side table for the exception, not a return
to per-type tables.
