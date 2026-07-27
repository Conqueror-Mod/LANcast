# ADR 0017 — Collections and multi-part works

Date: 2026-07-26 · Status: accepted

## Context

The library contains works that do not fit "movie" or "episode of a show"
cleanly. The roadmap collects four motivating cases:

- **Anne of Green Gables / Anne of Avonlea** — separate films, each watchable on
  its own, that continue one story.
- **Baahubali** — one story deliberately released as two theatrical films.
- **1940s Batman / Superman serials** — a single film released as ordered
  chapters.
- **Storm of the Century** — a Stephen King miniseries: one closed story told in
  several ordered parts.

Left unmodelled, these scatter: two `Anne` movies sit unrelated in a grid, and a
serial's twelve chapters look like twelve unrelated features. The taxonomy is
deliberately open ([ADR 0002](0002-one-wide-media-item-table.md)); this is where
that openness has to earn its keep.

The trap is treating the four as one problem and reaching for one mechanism.
They split along a single axis:

> **Are the pieces independent works, or parts of one work?**

An `Anne` film is a complete, top-level, browsable movie that *also* belongs to a
continuing series — and could belong to more than one grouping (a franchise box
set and a themed collection). A Baahubali half, a serial chapter, and a
miniseries part are not independent: the *work* is the whole, and the pieces are
ordered segments of it that no one browses in isolation.

Those are two different relationships — **membership** (many-to-many, members
stay independent) and **containment** (one-to-many, parts belong to a parent —
the relationship ADRs [0002](0002-one-wide-media-item-table.md) and
[0010](0010-shows-as-media-items.md) already reserved their escape hatches for).

## Decision

Model the two relationships with two different mechanisms, each the one the
prior ADRs predicted.

### Multi-part works → containment via `parent_id`

Reuse the show → season → episode machinery generalized. A parent `media_item`
holds the work's canonical identity — title, artwork, overview, rating, match
state, locks, theme music — exactly as a show does under
[ADR 0010](0010-shows-as-media-items.md). The parent **is the work**; the parts
are lightweight ordered children carrying a file and an order, not a second
metadata identity to keep consistent.

This needs no new table. It adds `kind` values, which are data:

- `part` — an ordered piece of a single work (a Baahubali half; a miniseries
  part). Ordered by the existing `season`/`episode` columns.
- `chapter` — an ordered piece of a theatrical serial. Distinguished from `part`
  only so a client can label it correctly ("Chapter 3" vs "Part 3"); the
  hierarchy is identical.

A closed, finite, single story gets a distinct top-level kind rather than being
forced into `show`:

- `serial` — a miniseries or chaptered serial: one bounded story, meant to be
  played through as a whole. Its parts hang off `parent_id` like a show's
  episodes, but the kind tells browse and playback that "play the whole thing"
  is the natural action, where a `show` is open-ended and browsed by season.

Baahubali is a `movie` whose two `part` children play in order. Parts are
excluded from `GET /api/items`' default playable-kinds filter the same way
seasons are, and surface through the parent's detail page.

### Collections → membership via a side table

A collection is not containment, so it does not use `parent_id`. It is the
long-tail case [ADR 0002](0002-one-wide-media-item-table.md) explicitly reserved
a side table for.

A collection is a `media_item` with `kind = 'collection'` — it gets its own
poster, overview, and artwork, because providers (TMDB `belongs_to_collection`)
supply them. Membership is a join table:

```sql
CREATE TABLE item_collection (
    item_id       INTEGER NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    collection_id INTEGER NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    ord           INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (item_id, collection_id)
);
```

Members remain independent, top-level, browsable items. A movie can belong to
several collections. The collection is an additional lens over items that exist
in their own right — which is precisely why `parent_id`, a single-parent
column, cannot express it and a join table can.

## Consequences

**Good.** Everything already built for `media_item` works for both new shapes
for free. A `serial` and a `collection` each get artwork, field-level locking
([ADR 0008](0008-field-level-locking.md)), match state, overview, and the
`GET /api/items/{id}` shape with no per-type branching — the same payoff
[ADR 0010](0010-shows-as-media-items.md) took for shows.

**Good.** The prior ADRs are vindicated, not strained. Multi-part works need
**zero** new tables — new `kind` values and `parent_id`. Collections need exactly
one join table, which is the sanctioned escape hatch, not a return to per-type
tables. Both "revisit if" clauses resolve the way they said they would.

**Good.** The membership/containment split is the model, not an accident of
storage. It answers future cases without re-litigation: a franchise is a
collection; a two-part finale is parts; a box set of a miniseries is a
collection whose member is a serial.

**Cost.** More `kind` values that queries must filter on, and a second way for a
row to be "not a top-level playable item" (parts, like seasons). Getting the
browse filter wrong shows chapters as if they were features — the same failure
[ADR 0010](0010-shows-as-media-items.md) flagged for seasons, and it needs the
same test.

**Cost.** `collection_id` referencing `media_item(id)` means a join table whose
both columns point at the same table. `ON DELETE CASCADE` on both keeps it clean
when either the collection or a member is deleted, but it is a self-referential
relationship a reviewer should read twice.

**Cost.** Two grouping concepts a user must not confuse. The UI has to make
"parts of one work" and "a series of works" feel different, or the distinction
that justifies two mechanisms is invisible and therefore pointless. That is a
design.md concern, deferred with the client work.

**Deferred deliberately.** This ADR decides the schema and API contract — the
expensive-to-retrofit part — and stops there. Provider ingestion (TMDB
`belongs_to_collection`, filename heuristics for "Part 1/2", serial chapter
detection in `internal/media`) is M2-provider depth, planned when built per the
roadmap's ordering principle. The schema carries the shape ahead of the
providers that fill it, the same way `playback_state.user_id` preceded
multi-user.

## Revisit if

A grouping appears that is neither membership nor containment — for example an
ordering that must differ per viewer, or a work that is genuinely a part of one
collection *and* a container of its own parts in a way the two mechanisms cannot
compose. The response is still a side table for the exception, not per-type
tables.
