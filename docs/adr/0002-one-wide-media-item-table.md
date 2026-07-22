# ADR 0002 — One wide `media_item` table

Date: 2026-07-22 · Status: accepted

## Context

The obvious relational model for a media library is a table per media type:
`movie`, `show`, `season`, `episode`, later `album`, `track`, `photo`. It is
what most schemas in this space look like, and it is what a normalization
instinct produces.

But one of LANcast's four principles is that **everything interesting is a
provider** — and the intended end state is that *what counts as a library type*
is itself plugin-defined. Kodi's real architectural win was that TV shows were
not special-cased into the core; Plex's structural limitation is that its
taxonomy is fixed and you cannot add a genuinely new kind of thing.

A table per type hardcodes the taxonomy into the schema, which is the hardest
place to change it. Every new media type would then mean a migration, new
queries, new handlers, and new client code — which in practice means new media
types never happen.

## Decision

**One `media_item` table** with a `kind` discriminator and nullable hierarchy
columns (`series`, `season`, `episode`). Normalize only when a real second
consumer needs it.

## Consequences

**Good.** A new media type is a new `kind` value plus provider logic, not a
migration. Cross-library queries ("everything added this week", global search)
are one query against one table rather than a union across five. The API surface
stays small: one item shape, one list endpoint, one detail endpoint. Clients
built against that shape keep working when new types appear.

**Cost.** Nullable columns that only apply to some rows. `series`, `season`, and
`episode` are meaningless for a movie, and the schema does not stop you writing
them. This is a real loss of database-enforced integrity, accepted deliberately
and mitigated by keeping all writes behind typed `store` methods rather than raw
SQL.

**Cost.** The table gets wider as media types accumulate. If it becomes
genuinely unwieldy — the threshold is roughly when type-specific columns
outnumber shared ones — the answer is a `media_item_attr` key-value side table
for the long tail, not a split into per-type tables.

**Cost.** Some queries need `WHERE kind = ?` where a typed schema would not.
Cheap, and indexed.

## Revisit if

Type-specific columns come to outnumber shared columns, or a media type appears
whose hierarchy genuinely cannot be expressed in the existing nullable columns.
The response is a side table, not a taxonomy in the schema.
