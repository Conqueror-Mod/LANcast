# ADR 0012 — Probe and decide before building a transcoder

Date: 2026-07-22 · Status: accepted

## Context

M3's goal is "plays anywhere", and the obvious first move is to build the
ffmpeg pipeline. But a transcoder is only useful once something decides *when*
to invoke it, and through M2 LANcast knew nothing about file contents — every
`duration_ms` was null. There was no basis for that decision at all.

Two shapes were available: build the transcoder and probe as part of it, or
build probing and the decision as their own layer first.

## Decision

Probe first, as a separate package with the decision engine beside it.
`internal/probe` wraps ffprobe, persists results, and exposes `Decide()` which
returns direct play, remux, or transcode with a stated reason.

Parsing is split from process execution: `ParseJSON` is pure, so the logic is
tested against fixtures with no ffmpeg installed and no media on disk.

Probing runs in its own worker, not inside metadata enrichment.

## Consequences

**Good — the decision is testable in isolation.** Whether a file should
transcode is answerable without spawning ffmpeg, which means the rules can be
exercised against dozens of codec combinations in milliseconds. Entangled with
a transcoder, each case would need a real encode.

**Good — it paid off before any transcoding existed.** Duration is now known
for every file, so progress bars and resume percentages work. Resolution and
codec are available for display and filtering. These were free side effects of
needing the data anyway.

**Good — measuring beat guessing.** Run against a real 225-film library, the
probe data corrected two assumptions:

- A browser profile capping audio at 2 channels needlessly transcoded 18 files
  whose multichannel AAC browsers decode fine. The limit conflated downmix
  preference with capability.
- 33% of the library has playable video and undecodable audio (AC-3, E-AC-3).
  Keeping audio-only transcode as a distinct outcome means 75 films copy their
  video instead of re-encoding it. A simpler design that transcoded whole files
  on any incompatibility would have burned CPU on all of them.

Neither would have surfaced from reasoning about it.

**Good — every decision explains itself.** "Why is my server at 100% CPU" is
the question a media server most often has to answer, and `Reason` is surfaced
in the UI rather than buried in logs.

**Cost — probing is a separate worker.** More machinery than folding it into
enrichment. Necessary: enrichment needs an API key and stops early without one,
so a library with no TMDB key would never be probed, and probing is what
playback depends on.

**Cost — a failed probe is stamped as probed.** A file ffprobe cannot read
would otherwise re-probe on every pass forever and the queue would never drain.
The item keeps null codec fields, so `Decide` falls back to direct play, which
is what LANcast did before probing existed.

**Cost — ffmpeg is now a real dependency** for informed decisions. It stays
optional: without it, playback decisions assume direct play and the server logs
that once rather than failing per item.

## Measured result

For the reference library: 45% direct play, 5% remux, 33% audio-only
transcode, 16% full transcode. 225 files probed in 5 seconds, zero failures.

That distribution is the argument for the three-way decision. A binary
play-or-transcode split would have sent 54% of the library through a full
re-encode instead of 16%.
