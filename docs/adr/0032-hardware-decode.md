# ADR 0032 — Hardware decode, and where the cost actually is

Date: 2026-08-13 · Status: accepted · shipped by v0.3.2 · Amended 2026-08-13 after measurement

> **Amendment note.** This ADR was first written proposing a two-stage rollout
> with decode-only shipping first. It was measured before any code was written,
> and the measurement overturned that decision: decode-only delivers a large CPU
> saving and *no* wall-clock improvement, because the dominant cost is the CPU
> scale it leaves in place rather than the decode it removes. The staging is
> revised below. The original reasoning is kept rather than deleted — the plan
> was wrong for a specific, instructive reason, and the numbers are now in the
> record.

## Context

LANcast is half-accelerated, and it is accelerated on the cheaper half.

[ADR 0013](0013-transcode-pipeline.md) built the transcode pipeline and
`hwaccel.go` added encoder detection: NVENC, Quick Sync, AMF and VideoToolbox
are each verified by a real test encode and used in preference to libx264. What
was never added is the other side of the pipe. The comment at
[hwaccel.go:61](../../internal/transcode/hwaccel.go) states it plainly, as a
justification for something else:

> There is no `-hwaccel` decode, so frames are in system memory

So every transcode today decodes on the CPU and encodes on the GPU. On the
files that actually need transcoding that is backwards. A 4K HEVC Main10 source
is expensive to *decode* — it is the reason the file needed touching at all —
and H.264 output at 720p is comparatively cheap to encode. The accelerated half
is the half that was already affordable.

This has become more valuable than it was. [ADR 0031](0031-quality-selection.md)
added a client-selectable quality ceiling, and a ceiling's whole purpose is to
force encodes that would not otherwise happen: capping a direct-playable 4K file
at 720p converts a zero-cost delivery into a full decode-scale-encode. The
feature most likely to make the server work hard is now a user-facing control,
which raises the price of the decode gap from "a slow transcode occasionally" to
"the setting people reach for is the expensive one".

## The two designs

There is not one hardware-decode change; there are two, and conflating them is
the main way this goes wrong.

**Decode-only.** `-hwaccel cuda` before the input. The GPU decodes, and ffmpeg
downloads the frames back into system memory. Every existing filter and format
flag keeps working unchanged, because from `-vf` onwards the pipeline looks
exactly as it does today. Saves the decode cost, pays a readback.

**Full GPU residency.** `-hwaccel_output_format cuda` keeps the decoded surfaces
on the device, a vendor scaler (`scale_cuda`, `scale_qsv`, `scale_vt`) resizes
them there, and the encoder takes them without a round trip. Saves the decode
cost *and* the readback *and* the CPU scale. It also invalidates both flags the
current pipeline relies on.

## The measurement

Taken before writing any code, on an RTX 3060 (Ampere) with ffmpeg 8.1.2.
Every cell produces the same 480 frames of the same output; timings are
ffmpeg's own `-benchmark` accounting, best of three, discarding the first run's
file-cache and clock-ramp effects.

**4K HEVC 10-bit → 720p, 4 Mbps ceiling**

| Pipeline | Wall | CPU (user) |
| --- | --- | --- |
| A. Software decode → NVENC *(today)* | 4.37s | **49.4s** |
| B. CUDA decode → download → NVENC *(decode-only)* | 5.05s | 15.1s |
| C. CUDA decode → `scale_cuda` → NVENC *(GPU-resident)* | **1.46s** | **0.39s** |

**1080p HEVC → 720p, same ceiling**

| Pipeline | Wall | CPU (user) |
| --- | --- | --- |
| A. *(today)* | 1.28s | 8.27s |
| B. *(decode-only)* | 1.31s | 1.67s |
| C. *(GPU-resident)* | **0.74s** | **0.70s** |

Two things fall out, and both contradict the original plan.

**Decode-only buys CPU and not latency.** It is 3–5x cheaper in CPU and very
slightly *slower* on the clock. The readback is synchronous and the 4K→720p
scale still runs on the CPU afterwards — 15 of decode-only's 15.1 CPU seconds
are that scale, not the decode. Shipping it alone would be a change that makes
the server measurably cheaper and every individual playback no faster, which is
not what anyone reaching for hardware acceleration expects to have bought.

**The scale was the bottleneck, not the decode.** The original text asserted
that a 4K HEVC source "is expensive to decode — it is the reason the file needed
touching at all". That is true of the file and false of the *pipeline*: once the
decode moves to the GPU, downscaling 4K frames on the CPU becomes the dominant
cost, and it is only eliminated by the half that was deferred. The premise was
wrong about where the time went, which is exactly the thing a measurement is for
and a plan cannot supply.

## Decision

### Go to GPU residency; decode-only is a checkpoint, not a release

The target is pipeline C: `-hwaccel_output_format`, a vendor scaler, and format
conversion on the device, with no readback.

Decode-only survives as an **intermediate commit** — it is genuinely useful for
bisecting a regression, and it is where the per-vendor `-hwaccel` spellings get
proven independently of the filter chain. It is not a milestone worth shipping
or measuring a release against, because on its own it improves nothing a user
can perceive.

This raises the risk the original staging existed to manage, and the mitigation
moves rather than disappears: the fallback below is now load-bearing, and the
per-vendor filter work gets built one vendor at a time behind the same
detection, rather than four at once.

### 10-bit conversion moves onto the device, and HDR comes with it

A hardware decoder hands back `p010` surfaces for 10-bit HEVC. On the current
path `-pix_fmt yuv420p` converts them through swscale; on a GPU-resident path
that conversion happens in the scaler (`scale_cuda=format=yuv420p` and its
per-vendor equivalents) and `-pix_fmt` comes out of the command line entirely.

10-bit HEVC Main10 is not an edge case — it is most of a modern 4K library, and
it is also where [ADR 0033](0033-hdr-tonemapping.md) lives. The two must be
designed together: both rewrite the same filter chain, and a tonemap inserted
into a GPU-resident pipeline is a different filter from one inserted into a CPU
one. Neither should land without the other having been read.

### The accelerator is data on the Encoder, not a branch

`Encoder` already spells one concept four ways: `-crf` / `-cq` /
`-global_quality` / `-qp_i` all mean "quality", and the struct carries the
spelling rather than the call site carrying a switch. Decode is the same shape
of problem and gets the same treatment — a `decodeAccel` field:

| Encoder | `-hwaccel` | Notes |
| --- | --- | --- |
| `h264_nvenc` | `cuda` | NVDEC. Best-documented path of the four |
| `h264_qsv` | `qsv` | Intel's own; `d3d11va` is the fallback if it proves fussy |
| `h264_amf` | `d3d11va` | AMF has no decode accelerator of its own on Windows |
| `h264_videotoolbox` | `videotoolbox` | macOS |
| `libx264` | *(none)* | Software decodes in software |

`Args` stays a pure function of `Options`. That is a standing rule of this
package — the split between argument construction and process execution is what
lets the decision rules be tested against fixtures with no ffmpeg installed —
and it is what makes "every vendor, every decision shape" a table of fast tests
rather than a hardware lab.

### Acceleration only when something is actually decoded

`-hwaccel` is emitted only when `VideoAction == "encode"`.

A remux copies the video stream and never decodes a frame; an audio-only
transcode maps no video at all. Asking for a hardware decoder in either case
initialises a GPU device to do nothing, which is latency added to the cheapest
paths in the system — and on a headless or contended GPU it is a new way for a
remux to fail that has no upside whatsoever.

### Detection is per accelerator *and* per codec

`DetectEncoders` verifies encoders with a real test encode, because ffmpeg lists
encoders the machine cannot run. Decode has the same problem and one more on
top: decode support is **per codec and per GPU generation**. Every NVDEC
generation decodes H.264; HEVC, VP9 and AV1 each arrived later, and AV1 decode
needs a card several generations newer than one that handles H.264 perfectly
well. A single "hardware decode works" boolean would claim AV1 on a card that
has never been able to do it — and the exact generation cut-offs are vendor
documentation to be looked up during implementation, not remembered here.

So detection produces a set of `(accelerator, codec)` pairs, and the decision to
use it is made per file against the source codec the probe already recorded.

### A runtime failure falls back once, and is remembered

Detection cannot be complete. A stream can be within a codec the GPU decodes and
still outside what its fixed-function block accepts — an unusual profile, an
interlaced source, a resolution above the decoder's limit, a file with a damaged
header. These surface as an ffmpeg that exits early rather than as anything a
capability listing could have predicted.

So a session that fails with hardware decode is retried once in software, and
the pairing that failed is remembered and not tried again.

**Once**, and remembered — both halves matter. The client already learned this
exact lesson from the other end: `retryWithoutClaims` in the playback provider
drops a codec claim, records the denial, and retries, and its comment explains
why a transcode failure must *not* be retried the same way. A retry that does
not narrow is the same request again, which is how a failing file becomes an
infinite loop instead of an error.

### Its own setting, defaulting to follow the encoder

`hardware_decode`: `"auto"` | `"off"`, alongside the existing
`hardware_encoder`. `"auto"` means "whatever the encoder preference resolved
to", so the ordinary case stays one knob.

Decode gets an escape hatch that encode does not share because it is the more
dangerous half. An encoder handles frames this server produced; a decoder is
parsing arbitrary bytes off disk through a vendor driver and a fixed-function
block, and a malformed stream that wedges a driver is a class of failure the
encode path does not have. Being able to turn that off without also giving up
the acceleration that already works is worth one config field.

## Consequences

**The scale filter added in ADR 0031 is now on the critical path, not a later
problem.** `-vf scale=-2:720` assumes frames are in system memory, and the
measurement shows that assumption is where the remaining cost lives: it is 15 of
decode-only's 15.1 CPU seconds. Adding `-hwaccel_output_format` without changing
it forces a download, a CPU scale and an upload — *slower* than the software
pipeline it replaced, presenting as a hardware regression rather than a filter
mistake. Two places carry the assumption and must move together: the `-vf` in
`args.go` and the `-pix_fmt yuv420p` in `hwaccel.go`, whose comment already
names `-hwaccel` as the thing that would invalidate it.

**The win is bounded to full transcodes.** Direct play decodes nothing; a remux
copies; an audio-only transcode has no video. This changes nothing for most of a
library on a LAN, and a great deal for the capped-quality case, which is exactly
the case ADR 0031 introduced.

**Detection gets slower at startup.** Verifying decode means decoding a real
sample per codec, which means having one — generated at build time or encoded on
the fly. This is already an accepted cost for encoders and the same argument
applies, but it is a second or two more before the first transcode of a run.

**HDR is not addressed here and is worse than "not addressed" suggests.**
Measured on a real BT.2020 PQ sample through the current command line, the
output loses 3.7x of its saturation *and* carries `smpte2084` / `bt2020` tags
copied from the source onto 8-bit H.264 that cannot honour them. This is true
**today**, on the software path — this ADR neither causes nor fixes it — but the
filter chain being rewritten here is the same one that has to carry the fix.
[ADR 0033](0033-hdr-tonemapping.md) covers it, and the two should be read
together.

## Work breakdown

Ordered so that each step is verifiable on its own. Steps 1–4 are the
intermediate decode-only commit; the win arrives at step 6.

1. `decodeAccel` on `Encoder`, populated for all four hardware candidates.
2. `Args` emits `-hwaccel` before `-i`, gated on `VideoAction == "encode"`.
   Table tests per vendor, plus the negative cases: copy, audio-only, software.
3. Decode detection: `(accelerator, codec)` pairs, verified against a real
   sample, surfaced through `Manager` beside `AvailableEncoders`.
4. Runtime fallback: one software retry per failing pairing, remembered.
   Load-bearing now that decode-only is not a shipping checkpoint.
5. `hardware_decode` setting, `"auto"` default, through `config.Settings`,
   `/api/settings`, and the settings UI. `docs/api.md` in the same commit.
6. **GPU residency, one vendor at a time.** `-hwaccel_output_format` plus the
   vendor scaler, replacing `-vf scale` and removing `-pix_fmt` from the
   command line. CUDA first — it is the best-documented path and the one these
   numbers were taken on. A vendor without a usable scaler falls back to the
   decode-only shape rather than blocking the others; AMF is the likeliest,
   having no scaler of its own and riding d3d11.
7. Re-measure per vendor against the table above, which is the baseline.

## Resolved: `-ss` with `-hwaccel`

The original open question asked whether fast-seek and hardware decode
co-operate, since every resumed playback in LANcast uses both, and a carve-out
for offset starts would have excluded the most common case.

**They co-operate.** Tested at a 2400s offset into a real 83-minute HEVC file
across all three pipelines, and at 20s into the 4K sample: no failures, in
either argument order. The offset does cost something — roughly 3% on the
1080p file — but it costs the same in the pure *software* pipeline, so it is
seek cost rather than any interaction with the decoder.

One GPU and one driver, so this is evidence rather than proof; the per-vendor
work in step 6 should re-run it. But the carve-out this worried about is not
needed.
