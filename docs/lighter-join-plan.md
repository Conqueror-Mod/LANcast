# A lighter way to join: design plan

*Written 2026-10-11. A plan for deciding, not a decision: the options below end
in an ADR, and nothing is built yet.*

## The problem, stated as a test

A friend who has never heard of port forwarding should be able to go from "you
sent me a link" to watching a film with the host in under ten minutes, without
opening anything on their router.

Today they cannot. The [join guide](https://claude.ai/artifact/BcBeNBLHb7xaiUV41CWSm8)
has six steps, and step 3 is the one that loses people: **the friend's server
must be reachable from the host's**, by forwarding TCP 8080 or by joining the
host's Tailscale network. Everything else is clicking.

## Why step 3 exists

Federation calls go both ways. Checked against the code on 2026-10-11:

| Call | Direction | Needed to watch the host's film? |
|---|---|---|
| Browse the host's libraries, play a film | friend → host | yes |
| Ask to join, poll the request, join and follow a room | friend → host | yes |
| Fetch the friend's roster (`refreshPeer`), which also promotes the pairing to `paired` | **host → friend** | **yes, indirectly** |
| Ask what the friend is watching (`askPeerPresence`) | **host → friend** | no |
| List the friend's libraries | **host → friend** | no |

The indirect "yes" is the whole problem. Presence is granted per person
(ADR 0045). The host can only tick **Let them see me** for somebody in its
`remote_person` table, and only the roster fetch fills that table. Without the
grant, the friend's People page never shows the host watching, so **Ask to
join** never appears. That's the one-way failure
recorded in memory as "presence needs two-way reach".

So the host reaching the friend is needed for **one thing**: learning who is on
the friend's server. Everything else the friend needs flows towards the host,
which is already reachable.

## Constraints any design must keep

- **No phone-home** (README principle). LANcast operates no relay, rendezvous or
  account service. A third-party relay the operator chooses (Tailscale) is
  fine; one the project runs is not.
- **The pairing stays mutual.** Mutual TLS with pinned identity keys (ADR 0044)
  is how a server knows who is calling. Weakening it to make joining easier is
  off the table.
- **A guest's power stays readable in one file** (`guestgate.go`, ADR 0046).
- **An unsecured server binds loopback only.** Nothing here may relax that.

## Options

### A. Let the friend's server push what the host would have fetched (recommended to explore first)

The host is reachable, and the friend's server already calls it several times
a minute. So the friend's server **sends** its roster to the host, instead of
the host fetching it:

- A new federation route, for example `PUT /api/federation/roster`. It's
  authenticated by the same mutual-TLS pin as every other one, and the host
  stores it with the same `ReplaceRemotePeople` the fetch uses.
- The friend's server sends it on pairing, when its roster changes, and on a
  slow timer, the same schedule as the ADR 0079 watcher.
- **Promotion to `paired`** happens when the host receives a roster it could
  not have got any other way. The ADR 0079 kick already treats a call-in from an
  `added` peer as proof the pairing is mutual.
- **Presence the other way** (the host seeing the friend watching) can ride the
  same push, or stay host → friend and simply be absent when the friend is
  unreachable. "The host can't see me watching" is a far smaller loss than
  "I can't join".

**What it buys:** only the *host* needs to be reachable. Step 3 disappears for
every friend of a host who has done it once, which is exactly your situation.

**Costs and questions:**
- **One new federation route,** in `peerAuthenticated` and the spec. Phase 5
  already learned the gate-list lesson the hard way.
- **Two sources for one table.** If both a push and a fetch can write the
  roster, a stale push must not undo a fresh fetch. A version or timestamp on
  the roster answers that.
- **Does the host still try to call an unreachable friend?** It would, every 10
  seconds per open window, failing quietly. The watcher's back-off covers
  it, but "this peer pushes, don't fetch" may be worth remembering per peer.
- **Peer libraries the other way** (the host browsing the friend's films)
  still need host → friend. That's fine to leave: it isn't joining.

### B. A long-lived connection from the friend's server to the host

A generalisation of A. The friend's server holds one outbound connection open
to the host (WebSocket or HTTP/2), and the host sends its requests **down that
connection** instead of dialling out. Every federation call then works with
only the host reachable, presence and libraries included.

**What it buys:** complete one-way federation. Nothing is lost when the friend
can't be reached.

**Costs:**
- A multiplexing layer under every `callPeer`/`sendPeer`.
- A connection that has to survive sleep and roaming.
- A second transport to test.

It's a large change, and A gets most of the benefit. **Worth revisiting only if
A's losses** (no presence or libraries from unreachable friends) **turn out to
matter.**

### C. An account on the host's server instead of a server of their own

The friend installs nothing, or only the client. The host makes them a member
account and sends a sign-in link. Watch Together on one server already works
(#780), and the desktop client can trust a server it didn't install (ADR 0070).

**What it buys:** the lightest possible join: a password and a link.

**Costs and questions:**
- **The friend has no library of their own,** and their watch history lives on
  the host's server. That's a different relationship from federation, closer
  to Plex's "managed user". It may be exactly what a casual friend wants.
- **A browser hits the self-signed certificate warning.** The desktop client
  pins instead; ADR 0070 covers that path. A guide would steer people to the
  client.
- **The host's server exposes its whole API to that account,** within
  member rights. Members already exist and are gated (`adminOnly`), so this
  is about trusting a person, not a new surface.
- **Needs only UX and docs:** "invite a friend" creating a member account and
  a one-time sign-in link. Probably no ADR.

### D. Automatic port mapping (UPnP / NAT-PMP) on the friend's server

The friend's server asks their router to forward 8080 itself, opt-in, at
pairing time.

**What it buys:** step 3 automated for many home routers.

**Costs:**
- Many routers ship with UPnP off, and some ISPs (CGNAT) make it useless.
- It opens a port on the internet without the person doing it knowingly,
  which needs a clear consent screen. LANcast requires a password before it
  binds wider, which limits the damage, but this is still a security
  decision for an ADR.

### E. Rejected up front

- **A relay or rendezvous run by the LANcast project.** That breaks no
  phone-home.
- **Hole punching without a rendezvous.** It needs one. The host could be the
  rendezvous, but then A is simpler.

## Suggested order for the design session

1. **Measure before choosing.** On Kiousu's setup, or a test pair with one side
   firewalled, confirm the table above in practice: which features fail when
   only friend → host works. (ADR 0079's watcher logs "peer not answering"
   per peer in `lancastd.log`.)
2. **Write the ADR for A,** covering the push route, how a push and a fetch agree
   on one roster, promotion, and what an unreachable friend loses (presence
   and libraries towards the host).
3. **Decide C separately.** It's a product question (members versus peers), not
   a federation one, and could ship on its own as "invite a friend".
4. **Park B and D** unless step 1 shows A's losses matter, or most friends turn
   out to have UPnP routers.

## What a LAN test will not prove

The [two-server harness](../internal/api/togetherrelay_test.go) runs both
servers in one process. To test A, make one side's address unreachable from
the other (an unroutable address in the invite, as
`TestThePeerGoingQuietIsAChangeInPresence` does) and assert that joining still
works. A real NAT, and the host's public address, are still only proven by a
friend on another network: the two-machine check owed since v0.9.75.
