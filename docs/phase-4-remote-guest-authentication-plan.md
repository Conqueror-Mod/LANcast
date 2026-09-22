# Phase 4 — Remote guest authentication: implementation plan

Date: 2026-09-21 · Status: plan, not yet begun

The [federation plan](watch-together-federation-plan.md) calls this phase
*"Large, and the security-critical phase. Budget review time, not just build
time."* This document is that budget: what gets built, in what order, what each
piece must refuse, and what is deliberately left out.

Two accepted ADRs specify it and neither one is complete on its own:

- [ADR 0046](adr/0046-remote-guests.md) — the guest: admitted to **a room**,
  may stream **the item that room is playing**, writes nothing, dies with the
  room.
- [ADR 0071](adr/0071-a-shared-library-is-a-standing-grant.md) — the friend:
  admitted to **shared libraries**, may browse and search within them, and has
  no room to derive anything from.

They share a credential and differ in what it buys. Building the credential
once, for both, is the point of doing this as one phase.

## What already exists

Phases 1–3 are built. Worth stating precisely, because the gap is narrower than
"authentication is missing":

- **Server identity** ([ADR 0044](adr/0044-server-identity-and-peering.md)): an
  Ed25519 key per server, never regenerated, exposed as a `crypto.Signer`.
- **Pairing**: `peer`, `peer_address`, `remote_person`, and a mutual-TLS
  channel on the same port, separated by the `lancast-peer/1` ALPN. A peer
  proves which *server* it is by presenting the key pinned at pairing.
- **Presence** ([ADR 0045](adr/0045-live-presence-between-paired-servers.md)):
  the first federated endpoint, authenticated by that pin, with *which person
  is asking* taken as the far server's word.
- **The grant** ([ADR 0071](adr/0071-a-shared-library-is-a-standing-grant.md)
  phases 1–2): `library_share`, and a ceiling resolver that fails closed for a
  principal with no account.

What is missing is a credential a **client** can carry to a server its own
household does not run. The mTLS channel authenticates server-to-server; it
cannot authenticate Georgia's browser to Chris's server.

## Why not proxy through the friend's own server

It would work, and it needs no new credential: Georgia's client talks only to
her server, which fetches from Chris's over the existing pinned channel, as
presence already does.

It is rejected for one reason, and it is about video rather than architecture.
A proxied stream crosses Georgia's server on its way to Georgia's screen —
twice the bandwidth, on the leg most likely to be a domestic uplink, for every
remote viewing. [ADR 0047](adr/0047-remote-streaming-is-capped-by-the-host.md)
caps what a host serves precisely because that leg is scarce.

Recording it here so the option is visibly declined rather than never
considered.

## 1. The ticket

### Claims

Minted by the friend's server, signed with its identity key.

| claim | meaning | why it is not optional |
|---|---|---|
| `iss` | issuer fingerprint | which pinned key verifies it |
| `sub` | the person, as that server's own account id | the same id already in the host's `remote_person`, so it joins |
| `aud` | audience fingerprint — the host | without it, a ticket for Chris replays against every peer she has |
| `iat` | issued at | skew window, and audit |
| `exp` | expiry, short | bounds replay to the nonce window |
| `jti` | nonce | spent on use, remembered until `exp` |

**It does not name what it may reach.** No library ids, no room id, no
permissions. The host resolves what this person's server was granted, from its
own `library_share` rows. A ticket that carried its own permissions would be a
capability the issuer writes and the host honours, which is the opposite of the
trust direction everywhere else here.

### Encoding

Deterministic — the bytes signed must be reconstructible byte-for-byte by the
verifier. A canonical concatenation of fields with explicit lengths, not JSON:
two JSON encoders disagree about key order and whitespace, and a signature over
"whatever the encoder produced" is a signature over something nobody can
re-derive.

Domain-separated with a fixed prefix (`lancast-guest-ticket/1`), so a signature
from this key can never be replayed as a signature for anything else the same
key signs — the identity key also signs TLS certificates.

### Verification, in order

1. Parse. Reject anything malformed without saying which field.
2. `aud` equals **this server's** fingerprint. Reject otherwise.
3. `iss` is a **paired** peer. Unknown key → refuse; this is also what makes
   unpairing revoke immediately.
4. Signature verifies against the key pinned at pairing.
5. `exp` is in the future and `iat` is not far in the future — a small skew
   allowance, named as a constant, not a magic number.
6. `jti` has not been spent. Record it until `exp`.

Every refusal is the same refusal from outside. A verifier that distinguishes
"bad signature" from "expired" from "replayed" is an oracle.

### The nonce store

In memory, keyed by `jti`, swept on expiry. Bounded, and the bound is a
decision: a peer that floods nonces must not grow the host's memory without
limit. Over the bound, refuse rather than evict — evicting the oldest makes
replay possible again, which is the one thing this store exists to stop.

Lost on restart, which is correct: every outstanding ticket expires in minutes,
and the alternative is a durable table of credentials.

## 2. The session

Verification mints a **restricted session**, and the two kinds differ only in
what they carry:

- **Guest** (ADR 0046): the room id it was admitted to. Dies with the room.
- **Friend** (ADR 0071): the peer fingerprint. Its reach is resolved per
  request from `library_share`, so un-sharing takes effect on the next request
  with nothing to invalidate.

A friend session has **no room to die with**, which ADR 0046 never had to
answer. It therefore needs its own lifetime, and the plan is a short expiry
with re-presentation of a fresh ticket — the client already has to be able to
obtain one, so renewal is not new machinery. **This is the one design question
neither ADR settles, and it should be decided before the session type is
written**, not discovered while wiring it.

The credential is a **bearer token, never a cookie** (ADR 0046 §6): a guest is
cross-origin by construction, and a cookie that works cross-origin is a cookie
with `SameSite=None`, which is exactly the property the host's own CSRF
defence depends on not having.

## 3. Default-deny middleware

**The load-bearing decision** (ADR 0046 §3). A restricted session is a distinct
principal type behind its own middleware with an explicit allow-list. A route
not on the list is refused; a route added next year is refused until somebody
adds it deliberately.

Not a third value of `role`. With roles, the *absence* of a check is an allow,
and there are hundreds of handlers.

**Object-level, never route-level.** Allow-listing `/api/stream/{id}` permits
the library, because that handler streams whatever id it is handed. Each of
stream, subtitles and artwork verifies the *object* at request time:

- guest → is this the item the room is playing, now
- friend → is this item in a library shared with this peer, now

`Store.MayPlay` with a `Friend` principal already answers the second correctly
and fails closed, which is what phase 2 of ADR 0071 was for.

Every handler turning a row into a filesystem path re-verifies containment
within the owning library root. `CLAUDE.md` names this the boundary that rule
was written for, and a remote principal is that boundary with somebody else's
machine behind it.

## 4. Sequencing

Each step is reviewable on its own and lands as its own commit.

1. **`internal/guestticket`** — mint, encode, verify. Pure, no HTTP, no store.
   Table-driven tests: wrong audience, unknown issuer, bad signature, expired,
   not-yet-valid beyond skew, replayed nonce, malformed, truncated, and a
   signature from the right key over a *different* domain prefix.
2. **The nonce store** — bounded, swept, with a test that the bound refuses
   rather than evicts.
3. **The mint endpoint**, on the friend's own server: which of *its* people may
   ask, for which peer. Authenticated as an ordinary session.
4. **The redeem endpoint**, on the host: ticket in, restricted session out.
5. **The middleware and the allow-list**, with the list in one file and a test
   that enumerates it — so the guest's entire power stays readable in one
   place, which is the property ADR 0046 §3 is buying.
6. **Object-level checks** on stream, subtitles and artwork, each with a test
   that a valid session for item A is refused item B.
7. **CORS to paired origins only, on guest routes only** (ADR 0046 §7).

`docs/api.md` and `docs/openapi.json` change in the same commits as the
handlers. Both are enforced.

## 5. What must be true before this is called done

- A ticket for one peer is refused by every other peer.
- A replayed ticket is refused.
- An expired ticket is refused, and the clock skew allowance is a named
  constant with a test at its edges.
- Unpairing refuses the next request, with nothing per-person cleaned up.
- A guest session for room A cannot stream an item in room B.
- A friend session cannot reach an item in a library that was not shared, and
  cannot reach anything administrative, anybody's history, or playlists.
- A friend under a share ceiling is refused an item above it **and** refused a
  room playing one (ADR 0071 §6 — otherwise the room is a complete bypass).
- Nothing in the restricted paths writes to the host's database.
- The allow-list test fails when a route is added without being considered.

## 6. Deliberately out of scope

- **Phase 5**, the room crossing the boundary. This phase admits a principal;
  making rooms federated is separate.
- **The sharing UI** (ADR 0071 §7). It is host-side, needs no ticket, and can
  be built before or after this.
- **Discovery.** How Georgia's client learns Chris's address is pairing's job
  and already done.
- **Anything durable about a remote person.** No rows, by design; that is what
  makes unpairing complete.
