# ADR 0057 — What a forty-thousand-item library may cost

Date: 2026-09-03 · Status: **accepted** 2026-10-09, with the
[amendment](#amendment--2026-10-09-measured-where-people-wait) below — read it
first; the table directly under this paragraph is what was believed in
September, and part of it was measuring the wrong query.

The roadmap has carried *"performance targets — budgets for a 40k-item
library"* as unplanned since M3. This is the budget, and the reason it never
moved before is that a budget without a way to check it is a wish.

## What was measured, and what was not

Read queries against a synthetic 40,000-item library, in
`internal/store/scale_test.go`. Deliberately **not** a scan of 40,000 real
files: the scan path was measured directly this week and its cost is dominated
by touching media, which is a fact about disks rather than about scale. What
nobody had ever measured is whether the queries behind a browse page still
answer promptly at that size — and those are the ones a person waits on while
looking at a spinner.

40,000 is about twice the largest real library measured here (18,777 items).

| query | measured | note |
|---|---|---|
| browse, first page | **33ms** | 60 items and the total count |
| browse, deep page | **142ms** | offset 39,000 |
| browse, two facets | **121ms** | two genres and a resolution |
| **filter bar (all facets)** | **353ms** | every count over the whole library |
| search | **57ms** | per keystroke |
| Continue Watching | **2.3ms** | the home page's first shelf |

Taken on a 24-core desktop with a warm database, which is the *favourable* end
of what LANcast runs on. A budget set from these numbers is therefore already
optimistic, and that is the right direction for it to be wrong in.

## Decision

**These are the budgets, and one of them is already missed.**

| surface | budget | today |
|---|---|---|
| Continue Watching, and any home shelf | **50ms** | 2.3ms ✓ |
| search, per keystroke | **100ms** | 57ms ✓ |
| a browse page, any offset | **150ms** | 33ms first, **142ms deep** — at the edge |
| the filter bar | **250ms** | **353ms ✗** |

The numbers come from what a person notices rather than from what is
convenient. Under about 100ms a response feels instant; up to about 250ms it
feels like a button working; past half a second it needs a spinner, and a
spinner on a filter bar is an admission that browsing your own library is slow.

**The filter bar is over budget by 40%** and is the one thing here that needs
work. It cannot be limited — every facet count is over the whole library by
definition — so it is the query most exposed to size, and at 353ms it is
already the slowest thing between a person and their films.

**Deep paging is 4.3× the first page** (142ms against 33ms). Within budget, but
not flat, and the shape says an offset is being walked rather than sought. It
becomes the next problem at 80,000 items.

## Consequences

**A budget nobody checks is a wish**, so this ADR ships with the means to check
it. `-bench Scale` produces the table above, on demand.

`TestBrowseStaysFlatAcrossTheLibrary` runs in the **ordinary suite**, and to
earn that it builds **6,000 items rather than 40,000** — twelve seconds against
two and a quarter minutes. A guard that adds two minutes to every
`go test ./...` is a guard somebody eventually deletes, and the failure it
exists to catch — an index lost, a page becoming a scan of the whole table —
shows up just as clearly at 6,000.

It asserts a **shape, not a millisecond figure**, and deliberately. An absolute
threshold would fail on whichever machine CI happened to allocate, and a flaky
performance test is deleted rather than fixed — at which point the budget is
gone and nobody notices.

**The fixture is built once and shared** across the benchmarks. Building a
40,000-item library per benchmark took longer than every other test in the
repository combined, which is its own small lesson about what 40,000 costs.

**This ADR does not fix the filter bar.** It measures it, names the budget it
misses, and stops there — the fix is a separate decision with its own
trade-offs (a materialised count, a cache with an invalidation rule, or
accepting a coarser filter bar), and choosing between those without first
agreeing what "fast enough" means is how the wrong one gets built.

**Nothing here covers write throughput, transcode start-up, or memory.** They
are real budgets too and this is not them; naming three numbers that were
measured is worth more than naming ten that were estimated.

## Alternatives rejected

**Scan 40,000 real files.** The obvious reading of "a 40k-item library", and it
would measure the disk rather than LANcast. The scan cost is already understood
— reading tags, probing, and the reconcile — and all three were measured
against real media this week. Generating 40,000 files to re-learn that would
take hours per run and no one would run it.

**Set budgets from what the code currently does.** Tempting, and it makes every
budget green on the day it is written. It also means the filter bar's 353ms
becomes the target, and a budget that ratifies the current behaviour cannot
ever be missed — which is the same as not having one.

**Assert absolute times in CI.** Rejected above: the failure mode is a flaky
test that gets deleted, taking the budget with it.

---

## Amendment — 2026-10-09: measured where people wait

Accepted, with every budget above kept as written, and three things learned
on the way to meeting them.

**The September table measured a query the grid never sends.** The benchmarks
asked for `kind=movie`; the client's grid asks for a library's present top
level with collections and playlists left out, which adds the top-level rule
and the collection rule to every row. That is the slower query, so the table
was optimistic in a way the ADR did not intend. The benchmarks now send what
the client sends, and three surfaces the table never had are added: the
search box (which searches every library at once), the home page's Recently
Added shelf (also across every library), and its Unwatched shelf.

**The misses were bigger than recorded, and two were not recorded at all.**
On `main` at v0.9.73, measured fairly:

| surface | budget | main | now | now, `-bench Scale` |
|---|---|---|---|---|
| the filter bar | **250ms** | **681ms ✗** | 112ms ✓ | 65ms ✓ |
| grid, first page (title / year / added) | **150ms** | 77 / 79 / **216ms ✗** | 16 / 18 / 19ms ✓ | 14ms by title ✓ |
| grid, page 39,000 (title / year / added) | **150ms** | **217 / 345 / 349ms ✗** | 35 / 34 / 70ms ✓ | 30 / 26ms ✓ |
| grid, page 39,000 (rating / running time) | **150ms** | **336 / 216ms ✗** | **181ms ✗** / 111ms ✓ | 140ms by rating — at the edge |
| grid, two facets | **150ms** | **168ms ✗** | **165ms ✗** | 139ms — at the edge |
| search box, per keystroke | **100ms** | 99ms | 99ms | 54ms ✓ |
| home: Continue Watching | **50ms** | — | — | 2.5ms ✓ |
| home: Recently Added | **50ms** | **233ms ✗** | 21ms ✓ | 17ms ✓ |
| home: Unwatched | **50ms** | **16.5s at 6,000 items; one call at 40,000 was stopped after 10 minutes ✗** | 11.6ms at 6,000 | **57ms ✗** |

"Main" and "now" were taken in the same process, interleaved, fastest of nine,
against two copies of one 40,000-item library — the old code on a copy
without the new indexes. The machine was heavily loaded throughout, which is
why it was measured that way: absolute figures drifted by 2× between runs,
and only a side-by-side comparison survived that. Read those two columns as
ratios. The last column is the benchmark suite on a freshly built library
with the machine quieter, and is the one to compare a future run against.

**What was wrong, and what changed:**

- *The filter bar walked the library eight times.* Seven facets each read
  every row to collect one column, genres joined per item, and the favourites
  and watched checks probed the user's table once per item. It is now one
  pass for every column facet (`json_group_array(DISTINCT …) FILTER`, each
  facet keeping its own rule), genres asked per genre from the genre's side
  (revision 68 indexes `item_genre(genre_id)`, because the genre table is
  shared and a music genre is a miss in a film library), and the per-person
  checks start from that person's rows.
- *A page sorted every column of every row to return sixty.* A page is now
  chosen by id and its rows read afterwards; revision 69 indexes the grid in
  the three orders the video libraries default to or offer first — title,
  year, added — so a plain page is a walk along an index with no sort and no
  row visits at all.
- *The Unwatched shelf was quadratic.* "Not begun" was asked per row, as a
  search of the table for that row's children through an OR no index can
  answer. It is now asked from what the person has played — the played
  items, their parents and grandparents — as one set.

**An index that does not cover a query is worse than no index.** The finding
most likely to be relearned the hard way. With the grid indexes in place, a
filtered page went from 168ms to 600ms and a rating sort to 670ms: a filter
on a genre or a sort on rating has to visit every row regardless, and through
a grid index the rows arrive in title order — random order on disk — where
the old `(library_id, missing)` walk read them in the order they are stored.
The planner cannot see that difference. So a listing the grid indexes cannot
answer by themselves is told to walk the rows in stored order, which is the
plan it chose before those indexes existed; the fields that *are* covered are
named in a list, so a filter added later takes the old plan until somebody
decides otherwise. A fourth index, for search, was tried and dropped for the
same reason: it made the collection rule count members by walking the table,
and that subquery now pins its join order so no future index can offer it
the choice again.

**Three surfaces are at or past their budget, and are recorded rather than
fixed.** A rating-sorted page 39,000 deep and the two-facet page both sit at
about 140ms on a quiet machine and over 150ms on a busy one: each has to
visit every row, and going further would need an index per sort or per facet
combination — a cost paid on every write, for a page 650 screens down or a
filter at the edge of its budget. The Unwatched shelf is 57ms against 50: it
was quadratic and is now an index-only walk, and what remains is the shuffle
giving every candidate in the library a key. A shuffle over a sample rather
than the whole library would meet the budget, and would change what the shelf
can show, which is a product decision rather than a performance one. Search
meets its budget on a quiet machine and sits on it on a busy one; a
substring match reads every row, and the fix for that is a full-text index,
which is its own decision with its own storage and tokenizer trade-offs.

**The guard had to change kind.** `TestBrowseStaysFlatAcrossTheLibrary`
asserted that a deep page costs under ten times the first, and on `main` it
passed — at three times — because the first page was a full sort too. A
timing ratio cannot tell "sorted forty thousand rows" from "walked an index".
So how the grid is read is now asserted on the query plan
(`gridplan_test.go`): a plain page must be a covering-index walk with no
sort, and anything the indexes cannot answer must stay off them. That needs
no large library and cannot flake on a busy machine, and it fails with the
indexes removed or the steering disabled. The filter bar and the Unwatched
shelf, which are not one query each, are guarded as ratios to a single walk
of the library measured in the same moment: about 8 walks against about 30
for the old filter bar, and about 8 against about 11,000 for the old shelf.

**Not measured, still:** write throughput, transcode start-up, memory —
unchanged from the original — and **a real library**. Every number here is
from the synthetic fixture, whose rows carry no overviews and so are
narrower than real ones. That makes full-walk costs optimistic, which is the
same direction the original table was wrong in.
