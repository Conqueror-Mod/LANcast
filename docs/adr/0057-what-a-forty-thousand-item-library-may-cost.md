# ADR 0057 — What a forty-thousand-item library may cost

Date: 2026-09-03 · Status: **proposed**

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
