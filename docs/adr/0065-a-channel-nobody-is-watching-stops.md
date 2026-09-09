# ADR 0065 — A channel nobody is watching stops

Date: 2026-09-08 · Status: **proposed**

Amends [ADR 0013](0013-transcode-pipeline.md)'s live-TV amendment, which moved
live channels to an HLS playlist and, in doing so, removed the only signal the
server had that a viewer had left.

## Context

Live TV was observed leaving channels running after the viewer had gone. The
whole episode is in one server log, on 2026-09-08:

```
19:36:38  live transcode started  channel=30087  output=hls
19:37:13  live transcode started  channel=30354  output=hls
19:37:32  live transcode started  channel=30594  output=hls
19:38:27  live transcode started  channel=30595  output=hls
19:38:43  refused a channel: at the session ceiling  channel=30599  running=3 max=3
19:38:48  refused a channel: at the session ceiling  channel=30600  running=3 max=3
19:38:56  live transcode started  channel=30598  output=hls
19:39:19  refused a channel: at the session ceiling  channel=30329  running=3 max=3
```

Two minutes of ordinary channel surfing. By the fifth channel the server was
**refusing to play anything** — the three session slots were held by channels
nobody was watching any more. At 19:42, three minutes after Live TV was exited
entirely, two ffmpeg processes were still alive and one was still writing
segments at about 120 KB/s: a provider's stream, pulled at full rate, for
nobody.

Nothing was leaking in the sense of never being cleaned up. Everything here is
working as written.

### Why the server cannot tell

The progressive live path tied ffmpeg's life to the request, and that was exact:
one request, one stream, and a closed tab ends both. Moving to HLS removed it,
deliberately and correctly — `channellive.go` explains why:

> Here the request is one poll of a playlist among many, and tying the encode to
> it would kill the channel between the playlist and its first segment.

That is right, and it is why `context.WithoutCancel` is there. But it leaves the
server with **no signal at all** that a viewer has gone. The replacement named in
the same comment is `IdleTimeout`.

### The number is a film's number

`IdleTimeout` is ten minutes, and its own comment records where that came from:
it was raised to ten minutes *"so a paused film keeps its ffmpeg"*.

That is a good answer for a film. Somebody who pauses is coming back, restarting
the encode would mean re-seeking and re-buffering, and the cost of waiting is one
idle process and some scratch disk.

It is the wrong answer for a channel, and every term of the trade differs:

- **A paused film costs nothing while it waits.** An abandoned channel is pulled
  at full rate for the whole ten minutes, from somebody else's server.
- **Nobody pauses five films in two minutes.** Surfing five channels is one
  ordinary minute of use, and each one holds its slot.
- **A film's session is worth keeping** because resuming is expensive. A channel
  is live; coming back means joining at *now*, and the old session is not the
  thing anybody would resume into.

So one constant is answering two questions, and it was tuned for the question
where waiting is cheap.

### The failure is not the bandwidth

The bandwidth is real and it is not what a person notices. What they notice is
`too many streams are already running on this server` while they are alone in
the house, on a server that is doing nothing they asked for. `MaxSessions`
defaults to 3, which is a sensible ceiling for real viewers and a very low one
for ghosts.

### What was not tested

`TestLiveStopsFFmpegWhenTheClientGoes` exists, and its comment calls this "the
property this feature lives or dies on". It exercises the **progressive**
endpoint — the one path that can satisfy it by construction. Nothing asserts
what the HLS path does when the viewer leaves, which is the path that ships.

This is the same shape as the fault found the same day in the file playlist: an
instrument aimed at one of two paths, and the bug in the other.

## Decision

### 1. The client says when it has stopped watching

A channel gains an explicit stop, and the client sends it when it stops playing
one — pressing Stop, switching channels, leaving the page, or closing the window.

Today the Stop button is entirely client-side: it pauses the element and clears
some state, and the server hears nothing. It is the one moment where the
software knows the answer for certain, and it currently throws it away.

**Sent as a beacon, not an ordinary request.** A `fetch` issued while a page is
unloading is routinely cancelled, and a stop that only arrives when the tab
survives is a stop that misses the case it exists for.

**Idempotent, and unauthenticated by nothing.** Stopping a channel that is
already stopped is success, not a 404: the caller asked for it to not be running,
and it is not running.

### 2. Live sessions get their own idle timeout, and it is short

A separate constant, not a reuse. **Thirty seconds** for a live session against
ten minutes for a file.

The number is derived rather than picked. A viewer of an HLS channel polls for a
segment about every `SegmentSeconds` — six today — so a session nobody has asked
about for thirty seconds has missed roughly five polls. That is not a slow
network; it is nobody there.

**This is the backstop, not the mechanism.** The explicit stop above is exact
and immediate; this catches every case where it cannot arrive — a crashed
client, a killed browser, a laptop lid, a network that vanished. Both, because
either alone is wrong: a timeout on its own is what we have now, and an explicit
stop on its own trusts a client that may never speak again.

### 3. `MaxSessions` is not raised

Raising the ceiling is the obvious response to seeing it refuse, and it treats
the symptom. Three concurrent real streams is a reasonable bound for a home
server, and the refusals happened because the sessions were not real. Fixing
what holds a slot is the change; the ceiling stays where it is until something
other than ghosts pushes against it.

### 4. What is not decided here

**Sharing a channel between viewers stays as it is.** Two people watching one
channel share a session, and that is why the session outlives any single request
in the first place. A stop from one viewer must therefore not end a session
another is still polling — which the idle timeout handles naturally, and the
explicit stop must respect. Making the stop reference-counted, or per-viewer, is
a larger question about identity that this does not open.

## Consequences

**A channel stops when the viewer stops.** Immediately in the ordinary case,
within half a minute in every other.

**The ceiling stops being reached by accident**, which is the visible half. Five
channels in two minutes leaves nothing behind.

**Two timeouts exist where there was one**, and that is the real cost: a reader
now has to know which applies. It is mitigated by the two being named for what
they are rather than sharing a constant, and by the reason living here.

**A test that asserts the shipping path.** The existing one covers progressive;
this needs its sibling — a live HLS session, nobody polling, and the session
gone. Without it this ADR is a decision with the same hole underneath it.

**Some bandwidth stops being spent.** Worth stating last, because it is the
consequence that reads as the point and is not: the reason to fix this is that a
person surfing channels gets told their own server is busy.

## What would make this wrong

**If thirty seconds turns out to strand real viewers.** A client on a poor
connection, or one that backgrounds its polling — a phone with the screen off,
a television that suspends the tab — could go quiet for longer than thirty
seconds and still have somebody in front of it. If that happens the answer is not
to creep the number back toward ten minutes, but to have the client say it is
still there, which is a different mechanism with a different name.

**If the explicit stop turns out to be reliable enough alone.** It will not be —
a process that is killed sends nothing — but if measurement showed the timeout
never firing in practice, that would be evidence the beacon is doing the work and
the timeout could be longer rather than the reverse.
