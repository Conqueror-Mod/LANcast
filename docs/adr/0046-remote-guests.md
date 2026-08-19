# ADR 0046 — Remote guests

Date: 2026-08-19 · Status: accepted

Third ADR of the work planned in
[the Watch Together federation plan](../watch-together-federation-plan.md), and
the security-critical one. Builds on
[ADR 0044](0044-server-identity-and-peering.md), which establishes that two
servers can know each other, and settles what that then permits.

## Context

Pairing grants nothing. ADR 0044 is careful about this: it establishes only that
two servers know who each other are. This ADR answers the next question — when
Georgia joins a room Chris is hosting, **what is her client talking to, and what
may it do?**

The federation plan chose the shape: she authenticates to *his* server as a
guest, rather than the two servers proxying for each other. What follows is that
decision met with three facts about this codebase, each of which constrains the
design more than the plan anticipated.

### Fact 1 — a session is a session

[`requireAuth`](../../internal/api/auth.go) is all-or-nothing. It resolves a
session, stashes it, and every non-admin route is then reachable by anybody
holding one. The single gradation in the system is `adminOnly`, and it works by
being applied to the handful of routes that need it.

That is a sound model for a household where every account belongs to somebody
you live with. It has one property that matters enormously here: **a route with
no check on it is a route everybody can reach.** Access is granted by default
and withdrawn by exception.

A weaker principal cannot be safely bolted onto that. If "guest" is expressed by
adding checks to the handlers that need them, then every handler written
afterwards — by anybody, for years — is a hole that opens silently and looks
exactly like ordinary code.

### Fact 2 — per-user state is keyed off the caller

`s.userID(r)` runs through the system: resume positions, ratings, history,
continue-watching, trending. Give a remote guest a user id and she immediately
begins **accumulating a record in Chris's database** — the exact artefact
[ADR 0035](0035-who-may-see-whose-viewing.md) identifies as the thing worth
protecting people from, now created on a machine that is not hers, about
somebody who never agreed to it.

### Fact 3 — a guest is cross-origin by construction

CSRF is defended twice, and CLAUDE.md says to keep both: `SameSite=Strict` on
the cookie, and an `Origin`/`Referer` check on every state-changing method.

Georgia's client is served by *her* server. Every request it makes to Chris's
server is cross-origin. `SameSite=Strict` means the cookie is not sent at all,
and the origin check refuses the request even if it were. **Both defences reject
a remote guest by construction** — not as a bug, but because they are working.

There is also no CORS handling anywhere in the codebase today. Nothing has ever
needed it.

## Decision

### 1. A remote guest is a principal, not an account

No `user` row, no password, no entry in the household's people list. Georgia
does not become a member of Chris's server by watching a film with him, any more
than somebody on your sofa becomes a member of your household.

This is not merely tidiness. An account is a thing that outlives the evening,
has to be listed, managed and deleted, and — under Fact 1 — carries full member
access to the entire library the moment it exists.

### 2. Admission is by signed ticket, verified against the pinned peer key

1. Georgia's client asks **her own server** for a ticket for peer Chris.
2. Her server signs one with its identity key (ADR 0044).
3. Her client presents it to **Chris's server**.
4. His server verifies the signature against the key pinned at pairing, and
   mints a restricted session.

The ticket names the remote person, and carries an **audience** — the
fingerprint of the server it is for — an issue time, a short expiry, and a
nonce. Audience binding is not optional: without it a ticket minted for Chris is
replayable against every other peer Georgia's server has. The nonce is spent on
use and remembered until it expires, in memory.

What this buys, and why it is worth the machinery over just giving her a login:

- **No password crosses.** There is none to cross.
- **No account is created**, so none has to be removed.
- **Revocation is unpairing** — one action, complete, with nothing per-person to
  remember.
- **Each server governs its own people.** Georgia's server decides who on it may
  ask for a ticket; Chris never administers Georgia's household, and his user
  list stays his household rather than a list of everyone he has ever watched a
  film with.

### 3. The guest session is default-deny, enforced in middleware

**This is the load-bearing decision of this ADR.** Given Fact 1, everything else
here is decoration if this is got wrong.

A guest session is a **distinct principal type**, gated by its own middleware
holding an explicit allow-list of routes. A route not on the list is refused. A
route added next year is refused until somebody deliberately adds it.

The property being bought is that **the guest's entire power is readable in one
place**, and that the default for all future code is closed. Expressing this as
a third value of `role` alongside `admin` and `member` was considered and
rejected for precisely the reason in Fact 1: with roles, the *absence* of a
check is an allow.

### 4. What a guest may do, exhaustively

- **Join, poll and leave** the room it was admitted to.
- **Stream** the item that room is currently playing — plus its **subtitles**
  and its **artwork**, because a player that cannot draw the thing it is playing
  is not a player.
- Read **who it is**, for its own UI.

And nothing else. No browsing, no search, no library listing, no playlists, no
people, no downloads, no settings, no metadata for anything other than the
item in the room, and nothing administrative under any circumstances.

**The item check is object-level, not route-level.** Allow-listing
`/api/stream/{id}` is not enough: that handler streams whatever id it is handed,
so a guest permitted the *route* is a guest permitted the *library*. The stream,
subtitle and artwork paths must each verify that the requested item is the one
the caller's room is playing, at request time. A room that moves to a new item
moves the permission with it; a room that ends takes it away.

The path containment rule in CLAUDE.md applies with full force on every one of
these — this is the boundary that rule was written for, and it is now a boundary
a stranger's machine is on the far side of.

### 5. A guest writes nothing

No `playback_state`, no rating, no history, no continue-watching entry, and **no
contribution to trending** — which counts accounts, and must not count somebody
else's household.

Where Georgia stopped is her own server's business. This is ADR 0035's
protection pointed in the other direction, and the symmetry is deliberate:
[ADR 0045](0045-live-presence-between-paired-servers.md) says Chris's watching
leaves no record on Georgia's server, and this says Georgia's leaves none on
Chris's.

### 6. The guest credential is a bearer token, never a cookie

Required by Fact 3, and better than a workaround. A bearer token is not ambient
authority: it is attached deliberately by the client on each request, so a
malicious page cannot cause it to be sent. **CSRF does not apply to it by
construction**, which is why this does not weaken the two defences CLAUDE.md
says to keep — those continue to guard the cookie path, untouched.

The two mechanisms stay **disjoint, and that is a rule rather than an
observation**: a cookie never authenticates a guest, and a bearer token never
authenticates a member. Either crossing would produce a path with neither
defence.

### 7. CORS is opened to paired origins only, on guest routes only

Never `*`, never to the whole API, and never to an origin that is not a
currently paired peer. Unpairing closes it.

This is genuinely new exposure in a codebase that has never sent an
`Access-Control-Allow-Origin` header, so it stays as small as it can be while
working.

### 8. A guest session dies with the room

Not the member `SessionTTL` of thirty days. The session is admitted for a room
and expires with it — when the room ends, when the guest leaves, or when the
sweep drops them for not polling.

A credential that outlives the reason it was issued is a credential nobody will
think to revoke.

### 9. No transport control

Unchanged from the existing room design, and the reasoning holds harder over a
WAN: two people scrubbing the same film is not synchronised playback, it is a
fight, and the loser cannot tell it from a bug.

## Alternatives considered

**Give Georgia an account on Chris's server.** This is what testing does today,
and it is the obvious answer. Rejected on four counts: under Fact 1 it grants
her the entire library, browse and search included; it needs a password to
create, transmit and manage; it leaves a row behind that outlives the evening
and that somebody must remember to delete; and it turns the household's people
list into a list of everyone Chris has ever watched something with.

**Redirect her into his client, so she is same-origin.** Genuinely tempting: the
cookie works, CSRF is untouched, and CORS never enters the picture. Rejected
because the ticket then travels in a URL — and therefore into browser history,
server logs and referrers — and because she ends up in a UI whose version she
did not choose. It also sits badly with *clients are thin, one documented API*:
if a first-party client cannot do this over the API, no third-party client can
either. **This is the fallback if CORS proves miserable in practice**, and the
retreat is not expensive.

**Server-to-server proxy — her server fetches from his and re-serves it.**
Already declined in the plan; restated because it will be raised again. It
doubles the bandwidth, doubles the failure modes, and makes an ffmpeg session on
Chris's machine belong to *a machine* rather than to a person, which is the
wrong unit for both the session cap and the audit trail.

**A `guest` role alongside `admin` and `member`.** See §3. Roles in this system
gradate by exception, and a principal that must be limited cannot be expressed
in a mechanism whose default is permission.

## Consequences

**Good — the guest's entire authority fits on one screen.** An allow-list in one
middleware is reviewable in a way that "the absence of checks across sixty
handlers" is not, and it stays reviewable as the API grows.

**Good — revocation is one action and it is complete.** Unpair: the pinned key
is gone, no ticket verifies, no session mints, CORS closes, and there is nothing
left behind to find.

**Good — nobody accumulates a record on anybody else's machine**, in either
direction.

**Cost — a second authentication path.** Two mechanisms are more to get wrong
than one. Mitigated by their being disjoint by rule (§6) and by the guest path
being default-deny, so the failure mode of forgetting something is a guest who
cannot do a thing rather than a guest who can do everything.

**Cost — CORS where there was none.** Bounded to paired origins and guest
routes, and it is the price of Georgia keeping her own client.

**Cost — somebody else can start ffmpeg on your machine.** Intended, and it
needs a ceiling. The cap belongs to ADR 0047 and is named here so it is not
assumed to be somebody else's problem.

**Cost — version skew is now real.** Her client, his server, versions chosen
independently. [ADR 0018](0018-api-contract-and-versioning.md) is what makes
this survivable — the guest routes are new endpoints, which it classes as
additive and non-breaking — but the guest path is the first place where a
third-party client contract stops being hypothetical.

## What this does not decide

**Bandwidth, quality, or the concurrent-session cap.** ADR 0047.

**What a peer may see about people.** ADR 0045.

**Whether a guest may ever browse a shared library.** Deliberately forbidden
here. Sharing a library is a much larger question than watching one film
together, and answering it as a side effect of this ADR would be smuggling it
in.

## Revisit when

Somebody wants a guest to choose what to watch rather than join what is already
playing, a guest needs to be in two rooms, or the object-level item check proves
too tight for something legitimate — in which case the answer is a wider
*object* scope, never a wider route list.
