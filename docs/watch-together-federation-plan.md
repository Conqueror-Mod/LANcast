# Watch Together across two servers — plan

Status: **planning**. Nothing here is built. Written 2026-08-19.

## The goal, stated as a test

> Georgia's LANcast instance and Chris's can each see that the other is online.
> Georgia opens the People tab, sees that Chris is watching a film, and joins
> him with Watch Together.

That sentence is one feature to a user and five to this codebase. The point of
this document is to say which five, in which order, and which of them cannot be
started until a decision is written down.

## What exists today

Watch Together is built and works — **inside one server**.
[`internal/together`](../internal/together/together.go) holds rooms in memory:
what is playing, the position, whether it is paused, who is in it. Clients poll
and converge. One host drives; the host leaving ends the room. Members are
sweep-expired when they stop polling, because nobody presses leave, they close
the laptop.

Every one of those members is a **local user account row**. There is no concept
of another server, no server identity, no peer, and no presence. The People page
says so in its own header comment: *"there is no directory to search and no
second server to federate with."*

So the existing room is not the thing to rewrite. It is close to correct
already, and most of this plan is the scaffolding that lets a second server's
person become a member of it.

## The four decisions taken

Settled in planning, 2026-08-19. Each becomes an ADR before its phase starts.

**1. The servers reach each other over an overlay network you own** — Tailscale
or WireGuard. A peer address is just an address on your tailnet.

This is not a compromise, it is the README's existing promise: *"Remote access
is opt-in and self-owned (WireGuard, Tailscale, your own reverse proxy) — never
a relay you rent."* The alternative, LANcast punching its own NAT holes,
requires a rendezvous server, and a rendezvous server is phone-home wearing a
different hat. Port-forwarding is rejected for the reason
[security.md](security.md) already gives.

The cost is real and should be said plainly: **the feature does not work for
someone unwilling to run a VPN.** That is the price of the fourth principle, and
it is the right price.

**2. A remote person authenticates to the host's server as a guest**, rather
than the two servers proxying for each other. Her client talks to his server
directly. This reuses the room, the player, the streaming stack and the session
machinery whole. True server-to-server federation is cleaner in the abstract and
is roughly triple the work, with double the failure modes.

**3. The bytes come from the host's copy**, streamed over the WAN.

The cheaper design — sync the room only, each side plays its own local file — is
genuinely attractive and is written down here so it is not reinvented later as a
novelty. It was not chosen because it works only when both people own the same
film, and "the same film" is a matching problem with cuts, runtimes and editions
in it. Revisit if WAN streaming proves too painful in practice.

**4. Presence reveals that a peer is online and what they are watching**, by
name of title. This one requires amending an accepted ADR — see below.

**5. Presence is granted to a person, not to a server.** "Georgia may see me",
not "anybody on Georgia's server may see me". This matches ADR 0035, where the
unit of consent is a person throughout, and it is the answer that stays correct
when her server grows a second account.

It costs an ordering change, recorded here because it is easy to get wrong: a
grant naming Georgia requires Georgia to *exist* on this server, and the obvious
reading puts remote people in Phase 4 with the authentication that lets them in.
So the two are split. **Phase 2 establishes who exists; Phase 4 establishes who
may get in.** Pairing exchanges a roster of the accounts on each side that have
chosen to take part, each stored locally as a remote person with a stable id.
Presence consent in Phase 3 is then granted against a row that is already there,
and Phase 4 adds only ticketing and sessions — not the existence of the people
it authenticates.

The roster exchange is itself a disclosure ("these are the accounts on my
server"), so it takes its own per-account opt-in, defaulting off, in the same
spirit as everything else here. A person who has not opted in is not in the
roster and cannot be named by anybody's grant.

## The blocker nobody had flagged

**[ADR 0035](adr/0035-who-may-see-whose-viewing.md) does not permit this
feature.** Not "does not cover" — does not permit.

0035 decided that viewing is private by default and shared only by explicit
opt-in, and it bounded exactly what the opt-in shares: *what you have watched
and finished — titles, and when.* It then names what sharing deliberately does
**not** include, and the second item is:

> **resume positions**, which say where you stopped rather than what you
> watched, and are useful to nobody else.

"Chris is watching Blade Runner right now" is a strictly stronger disclosure
than a resume position. It is live, it is continuous, and it is precisely the
intimate record 0035's context section argues for being conservative about. It
is also being disclosed **to another server**, which 0035 never contemplated
because no second server existed.

This is a decision to be made deliberately, not a column to add. **The ADR comes
before the code**, and Phase 3 does not start until it is written and accepted.

What the amendment has to settle, at minimum:

- Presence is a **third disclosure category**, not an extension of
  `share_activity`. Somebody who agreed to publish their finished films did not
  thereby agree to be watched in real time, and silently widening an existing
  opt-in is the failure 0035 exists to prevent.
- It is **per account and per peer**. "Georgia may see me" is a different
  sentence from "anybody my server is paired with may see me", and a household
  server will eventually have both.
- It is **bounded**: online, and a title. Not the position, not the history, not
  what was abandoned. A presence feed that leaks a resume position has
  reintroduced the thing 0035 excluded by name.
- **Off by default, and retroactive off** — the same two rules 0035 already
  established, for the same reasons.
- Presence is **never persisted**. The same argument the rooms make for
  themselves: a presence record that survives a restart is a statement about the
  present that is false.

## Phases

Ordered so that each one is provable on its own, and so the risky,
security-critical work happens after the cheap work has de-risked the shape.

### Phase 1 — Server identity

**Small. No dependencies. Fully unit-testable.**

A server gets an Ed25519 keypair on first run, persisted `0600` under
`<data>/identity/`, stable across restarts. The public key's fingerprint is the
server's identity, rendered as a short grouped human-readable string — the thing
you read down the phone.

Precedent already in the tree:
[`internal/certpin`](../internal/certpin/certpin.go) establishes key-pinning as
this project's trust primitive, and its reasoning ("not *ignore certificate
errors*, which would accept anything on the LAN pretending to be the server —
the exact attack TLS is here to stop") is the reasoning federation needs too.
Federation pins peers the way the desktop window already pins its own server.

- `GET /api/identity` → fingerprint, display name. No peer concept yet.
- ADR 0044 — server identity and peering.

### Phase 2 — Peers: pairing and reachability

**Medium-large. The first half of the stated test lands here.**

- Schema revision 27: a `peer` table — fingerprint, display name, address, when
  added, state — and a `remote_person` table, rows owned by a peer, each with a
  stable id and display name.
- **Per-account `visible_to_peers`, default off.** Being listed in the roster
  the two servers exchange is a disclosure and gets its own opt-in. It is also
  the precondition for anybody granting you anything: an invisible account
  cannot be named.
- **Pairing is an out-of-band invite blob**: a copyable string carrying
  fingerprint, name and address. Georgia pastes Chris's; Chris pastes hers.
  Syncthing's model, and it is the only introduction mechanism compatible with
  having no directory to phone.
- **Mutual by construction.** Both sides add each other or there is no peer. A
  one-way follow is a thing to be hijacked.
- Server-to-server calls are mTLS with **the peer's key pinned**. Every other
  certificate validates normally.
- A reachability poll, so "online" is a fact rather than an assumption.
- A roster exchange on pair and on refresh, populating `remote_person`. This is
  what lets Phase 3 grant presence to a named person rather than to a machine.
- Routes: list, add-by-invite, remove. Removal is the revocation mechanism for
  everything in later phases, so it has to be complete rather than cosmetic.

> **Test milestone:** Georgia's server appears in Chris's peer list, marked
> online, and his in hers. This is half the goal, and it is reachable without
> touching presence, auth or streaming.

### Phase 3 — Presence

**Medium. Gated on the ADR 0035 amendment. Do not start it early.**

- Schema revision 28: the grant table — one row per (local account, remote
  person) pair. **Not a `share_presence` boolean.** A single switch is a grant
  to everybody, which is the per-server answer this project explicitly did not
  choose, wearing a per-account disguise.
- An in-memory active-playback tracker with a sweep, exactly parallel to the
  room's member sweep — and see the traps below, because this is a bug this
  project has already shipped once.
- A server-to-server presence endpoint, returning only what the amendment
  permits.
- The People page gains a peers section. It must distinguish *offline*, *online
  and not sharing presence*, and *online and idle* — the same discipline the
  page already applies to `Not sharing`, which states a choice rather than an
  absence.

> **Test milestone:** Georgia sees "Chris is watching *Blade Runner*."

### Phase 4 — Remote guest authentication

**Large, and the security-critical phase. Budget review time, not just build
time.**

The flow, and the reason it is worth the complexity:

1. Georgia's client asks **her own server** for a ticket for peer *Chris*.
2. Her server signs a short-lived ticket with its identity key.
3. Her client presents the ticket to **Chris's server**.
4. Chris's server verifies the signature against the pinned peer key and mints a
   short-lived, **restricted** session.

**No password crosses. No account is created on Chris's server. Unpairing
revokes everything, immediately, with no per-person cleanup.**

The restricted session is the security boundary and must stay small enough to
review: it may join a room it was invited to, and stream *the item that room is
playing*. It may not browse, may not search, may not list libraries, may not
read anybody's history. That is a far smaller surface than a real account, and
it is what makes this phase defensible rather than merely convenient.

The path containment rule in [CLAUDE.md](../CLAUDE.md) applies with full force
here — every handler turning a row into a filesystem path re-verifies
containment. A remote principal is exactly the boundary that rule was written
for.

- ADR 0046 — remote guests.

### Phase 5 — The room crosses the boundary

**Medium. Mostly extension, little invention.**

- `together.Member` carries where it came from. Names are already frozen at join
  time (the audit-log pattern), so a remote member displays correctly without
  reaching across the network to render a list.
- Room visibility to a paired peer is the **host's** opt-in, separate again from
  presence — being seen to watch something is not the same as offering to be
  joined.
- Join from the peers section of People.
- **The host still drives, and a remote guest never gets transport control.**
  Unchanged, and the existing reasoning holds harder across a WAN: two people
  scrubbing the same film is not synchronised playback, it is a fight, and the
  loser cannot tell it from a bug.

### Phase 6 — Remote streaming

**Smaller than medium, once ADR 0047 was written — see below. A hardware
reality is still attached.**

- [ADR 0047](adr/0047-remote-streaming-is-capped-by-the-host.md) — remote
  streaming is capped by the host. **Written, and it shrinks this phase**: ADR
  0031 already built the ceiling mechanism, so the cap is expressed in the
  profile and `decide.go` does not change at all. Mostly configuration.
- The direct-play/remux/transcode decision gains a *remote* input. Today it
  reasons about codec and container only, which over a WAN will cheerfully
  choose to direct-play a 40 Mbps remux down a domestic uplink.
- **Keep `probe.ParseJSON` and the decision function pure.** CLAUDE.md is
  explicit about this, and it is what lets every WAN case be a fixture test in
  milliseconds instead of a real encode.
- A concurrent-remote-session cap. A paired peer can now start ffmpeg on your
  machine; that is fine and intended, and it still needs a ceiling.

## Traps, named in advance

**1. The sweep-before-record bug will recur in presence.** The roadmap records
it: *"A presence check that runs before recording presence deletes the
punctual."* Watch Together swept members before recording the caller's poll, so
a host polling exactly on the interval was judged absent and took down their own
room mid-film for being on time. Presence tracking is the same shape and will
invite the same mistake. Write the fake-clock test first.

**2. Clock skew between two machines.** The room's position maths uses
`UpdatedAt` to work out how far the film has moved since the host last reported.
That is safe today because one server owns the clock. Across two servers it is
not. **The host's server stays the sole clock** — never compute a position from
a guest's local time. This one is silent when it breaks: everybody just drifts.

**3. You cannot test this on one machine.** LANcast holds a single-instance lock
per machine, and a different port or data directory does not help. **Resolved:
a second machine runs the other instance.** No dev-build relaxation of the lock
is planned, and none should be added quietly — the lock is a real invariant and
a build tag that disables it is a build tag that eventually ships.

**4. Testing needs Georgia.** Phases 1 and 2 are the only ones fully provable
solo. Everything from Phase 3 needs a second person on a second network at the
same time as you. Sequence accordingly, and batch what needs her.

**5. `docs/api.md` moves in the same commit.** Federation adds a lot of routes,
and CLAUDE.md names that doc drifting from the handlers as the most damaging
documentation failure in this project.

**6. `GOOS=linux go vet ./...` before pushing** anything that touches a
`_windows.go` file. CI builds on Linux.

## What this plan does not decide

**Whether a peer can be a server you do not control.** Everything above assumes
two people who know each other and exchanged a fingerprint deliberately. A
public directory of LANcast servers is a different product.

**Whether libraries are ever browsable across peers.** The restricted session in
Phase 4 deliberately forbids it. Sharing a library is a much larger question
than watching one film together, and answering it here would smuggle it in.

**Whether more than two peers work.** Nothing above prevents it; nothing above
has been thought through for it either.
