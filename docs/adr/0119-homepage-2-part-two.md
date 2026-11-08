# ADR 0119 — Homepage 2.0, part two: a spotlight from your history, shelves you pin, and On this day beyond photographs

Date: 2026-11-08 · Status: **proposed** · Related: [ADR 0118](0118-the-home-page-is-a-bridge.md) (its v0.10.10 list), [ADR 0074](0074-a-viewing-is-a-row.md), [ADR 0051](0051-sensitive-content-is-obscured-until-asked-for.md), [ADR 0057](0057-what-a-forty-thousand-item-library-may-cost.md)

## Context

ADR 0118 split Homepage 2.0 across two versions. v0.10.9 is built (PR #1032):
the Bridge, the resume console, the friends and On this day panels, the
spotlight's tint, and New this week. It left three items for v0.10.10, each
given one line:

> a hero mode that learns from your own play history ("Tonight, because…"),
> offered beside the existing modes; shelves you pin yourself, kept per account
> on the server (a schema change with its own ADR); On this day beyond
> photographs.

One line each was enough to reserve the version and not enough to build from.
This ADR is the design for each, written overnight from what the code and the
database already hold. Every point marked **Decide** needs an answer before
that piece is built. Each one gives a recommendation; none of them is decided
here.

Three facts about the existing data shape all three designs:

1. **Watch history starts on 2026-09-30.** The `viewing` table (ADR 0074,
   schema revision 53) records each finish with its time. Everything older was
   backfilled from `playback_state` and marked `estimated`: its date is when
   the watched flag was last written, not when the film was watched.
2. **History covers films and episodes only.** Music, audiobooks, comics and
   retro games keep a position but have no dated record of finishing.
3. **There is no per-account preference store.** The spotlight mode, tile size
   and big-screen layout are per device (`useDevice`, `lib/heroMode.ts`),
   because "inventing one for a spotlight would be a schema decision made by a
   radio button". Pinned shelves are that schema decision, made on purpose.

## 1. A spotlight from your history

### What exists

Four modes: `continue` (the default), `recent`, `recommended` and `pinned`.
`recommended` already says "Because you watched X". It takes the first title
you are part-way through, reads its genres, and picks an unwatched title in the
same library that shares one. It is attributable by construction, and a pick
that cannot be attributed is not shown.

Its weakness is the one ADR 0118 named: it seeds from what is *in progress*. A
household that finishes what it starts has nothing in progress, so
`recommended` falls back, and the spotlight becomes the monument the mode was
built to avoid.

### Proposal

A spotlight **seeded from what you finished**:

- **Seed.** Your last 20 finished films and episodes from `viewing`, estimated
  rows included. A rough date is fine for "lately", but not for "on this day"
  (see §3). The genre those finishes share most often wins. Ties go to the
  most recent finish.
- **Pick.** An unwatched film or show that carries that genre, from a library
  you can see, with fanart (the existing `heroEligible` rule). It is seeded
  per visit like Unwatched, so it changes from one visit to the next and does
  not change under the pointer.
- **Sentence.** "Because you've finished 6 thrillers lately", built from the
  count it actually used. If the genre appears fewer than three times, it says
  nothing and falls back to `continue`. The sentence has to be true, and
  "lately" over one film is not.
- **Where it runs.** On the server: `GET /api/home/suggestion?from=history`.
  The client cannot do it without one detail request per finished title,
  because the list shape carries no genres. It is about three queries, and it
  is cheap under the ADR 0057 budget.

**Decide (1a): a fifth mode, or `recommended` falling back to history?**
Recommendation: **the fallback.** Two modes that both say "Because…" are a
setting that asks people to understand the difference. With the fallback,
`recommended` seeds from what you are watching when there is something, and
from what you finished when there is not. The mode list stays at four, and
the monument case goes away for everybody already using `recommended`.
ADR 0118 said "offered beside the existing modes", so choosing this amends
that line.

**Decide (1b): does "Tonight" mean the time of day?** Picking by the clock,
such as shorter films after 22:00 or an episode rather than a film on a
weeknight, is the part of the original line that this ADR does not propose.
It needs a duration or time pattern from your history that is strong enough
to state in a sentence, and five weeks of real history is not enough to show
one exists. Recommendation: **not now.** Name it for what it does. Come back
to this when there is a year of history to measure.

## 2. Shelves you pin

### What a shelf is

A pinned shelf is **a saved library view**: one library, plus the filter and
sort query the library grid already understands (`FILTER_PARAM_KEYS` in
`lib/browseFilters.ts`), plus a title. "Unwatched 80s action", "Kids' films
rated U", "Comics by one publisher". Anything the grid can show, a shelf can
show. No second query language is needed, because the grid's query is
already a contract that `docs/openapi.json` describes and tests enforce.

A collection, a playlist and a genre are all things the grid can already
filter by, so each is a special case of the above and needs no kind of its
own.

### Where it is kept

```sql
CREATE TABLE home_shelf (
    id         INTEGER PRIMARY KEY,
    user_id    TEXT    NOT NULL,          -- no FK, like viewing: 'local' has no row
    position   INTEGER NOT NULL,
    title      TEXT    NOT NULL,
    library_id INTEGER NOT NULL REFERENCES library(id) ON DELETE CASCADE,
    query      TEXT    NOT NULL,          -- the grid's query string, validated
    created_at INTEGER NOT NULL,
    UNIQUE (user_id, position)
);
```

- **Per account, on the server**, as ADR 0118 decided. Your shelves follow you
  to the TV and the phone. That is the whole reason this is not `useDevice`.
- **`query` is validated on write** against the parameters `GET /api/items`
  accepts, and it is re-checked when a shelf is read. A shelf can only ever be
  a listing you could have asked for by hand. It is never a way around
  `PermittedItems`, which still applies to every row it returns.
- **Deleting the library deletes its shelves.** A shelf over a library you
  have since lost access to is not shown, and it is kept in case access
  returns.

### How you make one

From the library grid: once filters are on, **"Pin to Home"** sits beside
"Clear all" and asks for a title, with the filter pills as the default. On
Home a pinned shelf has a menu with Rename, Move up, Move down and Unpin. On a
TV every one of those is reachable by D-pad, and dragging is never the only
way to do anything.

### API

`GET /api/home/shelves`, `POST /api/home/shelves`,
`PATCH /api/home/shelves/{id}` (title or position) and
`DELETE /api/home/shelves/{id}`. Each shelf's items come from the existing
`GET /api/items` with its stored query, so there is no new listing endpoint.
All four routes go in `api.md` and `openapi.json` in the same commit.

**Decide (2a): where do pinned shelves sit?** Recommendation: **above New
this week, below Rate.** You put them there on purpose, and the automatic
shelves are what you scroll past to reach them.

**Decide (2b): can pinning hide the automatic shelves?** For example,
"Unwatched in Movies" off. Recommendation: **yes, as a second, smaller step,
in the same table** (a row naming a built-in shelf, with no query). It is not
in the first cut. Hiding is the natural next request after pinning, and the
table should not need a second migration to allow it, so the first migration
makes `library_id` and `query` nullable and adds a `builtin` column.

**Decide (2c): a limit.** Recommendation: **12 per account.** Each shelf is a
request on the first screen, and 12 is more than any home page comfortably
shows.

This is a new table, which CLAUDE.md requires an ADR for. This section is
that ADR, if accepted.

## 3. On this day, beyond photographs

### What it can honestly say

"On this day" makes a dated claim: this happened on today's date in an
earlier year. The only dated record beyond photographs is `viewing`, and only
its non-estimated rows carry a real date. The backfilled rows carry the day
the flag was written, which could make a film watched in June appear as
"a year ago today" in October. So:

- **Films and episodes you finished on today's date in an earlier year**, from
  `viewing` rows where `estimated = 0`. The day is decided on the server with
  SQLite's `'now', 'localtime'`, as `/api/memories` already does, for the same
  UTC reason.
- **Nothing from estimated rows**, and nothing for music, books, comics or
  games, which have no dated finishes.

**The consequence is that this shows nothing until 2026-09-30 comes round
again.** Real history began that day, so the first genuine "a year ago today"
is 2027-09-30. Built in v0.10.10, it would sit empty for ten months. The panel
already hides when it has nothing (ADR 0118 §3), so that costs nothing on
screen, but it would ship unused.

**Decide (3a): build it now, or when it can show something?**
Recommendation: **build it now.** It is small: one store query and a second
source for the existing `MemoryPanel`. It starts working on its own in
September 2027, with nothing to remember to do. Building it later means
reloading this context then.

**Decide (3b): start recording finishes for the other kinds?** Adding rows to
`viewing` for an album played through, a book finished, a comic read to its
last page and a game completed would give On this day something to say about
them a year from now. It would also give §1 more to seed from. It is a
behaviour change to ADR 0074's table, which today is films and episodes only
and feeds the Trakt export. Recommendation: **yes, for books and comics**,
which have a clear "finished" moment. **Not music**, where a finished album is
a weak signal, or **games**, where completion is not something LANcast
observes. Trakt export filters to films and episodes, as it does now.

## Not proposed

- **A recommender.** Nothing here learns a model, ranks by similarity across
  libraries, or contacts anything. Every pick can say in a sentence where it
  came from, or it is not shown. This is the rule `recommended` already
  follows.
- **Time-of-day picking** (§1b), until there is history to justify it.
- **Pinning across libraries.** A shelf is one library's view, because the
  grid's query is one library's query. A cross-library shelf would need a
  query language the grid does not have.

## Build order, if accepted

1. Recording finishes for books and comics (§3b), first, because every day
   it ships is a day of history that exists a year later.
2. The history fallback for `recommended` (§1).
3. Pinned shelves (§2): migration, store, API and contract, then the grid's
   Pin to Home, then Home.
4. On this day from history (§3a).

Each step is its own PR. Step 3 is the only one with a schema change.
