# ADR 0040 — A season is not a searchable work

Date: 2026-08-17 · Status: accepted · shipped in v0.6.43

## Context

A real library, 15 shows, was showing a Thai drama's poster across whole seasons
of unrelated series. Season 2 of *The League*, *Black Books*, *Silicon Valley*,
*Deep Space Nine*, *The Next Generation*, *Voyager*, *Blue Mountain State* and
*It's Always Sunny* all carried the same two artwork hashes, and every episode
under them inherited that poster in the grid.

Nothing was wrong with the artwork cache, the merge engine or the scanner. The
season rows had simply been matched, at high confidence, to the wrong shows.

A season was enriched like any other item: `fetchRemote` built a query from
`item.Title` and sent it to `/search/tv`. A season's title is a *position* —
"Season 2" — so the query that went out was, verbatim:

```
/search/tv?query=Season+2
/search/tv?query=Season+10
/search/tv?query=Season+13
```

TMDB answers those with real shows whose names contain the phrase. The scorer
strips non-ASCII and punctuation before comparing (so a Thai title normalizes
down to the "season 2" it ends with), the year agreed exactly, and the result
scored **0.905** — above the 0.85 auto-apply threshold. The record was applied:
title, year, overview, poster, fanart.

The decisive property is that **the query depended only on the season number**.
The same wrong show therefore won for every show in the library:

| season | matched to | score |
|---|---|---|
| 1 | 如果古建筑会说话S01 | 0.903 |
| 2 | รักหลับ กับ ออฟ - กัน season 2 | 0.905, nine times |
| 10 | MTV's 10 on Top | 0.903 |
| 12 | Big Zuu's 12 Dishes in 12 Hours | 0.905 |
| 13 | Club Friday Season 13 | 0.910 |
| 15 | Jamie's 15-Minute Meals | 0.911 |

The reasoning was already written down. `store.notReviewable` says a season's
name "can only ever fail" a name search, and that routing `KindSeason` to the
show providers is "right for *fetching* a known season, and wrong for searching
one by name". But the remedy stopped at hiding seasons from the review queue —
the place the cost lands on a person. The search kept running, and the failure
mode that mattered was not the search failing. It was the search *succeeding*.

Two further things kept it invisible. `EnsureSeason` stamps a season resolved at
birth, so seasons never queue during a normal scan and the bug needed a trigger:
refreshing a library's metadata clears every stamp, seasons included. And once
poisoned, a season row is stamped `matched`, so it is not pending, and seasons
are excluded from review, so no human is offered it. The wrong answer was
self-sealing.

## Decision

**A season is never searched for. It is resolved from the show that owns it, or
not at all.**

1. `tmdb.Search` returns nothing for `KindSeason` without issuing a request. A
   season query has no correct answer, so there is none to rank.
2. `enrich.fetchSeason` reads the parent show's provider and external id and
   fetches `/tv/{show}/season/{n}` directly. No search, no scoring, no
   threshold — the show's identity was already established and the season
   number is an exact lookup.
3. A season under an **unmatched** show is left *pending*, not recorded as
   unmatched. Enriching the show later brings its seasons with it; stamping a
   verdict now would strand them permanently.
4. `tmdb.Fetch` for `KindSeason` no longer aliases to `fetchShow`. A season gets
   its own name, overview and poster, and does not claim the show's `series`.
5. `ReparseTargets` excludes seasons, for the reason `notReviewable` already
   gives: a filename guess has nothing better to offer a season than
   "S02 480p Bluray", and re-parsing one clears the metadata stamp that was
   keeping it out of the enrichment queue.
6. Revision 26 strips the identity and artwork from every season a name search
   matched and clears its stamp, so it is resolved again properly. Locked
   seasons are untouched.

Confidence is reported as 1.0 rather than a score, because there is nothing
uncertain to report: either the show is matched and this is its season *n*, or
the show is not and there is no answer yet.

## Consequences

A season's sort key becomes its number, zero-padded — `season 002`. The default
listing order leads with `sort_title`, and "Season 10" sorts before "Season 2"
as text. This is a numeric key for a row whose name *is* a number, not a second
opinion about title normalization competing with `internal/media`.

Seasons of an unmatched show now sit in the pending queue instead of leaving it
with a verdict. That is the honest state and it is self-correcting, but it means
the pending count on a library with unmatched shows no longer trends to zero
while those shows stay unmatched.

The general rule this is an instance of: **an item whose name is a position
rather than a title must be resolved by structure, never by search.** Scoring
cannot save such a query, because the scorer is being asked to rank answers to a
question that has none — and a confident wrong answer is worse than the failure
the threshold was built to prevent. Collections already work this way, arriving
with a provider id from the record that mentions them. Extras
([ADR 0038](0038-extras-are-not-works.md)) are the same shape from the other
direction: not works, so not searched.

This does not fix the DS9 folder layout that produced four `show` rows for one
series, or the duplicate synthetic shows beside real ones. Those are
[ADR 0037](0037-show-identity-is-the-series-title.md) territory and are still
open.
