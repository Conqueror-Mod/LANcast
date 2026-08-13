# ADR 0032 — Hardware decode, and why it lands in two stages

Date: 2026-08-13 · Status: **proposed**

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

## Decision

### Stage 1 is decode-only, and it ships on its own

Frames come back to system memory. `-vf scale` and `-pix_fmt yuv420p` are
untouched.

This is not timidity, it is the only ordering that produces a measurement. Stage
2 changes decode, scaling and pixel-format conversion simultaneously, across four
vendors; a regression there has twelve places to hide and no baseline to compare
against. Stage 1 changes one thing, cannot regress the filter chain because it
does not touch it, and answers the question stage 2 depends on — *what is the
decode alone worth on this hardware?* If the answer is "most of it", stage 2 is
a smaller prize than it looks.

It also keeps 10-bit safe for free. A hardware decoder hands back `p010`
surfaces for 10-bit HEVC, and the existing `-pix_fmt yuv420p` converts them
through swscale exactly as it converts a software-decoded 10-bit frame today.
On a GPU-resident path that conversion has to happen on the device instead, in a
different spelling per vendor, and 10-bit HEVC Main10 is not an edge case — it
is most of a modern 4K library.

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

**The scale filter added in ADR 0031 becomes a stage 2 problem.** `-vf
scale=-2:720` assumes frames are in system memory. They are, in stage 1. The
moment `-hwaccel_output_format` is added, that filter forces a download, a CPU
scale and an upload — a pipeline *slower* than the software one it replaced, and
one that presents as a hardware regression rather than a filter mistake. Two
places carry the assumption and both must move together in stage 2: the `-vf` in
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

**HDR is not addressed and is worth its own work.** `-pix_fmt yuv420p` converts
a BT.2020 PQ source to 8-bit without tonemapping, which produces the washed-out,
desaturated picture that HDR-to-SDR conversion is notorious for. That is true
**today**, on the software path — this ADR neither causes it nor fixes it — but
anyone reading the pixel-format handling because of this work will walk straight
into it. It needs a tonemap filter and its own decision record.

## Work breakdown

**Stage 1 — decode-only**

1. `decodeAccel` on `Encoder`, populated for all four hardware candidates.
2. `Args` emits `-hwaccel` before `-i`, gated on `VideoAction == "encode"`.
   Table tests per vendor, plus the negative cases: copy, audio-only, software.
3. Decode detection: `(accelerator, codec)` pairs, verified against a real
   sample, surfaced through `Manager` beside `AvailableEncoders`.
4. Runtime fallback: one software retry per failing pairing, remembered.
5. `hardware_decode` setting, `"auto"` default, through `config.Settings`,
   `/api/settings`, and the settings UI. `docs/api.md` in the same commit.
6. A measurement: same file, same ceiling, decode on and off, wall-clock to
   first byte and CPU seconds. Without this there is no way to know stage 2 is
   worth doing.

**Stage 2 — GPU residency** *(separate ADR, gated on stage 1's numbers)*

`-hwaccel_output_format` per vendor; `scale_cuda` / `scale_qsv` / `scale_vt`
replacing `-vf scale`; on-device format conversion replacing `-pix_fmt`; a
software-scale fallback for whichever vendor's filter proves unavailable —
AMF being the likeliest, since it has no scaler of its own and rides d3d11.

## Open question

`-ss` before `-i` seeks by keyframe without decoding, which is what keeps
resuming a two-hour film from costing minutes. Fast-seek combined with
`-hwaccel` is known to be fussier than either alone on some driver versions, and
every resumed playback in LANcast uses both. Stage 1 needs a test against a real
file at a real offset before this is called done; if the combination proves
unreliable, decode acceleration may have to be skipped for offset starts, which
would be an unwelcome carve-out of the most common case.
