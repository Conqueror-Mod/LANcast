# ADR 0041 — A misplaced file is corrected on disk

Date: 2026-08-17 · Status: accepted

## Context

Two files in one TV library, both classified `movie` by the scanner. They look
identical to the parser and have **opposite** correct answers.

**Case 1 — a miniseries part.** `Y:\TV Shows\Storm Of the Century (1999)\` held
three files of the same 734 MB:

```
Storm of the Century (1999).avi                        ← renamed 2016, marker lost
Storm.Of.The.Century.[1999].DVDRip.XviD.EP2-...avi
Storm.Of.The.Century.[1999].DVDRip.XviD.EP3-...avi
```

The first is part 1. Its filename lost the `EP1` marker years ago, so `Parse`
finds no season/episode pattern and no ordinal, falls through to `KindMovie`
([parse.go:364](../../internal/media/parse.go)), and the row lands top-level with
`parent_id = NULL`. Verified against the real parser:

```
Storm of the Century (1999).avi        kind=movie    series=""                S00E00
...XviD.EP1-BLiTZKRiEG.avi            kind=episode  series="Storm Of The..."  S01E01
```

Being a `movie` sent it to `/search/movie`, where it matched a different,
same-named film — precisely the failure the comment directly above that
fallthrough warns about. **The parse was wrong**, and it was wrong because the
filename no longer carried the fact it needed.

**Case 2 — a special that really is a work.** `A Very Sunny Christmas.mkv`, in
the Always Sunny folder, also parses as `movie`. TMDB agrees:

```
/movie/1113686  →  "A Very Sunny Christmas"  2009-11-17  43 min
```

A direct-to-DVD special with its own movie entry, which is *also* an episode of
season 6. It matched at 0.756 and sat in review — the honest verdict for
something genuinely ambiguous. **The parse was defensible.** The file was simply
in the wrong library.

### What hurt, and what it taught

The user hit case 1, opened **Fix match**, and chose the correct title. It
worked, and left the row half-fixed.

`ApplyMatch` sets **identity, not kind**. It already accepts a `matchKind`
differing from the item's own — its docstring names "correcting a movie-scanned
miniseries to its TV entry" as the reason — fetches from `/tv` accordingly, and
then applies the record *under the item's own kind*. So the user said "this is
television", the code believed them enough to query the TV endpoint, and wrote
the answer onto a row that stayed a parentless movie.

Then it got worse: a confirmed match sets `match_state = 'locked'`, and a locked
identity is never re-litigated. The row became permanently a movie-shaped tile
that no rescan, reparse or refresh would ever touch. **Correcting it made it
uncorrectable.** The only escape was renaming the file on disk.

## Decision

**Reparenting is not built. A file in the wrong place is corrected where the
wrongness lives — on disk.**

Two remedies, both already available:

- **A filename that lost information gets it back.** Case 1 was resolved by
  restoring the `EP1` marker; the next scan parsed it as `S01E01` and it joined
  its show. The stale row was marked missing, and `topLevelPredicate` excludes
  missing rows, so the duplicate tile disappeared without anything being deleted.
- **A work goes in the library whose kind matches it.** Case 2 is a film, so it
  belongs in the film library — where the Star Trek films in this same install
  already live. A library's kind is the declaration of what its folder holds, and
  moving a file across is how that declaration is honoured.

**One change is accepted and — as of 2026-08-25, in v0.8.13 — built: a confirmed
match must not lock a row whose shape is still wrong.** Locking an identity onto a parentless movie row
in a show library is the trap above, and it is what turned a two-minute filename
fix into a dead end. Either the shape is settled first, or the row stays
reviewable. This is small, independent of everything else here, and it is the
only part of this ADR that requires code.

The parser is deliberately left alone. Case 2 shows the fallthrough to `movie` is
sometimes right, and 0.756-in-review is the correct output for a genuinely
ambiguous file. Guessing harder is not the answer.

## Why not reparenting

Because the project already decided a show library may hold loose files, and
said so. [`shapecheck.go`](../../internal/scan/shapecheck.go), on why there is no
"movies in a show library" warning:

> A shows library legitimately contains a few loose files — an extras folder, a
> documentary shipped beside a series — so "any movie at all" would cry wolf on
> ordinary libraries, and a check that cries wolf gets ignored, which is worse
> than no check.

A movie-kind row in a show library is therefore an *accepted state*, not a defect
to engineer away. Reparenting would be a database feature built to paper over a
disk-level fact, and it is not a small one: a mutation that changes `kind` (which
nothing else does), locks on `kind`/`parent_id`/`season`/`episode` so the next
scan cannot re-derive the movie shape from the filename, a new endpoint, an
`api.md` change distinguishing a library's immutable kind from an item's, and a
UI for choosing a target show and an episode number.

That is a real build to serve two cases which each had a correct answer already,
neither of which reparenting was even the right tool for: case 1 needed the
filename repaired, and case 2 needed to be in the other library.

## Alternatives considered

**Never emit `movie` in a show library.** Rejected: breaks case 2, contradicts
ADR 0038, and needs a representation for "an episode whose number is unknown"
that the schema does not have.

**Infer from siblings** — a lone unmarked file among numbered episodes of one
work, filling a gap in the numbering, is that missing episode. Appealing, and it
would get both cases right (case 2 sits among 16 seasons of contiguous episodes
with no gap to fill). Rejected on two counts. `Parse` is a pure function of
`(root, path, libKind)` and deliberately sees directory *names*, not sibling
*files*; giving it directory state makes every filename test require a fixture
tree. And gap-filling invents an episode number, which is the confident-wrong-
answer failure [ADR 0040](0040-a-season-is-not-a-searchable-work.md) was written
against — a file assigned to the wrong episode is worse than one left
unassigned. Worth revisiting as a *suggestion in review*, never as a silent parse.

**A `movies_in_show_library` shape warning**, as a cheap middle path. Rejected by
the very thresholds it would have to pick: `shapecheck.go` sets
`episodeShareInMovieLibrary = 0.4` precisely so a few loose files do not trigger
anything, and this library is one movie-kind row in twelve items — 8%. A warning
that fires there is the wolf-crying that file exists to prevent. The library is
not shaped wrong; one file is in the wrong place.

**Reparenting** — see above.

## Consequences

The documented answer to "this file is the wrong kind" is now a filesystem
answer, and it needs to *be* documented rather than rediscovered. Both remedies
are undramatic once known and unguessable until then.

A filename year is worth more than it looks. Case 2 scores 0.756 with no year in
the name and **0.901–0.905 with one**, which is the difference between sitting in
review forever and being matched silently — measured against the real scorer:

```
pop=1.00   no year: 0.755 (review)   with year: 0.905 (matched)
```

That is a general fact about this library layout, not a fact about this file.

Moving a file between libraries leaves the old row behind, marked missing on the
next scan of the library it left. That is correct — scanning marks missing, never
deletes — and the grid already excludes missing rows, so the effect is invisible
and the row stays recoverable.

## What would reopen this

**Media that cannot be renamed or moved**: a NAS the user does not control, a
library shared from another machine, or read-only media. Every remedy above is a
write to the filesystem, so where that write is impossible this decision offers
nothing, and reparenting becomes the only remedy rather than a redundant one.
That is the condition to watch for — not a reason to build it now.

## What was built

`store.ShapeUnsettled(libraryKind, item)` is the rule, deliberately narrow: a
**parentless** `movie` row in a `show` library, which is the exact shape a lost
episode marker produces. It is *not* "a shows library contains a film" — that is
ordinary and legitimate, it is Case 2 above, and a check that cries wolf gets
ignored, which is worse than no check. `shapecheck.go` already declines to cry
wolf about the same thing at library level.

`enrich.ApplyMatch` consults it and passes `StateReview` instead of
`StateLocked`. **The identity is still applied** — the person's choice is
honoured, the fields are written, nothing is refused or silently dropped. What
changes is only whether the door closes behind it.

A failure to read the library falls to the reviewable side. The cost of being
wrong that way is one row a rescan may revisit; the cost of the other way is a
wrong identity welded on for ever.

Somebody who presses Confirm has to be told, or the row simply reappears in the
queue and looks like a bug. The handler already returns the updated item, so the
client reads `match_state`: anything but `locked` after a successful confirm
means the door was left open deliberately, and Fix match says so and stays open
rather than closing. No API change was needed — which is why the contract is
unchanged here.
