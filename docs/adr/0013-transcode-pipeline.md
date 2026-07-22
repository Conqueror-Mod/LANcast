# ADR 0013 — Transcode pipeline: progressive by default, HLS alongside

Date: 2026-07-22 · Status: accepted

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
