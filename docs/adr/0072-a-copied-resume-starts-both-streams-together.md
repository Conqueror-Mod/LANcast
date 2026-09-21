# ADR 0072 — A copied resume starts both streams together

Date: 2026-09-21 · Status: proposed

Records a decision that has until now lived only in comments in
[`internal/transcode/args.go`](../../internal/transcode/args.go), and changes
it. Amends the seek behaviour described there and referenced by
[ADR 0013](0013-transcode-pipeline.md).

## Context

Reported from a phone: resuming a film mid-way, *the picture runs about two
tenths of a second behind the sound.* Resuming a different film at a different
point, worse.

### What is happening

When the video track is copied, `-ss` cannot land where it is asked. There is
no decoding, so ffmpeg must begin at a keyframe — while the audio, which *is*
being re-encoded, begins exactly where it was told. The two streams therefore
start at different points in the film, and the gap is the distance back to that
keyframe.

Measured on `Jay and Silent Bob Reboot (2019).mkv`, H.264 + DTS, with
`-copyts` so the numbers are positions in the film rather than rebased ones.
Keyframes near the resume point are at **394.603**, **399.649** and **404.571**:

| seek | video begins | audio begins | gap |
|---|---|---|---|
| 396.000 | 394.603 | 395.979 | 1.376 |
| 399.000 | 394.603 | 398.979 | 4.376 |
| 399.649 *(exactly a keyframe)* | 394.603 | 399.628 | 5.025 |
| 399.700 | 394.603 | 399.679 | 5.076 |
| **399.900** | **399.649** | 399.879 | **0.230** |
| **400.000** | **399.649** | 399.979 | **0.330** |
| 401.000 | 399.649 | 400.979 | 1.330 |
| 403.000 | 399.649 | 402.979 | 3.330 |

Two things fall out of that table, and the second is the one that matters.

**The gap is `seek − keyframe`.** Nothing else. It is a property of where the
resume lands relative to the previous keyframe, which is why it was 0.33s on
one resume and 3.1s on another of the same film.

**ffmpeg will not use a keyframe until the seek is about 200ms past it.**
Seeking to 399.700 — fifty milliseconds *after* a real keyframe — still starts
the video at the one five seconds earlier. Only at 399.900 does it use 399.649.

### Why the obvious fix is wrong

Snapping the requested position back to the keyframe is the first thing anyone
would try, and it makes things **fifteen times worse**: every value from
399.649 to 399.749 produced a gap of 5.0 seconds rather than 0.33, because each
of them falls inside that margin and drops to the previous keyframe.

The margin also puts a floor under the whole family of seek adjustments. The
best a seek can do is land just past it, which leaves `margin − one audio
frame` ≈ **0.18s**. Better than 0.33 and still visible, still varying by file.

**So the streams cannot be aligned by choosing a better seek.** That is the
finding this decision rests on, and it was reached by measuring six candidate
values, all of which were worse than doing nothing.

### The output is not wrong

Worth stating, because it decides where the fix belongs. The timestamps
honestly encode what happened: video begins at 399.649, audio at 399.979, and
the container says so. A player that honours them shows a third of a second of
silent picture and is then in sync for the rest of the film.

The player on the reporting phone does not. It starts both streams together, so
the picture it shows is always that gap behind the sound. We do not control
that player, and there is no reason to think we control the next one either —
so the fix is to stop producing a gap rather than to explain it.

## Decision

**Video and audio are taken from two demuxers of the same file, and the audio
is seeked to the keyframe the video will actually start on.**

Measured, same file and resume point:

| | video begins | audio begins | gap |
|---|---|---|---|
| today, one input at `-ss 400` | 399.649 | 399.979 | 0.330 |
| **two inputs, audio at the keyframe** | **399.649** | **399.628** | **−0.021** |

Twenty-one milliseconds is one AAC frame. The streams start together.

### 1. The server finds the keyframe before it starts the conversion

It has to: nothing else knows where the video will begin, and ffmpeg will not
say in advance. A bounded `ffprobe` over the seconds before the resume point,
decoding keyframes only, answers it.

Measured cost on the reference machine, at three points in a 105-minute film:

| window | 400s | 3,000s | 6,000s |
|---|---|---|---|
| 5s | 89 ms | 117 ms | 108 ms |
| 10s | 128 ms | 136 ms | 67 ms |
| 15s | 211 ms | 165 ms | 96 ms |

**A ten-second window is the choice.** It found the keyframe at every point
tested — the furthest was 2.4 seconds back — and costs under 150ms against a
conversion that takes a hundred times that.

### 2. Only when the video is copied, and only when resuming

A re-encoded video starts exactly where asked, because it is decoded; there is
no gap and nothing to fix. A resume at zero has nothing to be out of step with.
Both cases skip the probe entirely, which is most playback.

### 3. No keyframe found means today's behaviour, not a failure

If the probe finds nothing in its window — a file with very long GOPs, a probe
that fails, a format that does not answer — the conversion runs exactly as it
does now, with one input and the lead-in it produces. That is a worse picture,
not a broken one, and it is what people have been watching.

**A conversion must never fail because an alignment could not be improved.**

### 4. The keyframe lookup is pure parsing over a thin process call

`internal/probe` already exists to keep process execution split from the
decisions that use it ([ADR 0012](0012-probe-before-transcode.md)), and the
same rule applies here: parsing ffprobe's answer is pure and tested against
fixtures, running it is a few lines. The alternative is a rule that can only be
tested with a film on disk.

## Rejected

**Snap the seek back to the keyframe.** Disproven above — six values, all
worse, the best of them fifteen times worse than doing nothing.

**Seek just past the keyframe.** The honest version of the same idea, and the
200ms margin puts a floor of ~0.18s under it. A smaller gap is not an aligned
stream, and the remaining error still varies by file.

**Pad the audio with silence.** Prepending silence equal to the gap would make
a player that ignores timestamps correct. It needs exactly the same keyframe
knowledge as the decision above, so it costs the same probe — and it throws
away audio that exists and could simply be played instead.

**Re-encode the first group of pictures** so video can start exactly where
asked. Correct, and it gives up the copy on the path whose entire purpose is
not re-encoding.

**Leave it.** Defensible while it was believed to be milliseconds. It is
bounded by the GOP, which was 5 seconds on this file and is longer on others,
and 3.1 seconds of lip-sync error was measured on a real resume.

## Consequences

**Every resume of a copied-video file gains a process launch.** Under 150ms,
before a conversion that takes minutes, and skipped entirely for direct play
and for re-encodes. It is a real cost and a small one.

**Two demuxers read the same file at once**, briefly, at two positions. That is
more IO than one, on a path that is already IO-bound, and the second demuxer
only reads audio.

**The alignment is only as good as the keyframe list.** A file whose keyframes
ffprobe reports inaccurately would get a wrong audio seek and a *worse* result
than today. This is the risk worth watching, and it is why §3 falls back rather
than trusting a doubtful answer.

**This does not fix the drift for anyone already watching.** It applies to
conversions started after it ships.

## What this does not decide

**Whether the picture should start at the keyframe or the requested second.**
It starts at the keyframe, as it does today, which means a resume begins up to
a GOP early. That is the most a copy can offer and nobody has complained about
it.

**Anything about the live path.** Live has no resume.

## Revisit when

A file is found whose keyframes ffprobe reports wrongly (§ Consequences), or
ffmpeg's seek margin changes and the measurements in Context stop holding.
