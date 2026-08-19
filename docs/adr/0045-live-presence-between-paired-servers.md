# ADR 0045 — Live presence between paired servers

Date: 2026-08-19 · Status: accepted

Amends [ADR 0035](0035-who-may-see-whose-viewing.md).

Second ADR of the work planned in
[the Watch Together federation plan](../watch-together-federation-plan.md), and
the one it names as a blocker: as things stand, **ADR 0035 forbids the feature**.

## Context

The goal is that Georgia, on her own LANcast, opens the People tab, sees that
Chris is watching a film, and joins him. [ADR 0044](0044-server-identity-and-peering.md)
gives the two servers a way to know each other. This is the question of what
they may then say about the people using them.

### Two changes, and they are not the same kind of change

It matters that these are separated, because one is invited and one is not.

**Change A — sharing with a named person rather than with everybody.** ADR 0035
parked this deliberately, in as many words:

> **Whether sharing should ever be granular** — per library, per media type, or
> to named people rather than everybody on the server.

and listed it first among its revisit triggers: *"Somebody asks to share with
one person rather than everybody."* That has now happened. This half of the
amendment is ADR 0035 working exactly as designed.

**Change B — disclosing what somebody is watching right now.** This one is not
invited, and dressing it up as though it were would be the dishonest way to
write this document.

ADR 0035 bounded what sharing means and then named what it excludes. The second
exclusion is:

> **resume positions**, which say where you stopped rather than what you
> watched, and are useful to nobody else.

Live presence is *stronger* than a resume position — it is where you are while
you are still there — and it is disclosed across a boundary that did not exist
when 0035 was written. **0035 would not have permitted this**, and this ADR is
extending it rather than interpreting it.

### The argument that makes Change B acceptable

Not "it is useful", which is an argument for every disclosure ever made. The
distinction is in 0035's own reasoning about *why* viewing deserves protection:

> a media server knows something about people that almost nothing else does.
> What somebody watches, at what hour, and what they abandoned halfway through
> is a more intimate record than most services hold, and **it accumulates
> silently**.

The harm 0035 identifies is **a record that accumulates**. Something written
down, growing, readable later, correlatable — the thing that lets a reader
characterise somebody rather than merely notice them.

Presence, specified as below, is not that. It exists only while the film is
playing, is never written down, answers only *now*, and leaves nothing behind
when it stops, because there was never anything behind it. It is closer to a lit
window than to a diary.

That distinction is the whole justification, so it also generates the rules. Any
design that lets presence accumulate has not bent this ADR — it has collapsed it
back into the thing 0035 protects against.

## Decision

**A person may grant a named person on a paired server the ability to see, while
it is happening, that they are watching a particular work.**

### 1. Presence is a third disclosure category

It is **not** an extension of `share_activity`, and no existing opt-in widens
into it. Somebody who agreed to publish the films they had finished did not
agree to be observed in real time, and silently reinterpreting an old consent to
cover a new disclosure is the precise failure ADR 0035 exists to prevent.

Existing accounts get nothing new switched on. There is no migration in which
anybody starts being visible.

### 2. The grant names a person, and it is a table

One row per (local account, remote person). **Not a `share_presence` boolean.**

A single switch is a grant to *everybody* — the per-server answer this project
considered and declined — wearing a per-account disguise. The unit of consent in
ADR 0035 is a person throughout, and it stays a person here.

Granting requires the remote person to exist locally, which is why pairing
exchanges a roster (see the federation plan). Being **in** that roster is its own
per-account opt-in, `visible_to_peers`, defaulting off: appearing in a list your
server hands to another server is itself a disclosure. An account that has not
opted in cannot be named by anybody's grant, in either direction.

### 3. What presence discloses, exhaustively

- that the person is **online**;
- that they are **watching**, or are idle;
- **the work**, by title.

And nothing else. Specifically **not**: the position, how long they have been
watching, what they watched before, what they abandoned, the library it came
from, or anything at all about anybody who has not granted this reader.

**Presence names the work, not the episode.** "Chris is watching *Cowboy
Bebop*", never "*Cowboy Bebop* S01E02 — Stray Dog Strut". The client already
holds itself to this standard: `spoilers.ts` hides an unwatched episode's
synopsis by default, on the reasoning that *"doing nothing here is itself a
choice, and the wrong one to make by accident"*. A presence feed that announces
episode titles to a friend who is three seasons behind is that same wrong
choice, made across a network.

**Video only.** Music and photographs are excluded rather than left to leak,
because Watch Together is about watching and nothing here has thought about
what being seen listening should mean.

### 4. Presence is never persisted

No table, no column, no log line, no crash report. In memory, swept, gone on
restart — the same standing the rooms already have, for a reason
[`internal/together`](../../internal/together/together.go) gives about itself:
persisting live state would resurrect something that is no longer true.

Three consequences that are the actual teeth of this ADR:

- **There is no presence history, and there will not be one.**
- **There is no "last seen watching".** This is the feature request that will
  arrive, it sounds harmless, and it is a record wearing presence's clothes. The
  moment presence can be read about the past it has become the accumulating
  thing 0035 protects against, and it will have got there without anybody
  deciding to.
- **The sweep must actually delete.** Presence that lingers because expiry was
  implemented as a display filter is persistence with a polite UI.

### 5. Off by default, and revocation is immediate

Default off, per ADR 0035's posture and for the same reason: a server upgraded
into this ADR discloses nothing until somebody deliberately says so.

0035 required opting out to be retroactive. Here that is satisfied trivially —
there is no past to take back — so the equivalent requirement is **immediacy**:
revoking a grant stops the disclosure on the next poll, mid-film. It does not
wait for the film to end, and it does not wait for a restart.

Unpairing the server revokes every grant to everybody on it, at once.

### 6. An administrator has no privileged position

Consistent with ADR 0035 throughout. An admin cannot grant presence on somebody
else's behalf — a switch an admin can flip is not consent — cannot see presence
not granted to them, and gains nothing by being the person who owns the disk.

### 7. A presence grant carries the right to ask, and the host answers in the moment

Somebody you have granted presence to may **request to join** what they can see
you watching. They do not thereby get in: the host accepts or declines, then,
while it is happening.

This replaces an earlier draft of this rule, which made room visibility a second
standing opt-in on top of the presence grant. That was over-cautious and it got
the consent in the wrong place. Three reasons it is wrong:

- **It priced consent by the switch.** The model already costs four settings for
  two people to see each other (§2); a fifth to make what you can see actionable
  is the point at which somebody stops reading them and turns everything on.
- **The second switch was answering a question the first had already asked.**
  "May Georgia see that I am watching this" and "may Georgia ask to watch it
  with me" are not meaningfully different disclosures. The title is already out;
  what remains is whether the film is watched alone, which is not a privacy
  question.
- **A standing switch is the weaker consent of the two available.** This ADR
  already said so in its own next breath, about invitations: a decision made in
  the moment, about this person and this film, beats one set months ago and
  forgotten. Requiring the switch *and* offering no moment was choosing the
  weaker mechanism and then adding friction to it.

So the principle behind the old rule survives — **being seen is still not being
joinable** — and it is now carried by the host's answer rather than by a
setting. Nobody arrives in a room because a toggle was left on.

The converse still holds, and it is the same reasoning from the other side:
**inviting somebody into a room discloses the title to them regardless of any
standing grant.** A deliberate invitation is consent of a stronger kind than a
switch, which is exactly why the accept can carry the weight the switch was
carrying.

Two consequences for whoever builds Phase 5. A request is **not** a
notification-and-timeout that defaults to yes — an unanswered request is
declined, because a host who is asleep has not agreed to anything. And declining
is **silent to the asker beyond "not now"**: a decline that explains itself
invites a negotiation about why, which is the thing a host reaches for the
decline in order to avoid.

### 8. The "libraries the reader can see" rule does not carry over

ADR 0035 excluded from sharing *"anything from a library the reader cannot
themselves see"*. Read literally against a remote person that would forbid all
presence, because a remote guest can see no library at all — ADR 0046 gives them
no browsing.

The rule does not carry over because it was answering a different question. On
one server it prevented sharing from leaking the *existence and contents of
libraries* somebody had not been given. Across a pairing, naming one title to
one named person leaks nothing about a library, and the disclosure is the entire
point: it is how the invitation happens.

## Alternatives considered

**Presence as reachability only — "Georgia is online", no title.** Genuinely
defensible, and it makes Watch Together purely invitation-driven: you open a
room and it appears to the people you invited. It was not chosen because the
stated goal is that Georgia can see there is something to join *before* asking,
and an invitation-only feature means the person already watching has to think of
you. But this is the fallback if presence proves uncomfortable in practice, and
it is a smaller change than it looks — rule 3 shrinks by one line.

**Requiring reciprocity — you may only see people you also share with.** Sounds
fair and is coercion with good manners: it makes one person's consent the price
of another's, which is exactly what a per-person opt-in is for avoiding. Grants
are independent. Somebody who shares with a person who does not share back has
made a choice they are allowed to make.

**Deriving presence from `playback_state` rather than tracking it live.** Would
need no new machinery — the resume position is already written as you watch. It
is rejected because that is *the record*: reading presence out of it means
presence and history are the same data, one query apart, and the separation this
ADR rests on would exist only in the handler that happened to be written today.

## Consequences

**Good — the blocker on the federation plan is lifted**, with the reasoning
written down rather than assumed by whoever builds Phase 3.

**Good — the conservative default survives contact with a new feature.** Nothing
becomes visible as a side effect of an update, which is the one failure here that
cannot be undone.

**Good — "never persisted" is a testable claim.** No schema change, no
migration, and a test can assert that a restart forgets everything. A rule that
can be checked outlives the person who wrote it.

**Cost — two switches per relationship, in each direction.** Be visible in the
roster; grant presence to a named person. Four decisions for two people to see
each other. That is what per-person consent costs, and the alternative — one
switch meaning everybody — is the thing declined in rule 2.

**Cost — "not sharing" is visible.** The People page distinguishes offline from
online-and-not-sharing, so a peer can tell that a grant was withheld. That is
the existing precedent rather than a new exposure: the local People page already
prints `Not sharing`, deliberately, so an empty list reads as a choice rather
than as a fault. The alternative — making withholding indistinguishable from
being offline — hides the choice by lying about the state, and would make a
genuinely offline server look like a snub.

**Cost — a standing invitation to build the thing this forbids.** "Last seen
watching", "what have they been up to", a presence log for debugging. Each will
be one small commit and each ends this ADR. Rule 4 is written to be quotable in
a review for exactly that reason.

## What this does not decide

**Whether a remote person may see anything else at all.** Nothing here grants
browsing, search, history, ratings or library visibility. Ratings and reviews
remain private unconditionally, as ADR 0035 has it, and federation does not
touch that.

**How presence is transported or how often it is polled.** Implementation, and
the federation plan carries the trap that matters — a sweep that runs before
recording presence deletes the punctual, which this project has already shipped
once.

**What being seen listening to music should mean.** Excluded above rather than
answered.

## Revisit when

Presence proves to be something people turn off, somebody asks to see a friend's
history rather than their evening, or a third household makes "a named person"
an unreasonable amount of clicking. The honest response to the last one is a
better way to express a grant — not a switch that means everybody.
