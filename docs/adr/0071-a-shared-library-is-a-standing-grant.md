# ADR 0071 — A shared library is a standing grant

Date: 2026-09-20 · Status: accepted · Revised 2026-09-20 with the per-share ceiling (§6)

Answers the question [ADR 0046](0046-remote-guests.md) and the
[federation plan](../watch-together-federation-plan.md) both named and both
deliberately refused:

> **Whether a guest may ever browse a shared library.** Deliberately forbidden
> here. Sharing a library is a much larger question than watching one film
> together, and answering it as a side effect of this ADR would be smuggling
> it in.

This is that larger question, asked on purpose.

## Context

### What was asked for

> When a user is given a connection key, the host is prompted in People as to
> whether to share the host's library with them, and the user is added as a
> Friend. If content has been shared with you, that server's content appears in
> the left-hand navigation under the server's name, with its libraries beneath
> it. And in People you can see what another person is watching, and join them.

Two of those three are built or designed. The middle one is not, and it is the
one that changes the shape of the system.

### What already exists

More than it looks like, and none of it is visible:

| Piece | State |
|---|---|
| Server identity, Ed25519, never regenerated ([ADR 0044](0044-server-identity-and-peering.md)) | **Built** — `internal/identity`, `GET /api/identity` |
| Pairing by out-of-band invite | **Built** — `internal/peer`, `GET /api/peers/invite`, `POST /api/peers` |
| Live presence between paired servers ([ADR 0045](0045-live-presence-between-paired-servers.md)) | **Built** — `internal/presence` |
| Remote guests in a room ([ADR 0046](0046-remote-guests.md)) | Accepted, not built |
| Host caps remote streaming ([ADR 0047](0047-remote-streaming-is-capped-by-the-host.md)) | Accepted, not built |
| Presence in the People screen | **Built** — `PeersSection`, with per-person grants |
| **Pairing UI** — invite out, invite in, list, unpair | **Missing** |
| **Roster opt-in** — `PUT /api/profile/peer-visibility` | **Missing** |

So "add a Friend" is largely surfacing machinery that exists, and "see what they
are watching" is *already surfaced* — the People screen has listed people on
paired servers since ADR 0045, with the three-way distinction between not
sharing, offline and idle that page insists on.

An earlier draft of this table said there was no client UI at all. That was
wrong: it came from grepping for `/api/peers` in the client, which finds
nothing because the screen reaches presence through a hook at
`/api/people/peers`. The correction matters because it narrows the work — what
is missing is not the whole surface but the two ends of the chain, and the
chain is broken at both. **Nothing can pair**, and **no account can opt into a
peer's roster**, which the contract requires before anybody's grant may name
them. So the presence section that exists cannot show a single person today.

**"Browse their library" is still the new thing**, and it is new in a way that
touches the security boundary rather than extending it.

### Why the guest cannot simply be widened

ADR 0046's remote guest is deliberately tiny, and its smallness is the argument
for its existence:

> The restricted session is the security boundary and must stay small enough to
> review: it may join a room it was invited to, and stream *the item that room
> is playing*. It may not browse, may not search, may not list libraries, may
> not read anybody's history.

Its permission is **object-level and derived from a room** — a room that moves
to a new item moves the permission with it, and a room that ends takes it away.
There is no room in "browse Chris's films on a Tuesday afternoon", so there is
nothing for that model to hang a permission on. Relaxing it until there is
would dissolve the property that makes it reviewable.

There is also a lifetime mismatch. A guest session **dies with the room** and
**writes nothing**. Browsing a library is a standing relationship, and anyone
browsing will expect to resume what they started.

## Decision

**A shared library is a standing grant from one server to one paired server,
and it admits a second kind of remote principal: a friend.**

### 1. The unit of sharing is a library, chosen by the host, per peer

Not an item, not a collection, not "everything". A library is the unit people
already reason about, it is the unit the scanner and the ceiling already work
in, and it is the unit a host can hold in their head when deciding.

Per peer, because "Georgia may see my films" and "anyone I have ever paired
with may see my films" are different sentences, and a household will eventually
want both. This is the same rule [ADR 0045](0045-live-presence-between-paired-servers.md)
applies to presence, for the same reason.

**Off by default, and retroactively off.** Pairing grants nothing; it records
that two servers know each other. Un-sharing a library takes it away
immediately, and unpairing takes away everything with one action and nothing
per-person to clean up — the property ADR 0044 was designed for.

### 2. A friend is a principal, not an account

The rule ADR 0046 established, kept without exception: no `user` row, no
password, no entry in the household's people list. Georgia does not become a
member of Chris's server by being able to see his films.

A friend session is admitted exactly as a guest is — her server signs a
short-lived ticket with its identity key, audience-bound to the server it is
for, verified against the key pinned at pairing. **No password crosses and no
account is created.**

What differs is what the ticket asks for and what the session may then do.

### 3. Default-deny, in middleware, scoped to what was shared

A friend session carries the set of shared library ids, and **every** route is
denied unless it is on the friend list. The check is **object-level, never
route-level**, which is ADR 0046's rule and is more important here rather than
less: a friend has a much larger surface, and `/api/stream/{id}` still streams
whatever id it is handed.

A friend may:

- **List the libraries shared with them**, and browse, sort and filter inside
  those libraries.
- **Read metadata and artwork** for items in those libraries.
- **Search — scoped to those libraries.** An unscoped search is a read of the
  whole database with a filter applied afterwards, which is the same mistake as
  a route-level permission.
- **Stream** an item in those libraries, with its subtitles, under
  [ADR 0047](0047-remote-streaming-is-capped-by-the-host.md)'s cap.
- **Join a room** as ADR 0046 already allows.

A friend may **not**: see a library that was not shared, see who else is on the
host, read anybody's history or ratings or tags, see playlists (a playlist may
span libraries, and one that does would leak the names of items in libraries
that were never shared), download originals, or reach anything administrative.

**Every handler that turns a row into a filesystem path re-verifies containment
within the owning library root.** `CLAUDE.md` names this the boundary that rule
was written for; a friend is that boundary with somebody else's machine behind
it.

### 4. A friend's progress lives on the friend's own server

The one place this ADR departs from ADR 0046's "a guest writes nothing", and it
is the interesting decision.

Somebody browsing a library will start a film, stop, and come back. That
position has to live somewhere, and the host is the wrong place: a row keyed to
a remote principal is an account by another name, it outlives the evening, it
has to be listed and deleted, and unpairing would no longer be complete.

So **the friend's server stores it**, keyed by peer fingerprint and remote item
id, exactly as it stores progress for its own library. The host writes nothing
and knows nothing about where a friend is in a film.

This falls out of the identity model rather than being bolted on: the peer
fingerprint is stable across address changes and survives a restore
([ADR 0044](0044-server-identity-and-peering.md) §5, §6), so it is a durable
key. It also means a friend's viewing history is private to their own
household, which is the answer [ADR 0035](0035-who-may-see-whose-viewing.md)
would give if asked.

### 5. Shared libraries appear as their server, never merged

In the client, a peer's shared libraries appear under that peer's name, with
its libraries beneath it. They are **never mixed into the host's own lists** —
not in Home, not in Continue Watching, not in Recently Added, not in search
results, not in counts.

This is not only presentation. A person needs to know at a glance whose disk a
film is on, because everything about it differs: it is gone when that server is
off, it counts against that host's streaming cap, and deleting it is not
theirs to do. A merged list makes all of that invisible at exactly the moment
it matters.

A shelf of the shape *"new on Georgia's server"* is a reasonable thing to want
on Home later, and it is **deliberately not this decision**. It is additive,
it changes nothing decided here, and it is worth having only once the rest is
stable enough to be boring. Naming it now is what keeps it from arriving by
accident as somebody "just merging the lists".

### 6. A share may carry a ceiling, and an unrated item is not shown

A host's own ceiling is about the host's household
([ADR 0015](0015-multi-user-accounts.md)): it is applied per account, and a
friend deliberately has no account (§2). So a friend's limit rides on **the
share** — per peer, per library — chosen from the same rungs an account ceiling
offers.

This was deferred in the first draft on the grounds that `content_rating` was
NULL on every row, which made any ceiling a way to hide a whole library.
Counted again on 2026-09-20:

| kind | rows | rated |
|---|---|---|
| movie | 1,212 | **1,202** |
| show | 13 | **13** |
| episode | 995 | 0 — correct; judged by its show, two levels up if need be |
| track, album, artist, playlist, photo, gallery | — | 0 — correct; no certificate exists |

Twelve distinct labels, mostly MPAA with TV-\* and BBFC strays, which is the
mixed-system case `internal/rating`'s age ladder exists to reconcile. A ceiling
now costs **ten films**, not a library. Count the rows before designing on a
column; the first draft did not, and said the opposite of the truth.

#### The ceiling belongs to the principal, not the route

A friend who may not browse an R film **may not join a room playing one
either.** This is the consequence worth stating because it is the one that
would otherwise be discovered as a hole: ADR 0046 derives a guest's permission
from the room, so without this rule "join a room" is a complete bypass of every
ceiling a host set — the host starts the film, and the permission follows the
room rather than the person.

It costs something real. A host who has set a ceiling for a friend cannot then
invite that friend into a film above it, and the refusal will arrive at an
awkward moment. That is the correct trade: a ceiling somebody can be walked
around by being invited is not a ceiling.

#### An unrated item is not shown, and the host is told what that costs

Blocked, the same as the local rule, and for a stronger reason rather than
merely for consistency. An unrated item in a shared library is the one most
likely to be personal — home video is exactly where the gap sits — and a
ceiling that let the unrated through would leak precisely the material nobody
meant to share.

The cost is that those items are **invisible with no explanation**, which is
the same discomfort the local rule already carries. The mitigation is not to
explain it to the friend, who should not be told what they cannot see, but to
tell **the host at the moment they choose**: *"12 of 1,212 items in this
library are unrated and will not be shown."* A number the host sees beats a
mystery the friend does not.

#### Kinds with no certificate are exempt, so some shares cannot be limited

`artist`, `album`, `track`, `playlist`, `gallery` and `photo` are exempt from
every ceiling, because no certificate exists for them and never will. A ceiling
on a music or picture share is therefore **a control that does nothing**, and
the UI must say so rather than offer it — an inert switch on a sharing screen
is worse than no switch, because it reads as a limit that was applied.

#### Two ceilings may both apply, and neither server needs to know the other's

A friend's own household may limit its own people; that is their server's
business, applied by their server to their accounts. The host's share ceiling
is applied by the host. They compose without coordination, and neither side
has to disclose its rules to the other — which is the same property the
identity model gives everywhere else here.

#### Resolution is split from application, because fail-open must not travel

This is the finding that made the ceiling a revision rather than a patch, and
it is a real defect waiting in the current code for anyone who wires a friend
in naively.

`ceilingPredicate` and `rating.AllowedLabels` already take a **label** rather
than a user, so every listing, filter and search generalises untouched. But
`Store.MayPlay` — the object-level check that the code itself describes as
standing "between a hand-written request and a file" — resolves the ceiling by
reading `max_content_rating` **from the `user` row**, and returns *permitted*
when there is no such row:

> No account row means no ceiling, not a refusal. […] an unsecured loopback
> server has no accounts at all and reads everything as `store.LocalUserID`, so
> refusing the unknown emptied the entire library for the one configuration
> that is meant to work out of the box.

That default is right for every caller it has today and **wrong for a friend**,
who has no `user` row by design and would therefore be admitted past every
ceiling. Worse, it would fail **silently and openly** — the feature would look
like it worked.

So the two jobs that function currently does are separated:

- **Resolving** which ceiling applies to a principal. An account resolves from
  its `user` row and keeps today's fail-open, for the reason quoted. A friend
  resolves from the share record. **A friend whose ceiling cannot be resolved
  is refused**, which is the opposite default and deliberately so.
- **Applying** a ceiling to an item, which is already pure in effect and is the
  half that needs no change.

A friend principal must never reach the account-resolving path. That is a
property worth a test that fails loudly rather than a comment.

#### Lowering a ceiling takes effect on the next request

Not mid-stream on the segment already being served, and not retroactively on
what was watched. The same rule presence uses for revocation, for the same
reason: a permission check answers about now, and a stream already in flight
finishes.

### 7. Nothing here is reachable without UI, and the UI is the feature

Phases 1 to 3 of the federation plan are built and invisible. Repeating that
would produce a fourth invisible phase. So the People screen gaining **Friends**
— paired servers, their presence, what was shared, and the controls to share or
stop — is part of this decision and not a follow-up.

## Rejected

**Widen the ADR 0046 guest.** Its permission is derived from a room; there is
no room in browsing. Stretching it would cost the property that makes it
reviewable, and leave one principal doing two jobs with the union of both
surfaces.

**Share individual items, or a collection.** Finer control that is harder to
reason about and harder to audit. A host asked "which of these 1,200 films may
Georgia see" gives a worse answer than one asked "may Georgia see my films".

**Give the friend an account on the host.** The obvious implementation, and
ADR 0046 already rejected it: under LANcast's model an account carries full
member access the moment it exists, has to be managed and deleted, and
revocation stops being one action.

**Store friend progress on the host.** An account by another name. See §4.

**Merge shared libraries into the host's own lists.** Discussed in §5: it hides
the facts a person most needs.

**A public directory of shareable servers.** Phone-home, and a different
product. The federation plan says so and nothing here changes it.

## Consequences

The household's second viewer stops being a guest who can only be invited into
a film somebody else chose, and becomes somebody who can go and find one. That
is the difference between the feature working and the feature being used.

**The remote surface grows a great deal**, and honestly: from "one item, tied
to a room" to "every item in a shared library, standing". The mitigations are
that it is off by default, per peer, per library, object-level checked, and
revoked completely by one action — but the reviewable-in-an-afternoon property
of ADR 0046 does not survive this, and pretending otherwise would be the
failure. This ADR should be reviewed as a security change.

**Two principals now exist** where there was one: a room guest and a friend. A
principal doing two jobs is worse than two principals, but two is still more
than one, and the middleware that tells them apart is now security-critical
code.

**The ceiling's default inverts between them**, which is the sharpest edge in
this document. An account with no resolvable ceiling is permitted, because an
unsecured loopback server has no accounts and refusing the unknown would empty
the library. A friend with no resolvable ceiling is refused. Two opposite
defaults in one area is how a mistake becomes invisible, so §6 requires the
resolving step to be a separate, tested thing rather than a branch inside the
check.

**`internal/together`, `internal/peer` and `internal/presence` finally get a
screen**, which is overdue independently of this.

## What this does not decide

**Whether a friend may write anything to the host** — a rating, a tag, a
playlist entry. §4 keeps progress on the friend's side; everything else stays
forbidden and can be revisited once one of them is actually wanted.

**Whether more than two peers work.** Nothing here prevents it; nothing here
has been thought through for it either, which is the same position the
federation plan took.

**Transcoding policy for friends beyond ADR 0047's cap** — in particular
whether a host may refuse to convert for a friend at all and offer only what
direct-plays.

**Whether the ten unrated films can be rated.** §6 blocks unrated items and
tells the host the count. Backfilling those ten is worth doing and is not a
decision — it changes the number, not the rule.

**Whether a host may set a *floor* rather than a ceiling**, or exclude named
items from a share. Both are finer control than a library, which §1 rejected
for good reasons that apply again here.

**A "new on this server" shelf on Home.** §5 defers it deliberately.

## Revisit when

Somebody wants a friend to write something to the host's server (§4), or a
second sharing shape is wanted that a library cannot express (§1).
