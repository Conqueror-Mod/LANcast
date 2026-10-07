# Phase 5 — The room crosses the boundary: implementation plan

Date: 2026-10-07 · Status: plan, not yet begun

The [federation plan](watch-together-federation-plan.md) calls this phase
*"Medium. Mostly extension, little invention."* That still holds for the room.
It does not hold for **how a friend's player reaches the room**, which no
document answers correctly. That question is settled first, below, because
everything after it depends on it.

## The goal, stated as a test

> Georgia opens People, sees "Chris is watching *Blade Runner*", and presses
> **Ask to join**. Chris's player shows *Georgia would like to join*. He
> accepts. Georgia's player opens on *Blade Runner* at Chris's position and
> follows him when he pauses or seeks. When Chris stops, her player says the
> room has ended.

## What already exists

Phases 1–4 are built, and so is most of Phase 6, ahead of order:

- **Rooms** ([`internal/together`](../internal/together/together.go)): in
  memory, host drives, followers poll, the sweep records the caller first.
- **Presence** ([ADR 0045](adr/0045-live-presence-between-paired-servers.md)):
  a per-person grant, and `/api/federation/presence` answers what a named
  person on a paired server may see. The person is the asking server's word,
  carried as `?person=`.
- **A friend plays a host's film through their own server**
  ([ADR 0071](adr/0071-a-shared-library-is-a-standing-grant.md), amended).
  `/api/peers/{fp}/…` on the friend's side pipes to `/api/federation/…` on the
  host, over mutual TLS with the host's key pinned. Every playback route on
  the host goes through `federationPlay`, which asks `MayPlay(store.Friend(fp))`
  and fails closed.
- **The guest ticket** (Phase 4, ADR 0046): a bearer token, a default-deny
  allow-list in `guestgate.go`, and CORS for paired origins.

## The decision this phase needs first: a room is relayed, not ticketed

ADR 0071's amendment ends: *"Phase 5, the room crossing the boundary. A room
needs the host's own timing, and that is where the ticket earns itself."*

**That is wrong, for the reason the same amendment gives.** The ticket assumes
Georgia's player can reach Chris's server. It cannot. Her window pins exactly
one key, her own server's (ADR 0070), so a call to Chris's server fails the
TLS handshake with `ERR_CERT_AUTHORITY_INVALID`. That is why browsing and
streaming were moved onto a relay. A room's poll is an HTTPS request from the
same window to the same unreachable server, so it fails the same way.

So the room uses the same route as the film:

    Georgia's player → Georgia's server → (mutual TLS, pinned) → Chris's server

**The timing argument does not need a direct connection.** What it needs is for
**the host's server to remain the only clock** (federation plan, trap 2). That
is achieved by the host stating the room's age, not by shortening the path; see
[Clock](#clock) below. A relay adds one LAN hop and one proxy turn to a poll
whose tolerance is 1.5 s.

This needs an **amendment to ADR 0046**, written alongside this plan: the
ticket, the bearer token and CORS (§2, §6, §7) remain for a browser opened
directly at a host, and the desktop client never uses them. §4, §5, §8 and §9,
which say what a guest may do, apply unchanged to the relayed path. They
describe the principal, not the transport.

## Decisions

### 1. A remote member is `(peer, person)`, in that server's word

The same standing as presence: the caller proves which **server** it is with
the pinned key, and which **person** is asking is that server's statement.
Inside `together`, a remote member's id is `peer:<fingerprint>/<person>`. That
string cannot collide with a local user id (`u_…`), and the room needs no
second map. `Member` gains `Peer` (a fingerprint, or empty) and `Server` (the
peer's display name, frozen at join like `Name`), so a member list reads
"Georgia · Utopia" without a call across the network to draw it.

### 2. Who may ask: only someone the host already lets see them

Asking requires a **presence grant from the host to that person** (ADR 0045
§7). That grant is checked on the host's server **when the request arrives**,
using `ReadersOf`, never cached on the friend's side. Revoking presence revokes
the right to ask on the next request. Unpairing revokes everything, because
the peer channel stops authenticating.

What is asked is **"may I join you"**, addressed to a person, not to a room id.
The asker saw a title, not a room. The host may not be in a room at all, just
watching on their own. That is the common case, and making the host open a
room first would add a step nobody would think to take.

### 3. A request lives in memory, and silence is a no

On the host's server, beside the rooms and never persisted. Each request has:
asker `(peer, person)`, the asker's frozen name and server name, the host's
user id, a created time, and a state of `pending`, `accepted` (with the room
id) or `declined`.

- **Unanswered after 60 seconds is declined** (ADR 0045 §7: a host who is
  asleep has not agreed to anything).
- **The asker only ever sees "not now".** A decline and a timeout look the same
  to them, and neither gives a reason.
- **One pending request per asker and host.** Asking again replaces the earlier
  one rather than stacking prompts.
- **A short cooldown after a decline** (two minutes). Without it, "not now" is
  a prompt that reappears until the host gives in. The cooldown is invisible to
  the asker beyond the same "not now".
- The sweep that expires requests records the caller first, the same ordering
  rule `Poll` keeps. Write the fake-clock test before the code (federation plan,
  trap 1).

### 4. Accepting is the host's client acting on its own server

The host's client polls `GET /api/together/requests` while anything is
playing, and shows a prompt with **Accept** and **Not now**. A host may be on
any screen while a film plays in the docked player, so the prompt cannot live
only inside the full player.

Accept is two existing steps and one new one. If the host is not in a room,
the client opens one around what is playing (`POST /api/together`, unchanged),
then answers `POST /api/together/requests/{id}/accept` with the room id. The
server adds the remote member to the room **only then**. Nobody arrives in a
room because the room happened to be open.

### 5. Being in the room is what lets the guest play its film

ADR 0046 §4 lets a guest stream *"the item that room is currently playing"*,
whether or not its library is shared. The relayed path has to honour that, or
the feature only works for films that were already shared, which is a
different feature.

`federationPlay` therefore authorises if **either** `MayPlay(Friend(fp))`
passes, as today, **or** a live room on this server has `(fp, person)` as a
member and is playing that item. The second test is made per request against
the room, so it ends when the room ends, the host moves to another item, or
the sweep drops the guest. That is §8, "dies with the room", with nothing to
revoke.

This means the peer playback pipes must carry `person`, which today they do
not. They currently authorise the **server**, which is correct for a shared
library and too broad for a room. A request without `person` keeps today's
behaviour exactly, so the shared-library path does not change.

The object check stays where `guestgate.go` insists it lives: in the wrapper,
never in a handler.

### 6. Clock

Today a follower computes `position + (client now − updated_at)`, using **its
own** clock against **the server's** `updated_at`. That works on one LAN
because NTP keeps the two close. Across two households it is the drift trap
stated in advance, and it does nothing to warn you when it breaks.

The room snapshot gains `age_ms`: the **host's server** subtracts `updated_at`
from its own `now` at the moment it answers. The client takes the time it
**received** the poll and subtracts `age_ms`. No client clock is compared with
any server's clock. The relay passes the field through untouched. Its delay is
counted as part of the age, which errs late by a few milliseconds, well inside
the 1.5 s tolerance. `updated_at` stays in the response, because removing it
would break third-party clients.

This also fixes the same latent skew for local followers, at no extra cost.

### 7. The guest writes nothing on the host

ADR 0046 §5 is unchanged. Georgia's progress through the film is recorded on
**her** server, by the `PUT /api/peers/{fp}/progress/{item}` path that already
exists for shared films. The host's room keeps her `LastSeen` and nothing
about her outlives the room.

### 8. Out of scope, and refused honestly

- **The host watching somebody else's film.** Presence names a film on a third
  server since ADR 0045's amendment 10. A room around it would mean the host's
  server relaying a stream it does not own to a server that may not be paired
  with the owner. Asking is refused with the usual "not now". The People page
  does not offer **Ask to join** when the title is not the host's own.
- **Music.** Presence excludes it (ADR 0045), so there is nothing to ask about.
- **A guest in two rooms**, or **transport control**: both unchanged (ADR 0046
  §9).

## Routes

Every route under `/api/federation/` is added to `peerAuthenticated`
(`internal/api/auth.go`) in the same change. Handler tests cannot see the
gate, so a route missing from it answers a peer with "sign in to continue".
`docs/api.md` and `docs/openapi.json` are updated in each PR that adds a route.

**On the host, called by the friend's server** (mutual TLS, `?person=`):

| Method | Path | Does |
|---|---|---|
| POST | `/api/federation/together/requests` | Ask to join a named host person. Refused without a presence grant. |
| GET | `/api/federation/together/requests/{id}` | `pending`, `accepted` + room, or `not_now`. |
| POST | `/api/federation/together/{room}/join` | Accepted members only. |
| GET | `/api/federation/together/{room}` | Poll: records the member and returns the snapshot with `age_ms`. |
| DELETE | `/api/federation/together/{room}/members/me` | Leave. |

**On the host, called by the host's own client** (session cookie):

| Method | Path | Does |
|---|---|---|
| GET | `/api/together/requests` | Pending requests addressed to the caller. |
| POST | `/api/together/requests/{id}/accept` | `{room_id}`; must be the caller's room. |
| POST | `/api/together/requests/{id}/decline` | Silent to the asker beyond "not now". |

**On the friend's server, called by the friend's client** (session cookie;
relays, deciding nothing):

| Method | Path |
|---|---|
| POST | `/api/peers/{fp}/together/requests` (`{person}`: the host person on that server) |
| GET | `/api/peers/{fp}/together/requests/{id}` |
| POST | `/api/peers/{fp}/together/{room}/join` |
| GET | `/api/peers/{fp}/together/{room}` |
| DELETE | `/api/peers/{fp}/together/{room}/members/me` |

The friend's server supplies `person` from the caller's own session. It never
takes the asking person's id from the client. Otherwise one member of a
household could ask on another member's behalf.

## Build order

Each step is its own PR and each one is provable before the next.

1. **This plan, and the ADR 0046 amendment.** Docs only.
2. **`together`, pure.** `Member.Peer`/`Server`, `age_ms` in the snapshot, the
   request store with its expiry, cooldown and replace rules. Fake-clock tests
   first: a request answered at exactly 60 s, a poll at the sweep boundary, a
   repeated ask replacing the earlier one. The client's `expectedPosition`
   moves to `age_ms` in the same PR, with its tests.
3. **The host's routes.** Federation and local request routes, `peerAuthenticated`,
   and room-scoped admission in `federationPlay`. Tests for each refusal: no
   grant, grant revoked between ask and join, a member requesting an item the
   room is not playing, a room that has ended, an unpaired peer, a missing
   `person`. Then the shared-library path with no `person`, unchanged.
4. **The friend's relay routes**, plus `person` on the existing playback pipes.
5. **The client.** **Ask to join** on People, with a waiting state and
   "not now". The host's prompt. The follower opens a peer item from a room.
   Remote members are shown with their server in `TogetherPanel`. Run
   `npm run build` and commit `dist`.
6. **Watched with Georgia.** A tier 1 test (two machines, one LAN, addressed
   directly), as the installed service on both. This is the only step that
   needs her, so it comes last and as one session.

## What a LAN test will not prove

Taken from the federation plan's testing topology, and stated here so it is
not assumed later. Convergence under real latency and jitter, and how a seek
stalls when the host's seek restarts a remote transcode, are claims about
**the link**. Both need a third party on a different network with the overlay
in the path. Phase 5 is complete when the tier 1 test passes. The tier 2
caveats go into the release notes as unproven, not dropped.
