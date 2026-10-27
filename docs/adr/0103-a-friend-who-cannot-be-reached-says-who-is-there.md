# ADR 0103 — A friend who cannot be reached says who is there

Date: 2026-10-27 · Status: **accepted** · Amends [ADR 0044](0044-server-identity-and-peering.md) §3 and [ADR 0079](0079-the-server-says-when-something-changed.md) §5

## Context

Joining a friend's film needs only calls from the friend's server to the
host's: browse, play, ask to join, follow the room. One call goes the other
way, and it is the one that matters. The host's server fetches the friend's
**roster** (`GET /api/federation/roster`), and that fetch is the only thing
that:

- fills `remote_person`, without which the host cannot tick **Let them see
  me** for anybody there (ADR 0045), so the friend never sees the host
  watching and never gets **Ask to join**; and
- promotes the pairing from `added` to `paired` (ADR 0044 §3).

So a friend's server had to be reachable from the host's. In practice that is
port forwarding or joining the host's Tailscale network, the step of the join
guide that loses people. [docs/lighter-join-plan.md](../lighter-join-plan.md)
weighed five ways out and recommended this one.

## Decision

**A server sends its roster to its peers, as well as answering for it.**
`PUT /api/federation/roster` takes `{people, as_of}` from a paired server,
authenticated by the mutual-TLS pin like every federation route, and the
receiving server stores it exactly as it stores a fetched one
(`ReplaceRemotePeople`, wholesale, so an opt-out still takes back what it
gave and its grants cascade away, as ADR 0035 requires).

**Receiving one promotes the pairing.** A server only calls servers it holds,
so a push proves the far side holds us, the same proof ADR 0079 §5 already
accepts from any call-in. It is a stronger proof than the kick that rule
triggers, because the kick then has to reach back, and for an unreachable
friend it cannot.

**When it is sent.** The peer watcher (ADR 0079 §5) already looks at every
peer every 10 s. It now also pushes to each peer when:

- the roster differs from the last one that peer accepted (somebody opted in
  or out, renamed, or was removed);
- 5 minutes have passed, the fetch's own schedule; or
- the peer refused it as unknown and was added in the last 10 minutes. That is
  the window in which the host is likely to be pasting the friend's invite,
  and a 403 there is cheap. After it, refusals wait the 5 minutes.

A host running an older version answers `404` or `405`, and the sender treats
that as "does not take pushes yet", quietly, on the 5-minute schedule.

**A stale roster never undoes a fresher one.** Both the push and the fetch
answer carry `as_of`, the sender's clock in milliseconds when it read its
roster. The receiver keeps the last `as_of` per peer, in memory, and ignores
an older one, unless it is more than 10 minutes older, which is a clock that
moved rather than a message that was overtaken. A fetch answered by a server
too old to send `as_of` is always taken, as before.

**A push counts as a fetch.** Receiving one resets the receiver's own roster
schedule for that peer, so it does not dial out at once to learn what it was
just told.

**What a friend who cannot be reached still loses**, on purpose:

- **Their presence**, towards the host. Presence is decided per person who
  asks (ADR 0045 §6), so pushing it means pushing a separate view per person.
  The host not seeing the friend watching is a far smaller loss than the
  friend not being able to join. Revisit it if it matters in use.
- **Their libraries**, towards the host. Browsing the friend's films is not
  joining.

The host's People page still lists that friend's people under **not
answering**, which is true from the host's side.

**Still mutual, still pinned.** Nothing here adds a way in. The route is in
`peerAuthenticated`, refuses any caller that is not a held peer, takes at most
1,000 people and 256 KB, and changes nothing but that peer's roster and state.
Both sides still paste each other's invite. This removes the need to be
*reachable*, not the need to be *introduced*.

## Consequences

- A friend joins a reachable host without forwarding a port. Joining, Watch
  Together and the host seeing the friend in its roster all work with only
  friend → host connections.
- Two sources write one table, ordered by `as_of`.
- Federation is no longer pull-only. ADR 0079 §5 said "Nothing here adds a
  route between servers". This one adds one, and the reason is the line
  above.
- Proven in the two-server harness, where the guest's server has no peer
  listener at all. A real NAT is proven only by a friend on another network,
  and that check is owed before release.
- Option C of the plan, inviting a friend as a member of your server instead
  of pairing a server, is a separate product decision and a separate change.
