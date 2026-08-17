# ADR 0041 — Correcting what a file *is*

Date: 2026-08-17 · Status: **draft — decision needed**

## Context

Two files in one TV library, both classified `movie` by the scanner. They look
identical to the parser and have **opposite** correct answers.

**Case 1 — a miniseries part.** `Y:\TV Shows\Storm Of the Century (1999)\` holds
three files of the same 734 MB:

```
Storm of the Century (1999).avi                        ← renamed 2016, marker lost
Storm.Of.The.Century.[1999].DVDRip.XviD.EP2-...avi
Storm.Of.The.Century.[1999].DVDRip.XviD.EP3-...avi
```

The first is part 1. Its filename lost the `EP1` marker years ago, so
`Parse` finds no season/episode pattern and no ordinal, falls through to
`KindMovie` ([parse.go:364](../../internal/media/parse.go)), and the row lands
top-level with `parent_id = NULL`. Verified against the real parser:

```
Storm of the Century (1999).avi        kind=movie    series=""                S00E00
...XviD.EP1-BLiTZKRiEG.avi            kind=episode  series="Storm Of The..."  S01E01
```

Being a `movie` sent it to `/search/movie`, where it matched a different,
same-named film — precisely the failure the comment directly above that
fallthrough warns about. **The parse was wrong.**

**Case 2 — a special that really is a work.**
`A Very Sunny Christmas.mkv`, in the Always Sunny folder, also parses as
`movie`. But TMDB agrees with that:

```
/movie/1113686  →  "A Very Sunny Christmas"  2009-11-17  43 min
```

It is a direct-to-DVD special with its own movie entry, *and* it is an episode
of season 6. **The parse is defensible.** It matched at 0.756 and sits in
review, which is the honest verdict for something genuinely ambiguous.

So the tempting rule — *a show library contains no movies* — is wrong. It would
fix case 1 by breaking case 2, and ADR 0038 already establishes that a show
library holds things that are not episodes.

### The part that actually hurt

The user hit case 1, opened **Fix match**, and chose the correct title. It
worked — and left the row half-fixed.

`ApplyMatch` sets **identity, not kind**. It already accepts a `matchKind` that
differs from the item's own (the docstring names "correcting a movie-scanned
miniseries to its TV entry" as the reason), fetches from `/tv` accordingly, and
then applies the record *under the item's own kind*. So the user said "this is
television", the code believed them enough to query the TV endpoint, and then
wrote the answer onto a row that stayed a top-level movie with no parent.

Then it got worse. A confirmed match sets `match_state = 'locked'`, and a locked
identity is never re-litigated. The row became **permanently** a movie-shaped
tile that no rescan, reparse or refresh will ever touch. Correcting it made it
unfixable.

The remedy in the end was to rename the file on disk and rescan, at which point
the stale row was marked missing and `topLevelPredicate` dropped it from the
grid. That worked, but it is a filesystem workaround for a database problem, and
it is not something a user should have to reason their way to.

## The real gap

A parser guessing wrong is ordinary and always will be. The defect is that
**there is no way to correct a structural guess.** Every correction surface in
the product is about identity:

| Surface | Corrects | Cannot |
|---|---|---|
| Fix match | provider, title, artwork | kind, parent |
| Re-read filenames | title from filename | kind (and skips `matched`/`locked`) |
| Edit fields | field values, with locks | kind, parent |
| `PATCH /api/libraries/{id}` | name, path | kind, by design |

ADR 0040 concluded that an item whose name is a position must be resolved by
**structure, never by search**. This is the same lesson from the other side: a
wrong *structure* cannot be corrected by a better *identity*. Fix match is the
only tool the user is offered, and it is the wrong shape for the problem.

## Recommended decision

**Make "this file is an episode of that show" an action the user can take, and
stop letting a confirmed match lock a row into the wrong shape.**

1. **Reparenting becomes a real operation.** A movie-kind row in a show library
   can be reassigned as an episode of a chosen show, with a season and episode
   number the user supplies. It changes `kind`, `parent_id`, `season` and
   `episode` in one transaction, and it locks those, because a person just said
   what this is.

   This is narrower than "kind is editable". It is one direction
   (`movie` → `episode`) within one library, and the target show must already
   exist in that library. Library kind stays immutable —
   [api.md](../api.md) is right about why.

2. **`ApplyMatch` honours the kind the user chose.** When `matchKind` is
   `episode` or `show` and the item is a `movie` in a show library, the row is
   reparented as part of applying the match rather than fetched as television
   and stored as film. The API already carries the user's intent; today it is
   discarded halfway.

3. **A match cannot lock a row whose shape is still wrong.** Locking identity
   on a row that has no parent in a show library is the trap that made case 1
   permanent. Either reparent first, or leave it reviewable.

The parser is left alone. Case 2 shows the fallthrough to `movie` is sometimes
right, and 0.756-in-review is the correct output for a genuinely ambiguous file.
Guessing harder is not the answer; being correctable is.

## Alternatives considered

**Never emit `movie` in a show library.** Rejected: breaks case 2, contradicts
ADR 0038, and it needs a representation for "an episode whose number is
unknown" that the schema does not have.

**Infer from siblings — a lone unmarked file among numbered episodes of one
work, filling a gap in the numbering, is that missing episode.** This is
appealing and would get case 1 right and case 2 right (16 seasons of contiguous
episodes, no gap to fill). Rejected for now on two counts. `Parse` is a pure
function of `(root, path, libKind)` and deliberately sees directory *names*, not
sibling *files*; giving it directory state makes every filename test require a
fixture tree. And gap-filling guesses a number, which is exactly the kind of
confident invention ADR 0040 was written against — a file assigned to the wrong
episode is worse than one left unassigned. Worth revisiting as a *suggestion* in
review rather than as a silent parse.

**Tell users to rename their files.** This is what actually resolved case 1 and
it is a legitimate answer — but it is the answer LANcast exists to avoid being.
It is also unavailable for read-only or shared media.

## Consequences

Reparenting is a new mutation on `media_item` that changes `kind`, which nothing
else does. It needs a migration only if it grows columns (it should not), but it
does need the locked-fields rule applied to `kind`, `parent_id`, `season` and
`episode` — otherwise the next scan re-derives the movie shape from the
filename and undoes it. That is the same reasoning as ADR 0030's membership lock.

`docs/api.md` gains an endpoint, and the sentence about kind being immutable
needs to distinguish a **library's** kind (still immutable) from an **item's**
(now correctable in one narrow direction).

The stale rows already locked into the wrong shape are not fixed by any of this.
There are two in the live library and both are already resolved by other means
(one missing, one renamed), so a migration is probably not warranted — but if
one is written, it can only safely *unlock* such rows, never re-guess them.

## Decision needed

- Is reparenting worth building, or is "rename the file and rescan" the
  documented answer? It is a real feature with a real UI surface, on a project
  where the alternative is a two-minute file rename.
- If built: should it also work `episode` → `movie`, for someone whose special
  was wrongly folded into a season? The asymmetry is defensible but arbitrary.
- Should point 3 (refusing to lock a wrong-shaped row) land on its own
  regardless? It is small, it prevents the trap recurring, and it does not
  depend on the rest.
