# ADR 0047 — Remote streaming is capped by the host

Date: 2026-08-19 · Status: accepted

Fourth and last ADR of the work planned in
[the Watch Together federation plan](../watch-together-federation-plan.md).
Extends [ADR 0031](0031-quality-selection.md), which built the entire mechanism
this needs and gave it to the wrong person for this case.

## Context

[ADR 0046](0046-remote-guests.md) lets Georgia stream one item from Chris's
server. This settles what she gets, and at whose expense.

### Almost all of it already exists

ADR 0031 built quality selection in full: `?max_height=` and `?max_bitrate=`
resolved alongside the client profile, a ceiling that **only ever narrows**, the
rule that **a ceiling is not a target** (no upscale, no slack rate control), the
lower of profile and query winning, and the resolved targets riding on the
`Decision` so ffmpeg is built from one place. `Profile` already carries
`MaxHeight` and `MaxVideoBitRate`.

There is no mechanism to invent here. There is one assumption to overturn.

### The assumption

ADR 0031 put the ceiling in the client's hands, and justified it precisely:

> A ceiling narrows: it can force an encode that would not otherwise have
> happened, and the worst it can do is **cost the server CPU**.

and:

> A quality ceiling is a fact about the **link** — how much bandwidth there is
> between this screen and the server.

Both sentences are true and neither survives a remote guest. The worst it can do
is cost the server CPU *and the server's owner's uplink* — and the person
choosing is no longer the person paying. Georgia asking for 100 Mbps is a
sentence about Chris's house.

The link, too, has stopped being one thing. Inside a household it is a LAN
whose properties both ends share. Between two households it is an uplink
belonging to one of them, and only one of them is in a position to know what it
can stand.

### The cost of getting it wrong is not paid by the viewer

This is what decides the default. A cap set too high does not merely produce a
stuttering film: it saturates the uplink of a house where other people are doing
other things. The people who suffer are the ones **not** watching, who did not
agree to anything and cannot see why the internet got bad.

A cap set too low produces a softer picture for one person who knows why.

The asymmetry is total, so the default is conservative.

### The cheapest method is the most expensive one

`DirectPlay` is documented as *"cheapest by a wide margin"*, and it is — in CPU.
Over a WAN a direct-played 40 Mbps remux is the single most expensive thing the
server can do, and it is what today's rules choose, correctly, for a perfectly
compatible file.

Remote inverts the cost model that the words "cheap" and "expensive" refer to
throughout `internal/probe`.

## Decision

**Capability is the guest's; the ceiling is the host's.**

That sentence is the whole ADR. Georgia's client still declares what it can
decode — her browser is hers, and a wrong guess there produces a black
rectangle. What she may not do is decide how much of Chris's uplink to take.

### 1. The remote cap is server-side and mandatory

Configured by the host, applied to every remote guest session, and **not
reachable from a query string**. A guest may narrow it further — the ADR 0031
direction of travel is preserved — but nothing a guest sends can widen it.

Resolution is `min(host remote cap, anything the guest asked for)`, with the
guest's own profile supplying codecs and containers as it does today.

### 2. It is expressed in the profile, so the decision rules do not change

The cap is applied to `Profile.MaxHeight` and `Profile.MaxVideoBitRate` before
`Decide` is called. `decide.go` is untouched.

This is deliberate and it is the reason this ADR is small. CLAUDE.md requires
the decision function stay pure so that codec cases are fixture tests in
milliseconds; expressing "remote" as a new input to the decision would have put
a network fact inside the pure core. Expressed as a profile, every remote case
is **already testable** by the existing tests with a capped profile, and the
model `decide_test.go` sets — one named test per combination, asserting the
decision and its reason — extends to WAN cases with no new machinery.

The federation plan sized this phase as medium. It is smaller than that, because
ADR 0031 did the work.

### 3. The cap applies to direct play

A compatible file over the cap is transcoded down, even though it would play
untouched. This is not a special case bolted on: it is ADR 0031's existing
behaviour — *"it can force an encode that would not otherwise have happened"* —
meeting a ceiling that is now low enough to bind on files that never bound
before.

It needs saying out loud only because it reads as wrong to anybody who has
internalised "direct play is free". Direct play is free for the CPU and ruinous
for the uplink, and remote is the case where the second number is the one that
matters.

### 4. The default is conservative, and it is a setting

Default **720p / 4 Mbps**, changeable by the host.

Not a guess dressed as a measurement: it is chosen to be survivable on a modest
domestic uplink while leaving room for the rest of a household to use the
internet, on the asymmetry above. Somebody with fibre and a reason will raise
it, and it is theirs to raise. Nobody should have to lower it after a bad
evening they could not diagnose.

### 5. Concurrent remote transcodes are capped, default 1

Handed over by ADR 0046, which named it and left it here.

The cap counts **transcode sessions, not connections** — a guest holding a
direct-played, under-cap file costs the CPU nothing and should not consume the
budget. When the cap is reached, a further guest is refused with a reason that
says so, rather than being admitted into a room that then stutters for everyone
including the host.

Default 1 because the case this is being built for is two households and one
film.

### 6. The reason travels, and the guest is told

`Decision.Reason` already exists, and exists for this:

> "Why is my server pinned at 100% CPU" is the most common question a media
> server has to answer, and a decision that cannot explain itself makes that
> unanswerable.

The remote case adds a second unanswerable question — *why does this look worse
than it does at his house* — and it has the same fix. A remote-capped decision
says so in its reason, and the guest's client surfaces it. A quality limit the
viewer cannot see the cause of reads as the software being bad.

### 7. The remote path stays progressive fMP4

Remote streaming is the strongest argument anybody will ever make for adaptive
bitrate, and therefore for vendoring hls.js. It is refused here in advance.

[ADR 0013](0013-transcode-pipeline.md) traded HLS-by-default against shipping
~300KB of unaudited third-party code, and CLAUDE.md records it as a stated trade
rather than an oversight. A fixed conservative cap is this project's answer to a
variable link: worse than adaptation, and it costs no dependency. Nothing about
a second household changes the terms of that trade.

## Alternatives considered

**Let the guest choose, as ADR 0031 lets a local client choose.** Consistent,
and wrong for the one reason 0031's own justification names: the worst case is
no longer confined to the person choosing. Consistency with a rule whose stated
premise has been withdrawn is not consistency.

**Measure the link and adapt the cap.** The honest version of this is adaptive
bitrate, which is §7. The dishonest version is a bandwidth estimate computed
from stream progress, which is a poor measurement, unstable exactly when the
link is bad, and would produce a quality that wanders for reasons no one can
see. A fixed number the host set is worse in theory and legible in practice.

**Cap by resolution only, leaving bitrate alone.** Simpler, and it does not
solve the problem: a 1080p remux at 40 Mbps and a 1080p encode at 6 Mbps are the
same resolution and differ by everything that matters on an uplink. Bitrate is
the quantity the link actually cares about.

**Charge the guest's own server for the bandwidth by proxying through it.**
Already declined twice — the plan, and ADR 0046. It moves the cost rather than
reducing it, and doubles it in transit.

## Consequences

**Good — Phase 6 is mostly configuration.** No change to the decision rules, no
new command-line construction, no new tests of a new mechanism. ADR 0031 built
this and did not know it.

**Good — the host cannot be surprised by a guest.** The worst a remote guest can
provoke is one transcode at the configured ceiling.

**Good — it is honest at both ends.** The host set a number; the guest is told
one applied.

**Cost — a guest sees a worse picture than the host does, of the same file**,
and the two of them are watching together and can compare. This is the feature
working, and the reason string is the only thing standing between it and a bug
report.

**Cost — a host seeking restarts the guest's transcode at a new offset**, which
over a WAN is a visible stall where a local viewer would barely notice. The
host drives, so this is entirely within the host's control and entirely
invisible to them as a cost. Worth measuring before assuming it is tolerable.

**Cost — a low default will be somebody's first impression.** 720p to a person
who owns the 4K file is a worse demo than the software deserves. Accepted,
because the alternative failure — a saturated uplink in a house that did not
volunteer — is one nobody can diagnose from inside the app.

## What this does not decide

**Anything about who may connect.** ADR 0046.

**Whether the cap should ever differ per peer.** One number for all guests is
right for two households and obviously wrong for twenty. Nothing here prevents
it becoming per-peer later; the setting simply has one row today.

**Hardware encoding for remote sessions.** [ADR 0032](0032-hardware-decode.md)
governs that and does not change here. A remote session is an ordinary transcode
as far as the pipeline is concerned, which is the point.

## Revisit when

A host with real upstream bandwidth finds the default insulting, more than one
guest at a time becomes normal, or somebody demonstrates that the seek stall in
Consequences is bad enough to reopen the adaptive-bitrate trade — in which case
the thing being reopened is ADR 0013, deliberately and on its own terms, not as
a side effect of a feature request.
