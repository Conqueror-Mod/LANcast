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

## Amendment — 2026-09-11: an encoded film's playlist is written whole

**The risk above landed.** The desktop client fell back from segments to the
progressive stream on 21 of 21 file playbacks in the log. Every candidate that
could be tested from outside the app came back clean: playlist content, encoder,
declared level, MIME types, URL rewrite, throughput, TLS and the certificate
pin, and whole delivery (a superseded session served exactly twice the bytes on
disk).

### Reproduced

A real WebView2 window (`cmd/wv2harness`), fed by a server that delivers exactly
as `internal/api` does and logs every request (`cmd/segserve`), running the
server's own ffmpeg command:

| playlist | result |
| --- | --- |
| finished (`ENDLIST`), plain static server | plays |
| finished, LANcast delivery, HTTP | plays |
| finished, LANcast delivery, HTTPS + pinned certificate | plays |
| **growing, as ffmpeg writes it** | **code 4 `DEMUXER_ERROR_COULD_NOT_PARSE` at 0.45s** |

The request log is the mechanism:
1. The playlist is fetched, listing one segment.
2. `init` and `seg00000` go out whole.
3. **The playlist is fetched again 88ms later, unchanged.**
4. The error follows 20ms after that.

WebView2 treats a growing playlist as live, reloads it at once, and fails when
it has not grown. Chrome and Edge poll patiently, which is why the measurements
this ADR was built on never saw it.

Holding back the first playlist until four segments existed played, but from
the live edge. `seg00000` was never requested, so a film started from the
beginning would silently lose its opening. That was not a fix.

### The decision

**When video is encoded, the server writes the playlist itself: the whole of
what remains after `t`, `VOD`, closed with `ENDLIST`, from the first response.**
Segments the encode has not reached are waited for when they are asked for.

Measured with a 4K HDR film, started 22 minutes in, before any segment existed:
- Playback ran from `seg00000`, 36 seconds continuous.
- The playlist was fetched once.
- Segments were requested one at a time, about six seconds ahead of the
  picture, as the encode produced them.

Three conditions came with it.

**Durations are estimates, and may be.** A segment is the smallest whole number
of GOPs that reaches six seconds — 6.006s at 23.976fps, which is what ffmpeg
wrote for all 49 segments measured. With every segment listed deliberately wrong
(5.5s against a real 6.006s), the same engine played identically, because it
places a fragment by its own timestamps. The client never seeks inside one of
these playlists: a seek while converting still re-requests with a new `t`.

**A segment is served when ffmpeg has finished it, not when it exists.** A
complete playlist has the player asking ahead of the encode, and ffmpeg writes
segments in place. The first run of the experiment served `init.mp4` at 0 bytes
and failed with `APPEND_FAILED`. Readiness is now "listed in ffmpeg's own
playlist", which ffmpeg writes only after closing a segment.

**Only encoded video.** A segment starts on a keyframe. The encode places them;
a copied video track brings the source's own, which nothing has read. A copy
keeps the growing playlist, and on WebView2 still falls back — **that remains
open**. The playlist route says which kind it served (`X-LANcast-Playlist`), and
the client no longer counts a refused growing playlist against the device: three
copied films would otherwise settle the device as unable to play playlists, and
retire the path for every encoded film that now plays on it. The verdict key
moved to `-4`, because every refusal recorded before was of the growing kind.

**Not yet verified as the service.** The experiment ran the server's command
from a shell with the same encoder and tone-map filters. Delivery is not GPU
work, but the encode feeding it is, so the check that counts is still a playback
in the installed client against the installed service.

## Amendment — 2026-09-11 (later): the playback that counted, and what it found

**Playing it as the service found a second fault that no experiment could.** On
v0.9.13 the encoded film got `playlist=complete`, and ffmpeg ended 2.5 seconds
later with an empty reason. `startHLS` built ffmpeg on the playlist request's
context, and Go cancels that context when the handler returns, which is the
moment the playlist has been sent. Every segmented session had died after one
segment. The harness ran ffmpeg itself, so it never went through the handler.
v0.9.14 detaches the session. The Fifth Element then played continuously on the
new path, 47 segments in two minutes, with no fallback.

**A copied video track then played, but restarted every forty seconds.** It's
Always Sunny S16E01 (`video=copy audio=copy`, growing playlist) produced a new
session every 36–45 seconds, each delivering the same ~52 MB. The client's
engine plays a growing playlist as far as its first fetch listed and fires
`ended`, and the client's cut-stream recovery restarts from there. The remux
had meanwhile written the remaining twenty minutes, 205 segments, and
`ENDLIST` in **three seconds**.

**So a copied session's playlist waits for ffmpeg to finish it**, up to twenty
seconds, and is served finished (`X-LANcast-Playlist: complete`). A finished
playlist is one that engine plays. Past the wait it goes out growing, as
before, and a log line says so. That answers "that remains open" above for any
remux that completes in time. A remux slow enough to miss the wait, such as a
long film over a slow network share, still restarts.
