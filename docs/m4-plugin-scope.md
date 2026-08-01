# M4 — plugin architecture: scope

The last milestone. This is a **scoping document**, not a set of decisions: it
frames the sub-problems, the decisions each needs, and a build order — so the
ADRs that follow are written one at a time, immediately before their area is
built, per the roadmap's ordering principle.

## The one rule that governs M4

From [roadmap.md](roadmap.md):

> Specifying the plugin contract today would mean designing an extension API with
> no extensions to validate it against — which is exactly how these projects
> calcify around the wrong abstractions.

So the guiding constraint is: **do not design the full plugin contract up front.**
Decide the load-bearing, expensive-to-retrofit pieces (isolation boundary, trust
model), then build the runtime against **one first-party plugin** that
reimplements something we already ship, and let that plugin's friction shape the
contract before a second one exists.

## What already exists to build on

M4 is not a green field. The metadata layer is already an in-process extension
system ([ADR 0007](adr/0007-provider-and-localsource-split.md)): a `Registry`
holds capability interfaces —

- `Provider` (search + fetch identity: TMDB),
- `LocalSource` (read sidecars: NFO),
- `RatingSource` (third-party scores: OMDb, [ADR 0019](adr/0019-external-ratings.md)),
- `TrailerProvider` (optional capability).

ADR 0007's explicit promise was that "the M4 plugin runtime registers into the
same `Registry` with no downstream change." **M4 is where that promise is
tested.** The extension *shapes* are already discovered and stable; M4 changes
*where the implementation runs* (out-of-process / sandboxed) without changing the
interfaces the merge engine, enricher, and API consume.

That reframes M4 from "design an extension API" to "move a known, working
extension surface across an isolation boundary, safely and trustably."

## The four sub-areas and the decision each needs

### 1. Isolation boundary — the load-bearing decision (first ADR)

How does untrusted third-party code run without being able to read the library,
the database, secrets, or the filesystem? Two viable mechanisms:

- **WASM (via `wazero`, pure Go, no cgo).** A plugin is a `.wasm` module the host
  instantiates with an explicit, host-granted set of imports. Fits the ADR 0001
  ethos exactly — no cgo, embeds in the single static binary, one artifact per
  platform stays true. Capabilities are deny-by-default: the module can touch
  only the host functions it is handed. Cost: plugins compile to WASM (Go, Rust,
  others can), and host↔module marshalling is hand-built.
- **Subprocess RPC.** A plugin is a separate executable the host launches and
  talks to over stdio/gRPC (the Hashicorp go-plugin shape). Simpler to author —
  a plugin is just a program — and reuses the existing Go interfaces almost
  verbatim. Cost: the single-binary distribution story breaks (now there are N
  executables to ship, sign, and update), and OS-level sandboxing of a
  subprocess is the host's problem, not the mechanism's.

**Lean (not a decision):** WASM, because the single-static-binary, no-cgo,
no-runtime posture is a stated identity of this project (ADR 0001), and an
extension model that reintroduces "ship and manage separate executables" fights
that identity. But the subprocess model's authoring simplicity is real and the
ADR must weigh it honestly, ideally against the first-party plugin below.

This is the decision that constrains everything after it, so it is decided
**first and alone**.

### 2. Capability / permission model (same ADR or its immediate follow-on)

Whatever the boundary, a plugin declares what it needs (outbound HTTP to a named
host, a cache scratch space, the item fields it reads) and the host grants each
explicitly. **Plugins never touch `*sql.DB`, the filesystem, or secrets
directly** — the same rule handlers already follow (CLAUDE.md), extended across
the boundary. A rating plugin gets "call this URL and return scores", not "here
is the database."

### 3. Extension-point catalog (after the runtime exists)

*What* is extensible, and — as important — what never will be. The starting set
is the existing registry interfaces. The catalog also draws the permanent line:
the four principles (server owns truth, clients thin, everything-a-provider,
no-phone-home) are not plugin-overridable, and neither is anything that would let
a plugin exfiltrate the library or weaken the loopback-until-secured guarantee.
This is written from what the first-party plugin actually needed, not speculated.

### 4. Distribution and trust (last of the runtime work)

The roadmap names the failure mode directly: **Kodi's.** Install, update, and
trust — signing, a manifest, an explicit capability grant at install time, and a
revocation story. This is deliberately last: a trust model for a plugin format
that has not stabilized is a trust model for the wrong format.

## Validation: one first-party plugin

The runtime is not "done" when it loads a hello-world module. It is done when a
**real** capability we already ship runs as a plugin with no downstream change —
the ADR 0007 promise, demonstrated. The natural candidate is **OMDb reimplemented
as a `RatingSource` plugin**: it is small, network-only, reads a narrow slice of
item data, and needs exactly the capability model above (one outbound host, no
DB, no filesystem). If the plugin boundary can carry OMDb cleanly, it can carry a
metadata provider; if it cannot carry OMDb, we learn that before the contract is
public. (A subtitle source is the fallback candidate if a second shape is wanted.)

## Proposed order

1. **ADR — isolation boundary + capability model.** WASM vs subprocess, decided
   against the OMDb-plugin use case. The one expensive-to-retrofit decision.
2. **Runtime + first-party OMDb plugin**, proving the ADR 0007 promise end to end.
3. **Extension-point catalog**, written from what step 2 actually needed.
4. **Distribution and trust** (signing, manifest, install-time grants, revocation).
5. **Client surfaces (TV, mobile)** — a restyle if the keyboard focus model held
   ([ADR 0004](adr/0004-keyboard-focus-model.md)), and independent of the plugin
   runtime; sequenced last because it validates a different foundation.

## What must wait for the real plugin (do not pre-decide)

- The exact host-function ABI and data marshalling shape.
- Versioning of the plugin contract (distinct from the HTTP API's ADR 0018).
- Whether plugins may contribute new `kind` values, new library types, or only
  new sources for existing kinds.
- Any client-facing "add-ons" surface (the feature-backlog Add-ons page is an M4
  consumer, not a prerequisite).

## Dependencies

- **Plugin contract → one full build of the core.** Satisfied: M0–M3 and the
  browse/ratings work are shipped.
- **TV client → keyboard focus model (ADR 0004).** Already the foundation; the
  TV surface is a restyle only if that model was not compromised.
- **Isolation choice → ADR 0001 ethos.** Pure-Go, single-binary, no-cgo is the
  frame the boundary decision is made inside, not a detail to trade away casually.
