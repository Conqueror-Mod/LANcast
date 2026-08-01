# ADR 0019 — External ratings (Rotten Tomatoes, Metacritic, IMDb) via OMDb

Date: 2026-08-01 · Status: accepted

## Context

The browse-experience work shipped a ratings *display* (Phase 3), but the only
rating LANcast holds is TMDB's single 0–10 score. The feature backlog asks for a
"ratings system that also ties to Metacritic / Rotten Tomatoes" — the scores
people actually recognise. This ADR decides how those arrive without violating
the four principles, and it is the schema-and-contract decision the roadmap says
to lock *before* building, because a second rating is expensive to retrofit into
a single `rating` column.

Three facts shape the decision:

- **RT and Metacritic have no free, official API.** The pragmatic source is
  **OMDb**, a third-party aggregator that returns IMDb, Rotten Tomatoes, and
  Metacritic scores for a title **keyed on its IMDb id**. It does not search for
  identity — you hand it an id you already trust.
- **LANcast does not store an IMDb id today.** `media_item` carries a TMDB
  `external_id` only. The `IMDBID` field on the OpenSubtitles query exists but is
  never populated, so that path is already running blind.
- **A rating is now many-per-item, not one.** IMDb (/10), RT (%), Metacritic
  (/100), and TMDB (/10) are different scales from different places, and a viewer
  wants to see them side by side, not collapsed into one number.

## Decision

Four parts.

### 1. OMDb is a `RatingSource`, not a `Provider`

A new narrow capability interface, registered alongside the existing ones:

```go
// RatingSource returns third-party scores for an item already identified by its
// IMDb id. It does not search — identity is resolved before it is ever called.
type RatingSource interface {
    ID() string
    Ratings(ctx context.Context, imdbID string) ([]Rating, error)
}

type Rating struct {
    Source  string  // "imdb" | "rotten_tomatoes" | "metacritic"
    Score   float64 // normalized to 0–10 for sorting and comparison
    Display string  // source-native form: "92%", "74", "7.7"
    Votes   int     // 0 when the source does not report a count
}
```

This is the same call ADR 0007 made for `LocalSource`, and that `TrailerProvider`
already makes for trailers: an interface must not carry a method its
implementations cannot honestly answer. Forcing OMDb to implement `Search`/
`Fetch` would mean faking a confidence-scored candidate for a service that has no
opinion on identity — the abstraction paying no rent. OMDb speaks *ratings for a
known id*, and the interface says exactly that.

### 2. Ratings live in a side table, keyed by source

```sql
CREATE TABLE item_rating (
    item_id    INTEGER NOT NULL REFERENCES media_item(id) ON DELETE CASCADE,
    source     TEXT    NOT NULL,            -- open set: imdb | rotten_tomatoes | metacritic | tmdb
    score      REAL    NOT NULL,            -- normalized 0–10
    display    TEXT    NOT NULL,            -- source-native string for the UI
    votes      INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (item_id, source)
);
```

A side table, not columns on the one-wide `media_item`, because the set of
sources is **open** — the same reasoning [ADR 0002](0002-one-wide-media-item-table.md)
used for keeping the taxonomy open. Adding "Letterboxd" later is a new `source`
value, not a migration. Scores are stored **normalized to 0–10** so any future
sort or aggregate is a pure numeric operation, and **also** as a display string
so `92%` and `74` render in their native scale rather than as `9.2` and `7.4`.

`media_item.rating` **stays** as the canonical scalar that Phase 3's badge and
`sort=rating` already read. It is the denormalized "primary" rating (default:
TMDB), so **nothing built so far changes**. Which source is primary can become a
setting later; the default keeps today's behaviour exactly.

### 3. A nullable `imdb_id` column on `media_item`

Populated from TMDB's `external_ids` (append-to-response on `Fetch`), nullable
because not every item resolves one. It is the join key OMDb needs, and it
**also** lights up the dormant OpenSubtitles IMDb path for free — a real
secondary payoff, not a rationalisation.

### 4. Enrichment, secrets, and the no-phone-home line

- A **rating pass** runs in the enrichment worker *after* identity is settled
  (`matched` or `locked`), keyed on `imdb_id`; items without one are skipped
  cleanly. It runs **only when an OMDb key is configured** — no key means the
  feature is dormant and nothing leaves the machine, honouring *no phone-home*.
- The **OMDb API key is write-only config**, stored `0600` in the config file,
  never in the database — the exact pattern TMDB and OpenSubtitles already
  follow. `GET` reports `{"configured": true}` and never the value.
- External scores are provider-derived and **replaced on refresh**; they are not
  user-editable and not individually lockable. Field-level locking
  ([ADR 0008](0008-field-level-locking.md)) continues to govern the primary
  `rating` field only. A rescan reconciles files, never identity or these scores.

## Consequences

**Good.** No existing behaviour moves. `media_item.rating`, the tile badge, and
`sort=rating` are untouched; the new scores are purely additive, surfaced as a
`ratings` array on the item-detail response (additive per
[ADR 0018](0018-api-contract-and-versioning.md) — no `/api/v2`).

**Good.** The open side table means a fifth or sixth source is a new row value,
not a schema change — the taxonomy stays open the way ADR 0002 intended.

**Good.** Storing `imdb_id` fixes a latent bug: OpenSubtitles hash-miss searches
can finally fall back to an IMDb query instead of title noise.

**Good.** `RatingSource` slots into the same registry the M4 plugin runtime will
inherit, so a future community rating source needs no new plumbing.

**Cost — third-party dependency.** OMDb is an unofficial aggregator, not RT or
Metacritic themselves. Availability, accuracy, and the **free tier's 1,000/day
limit** are real constraints; the pass must rate-limit and cache, and treat a
miss as "no score", never an error. This is the same fallible-remote posture the
TMDB provider already takes.

**Cost — coverage gaps.** OMDb thins out for older, non-US, and TV titles. Many
items will legitimately show only a TMDB score. The UI must render a partial set
gracefully rather than imply a title is unrated.

**Cost — terms of use.** OMDb redistributes RT/Metacritic data; displaying it to
the server's own owner (not re-serving it publicly) is the intended use, but the
OMDb and downstream terms should be confirmed before shipping, and the key is the
user's own. Flagged, not hand-waved.

**Cost — a migration and a schema-revision bump** (rev 9 → 10): one nullable
column and one new table. Forward-only, additive, but it *is* a data-model shape
change, which is why it gets this ADR.

## Alternatives considered

- **Columns per source on `media_item`** (`rt_rating`, `metacritic_rating`, …).
  Rejected: a closed set that needs a migration for every new source, against the
  open-taxonomy grain of ADR 0002.
- **OMDb as a `Provider`.** Rejected under ADR 0007: it cannot honestly `Search`,
  and a stubbed `Search` corrupts the confidence model or forces special-casing
  at every call site.
- **Scraping RT/Metacritic directly.** Rejected: fragile against markup changes
  and squarely against their terms — the opposite of a build that "will not ship
  unaudited dependencies".
- **Display-string only, no normalized score.** Rejected: forecloses per-source
  sort or any aggregate, for a trivial storage saving.

## Revisit if

- **A source gains an official API** (a direct RT/Metacritic feed). Swap the
  `RatingSource` implementation; the table and column are unaffected.
- **Users want to sort or aggregate across sources** (e.g. a weighted "critics vs
  audience" score). The normalized `score` column already supports it; this
  becomes a query and a settings choice, not a schema change.
- **A primary-source setting is wanted.** Point `media_item.rating` at a chosen
  source on refresh; the plumbing above already writes every source's score.
