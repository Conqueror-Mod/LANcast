# ADR 0007 — Separate `Provider` and `LocalSource` interfaces

Date: 2026-07-22 · Status: accepted

## Context

"Everything interesting is a provider" is one of LANcast's four principles, and
M2 is where it stops being a slogan. The metadata contract defined here is the
project's first real extension point, and the M4 plugin runtime will inherit its
shape — so it is worth getting right rather than getting quickly.

Metadata arrives from two genuinely different kinds of place:

- **Remote services** (TMDB) — you search with a fuzzy query, get ranked
  candidates back, then fetch a chosen one by id. Networked, rate-limited,
  fallible, cacheable.
- **Local sidecars** (`.nfo` files, embedded container tags) — you read a known
  path and either get a record or you don't. No search, no ranking, no network,
  no ambiguity.

The tempting move is one `Provider` interface for both. It looks tidy on a
diagram.

## Decision

Two interfaces.

```go
type Provider interface {
    ID() string
    Caps() Caps
    Search(ctx context.Context, q Query) ([]Candidate, error)
    Fetch(ctx context.Context, ref Ref) (*Record, error)
}

type LocalSource interface {
    ID() string
    Read(ctx context.Context, path string, kind Kind) (*Record, error)
}
```

Both produce the same normalized `Record`. A single `Registry` holds both in
precedence order, and the merge engine consumes `Record` values without caring
where they came from.

## Consequences

**Good.** Neither interface has a method its implementations cannot honestly
answer. A unified interface would force NFO to implement `Search`, and the only
available implementations are a lie (return the one record as a
perfect-confidence candidate, corrupting the scoring model) or a stub (return
`ErrUnsupported`, which every caller then has to special-case — the abstraction
paying no rent).

**Good.** Capability checks disappear from call sites. Code that needs to search
holds `Provider`; code enriching a known file holds both and asks each in turn.
The type system carries the distinction rather than runtime branching.

**Good.** The split has already proven load-bearing: confidence scoring applies
only to `Provider` results. Local sources are not *scored*, they are *trusted or
not* based on the NFO hash marker ([ADR 0009](0009-nfo-round-trip-safety.md)).
Two different trust models, correctly reflected by two different interfaces.

**Good.** The M4 plugin runtime registers into the same `Registry` with no
downstream change. That continuity is the entire reason for defining this now
rather than inlining TMDB calls and refactoring later.

**Cost.** Two interfaces to document and version instead of one. Real, and small.

**Cost.** Something that is genuinely both — a local database of scraped
metadata, searchable and on disk — implements both interfaces. Acceptable; it
honestly *is* both.

## Revisit if

A third category appears that fits neither shape. The likeliest candidate is a
*write* sink (push metadata out to another system), which is a different
direction of flow and should be its own interface rather than a third method on
these.
