# ADR 0034 — A library in more than one place

Date: 2026-08-13 · Status: **proposed**

## Context

A library is one row with one path:

```sql
CREATE TABLE library (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    path TEXT NOT NULL UNIQUE,
    ...
);
```

Real libraries are not always in one place. The case that prompted this is
ordinary: family films and animation on one drive, the main film collection on
an NVMe. They are one library by every meaning that matters — same kind, same
metadata rules, wanted in one A-Z, browsed as one thing — and the schema can
only express them as two.

The workaround is to create a second library beside the first, which is not
just cosmetically annoying. It splits the A-Z rail, splits "play all", makes
collections that span both impossible, and doubles every per-library setting.
Two libraries is a lie about the data that every feature downstream has to
inherit.

### A library is doing two jobs

The reason this is awkward is that `library` is simultaneously:

- the **scan unit** — a root to walk, with reconciliation scoped to it, and
- the **browse unit** — an entry in the sidebar, a filter on every listing.

Nothing requires those to be the same object, and the request here is
specifically for two of the first and one of the second.

### What the naive fix costs

The obvious move is a `library_root` table and a containment check that asks
"does *any* of this library's roots contain this path?".

That last part is the problem. Today the check is:

```go
containedPath(lib.Path, it.Path)   // one root, one boundary
```

Turning it into a search over roots makes it a **weaker** property. A row
pointing into root B when it belongs under root A would pass, because some root
matched. CLAUDE.md singles out this exact check as "the boundary where a bad row
becomes arbitrary file access", and a loop that accepts on first match is how
that boundary quietly stops being one.

## Decision

### `library_root`, and the item records which root it came from

```sql
CREATE TABLE library_root (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    library_id INTEGER NOT NULL REFERENCES library(id) ON DELETE CASCADE,
    path       TEXT    NOT NULL UNIQUE,
    created_at INTEGER NOT NULL
);
-- and, on media_item:
root_id INTEGER REFERENCES library_root(id) ON DELETE CASCADE
```

The second half is the point. With `root_id` on the item, containment stays:

```go
containedPath(root.Path, it.Path)   // still one root, still one boundary
```

**The invariant does not weaken — only the lookup moves.** There is never a
search across roots, because every item already knows which root it came from,
and the scanner knows it at upsert time because it is the root it was walking.
This is the whole reason to prefer this shape over the obvious one.

`library.path` moves into `library_root` rather than being kept alongside it.
Two places holding the same truth is a bug factory, and the migration has to
touch every row anyway.

### Partial availability is normal, not an error

With one root, a missing root means the scan can say nothing —
[#228](https://github.com/Conqueror-Mod/LANcast/pull/228) makes it refuse. With
several, the interesting case is that *some* are present, and it stops being an
error condition: an external drive being unplugged is a Tuesday, not a fault.

So a scan walks the roots it can see, and:

- **reconciles only within scanned roots.** `MarkMissing` is computed per root,
  over items whose `root_id` was actually walked. An unplugged family-films
  drive must not mark a single film on the NVMe missing.
- **reports which roots were skipped**, on `Progress`, so the UI can say "3 of 4
  locations scanned" rather than silently doing less than it appears to.
- **fails only when no root is available**, which is the single-root behaviour
  generalised rather than a new rule.

Without the per-root scoping, multi-root would take the bug #228 just fixed and
make it fire on a *healthy* server whenever an external drive was asleep.

### Removing a root deletes its items; a vanished root does not

These look like the same event and are opposites.

"Scanning marks missing, never deletes" is a rule about **inference**. A scan
deduces absence from not finding something, and a deduction from an unmounted
drive is wrong, so it must never be destructive. Removing a root from a library
is not a deduction — it is a person stating an intention, the same kind of act
as deleting the library, which already cascades.

So `ON DELETE CASCADE` on `root_id`, and the UI names the count before doing it.
The principle worth writing down is that the existing rule constrains what the
server may conclude, not what the user may ask for.

### Roots may not nest

A root that is a parent or child of any existing root, in any library, is
rejected at creation.

`UNIQUE(path)` already stops the exact duplicate. Nesting is the case
multi-root makes likely rather than exotic — someone adds `D:\Media` and later
`D:\Media\Kids` — and it has no good answer at scan time: the file is walked
twice, `media_item.path` is UNIQUE so the second upsert fights the first, and
`root_id` becomes whichever pass ran last. Refusing at the boundary is the only
place this is cheap.

## Consequences

**`lib.Path` has about 30 call sites** across `api`, `scan` and `enrich`. Every
one that resolves an item to a file must move to the item's root; every one that
walks or describes a library must move to the root set. This is the bulk of the
work and it is mechanical, but it is broad, and it touches the containment
checks — so it wants reviewing as a security change rather than a refactor.

**The API grows `roots` and keeps `path`.** `path` becomes the first root,
retained so existing clients keep working, and documented as superseded.
Creation accepts either a single `path` or a `roots` array. Additive under
[ADR 0018](0018-api-contract-and-versioning.md); a client that never learns
about `roots` sees a single-root library exactly as it does today.

**Migration is additive and backfills one root per library.** Revision 18: new
table, nullable `root_id`, one root per existing library from its current
`path`, `root_id` backfilled by `library_id`. `NULL` never persists past the
migration, but the column stays nullable so a partially-migrated database is
readable rather than broken.

**`RepointLibrary` becomes `RepointRoot`.** The prefix-swap logic in
[store.go](../../internal/store/store.go) is unchanged in substance — it already
rewrites paths under one root — but it now names a root instead of a library.
Its reasoning about not normalising and about moving the ignore list carries
over untouched.

**Per-root settings become possible and are not decided here.** Writing NFO
sidecars onto the family drive but not the main one is a reasonable thing to
want, and this schema is what would make it expressible. `write_nfo` is global
today ([config.Settings](../../internal/config/settings.go)) and moving it is a
separate decision with its own migration.

### What this is not

**Not a display grouping.** The alternative considered was leaving libraries
single-root and adding a "section" that unions several for browsing. It keeps
containment untouched, which is a genuine advantage, but every listing endpoint
then grows a "library or section?" mode and collections, A-Z and play-all each
have to choose. That is a lot of surface for a display convenience — and it
describes the data wrongly: these two locations are not related things being
grouped, they are one library that happens to live in two places.

**Not one-library-per-kind.** Plex's rule that a library is a kind plus a set of
folders would also solve this, by forbidding the second movie library outright.
It solves it by removing a capability people use — separate libraries for
genuinely separate collections, with different scan settings — and LANcast has
no reason to take that away to fix a schema limitation.

## Work breakdown

1. Revision 18: `library_root`, `media_item.root_id`, backfill, drop
   `library.path`. Migration test asserting a single-root library is
   indistinguishable afterwards.
2. Store: root CRUD, nesting validation, `RepointRoot`, and per-root
   `KnownFiles` / `MarkMissing`.
3. Containment: every handler resolves through the item's root. This is the
   security-relevant step and wants its own commit and its own review.
4. Scanner: walk each available root, per-root reconciliation, skipped-root
   reporting on `Progress`. Extends the `checkRoot` guard from #228 rather than
   replacing it.
5. API: `roots` on read and create, `path` retained as the first root.
   `docs/api.md` in the same commit.
6. Client: the library settings screen grows add/remove root, and the scan
   status says when a location was skipped.

## Open question

Whether a root should be able to move *between* libraries — reassigning the
family-films drive from its own library into the main one, rather than
re-adding and re-scanning it. It is the natural migration path for anyone who
already worked around this with a second library, which is everyone who has hit
it, and it is a single `UPDATE` of `library_id` plus a re-bucket of the items.
The reason to hesitate is that show and music libraries derive hierarchy
(seasons, albums) that is scoped to the library, so a move is not purely a
re-tag for every kind. Worth deciding before step 2 rather than after.
