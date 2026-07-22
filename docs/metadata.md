# Metadata and artwork (M2)

> **Status: implemented.** Design reference for the M2 subsystem. Decisions are
> recorded in ADRs
> [0007](adr/0007-provider-and-localsource-split.md),
> [0008](adr/0008-field-level-locking.md),
> [0009](adr/0009-nfo-round-trip-safety.md), and
> [0010](adr/0010-shows-as-media-items.md).

M1 gives you filenames in a grid. M2 makes it a library: titles, posters,
synopses, cast, correctly ordered seasons, and the fanart the detail pages in
[design.md](design.md) are built around.

**The dominant constraint is not fetching data.** It is that automatic matching
will sometimes be wrong, and that the way most servers handle being wrong — a
rescan silently reverting your correction — causes more frustration than missing
features do. This subsystem is designed backwards from preventing that.

## Sources

| Source | Type | Role |
|---|---|---|
| TMDB | `Provider` | Films and TV. One free key. |
| NFO sidecar | `LocalSource` | Kodi-compatible; migration and portability |
| Embedded tags | `LocalSource` | Container metadata, weakest signal |
| Filename | — | `internal/media` guess; the floor |

TMDB is the only network provider that ships first. One provider done properly
proves the interface; three done partially prove nothing.

**LANcast is fully functional with no key at all** — scanning, browsing, and
playback all work, NFO metadata still imports, and the absent key produces an
explanation rather than an error. The no-phone-home principle is only credible
if declining is a real option.

## Interfaces

`internal/meta`. Two interfaces, because remote search and local sidecar reads
are different shapes — see [ADR 0007](adr/0007-provider-and-localsource-split.md).

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

`Record` is the normalized payload every source produces. Normalizing at the
source boundary is what keeps everything downstream provider-agnostic — and it
is why the M4 plugin runtime can register into the same `Registry` without any
downstream change.

## Matching and confidence

### Scoring

Each candidate scores 0.0–1.0:

- **Title similarity, ~0.6.** Jaro-Winkler over normalized titles. **Reuse
  `clean` and `SortTitle` from `internal/media/parse.go`** — a second normalizer
  that disagrees with the first is a bug factory.
- **Year proximity, ~0.3.** Exact scores full; ±1 year most of it; 2+ years is a
  heavy penalty. An absent parsed year is neutral, never penalized.
- **Popularity, ~0.1.** Tiebreak only. It must never rescue a weak title match —
  that is exactly how servers confidently match obscure files to blockbusters.

Episodes score differently: the parent show match dominates, and season/episode
numbers must match exactly or the candidate is rejected outright.

### Thresholds

| Score | `match_state` | Behavior |
|---|---|---|
| ≥ 0.85 | `matched` | Applied silently |
| 0.55–0.85 | `review` | Applied **and** flagged for review |
| < 0.55 | `unmatched` | Nothing applied; queued |
| — | `locked` | User-confirmed; never scored again |
| — | `local` | Resolved from a sidecar; nothing to review |

### Not attempted is not an answer

`match_state` defaults to `unmatched`, so it cannot by itself distinguish "we
looked and found nothing" from "nothing has looked at this yet". That
distinction has to be made explicitly in three places, and getting it wrong in
any of them reports failures where no attempt was made:

- **The enrichment worker** leaves items pending when no provider is
  configured, rather than stamping them unmatched.
- **The review queue** requires `metadata_updated_at IS NOT NULL`.
- **Clients** must check `metadata_updated_at` before showing a "no match"
  indicator.

An item resolved entirely from an NFO gets `local` for the same reason: the
user already said what it is, so listing it for review would bury the items
that genuinely need attention.

The middle band carries the design. A low-confidence match is usually still
right, so applying it gives a good default — but it is recorded as uncertain
rather than presented as fact. **Uncertainty is data, not something to hide.**

### Correction

Correcting a match sets `match_state = 'locked'` with the chosen provider and
external id. Locked items are never re-scored, re-searched, or silently changed.
**Rescans reconcile files; they do not re-litigate identity.**

## Precedence and locking

Every field resolves independently:

> user lock → NFO → provider → filename guess

Editing a field locks that field only. See
[ADR 0008](adr/0008-field-level-locking.md) for why item-level locking was
rejected.

**The UI must show locked fields and allow per-field unlock.** A lock the user
cannot see or release is indistinguishable from a bug.

## NFO round-trip

Read and write. Writes are opt-in per library, atomic, skipped silently on
read-only mounts, and preserve unknown elements written by other tools.

Every file LANcast writes carries a hash marker so it can recognize its own
output on the way back in:

```xml
<lancast generated="2026-07-22T14:02:11Z" hash="sha256:9f2c…"/>
```

Hash matches → mirror, ignored for precedence. Hash differs or marker absent →
a human edited it, and it outranks the provider. Full reasoning in
[ADR 0009](adr/0009-nfo-round-trip-safety.md). The hash function is **one
function shared by the read and write paths** — two implementations that drift
would make every file look edited.

## Artwork

`internal/artwork`, under `<data>/artwork/`.

**Content-addressed.** SHA-256 of the source bytes is the identity:
`<data>/artwork/9f/9f2c4a…/original.jpg`. A shared backdrop is stored once, and
an upstream URL change orphans nothing.

Derived sizes are generated on first request and cached beside the original:

| Name | Width | Used by |
|---|---|---|
| `thumb` | 185 | Dense grids, search |
| `poster` | 342 | Poster grid |
| `poster2x` | 500 | Retina, TV |
| `fanart` | 1280 | Detail backdrops |
| `original` | — | Fallback, export |

Resizing uses `golang.org/x/image/draw` with CatmullRom — pure Go, no cgo, per
[ADR 0001](adr/0001-go-and-pure-go-sqlite.md).

Served with `ETag` = hash and `Cache-Control: public, max-age=31536000,
immutable`. Content addressing makes that safe: bytes behind a hash cannot
change.

**The cache is rebuildable.** Deleting `<data>/artwork/` must heal on next
access, and a test proves it.

## Enrichment

Scanning and enriching are separate phases, so a large first scan populates the
grid from filenames in seconds while metadata fills in behind it.

The queue needs no table — it is a query, and therefore restart-safe by
construction:

```sql
SELECT * FROM media_item
WHERE metadata_updated_at IS NULL AND missing = 0
ORDER BY added_at LIMIT ?
```

**Rate limiting.** Token bucket, default 5 req/s, 4 workers, both configurable.
Exponential backoff with jitter on `429`. Raw responses are cached in
`provider_cache`, so a rescan, a re-match, and a refresh of the same title cost
one API call rather than three.

**Targeted auto-refresh** is deliberately narrow: only shows with an episode
aired in the last 90 days are re-checked, and only for new episodes. Everything
else refreshes when asked. Metadata churning under a user who was happy with it
is its own kind of bug.

## Configuration

The TMDB key is a secret. It lives in the config file with `0600` permissions,
never in the database, and is never echoed back by the settings endpoint —
write-only, with a boolean `configured` flag for the UI.

## Verification

**Scoring is unit-tested against a fixture table, with no network.** This is the
highest-value suite in the project: it encodes what "correct" means for the
feature most likely to disappoint.

```
Blade Runner 2049 (2017)     → not Blade Runner (1982)
The Thing (1982)             → not The Thing (2011)
Ocean's Eleven (2001)        → not Ocean's 11 (1960)
Dune (2021) vs Dune (1984)   → year is decisive
Solaris, no year parsed      → low confidence, queued rather than guessed
Generic release-group name   → below threshold, not matched wildly
```

**Locking** — the regression that defines the milestone. Match, correct the
title, rescan, refresh; the correction survives while unlocked fields still
update. Permanent integration test.

**NFO round-trip** — mirror detection, hand-edit precedence, unknown-element
preservation.

**Artwork** — delete the cache directory, confirm it heals; verify `ETag` and a
`304` on repeat.

**Rate limiting** — enrich 500 items under the ceiling; `429` backs off rather
than cascading; a mid-enrichment restart resumes correctly.

**Offline** — with no key configured, everything works, NFO still imports, and
no error is shown.
