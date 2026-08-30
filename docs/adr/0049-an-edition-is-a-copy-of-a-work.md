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

## What has already happened

Option **D** below — *the report learns to be dismissed* — **shipped in
v0.8.29**. It was the cheapest item here and the one addressing the complaint
people actually feel, so the remaining question is narrower than it was when
this was drafted: not *how do we stop the report nagging*, but *is one tile
worth building*.

## What is actually true today

The first draft measured two files. This draft measures the whole library:
1,229 movie rows, 1,197 of them present, parsed with the real
`media.Parse` against their real paths, and read back out of a copy of the
running server's database.

**The parser works, and finds an edition on six files in 1,197.**

```
Alien DC (1979)                                     → "Alien"                            ed=DC
Alien 3 SE (1992)                                   → "Alien 3"                          ed=SE
Alien - Resurrection SE (1997)                      → "Alien Resurrection"               ed=SE
Aliens SE (1986)                                    → "Aliens"                           ed=SE
South Park Bigger Longer Uncut (1999)               → "South Park Bigger Longer"         ed=Uncut
Spider-Man Into the Spider-Verse (Alternate Cut)    → "Spider Man Into the Spider Verse" ed=Alternate Cut
```

**And the column is still empty. Every row, whole library.**

```sql
SELECT COUNT(*) FROM media_item WHERE edition IS NOT NULL;  -- 0
```

`edition` is written by `UpsertItem`, and the scanner **only upserts a file
whose size or mtime changed**. Every row predating the marker therefore has
`NULL` for ever, because nothing about those files will move again. Re-parse
does not rescue it: `store.Guess` carries title, sort title, year, series,
season and episode and **not** edition, and `ReparseTargets` only offers rows
whose match state is `review` or `unmatched`.

So the marker shipped, is correct, and is inert on every library that existed
before it. **No rule can group on a field that is null everywhere.**

### Four things the wider measurement changed

**1. Only one of the six is an edition *of* anything.** Grouping the parsed
output by (title, year) yields exactly one group in the entire library:

```
Spider Man Into the Spider Verse (2018)
    ed=''                Spider-Man - Into the Spider-Verse (2018)
    ed='Alternate Cut'   Spider-Man Into the Spider-Verse (Alternate Cut) (2018)
```

The four Alien rows are **the only copy of their film**. The marker is
accurate; there is no theatrical cut beside them to be an alternative to.

This breaks the first draft's option C as written. It proposed that *the row
carrying no edition is the work, and rows carrying one are alternative files
for it* — and in this library, four of five edition-bearing rows **have no such
base row**. A model that needs one has nothing to attach them to.

**2. The provider already says which rows are one work.** The pair shares a
provider identity:

```
#7742  Spider-Man: Into the Spider-Verse                matched  tmdb:324857  score 0.97
#7743  Spider Man Into the Spider Verse (Alternate Cut)  locked  tmdb:324857  score 1.00
```

`tmdb:324857` twice. This is the single most consequential reading here,
because the first draft priced option C on the assumption that saying "these
two rows are the same work" needed **a schema change and a new concept**. It
does not. `(provider, external_id)` already is that statement, it is already
stored, and it is *already the key ADR 0042's collision report groups on*.

And it is the only such group in 1,197 films — the same one pair, arrived at
from the other direction.

**3. The false positives never reach the database, because the provider
overwrites them.** `South Park Bigger Longer` is what the parser produces; the
row stores `South Park: Bigger, Longer & Uncut`, matched from tmdb. The four
Alien rows likewise store clean provider titles. The parser's edition-stripping
is doing exactly the job it was built for — making an edition match the work it
is an edition of — and its wrong guesses are transient.

That is worth stating plainly because it inverts the risk the first draft
worried about. The token list is not dangerous to *matched* rows. It is
dangerous to `review` and `unmatched` rows, which keep the filename guess, and
to any future rule that reads the parsed title instead of the stored one.

**4. The remaining motivating case is already resolved by hand, and the token
list still would not catch it.** `Final Fantasy VII - Advent Children COMPLETE
EDITION (2005)` matched tmdb's plain `Final Fantasy VII: Advent Children`.
`complete edition` is not in the vocabulary, so nothing marks it as an edition
— but with only one copy present, nothing needs to. It becomes a real case only
if the standard release is added later, at which point it is a
provider-identity collision like the Spider-Verse pair and the missing token is
what stops it being recognised as an edition.

## What is being asked for

That alternate cuts stop reading as duplicates, and that a collection be the
place they live.

Two complaints arrive together. One is that the **grid** shows two tiles for
what a person thinks of as one film. The other is that the **collision report**
lists them — which was 0042 working as designed, and which **v0.8.29 fixed**.

## Options

**A — Do nothing.** Both rows exist, both play, and since v0.8.29 the report
can be answered once and stays answered. The cost is two tiles where somebody
expects one — on **one film in 1,197**.

**B — Recognised editions join a collection automatically.** What was asked
for. It inherits a question the first draft raised and the measurements do not
resolve: a collection today means *a franchise the provider knows about*, keyed
on a provider id. A locally-invented collection of one film's cuts is a second
meaning for the same container, and the browse page, filter chips and artwork
inheritance all assume the first. It also does not describe the four Alien
rows, which are editions with nothing to be collected with.

**C′ — The work is the provider identity, and editions are rows under it.**
The first draft's C, re-costed against finding 2 and repaired against finding 1.

Rows sharing `(provider, external_id)` are copies of one work. The work is the
**identity**, not a designated base row — so a lone `Aliens SE` is simply the
one copy of `Aliens`, needing no theatrical cut to exist, and the Spider-Verse
pair is one tile with two copies. One detail page, a control saying which cut
plays.

The schema cost the first draft feared is gone. What remains is a **display and
playback** change plus one genuine decision, below.

**D — The report learns to be dismissed.** **Shipped, v0.8.29.**

## The decision C′ still has to make

Not every shared provider id is an edition. 0042 exists precisely because a
shared identity can be a **mis-match** — two different films handed one id by a
bad guess — and silently folding every collision into "one work, two copies"
would resolve exactly the case 0042 refuses to resolve, which is the failure
this project has a permanent test against.

The distinguishing evidence is already present and costs nothing to read: in a
collision where **exactly one row carries an edition marker and the other does
not**, the rows agree about what the work is and disagree only about which cut
it is. In a collision where **neither row carries a marker**, nothing
distinguishes them and the honest answer is still 0042's — report it.

That rule matches the data: the library's one collision has exactly one marker.
It is also falsifiable rather than a preference, which is the property to want
here. And it keeps the escape hatch the first draft asked for without inventing
one: a wrongly-grouped pair is separated by editing either row's identity,
which is the same lock that already governs everything else.

**This makes the backfill the load-bearing prerequisite it always was**, and
narrows it usefully. The rule reads `edition`, which is `NULL` everywhere, so
until something writes it the rule fires on nothing. Two properties it needs:

- It must reach `matched` rows, which `ReparseTargets` excludes by design. The
  exclusion is right about *titles* — a provider title beats a filename — and
  an edition marker is **not a title**; it is a fact about which file this is,
  which no provider can know and only the filename records. That is the
  argument for edition being the one field a matched row may accept from its
  filename, and it should be made explicitly rather than assumed, because it is
  a narrow exception to a rule that exists for good reasons.
- It must reach `locked` rows, since the one real case (#7743) is locked.
  Writing `edition` to a locked row is not re-litigating identity and does not
  re-score a match; the CLAUDE.md guarantee is about identity and locked
  fields, and `edition` is neither unless a person has edited it. Worth pinning
  in a test either way, because "the backfill touched a locked row" is exactly
  the sentence that should make a reviewer stop.

## Recommendation

**The measurements have made this a smaller decision than it looked, in both
directions.**

C′ is much cheaper than C appeared — the provider already supplies the "same
work" statement that was going to cost a schema change — and it is the only
option that describes all six edition rows rather than the one convenient pair.
If the model is built, it should be C′, not B: B costs a second meaning for
collections and still does not describe four of the six.

But the honest scale is **one film in 1,197**, and the complaint that actually
stung — a report that could not be answered — shipped in v0.8.29. So the case
for building anything now is weak, and the case for building it *carefully
later* is strong.

Concretely, in order:

1. **The backfill, on its own.** It is required by B and C′ alike, is useful
   under A because it makes the library able to say `Aliens — Special Edition`
   rather than silently dropping the marker, and is the only part with no design
   question left in it. It carries the two arguments above, both worth a test.
2. **`complete edition` as a token**, on the same reasoning as the rest of the
   vocabulary and with the same caveat: the list is a guess about the world, and
   every addition widens what a title can silently lose. Finding 3 lowers that
   risk for matched rows and does not remove it for unmatched ones.
3. **C′ if and when a second edition pair appears.** One instance is not
   evidence of a pattern, and the model is much easier to argue from three cases
   than from one.

Nothing here is built. The measurements are, and they are the part that would
otherwise be assumed.
