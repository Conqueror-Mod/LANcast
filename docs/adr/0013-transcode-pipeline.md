# ADR 0013 — Transcode pipeline: progressive by default, HLS alongside

Date: 2026-07-22 · Status: accepted · Amendment proposed 2026-08-23 for the live TV path

> **Amendment note.** The decision below stands unchanged for films and
> episodes. Live TV, which did not exist when this was written, turned out to
> be the workload a progressive stream cannot serve — six commits in a week
> hand-rolled what Media Source Extensions provides directly. The original
> reasoning is kept rather than deleted; the revisit is appended at the end of
> this file under **Amendment — 2026-08-23**.

## Context

The probe layer (ADR 0012) decides that ~55% of a real library needs remuxing
or transcoding for a browser. This ADR is the pipeline that acts on that.

The plan called for HLS with fMP4 segments, and the server produces exactly
that. But a browser cannot consume it: Chrome, Edge, and Firefox have no native
HLS, and playing it needs hls.js — a ~300KB third-party library this build does
not vendor. Shipping an unaudited binary blob to satisfy the default client is
the wrong trade for a self-hosted server whose whole premise is that you own
what runs.

## Decision

Build both outputs from one session machinery, differing only in ffmpeg flags:

- **Progressive fragmented MP4** to an HTTP response is the default. It plays in
  any browser with no client-side library. The cost is that seeking is limited
  to what has been produced — there is no playlist to seek within.
- **HLS with fMP4 segments** is served alongside, for clients that can use it (a
  future TV app, or a browser once hls.js is vendored deliberately).

The client asks `/playback` for the decision, then chooses the source. A file
that direct-plays uses the range-served `/stream` endpoint; anything else uses
the transcode endpoint. A direct-play guess that fails falls back to transcoding
once rather than showing a black rectangle.

## Consequences

**Good — it works in the default client today**, with no dependency to audit or
update. Verified: a HEVC file Chrome cannot decode plays through the progressive
endpoint, output confirmed as H.264 by ffprobe and by the browser reaching
`readyState: HAVE_ENOUGH_DATA`.

**Good — one pipeline, two outputs.** Argument construction, session lifecycle,
process supervision, and cleanup are shared. HLS is not a separate subsystem; it
is the same session with `-f hls` instead of `-f mp4`.

**Good — the decision drives the flags.** An audio-only transcode copies the
video stream (`-c:v copy`) and re-encodes only audio, which is what keeps a
third of the library off the CPU-heavy path. Argument construction is pure and
tested against every decision shape without spawning ffmpeg.

**Good — a closed tab kills ffmpeg.** The progressive reader is tied to the
session: closing the HTTP response stops the process. HLS sessions are reaped on
idle, since a segment client does not signal that it has left. Verified: closing
the player dropped active sessions to zero.

**Good — bounded concurrency.** Each transcode is a full ffmpeg process. A
ceiling (default 3) refuses the fourth rather than letting every stream stutter.
A home server that quietly falls over under load is worse than one that says no.

**Cost — progressive playback cannot seek past the transcoded point.** Dragging
the scrubber forward restarts the stream from `?t=`. This is the honest limit of
a format with no playlist, and it is stated in the UI rather than left to
surprise. HLS solves it, once a client can consume HLS.

**Cost — no hardware acceleration.** libx264 on the CPU. A full 4K HEVC
re-encode may not keep up with real-time playback on a modest machine. Per the
M3 plan this is deliberate: a correct software pipeline first, with the encoder
behind a flag so hardware slots in later. The measured library is 26 HEVC files;
most transcodes are the cheap audio-only path.

**Cost — subtitles are dropped.** Burning them in forces a video re-encode even
when the video is fine, and carrying them in fMP4 needs WebVTT conversion. Both
belong with the subtitle work, not here.

**Cost — ffmpeg is a hard dependency for the ~55% that need it.** It stays
optional: without it the transcode endpoints return a clear "not installed"
rather than failing obscurely, and direct-playable files are unaffected.

## The security note

The transcode endpoints hand a filesystem path to a subprocess, which is a
stronger reason to verify library containment than serving bytes directly is —
so they run the same `containedPath` guard as `/stream`. Segment names arrive in
URLs and become filesystem paths; they are validated to the exact shapes ffmpeg
produces, the same discipline as artwork hashes. Both are covered by tests that
assert traversal attempts are refused.

## Revisit when

A TV client exists (make HLS primary for it), hls.js is vendored deliberately
(HLS in the browser, enabling mid-transcode seeking), or transcode load on real
hardware justifies the hardware-acceleration swamp.

---

# Amendment — 2026-08-23: live TV is the case progressive cannot serve

Status of this amendment: **proposed**. The decision below is not taken until
somebody agrees to the dependency it asks for.

## What changed

Nothing about the original reasoning is wrong, and none of it is withdrawn. What
changed is that live TV arrived, and live TV is the one workload where the
missing control surface is not a stated limit but the entire problem.

The original text names the cost precisely — "progressive playback cannot seek
past the transcoded point ... this is the honest limit of a format with no
playlist". That was written about a film. Applied to a channel it understates
the case, because for live the missing playlist costs more than seeking: it
costs the ability to know how much media the element is holding, to skip a gap,
to sit at a live edge, or to tell being starved apart from being stuck.

Six commits between 2026-08-16 and 2026-08-23 are all one shape:

| commit | what it added |
| --- | --- |
| `91c0d12` | a head-start cushion before first play |
| `18416f5` | rebuilding that cushion after a stall, not only at start |
| `f2261ef` | keeping a channel near its live edge |
| `d26f552` | not pausing a player that already holds two minutes |
| `2ebb69e` | `internal/livebuf` — server-side jitter absorption |
| `61d9c78` | surfacing catch-up rate in the UI |

Every one of those is a hand-rolled reimplementation of something Media Source
Extensions provides directly: buffer-for-playback and buffer-after-rebuffer
thresholds, gap skipping, live-edge management, and a `buffered` range that
means what it says.

## Why the client cannot do this well, stated mechanically

A progressive `<video>` gives no control surface, and three specific
consequences fell out of that this week:

- **`buffered.end` does not mean what it appears to.** On a progressive
  response it reflects what the element has parsed, not what it holds and not
  what is in flight, so the obvious "how much media do I have" question has no
  reliable answer. `d26f552` exists because a player holding two minutes of
  media was being paused on the strength of that number.
- **`waiting` fires at roughly once a second regardless of buffer depth**, so it
  cannot distinguish *starved* from *stalled*. Plezy — an unrelated Flutter
  client for Plex/Jellyfin/Emby, examined for exactly this question — still
  needed a `BufferingStallPolicy` to draw that line even with a real player
  underneath: position advanced under 250 ms, buffer nonetheless adequate, 12 s
  elapsed, playback wanted. With a bare element the inputs to that test are not
  available.
- **Seeking a non-range-requestable stream strands the element.** `f2261ef`'s
  live-edge work had to avoid seeking altogether; the comment at
  [LiveTV.tsx:150](../../web/src/screens/LiveTV.tsx) records that an earlier
  version seeked to the edge and could not. Catch-up is therefore done by
  `playbackRate`, which is why the UI now has to *tell the viewer* the picture
  is running fast.

The last one is the tell. A workaround that needs its own onscreen explanation
is a workaround that has run out of room.

## The corroborating survey

Plezy is GPL-3.0 and LANcast is MIT, so it was read for architecture and
parameters only; no implementation is carried across. Two findings bear on this
decision.

**Every comparable client uses a player that speaks the format.** Plezy vendors
no Flutter media package at all — it maintains its own ExoPlayer plugin on
Android and its own libmpv binding elsewhere. Both speak HLS and MPEG-TS
natively, so no remux-to-progressive step exists anywhere in that codebase. The
question "does a real player just handle it" answers yes, and the price of yes
is a player, obtained at whatever layer is cheapest.

**Their buffer numbers validate ours and correct one shape.** ExoPlayer's
`LoadControl` as they configure it: 1,000 ms to begin, **5,000 ms to resume
after a rebuffer**, 30–60 s steady-state on a normal device, and a
user-selectable tier running to 240 s. `PREROLL_SECONDS` is a single 3 s number
used for both cases. Starting fast and resuming conservatively is free to adopt
and does not depend on this amendment.

Their `live_seek_accumulator.dart` also describes our disease from the other
side: re-reading a live position that has not finished settling, debounced at
300 ms and pinned for up to 1,500 ms. The general rule — never re-read a value
the stream has not finished producing — is the one `buffered.end` breaks here.

## What the original objection was, and what survives of it

The objection was never "a dependency". It was, precisely, an **unaudited binary
blob** shipped to satisfy the default client, on a server "whose whole premise is
that you own what runs". That premise is not up for revision and this amendment
does not revise it.

What is now in evidence is the cost of the alternative. The original trade was
"~300 KB of somebody else's code" against "a stated seeking limit". The real
trade turned out to be ~300 KB of reviewed, pinned, widely-deployed code against
roughly a thousand lines of our own buffer-management code — client and server —
that reimplements the same thing, is tuned against one provider's segment
interval, and is documented in `livebuf.go` as "a guess about a stranger's
segment interval — a guess that was wrong by half until it was measured". Ours
is not audited either. It is merely ours.

So the objection is met by *how* the dependency is taken, not by refusing it:

- vendored as **source into the repository**, pinned to a specific commit,
  reviewed as any other checked-in code — not pulled at build time from a
  registry, and not a prebuilt bundle
- **updated deliberately**, by a human reading a diff, never automatically
- confined to the live TV path, so that a decision to remove it later is a
  deletion rather than a rewrite

That is a different artefact from the one ADR 0013 refused. A blob you cannot
read and a file you can are the same size and not the same risk.

## A second symptom, on the VOD path

The live-TV case above is the reason for this amendment. A separate observation
belongs beside it, because it is the same shape and it is on the path this
amendment leaves unchanged.

A single playback of one film starts several progressive transcode sessions.
Measured on installed v0.8.17, one item, played and left strictly alone — no
seeking, no pausing, no navigation:

    run 1: 6 sessions, +57.8s +10.0s +4.9s +10.0s +1.7s, then quiet for 2m46s
    run 2: 5 sessions, +18.3s +0.7s +171.1s +3.6s

Two framings of this were wrong before the third, and both are worth recording
so they are not re-derived.

It is **not a leak and not unintended server behaviour.** `Manager.Progressive`
has no session reuse by design, `supersede(owner, itemID)` makes one stream per
viewer per item a guarantee, and only one ffmpeg is ever alive. `sameOffset` is
HLS-only and is not involved.

It is **not the client re-rendering.** Every client path that issues a new
request — the initial-load effect, `retryWithoutClaims`, and `seekTo` — reassigns
`v.src` and calls `v.load()`, which visibly resets the timecode and shows a
note. Polling the log until a session started and screenshotting immediately
caught playback continuous at 7:45 with no note and no reload. None of those
paths fired.

What is left is the media element itself, opening additional connections to a
long progressive response it cannot range-request. `internal/api/transcode.go`
is the only caller of `trans.Progressive`, so each session is exactly one HTTP
GET; with the client ruled out, the element is what issued them. That also
completes the note in `manager.go` beside `supersede`, which attributed two
starts six milliseconds apart to seeking: the attribution was incomplete rather
than wrong, since no seeking occurred in either run above.

**The discriminating reading has not been taken yet.** `start_at` was added to
the progressive log line for exactly this (v0.8.18); the runs above predate it.
Successive sessions carrying an advancing `t` near the playback position confirm
a reconnecting element; a fixed `t` means something else is asking. Until that
reading exists, the mechanism above is the surviving hypothesis and not a
finding.

Nothing is proposed for it here. Playback is unaffected, one process runs, and
on a copy path a reconnect costs an ffmpeg start and a re-probe — cheap. On a
full encode it is not cheap, and a file re-encoded from a fresh offset
repeatedly is real waste, which is what would make it worth acting on.

Its bearing on this ADR is that it is the same class of defect as the live-TV
one, arriving from the other direction: a progressive stream offers the client
no control surface, so the client's only way to ask for anything is to open
another connection, and the server's only way to answer is another ffmpeg. MSE
removes the class rather than the instance. That is evidence about the shape of
the decision; it is not on its own an argument for moving the VOD path, which
stays where the original ADR left it.

## Decision (proposed)

**Adopt MSE for live TV, and only for live TV.**

The live path becomes: server produces HLS with fMP4 segments — which it
*already does*, from the same session machinery, per the original decision — and
the client feeds it through hls.js into a `MediaSource`. Nothing new is built on
the server. The HLS output that has been served alongside since day one, for
"a future TV app, or a browser once hls.js is vendored deliberately", finally has
its consumer. This is the second of the three conditions the original "Revisit
when" listed, arriving on its own.

**Progressive fMP4 remains the default for VOD.** Films and episodes are not
bursty, do not have a live edge, and play correctly today. Changing them buys
mid-transcode seeking, which is real but is a separate decision with its own
regression surface, and it is not what this evidence is about. One workload, one
change.

**`internal/livebuf` stays.** It is on the correct side of the network and it
addresses a problem MSE does not: the provider's publishing rhythm is upstream
of anything the client can do, and 98% of a measured 42 s window was silence.
MSE removes the need for the client to *guess* at that rhythm; it does not
remove the rhythm. The client-side constants in `preroll.ts` are what MSE
replaces.

## Consequences

**Good — the live-edge, catch-up and cushion code becomes deletable.** Not
rewritten against a new API: deleted, because `MediaSource` answers the
questions those workarounds exist to guess at. The `playbackRate` catch-up and
its explanatory badge go with it.

**Good — starved and stalled become distinguishable**, which is currently
impossible and is why a slow provider and a wedged ffmpeg present identically as
a spinner.

**Good — mid-stream seeking within a live window becomes possible**, which is the
precondition for anything resembling time-shift or pause-live-TV. Not proposed
here; simply no longer structurally blocked.

**Cost — a third-party dependency in the client, permanently.** Confined to live
TV, vendored as reviewable source, pinned. This is the trade and it should be
named as one, not softened.

**Cost — a second playback path in the client.** Live and VOD now differ in how
the element is fed, and a bug can live in one and not the other. This is
mitigated by the paths already differing in every other respect — Plezy models
live as a wholly separate session type with its own capability flags, having
reached the same conclusion independently — but it is a real maintenance cost.

**Cost — browsers with native HLS take a different path again.** Safari plays
the playlist URL directly and must not be handed an MSE pipeline it does not
need.

**Unchanged — no client framework rewrite.** The evidence here is about the
playback layer and says nothing about React. A native or Flutter client would
fix live TV incidentally, by replacing everything else too, and the server —
which is where LANcast's value is — does not move in that trade. If TV and
mobile surfaces are wanted later, that is a reach argument to be made on its own
merits, as a *second* client rather than a refactor.

## Work breakdown

Ordered so each step is verifiable alone, and so the dependency is the last
thing added rather than the first.

1. Adopt the asymmetric cushion in `preroll.ts` — fast to start, conservative to
   resume after a rebuffer. Independent of everything below; ship it either way.
2. Confirm the existing HLS output plays a live channel end to end, using a
   throwaway harness. If it does not, this amendment is premature and the fault
   is on the server side.
3. Vendor hls.js as pinned source, with a `README` in its directory recording
   the commit, the review date, and who read it.
4. Live playback goes through MSE behind a setting defaulting to off, so the
   progressive path remains one toggle away during the transition.
5. Native-HLS browsers detected and given the playlist directly.
6. Default flipped, then `preroll.ts` and the live-edge workarounds deleted —
   deletion is the acceptance test. Client tests updated in the same commit;
   note that jsdom performs no media, so this needs looking at as well as
   asserting.
7. `docs/api.md` in the same commit as any endpoint or contract change.

## Revisit when

The original conditions stand for the VOD path. For live: if a native client
surface arrives (ExoPlayer and mpv both speak HLS, making this vendored library
redundant there), or if a hls.js review finds something that fails the standard
in step 3 — in which case the correct answer is not to soften the standard.
