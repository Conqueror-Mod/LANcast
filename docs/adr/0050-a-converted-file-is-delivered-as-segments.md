# ADR 0050 — A converted file is delivered as segments

Date: 2026-08-30 · Status: **accepted**

Amends [ADR 0013](0013-transcode-pipeline.md)
for films and episodes. 0013 chose progressive fMP4 as the default and built
HLS alongside it; its live-TV amendment left the file path explicitly
unchanged. This changes the file path, and does not reopen the part of 0013
that matters most — **hls.js is still not vendored, and nothing here needs it**.

## The fault

`All About the Benjamins` — 3.85 GB, 95 minutes, h264 copied, ac3 re-encoded,
5.4 Mbps — logged **twelve transcode sessions in eighteen minutes, every one at
`start_at=0`**:

```
11:22:46  session=dc0e5de9  item=6632  start_at=0  video=copy audio=encode
11:22:50  session=c06f103f  item=6632  start_at=0  video=copy audio=encode
11:25:49  session=ac427f3c  item=6632  start_at=0  video=copy audio=encode
…nine more
```

No ffmpeg error. Nothing reaped. One ffmpeg alive at the end.

Reported as *"lagging every few minutes; took about 15 minutes to start
experiencing problems"*, and both halves of that sentence fall out of the
mechanism. A progressive transcode is one response of unknown length that
cannot be range-served, because bytes ffmpeg has not produced do not exist yet.
The handler says so honestly:

```go
w.Header().Set("Accept-Ranges", "none")
// A live transcode has no known length and cannot be range-served …
// Saying so plainly stops browsers issuing range requests that could
// never be satisfied.
```

The first two sentences are true. **The third is a claim about a browser, and
the log disproves it.** Chromium caps how much media it will hold; at 5.4 Mbps
that cap is a few minutes. When it evicts and needs those bytes again it cannot
ask for a range — so it drops the connection and starts the film over from byte
zero. The further in the viewer is, the more must be re-streamed before the
picture moves, which is exactly why nothing was wrong for the first quarter of
an hour.

This is the fourth time in this project that a comment asserting what software
does in a failure case turned out to be reasoning rather than observation.

### Why it stayed invisible

Session *births* log at Info. The only ending that logs is a supersede — a
client closing the stream goes through `Stop`, which says nothing. So the
evidence was twelve starts and no endings, which reads as a leak rather than as
a client re-asking. That is the same half-fixed unreadability #431 addressed
when it raised the supersede line to Info.

## What was measured

Against a real VOD playlist, on Chrome 148:

| | result |
|---|---|
| Plays from `src`, no library | `readyState` 4, both tracks decoding |
| Duration | 30.03s, correct |
| Seekable range | 0 – 30.05 |
| Seek forward to 25s | landed in **52ms** |
| Seek backward to 2s | landed in **35ms** |

Native, seekable, no dependency. The progressive path can offer none of it: a
response with no ranges has no seekable range to expose, which is also why every
seek today re-requests the whole stream with a new `t=`.

## The decision

**A conversion is delivered as an HLS playlist where the engine can read one.
Direct play is untouched.**

Direct play is the file's own bytes over a range server — already seekable,
already re-askable. The eviction problem belongs to streams that cannot be
re-asked, so routing a direct play through a transcode-backed playlist would
spend an encode to fix a problem it does not have.

Evicting a segment now costs one segment, and the element asks for exactly that
one.

### The capability is discovered, not asked

`canPlayType('application/vnd.apple.mpegurl')` answers **"maybe"** on Chromium —
and answers "maybe" whether or not playback will actually work. As a gate it is
worth nothing: it cannot separate an engine that plays HLS from one that will
show a black rectangle, and the engines this project meets — WebView2 on
whatever runtime is installed, television browsers — are precisely where the
answer differs from the desktop Chrome this was measured on.

So the capability is **discovered by trying it, once, and remembered per
device**. An element that rejects the playlist outright is the only reliable
evidence available, and it costs one failed load in the life of a device.

Guessing from a string that means "maybe" would be the same mistake as the
comment that caused this ADR.

The fallback is narrow on purpose. Only `MEDIA_ERR_SRC_NOT_SUPPORTED` — the
element saying it could not make sense of the resource at all — counts as a
verdict on the engine. A decode error is about *this file* and a network error
about *this moment*; retiring the better path over either would be a permanent
decision made from a transient fault. Success is recorded too, so a later decode
error cannot be mistaken for the engine lacking HLS.

## What this does not change

**hls.js is still not vendored.** 0013 declined ~300KB of unaudited third-party
library and that trade is untouched: the measurements above are of a bare
element playing a playlist from `src`. Nothing here adds a dependency.

**Live TV is unaffected.** It has its own transport machinery and its own
transition setting, and it is parked. This reuses `mediaCapability()` and
`HLS_MIME` from it rather than growing a second capability check — one
normalizer — and changes nothing about how a channel is delivered.

**Seeking is unchanged in this step.** A seek while converting still re-requests
with a new `t=`, exactly as before. HLS makes a better seek *possible* — the
element now has a real seekable range — but taking it is a separate change with
its own risks, and bundling it would have made this one hard to judge.

**The API is unchanged.** `/api/stream/{id}/hls/index.m3u8` and its segment
route already existed and already take the same `t`, `audio` and quality
parameters. This is a client choosing an endpoint the server has served since
0013.

## The risk worth stating

The measurements are from Chrome 148, and **the client that ships is WebView2**.
That is the same shape of gap that cost a release when `-hwaccel auto` was
verified from a shell and failed as a service: the environment never exercised
was the one that ships.

The difference is that this failure mode is handled rather than assumed away. A
WebView2 that cannot read a playlist refuses the source, gets progressive on the
next assignment, and records the refusal so no later film pays for it again —
which is worth one reload, once, on such a device. That is the whole reason the
capability is learned by trial instead of asked for.
