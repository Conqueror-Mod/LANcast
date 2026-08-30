# ADR 0049 — An edition is a copy of a work, not a work of its own

Date: 2026-08-30 · Status: **proposed**

Extends [ADR 0042](0042-two-files-one-work.md), which decided that two files
claiming one work are **reported and never resolved** — never merged, never
ranked, never deleted. That decision is unchanged and this does not weaken it.
0042 is about the server refusing to *choose between* two files. This is about a
case where there is nothing to choose: both files are wanted, and the library
has no way to say so.

`internal/media/parse.go` names this as deferred work in as many words:

> Modelling editions as one work with several files is a real feature and a
> larger one; this is the half that stops the library lying about what the file
> is.

This is that feature, and the reason to write it down rather than build it is
that the cheap-looking parts are not the expensive ones.

## What is actually true today

Three readings, taken against the reporting library before anything was
designed.

**The parser already works.** Given the real paths:

```
…\Spider-Man Into the Spider-Verse (Alternate Cut) (2018)\… .mkv
   → title "Spider Man Into the Spider Verse"  year 2018  edition "Alternate Cut"
…\Spider-Man - Into the Spider-Verse (2018)\… H265-d3g.mkv
   → title "Spider Man Into the Spider Verse"  year 2018  edition ""
```

Two files, one title, one year, one of them carrying an edition. Everything a
grouping rule needs is derivable.

**And the column is empty. Every row, whole library.**

```sql
SELECT COUNT(*) FROM media_item WHERE edition IS NOT NULL;  -- 0
```

`edition` is written by `UpsertItem`, and the scanner **only upserts a file
whose size or mtime changed**. Every row that predates the edition marker
therefore has `NULL` for ever, because nothing about those files will ever move
again. Re-parse does not rescue it: `store.Guess` carries title, sort title,
year, series, season and episode and **not** edition, and it only touches rows
whose match state is `review` or `unmatched` — the Spider-Verse rows are
`matched`.

So the marker shipped, is correct, and is inert on every library that existed
before it. **No rule can group on a field that is null everywhere**, which makes
a backfill the first step of any option below rather than a detail of one.

**And one of the two motivating cases is not an edition at all.** The recognised
markers are `dc`, `se`, `ee`, `uncut`, `theatrical`, `final cut`,
`alternate cut`, `ultimate edition`, `directors cut`, `director's cut`,
`special edition`, `extended edition`. `COMPLETE` is not among them, so

```
Final Fantasy VII Advent Children COMPLETE (2009)
   → title "Final Fantasy VII Advent Children COMPLETE"  edition ""
```

The word stays in the title. Add the standard release later and the two are
different *works* — different titles, no collision, nothing to group. That is a
one-token fix and it is worth noticing that the token list is the entire
mechanism: a naming convention this library uses and that list does not know
about is a case the feature silently does not cover.

## What is being asked for

That alternate cuts stop reading as duplicates, and that a collection be the
place they live.

Worth separating two complaints that arrive together. One is that the **grid**
shows two tiles for what a person thinks of as one film. The other is that the
**collision report** lists them, which is 0042 working exactly as designed —
a shared identity that a human should look at, looked at once, and then listed
for ever because nothing records that it was.

## Options

**A — Do nothing.** Both rows exist, both match, both play. The library is not
lying about what the files are, which is what 0042's half already bought. The
cost is a permanent entry in a report meant for things needing attention, and
two tiles where somebody expects one.

**B — Recognised editions join a collection automatically.** What was asked for.
A collection already exists as a container with its own page, and a rule could
put `X` and `X (Alternate Cut)` in one. Cheap to describe, and it inherits a
question: a collection today means *a franchise the provider knows about* — the
Hills Have Eyes reboot collection, keyed on a TMDB id. A locally-invented
collection of one film's cuts is a second meaning for the same container, and
the browse page, the filter chips and the artwork inheritance all currently
assume the first.

**C — An edition is a copy of the work, and the work is the tile.** The row
carrying no edition is the work; rows carrying one are alternative files for it.
One tile, one detail page, a control that says which cut plays. This is what the
parser comment anticipates, and it is the only option that makes *play* mean
something unambiguous.

Its cost is the honest one: it needs a way to say "these two rows are the same
work", which is a schema change and a decision about what happens when the
provider disagrees — and it must not become a merge, because 0042 exists for the
case where the guess is wrong. An escape hatch that separates them again is part
of the feature rather than a follow-up.

**D — The report learns to be dismissed.** Orthogonal to the above and cheap: a
collision somebody has looked at and accepted stops being listed. It fixes the
*report* complaint without touching the model, and it is worth doing under any
of A, B or C — a report that cannot be answered trains people to ignore it.

## Recommendation

**Do the backfill and the token first, decide the model second.**

The backfill is required by B and C alike, is useful under A, and is the only
part with no design question in it: something has to write `edition` for rows
whose files will never change again. Extending `Guess` is the obvious route and
carries its own small decision, since re-parse currently declines to touch
matched rows for a good reason — a provider title is better evidence than a
filename. An edition marker is not a title, so it may be the one field a matched
row can accept from its filename; that is worth arguing explicitly rather than
assuming.

`COMPLETE` is one token, and the case for adding it is that it is *this
library's* naming. The case against is that the list is a guess about the world
and every addition widens what a title can silently lose.

Between the models, **C is the one that matches what an edition is**, and B is
the one that was asked for. They are not far apart in effect and are very far
apart in cost, so the decision worth making deliberately is whether "one tile,
choose your cut" is wanted enough to pay for a way of saying two rows are one
work — given that the thing actually irritating today may be **D**, which costs
almost nothing.

Nothing here is built. The measurements are, and they are the part that would
otherwise be assumed.
