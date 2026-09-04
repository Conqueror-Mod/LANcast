# Photographs of one person — implementation plan

Face grouping is built to the point where it stops. Detection, clustering,
naming, suggestions and — since v0.8.59 — the ability to say who somebody is
*not*, all ship. What does not exist is the thing all of it was for.

`FacePeople.tsx` says so in its own first paragraph:

> A group of unnamed faces is a curiosity. A named one is **how somebody finds a
> photograph**.
>
> …forty photographs of one person is **an afternoon of finding them later**.

There is no route from a named person to those photographs. `ItemFilter` carries
`PersonIDs` — which is *film credits* — and no face field; nothing joins `face`
to the item query; and every control on the people screen opens the namer,
toggles singletons, or goes back. Naming Georgia gets you a wall of cropped
faces and no way to see the pictures they were cut from.

So this plan is small, and it is the last 10% of an expensive feature rather
than a new one.

## Constraints

From [CLAUDE.md](../CLAUDE.md) and the ADRs:

- API changes are contract changes: `docs/api.md` **and** `docs/openapi.json` in
  the same commit, and `pendingSpec` is closed to new entries.
- Everything here is additive under [ADR 0018](adr/0018-api-contract-and-versioning.md).
  No `/api/v2`, nothing breaking.
- **Marked folders stay covered** ([ADR 0051](adr/0051-sensitive-content-is-obscured-until-asked-for.md)).
  This is the one rule in this plan that is a security property rather than a
  behaviour, and it is spelled out in Phase 1.
- Gold means *where you are* and nothing else — a person pill is a filter chip
  like any other and never gold.
- Before claiming done: `go test ./...`, `go build ./...`,
  `npm --prefix web test`, and commit the rebuilt `internal/web/dist` — this
  touches client runtime, so unlike the OpenAPI work the bundle does move.

## Does this need an ADR?

**No, and building it did not change that.** Every decision it makes is already made somewhere: the filter
is additive (0018), the exclusion rule is 0051's, and the identity model is
0052's. Nothing here re-litigates any of them.

One thing *would* need one, and it is named in Phase 1 so it does not get
decided by accident: **what a second person id means.** Two values on a
repeatable filter are OR everywhere else in this API — two genres widen the
grid. For faces the more valuable query is almost certainly AND: *photographs
with both of us in them*. Those are different features wearing one parameter,
and picking wrong is a breaking change to undo. This plan ships single-valued
and leaves the question open.

---

## Decide first: what the parameter is called

`person=` is taken, and it means a **film credit**. A face cluster is a second,
unrelated notion of "person" living in the same query string.

This is the shape of a bug this project has already paid for once. `types.ts`
declared `Rating` twice — the external score and the caller's own — TypeScript
merged them silently, and the merged type required fields no endpoint sent.
Nothing failed; it was simply wrong for as long as both existed. Two concepts
sharing one word is how that happens, and the cheapest moment to stop it is
before either name ships.

**Built as `face_cluster=`, and repeatable — not single-valued as first
planned.** That changed on a fact rather than a preference: `CollapsedPerson`
carries `clusterIDs`, plural, because naming does not merge groups, and the
face-fracture measurement puts one person's photographs at 277/73 across two of
them. Single-valued would have shown 277 of 350 and looked like an answer. So
repeated means OR, which is both the house rule and what a person needs; **AND —
two different people in one photograph — would be a separate parameter**, the
way `actor` and `director` are separate rather than a mode on `person`.

**Recommendation as first written: `face_cluster=`.** It matches the table (`face_cluster`), it
matches [ADR 0052](adr/0052-face-grouping-runs-in-a-native-sidecar.md), and it
cannot be misread as a credit. `cluster=` is the terser alternative and is
unambiguous within this API today; it is worse the first time anything else
clusters.

The **response** side needs no new name: a cluster is already `people` on
`GET /api/libraries/{id}/people`, and that stays.

---

## Phase 1 — the server can answer "which photographs is this person in"

`internal/store`

- `ItemFilter` gains `FaceCluster int64`, single-valued.
- The query uses `EXISTS` against `face` on `cluster_id`. **Built as `EXISTS`,
  not the `DISTINCT` this plan first said** — that is the house idiom and the
  existing `credited` helper already carries the reason, in a comment about the
  identical problem: "a person credited twice on one item… must not duplicate
  the row or double the total."
- **It is not an optimisation.** One photograph can hold two faces of the same
  person — a mirror, a photograph of a photograph, a group shot where the
  detector fires twice — and a plain join shows that picture twice and counts
  it twice with it.

`internal/api`

- `face_cluster` on `GET /api/items`, parsed the way `person` already is: a
  non-numeric value is `400`, because an id is machine-generated and a
  malformed one means the caller is confused rather than that they want the
  whole library.
- **`ExcludeSensitive` is derived, never accepted.** The line that does it
  already exists and already carries the comment:

  ```go
  // Derived, never accepted from the caller. See TakenMonth above.
  f.ExcludeSensitive = f.TakenMonth != "" || f.TakenUndated
  ```

  **Built one layer lower than that.** The rule is enforced in the store where
  the clause is written, not derived in the handler, because a security property
  that every caller has to remember to set is one caller away from not being
  one — and the test that proved it went red when the store did not own it. The
  handler line is unchanged and says why. This is the security property: faces inside
  marked folders are never listed by
  `GET /api/faces/clusters/{id}/faces`, and a person filter that did not apply
  the same rule would be a way to enumerate marked photographs through a
  different door. An embedding is derived from a photograph and is not less
  private than it — 0051's own argument, applied one endpoint along.
- **Missing photographs are *not* excluded, contrary to what this plan first
  said.** The grid's base clause is `WHERE 1=1` — every other filter lists a
  missing item with its flag rather than hiding it — and a person filter that
  alone dropped them would be inconsistent with the surface it appears on. The
  faces *listing* does exclude them, and that is a different question: it draws
  crops it cannot render from a file that is gone, where this lists pictures
  somebody is in.

Contract, in the same commit: `docs/api.md` gains the parameter beside `person`,
saying plainly that the two are different questions; `docs/openapi.json` gains
it with the sensitive rule in its `description`, because that is the part a
generated client's author will otherwise assume.

**Done when:** `GET /api/items?library_id=5&face_cluster=3` returns each
photograph once; a photograph in a marked folder is absent whether or not the
caller asked; a group shot with the same person detected twice appears once; and
a non-numeric value is `400`. Tests mirror the existing filter cases in
`browsefilter_test.go`, and the sensitive exclusion gets its own named test —
that one is a rule, not a behaviour.

---

## Phase 2 — the person tile leads somewhere

`web/src`

- **Offered from the name panel, not the tile — the tile was the wrong place.**
  It already opens that panel, which is where a person is renamed, cleared, and
  told who they are not (ADR 0052), so navigating away from it would have taken
  three things away to add one. The existing face-removal tests went red saying
  so, which is the suite doing its job. A named person's panel gains
  *View photographs*; an unnamed one does not, because there is nothing to find
  yet.
- The pill row resolves the id to a name, exactly as the cast pill does:

  ```ts
  const name = ctx.castNames?.get(id);
  ```

  The same shape, from `GET /api/libraries/{id}/people` rather than `/cast`.
  Filter state lives in the URL, so a bookmarked `?face_cluster=3` arrives with
  an id and nothing else, and a pill reading "person 3" is not a filter anybody
  can read.
- The pill says the person's name and nothing about faces. "Georgia" is the
  filter; that it is implemented by clustering embeddings is not the viewer's
  business.
- **One pill per person, not per group**, and removing it clears the whole
  parameter rather than one id out of three.
- An unnamed cluster is reachable the same way and its pill reads
  *Unnamed person* — the id is meaningless to a reader and the tile is already
  the only way to get there.
- `FILTER_CATEGORIES` gains nothing. This is not a category with a panel of
  values: a library has hundreds of people, which is the same three-orders-of-
  magnitude argument that made cast a search endpoint rather than an array on
  `/facets`. The entry point is the people screen.

**Done when:** naming a person and pressing their tile shows their photographs;
the pill reads their name; removing it restores the full grid; and a reload of
the filtered URL renders the same pill rather than a bare number.

---

## Phase 3 — the counts stop disagreeing

Worth doing *with* the grid rather than after, because the grid is what makes
the discrepancy visible.

`FaceClusters` counts with `COUNT(f.id)` — **faces, not photographs** — and it
filters neither marked folders nor missing files, while
`GET /api/faces/clusters/{id}/faces` excludes both. So a tile can say 41 where
the naming screen shows 38, and once a grid exists it will show 38 too, with the
tile above it still claiming 41.

The tile's label already says "photographs".

- Count `DISTINCT f.item_id` and exclude what a marked folder covers, so the
  number means what the label says. **One exclusion, not the two this plan first
  said** — missing photographs stay counted, because the grid still lists them,
  and the tile's job is to agree with the grid it opens.
- Expect the number to **fall** on a real library — that is the count becoming
  correct, not the feature losing anything, and it is worth saying in the commit
  so it is not read as a regression.

**Done when:** a person tile's count equals the number of tiles in that person's
grid, on a library with at least one marked folder and one missing photograph.

---

## Part B — Memories, independently

Not required by the above and not blocked by it; listed here because it is the
other cheap thing the picture library is missing.

`taken_at` is stored and indexed (`idx_item_taken`), the home page already
renders shelves, and "one year ago today" is a query rather than a feature. The
whole of it is a store method, a route, and a shelf.

Two rules it must not get wrong, both of which the picture work has already
settled elsewhere:

- **Marked folders are excluded**, same as the timeline, and for the stronger
  reason: a memory is unsolicited. The timeline is somewhere you navigated to.
- Months and days are computed in the **server's local time**, which is what a
  person means by "a year ago today". The timeline already does this and a
  second opinion about where a day begins is a bug waiting for a timezone.

**Done when:** a shelf appears only when there is something in it — a heading
over no tiles is the shape of something broken — and a marked folder never
contributes to one.

**Built as its own route, `GET /api/memories`, rather than a filter on
`/api/items`.** The plan said "a store method, a route, and a shelf" and did not
say why the route; the reason turned out to be the whole design. A
`taken_on=MM-DD` parameter would put a calendar date in the client's hands, and
a client computing one is a fault already written down twice — `toISOString()`
is UTC, so through a US evening it resolves to *tomorrow*, and the shelf would
show the wrong day for hours every night while looking correct. The response
returns the date the server used, so a page left open overnight can tell it has
crossed midnight.

A third exclusion joined the two the plan named: **the current year**. A
photograph from this morning is not a memory, and a card imported today would
otherwise be the entire shelf on the one day the real memories were there.

---

## Sequencing

Phases 1–3 are one piece of work and should land as one PR: the filter without
the tile is unreachable, and the count fix without the grid is invisible. Part B
is independent and can go before, after, or never.

## Verification

Beyond the three suites, this is picture work and the picture plan's own rule
applies — the unit that catches this class is the real library, not a fixture:

- the real test picture library, with a **marked folder containing a named
  person**, confirming their photographs are absent from the filtered grid
- a person whose count changes in Phase 3, with the before and after recorded
- the grid driven by keyboard alone, and focus returning to the person tile on
  the way back ([ADR 0004](adr/0004-keyboard-focus-model.md))

jsdom performs no layout, so it can prove the pill holds the right name and the
request carries the right id, and it cannot prove the grid is on screen. Looking
at it is still the only way to know.

## Deliberately not in this plan

**Semantic search over photographs.** The biggest real gap — a picture library's
filenames are `DSC_0042`, so `?q=` is close to useless there — and the argument
from the Immich study stands: it wants an ONNX runtime, a model download and a
vector index, and it should be the thing that *proves* a plugin SDK rather than
the thing that gets built into the server.

**Tags, and favourites.** Cross-cutting rather than photo-specific.
[ADR 0008](adr/0008-field-level-locking.md)'s locking is the mechanism tags
need, and a favourites affordance has to be decided against
[docs/design.md](design.md) first — gold is spoken for.

**AND across two people.** See the ADR note above.

**GPS, masonry, an all-photos view, editing, export.** All refused with
reasoning in [ADR 0028](adr/0028-pictures-library.md), and none of it has
changed.
