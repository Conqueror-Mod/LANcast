# ADR 0018 — API contract and versioning

Date: 2026-07-26 · Status: accepted

## Context

Two of LANcast's four principles are "clients are thin" and "no phone-home."
Thin clients are only viable if the contract they are thin against is stable,
and a self-hosted server has no telemetry to learn what clients depend on — so a
careless breaking change is discovered by a user's client breaking, not by a
dashboard. The API contract therefore has to be decided deliberately and early,
which is why the roadmap lists it as a Foundation item to settle *before any
third-party client exists*. It is cheap to decide now and expensive to retrofit
once something depends on it.

Today the surface already has a working shape, established piecemeal:

- Base path `/api`, JSON bodies, Unix-second times, millisecond durations.
- One error envelope, `{"error": {"code", "message"}}`, with a fixed code set,
  and a rule that handlers never surface raw SQL.
- A stated but under-specified versioning note in [api.md](../api.md): change
  freely until the first external client, then break at `/api/v2` with the
  previous version kept for at least one release.

Every route hardcodes `/api/...` in `internal/api/server.go`, and the embedded
web client hardcodes `/api` too. What the existing note leaves genuinely open:

1. **Is `/api` versioned?** If it is "unversioned," then when `/api/v2` ships,
   nobody can say whether `/api` means v1 or the latest — the ambiguity that
   makes unversioned base paths rot.
2. **What counts as breaking?** Without a written rule, every change is a
   judgement call, and the conservative reading ("touching anything is
   breaking") freezes the API while the permissive reading breaks clients.
3. **How does a client discover the version it is talking to?**

The trigger for settling this now is [ADR 0017](0017-collections-and-multi-part-works.md),
which adds `kind` values (`collection`, `part`, `serial`) that existing clients
have never seen. Whether that is a breaking change is exactly the question this
ADR has to answer — and it must answer "no," or the wide-table taxonomy of
[ADR 0002](0002-one-wide-media-item-table.md) breaks a client every time a new
media type appears, which would defeat the entire point of the wide table.

## Decision

**URL-path versioning, and `/api` is permanently version 1.** A breaking
revision lives at `/api/v2`; `/api` never changes meaning and is not retro-fitted
to `/api/v1`. Renaming forty routes and the client for a version segment that
guards nothing buys nothing today, and pinning `/api ≡ v1` forever removes the
ambiguity that "unversioned" would carry into the day v2 arrives.

Version is carried in the path, not a header. A path is visible in a log, a
`curl`, and a browser address bar; a client selects a version by choosing a URL,
with no content negotiation to get subtly wrong.

**The compatibility contract is additive-safe.** These changes are
**non-breaking** and may ship within a version at any time:

- Adding a field to a response.
- Adding a new endpoint.
- Adding a new value to an **open set** — `kind` and `match_state` are open by
  construction ([ADR 0002](0002-one-wide-media-item-table.md)); new media types
  and match states appear without a version bump.
- Adding an optional request parameter with a backward-compatible default.
- Adding a new error `code` for a condition that previously did not occur.

These are **breaking** and require `/api/v2`:

- Removing or renaming a response field, or changing its type or units.
- Removing an endpoint, or changing an existing status code for the same
  condition.
- Tightening validation so a previously accepted request is now rejected.
- Changing the meaning of an existing `kind` / `match_state` / error `code`.

**Two client obligations make the additive rule safe**, and are now part of the
contract rather than folklore:

- Clients **must ignore unknown response fields** rather than reject them.
- Clients **must tolerate unknown `kind`, `match_state`, and error `code`
  values** — degrading gracefully (render a generic tile, show a generic error)
  rather than crashing. A client that hard-codes an exhaustive switch over `kind`
  is relying on a guarantee this contract explicitly does not give.

**The error envelope is frozen.** The `{"error": {"code", "message"}}` shape and
the documented code set are part of the v1 contract; `message` is for humans and
not to be parsed, `code` is the stable machine signal.

**Version discovery:** `GET /api/health` reports `api_version` (an integer)
alongside the application `version` (a semver string). The two are independent —
the app versions on its own cadence; `api_version` changes only when a new
`/api/vN` prefix ships. A client can assert the contract it was built against
without parsing the app's release number.

**Deprecation:** when `/api/v2` ships, `/api` (v1) keeps working for at least one
subsequent release, and the removal is called out in release notes. A
self-hosted client cannot be force-upgraded in lockstep with the server.

## Consequences

**Good.** ADR 0017 and every future media type ship without breaking a client,
because new `kind` values are additive by written rule. The wide-table
taxonomy's promise is now backed by an API contract, not just a schema shape.

**Good.** "What breaks a client?" has a written answer, so a reviewer can decide
whether a change needs `/api/v2` from the diff alone, and `api.md` is the single
authority — as it already must be, since it and `internal/api/` are required to
agree in the same commit.

**Good.** No code churn today: `/api ≡ v1` means the forty existing routes and
the client stay exactly as they are. The only concrete change is the
`api_version` discovery field.

**Cost.** The additive rule is a discipline the schema does not enforce. Nothing
stops a handler from renaming a field; only review and the frozen-contract rule
in `CLAUDE.md` catch it. This is the same class of trade as the wide table's
nullable columns — real integrity given up for extensibility, mitigated by
process, not by the type system.

**Cost.** Client authors must actually honour the two obligations. A client that
crashes on an unknown `kind` will still break when `collection` lands — the
contract shields conforming clients, not careless ones. The obligations are
documented so that breakage is the client's bug, not the server's.

**Cost.** Path versioning means a future v2 duplicates route registration for
the overlap window rather than transforming responses in a middleware. Accepted:
explicit duplicated routes are readable and independently testable, where a
version-shim middleware is a place for the two versions to drift silently.

## Revisit if

A change is genuinely needed that is breaking under the rule above but too small
to justify standing up `/api/v2` and a deprecation window. That pressure is the
signal the additive boundary is drawn wrong — the response is to re-draw the
line in this ADR deliberately, not to slip a breaking change into v1 and hope no
client noticed.
