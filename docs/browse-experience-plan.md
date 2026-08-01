# Browse-experience backlog — implementation plan (Phases 1–3)

The direct payoff of the media-organisation work ([ADR 0017](adr/0017-collections-and-multi-part-works.md)):
media-type-aware library pages, Plex-style filters, and ratings display. External
RT/Metacritic ratings (Phase 4) is **out of scope here** — it needs its own ADR
(OMDb source + schema migration) and is noted only as the follow-on.

Constraints (from [CLAUDE.md](../CLAUDE.md) / [roadmap.md](roadmap.md)):
- API changes are contract changes — `docs/api.md` updated in the **same commit**.
- Everything here is additive per [ADR 0018](adr/0018-api-contract-and-versioning.md);
  no `/api/v2`, nothing breaking.
- No new normalizer; reuse `clean`/`SortTitle` in `internal/media/parse.go`.
- Gold means "where you are" only — ratings/quality badges never use gold.
- Before claiming done: `go test ./...`, `go build ./...`, `npm run build`, and
  commit the rebuilt `internal/web/dist`.

---

## Phase 1 — Media-type-aware browse (client only, no API change)

**Problem.** `Browse.tsx` renders one generic grid regardless of `library.kind`.
A movie library and a TV library deserve different filter rows and tile affordances.

**Approach.** Split the shared chrome out of `Browse.tsx` into a `LibraryView`
shell (header, count, search, filter row, grid, empty/error states), then select a
per-kind **config** on `library.kind`:

- Movie library — poster grid; sort {Title, Year, Recently added}.
- Show library — show-poster grid whose tiles surface `child_count` as "N seasons";
  sort {Title, First aired, Recently added}; "Search shows" placeholder.
- Unknown / `other` / `music` kinds fall back to the movie config — the open-set
  rule ([ADR 0018](adr/0018-api-contract-and-versioning.md)) means we never
  hard-switch on kind.

Container count labels are a general helper (`containerCountLabel` in
`lib/kind.ts`) driven by the item's own kind, so a collection reads "N films" and
a two-part movie reads "N parts" — not a show-only hack.

**Files.** `web/src/screens/Browse.tsx` (dispatcher), `web/src/screens/LibraryView.tsx`
(new shell), `web/src/screens/libraryConfig.ts` (new), `web/src/lib/kind.ts`
(count-label helper), `web/src/components/PosterTile.tsx` (sub-label).

**No API/schema/doc change.** Routes unchanged (`/library/:id`).

---

## Phase 2 — Richer filters + per-library counts (small additive API change)

### Server (additive, ADR-0018-safe)
- `GET /api/libraries/{id}/facets` also returns `content_ratings` present and a
  `has_unwatched` boolean.
- `GET /api/items` gains optional, **repeatable** `genre`/`decade`, plus new
  `content_rating` (repeatable) and `watched=false`.
- **`docs/api.md` updated in the same commit.** Store: typed query helpers only,
  no `*sql.DB` leak.

### Client
- Genre/decade/content-rating become **multi-select chip groups**; unwatched
  toggle. URL-encoded as repeated params so a filtered view stays linkable.
- Nav rail (`AppShell.tsx`) shows `item_count` next to each library.

**Tests.** Handler tests for each new param and multi-value combination against
fixture libraries; facet test for content_ratings/has_unwatched.

---

## Phase 3 — Ratings display (client only, existing data)

`Item.rating` (TMDB 0–10) and `content_rating` already arrive from the API but the
poster tile never badges them.

- Rating badge + content-rating pill on `PosterTile.tsx` (neutral chip, **never
  gold**); consistent rendering on `Detail.tsx`.
- `sort=rating` added to `GET /api/items` (one branch; additive; `docs/api.md` in
  the same commit) and to the sort menus.

---

## Sequencing

1. **PR 1 — Phase 1** (client-only, no contract change).
2. **PR 2 — Phase 2** (server + client; the one contract change).
3. **PR 3 — Phase 3** (mostly client + one sort value).

Each PR is independently shippable and revertable.

---

## Deferred — Phase 4 (not in this plan)

External RT/Metacritic ratings via **OMDb** (keyed off IMDb id): new provider in
`internal/meta/`, nullable multi-source rating columns (**migration + ADR 0019**),
OMDb key in provider settings. Gated behind its own ADR and an explicit go-ahead.
