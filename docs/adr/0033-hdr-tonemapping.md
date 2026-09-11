# ADR 0033 — HDR, and a file that lies about what it contains

Date: 2026-08-13 · Status: accepted · shipped in v0.6.44 · Amended 2026-08-17 on implementation

## Context

LANcast converts HDR to SDR by ignoring the problem, and the result is wrong in
two separate ways — one visible, one worse.

The transcode command line ends in `-pix_fmt yuv420p`. On a BT.2020 PQ source
that reduces bit depth and nothing else: no transfer-function conversion, no
gamut mapping, no tone mapping. Measured on a real HDR10 sample through the
exact arguments `transcode.Args` produces today, against the same sample with a
tonemap:

| | Mean saturation | Output tagged |
| --- | --- | --- |
| LANcast today | **28.3** | `smpte2084` / `bt2020` / `bt2020nc` |
| With a tonemap | **105.1** | `bt709` / `bt709` / `bt709` |

**The visible defect** is that 3.7x saturation loss — the flat, grey,
washed-out picture HDR-to-SDR conversion is notorious for when it is not done.

**The worse defect is the second column.** ffmpeg copies the source's colour
metadata to the output by default, so the delivered file is 8-bit H.264 High
profile *claiming to be HDR10*. It asserts a transfer function its contents do
not have. A client that ignores the tags renders it flat; a client that honours
them applies a PQ curve to values that were never PQ-encoded at that depth. The
file is not merely dull — it is inconsistent, and it is differently wrong on
different displays, which is the shape of bug that generates irreproducible
reports.

**This is the common path, not an edge case.** HDR content is HEVC Main10. The
`browser` profile excludes HEVC (deliberately — ADR 0012), so every HDR file
transcodes for a browser client. There is no configuration in which a browser
gets an HDR file and this code does not run.

**And it cannot currently be detected.** `internal/probe` records `pix_fmt` and
nothing else about colour. `yuv420p10le` is what HDR10 reports and it is also
what 10-bit SDR reports; the two are indistinguishable from anything in the
database today. Whatever else this decision contains, it starts with a probe
change, because right now the server has no way to know which files are affected.

## Decision

### Probe and store the colour metadata

`color_transfer`, `color_primaries` and `color_space` onto `media_stream`, read
from the same `ffprobe` output the existing stream fields come from.

Three nullable columns and a migration — additive, not a reshape of the data
model, so it does not need a decision record of its own beyond this one. `NULL`
means "not probed yet" and behaves exactly as today, which matters because every
existing row will be `NULL` until re-probed and nothing may break in the
meantime.

### HDR is defined by the transfer function

`color_transfer` of `smpte2084` (PQ, which is HDR10 and Dolby Vision's base
layer) or `arib-std-b67` (HLG). Not bit depth, not primaries.

Bit depth is the trap worth naming: 10-bit is *correlated* with HDR and does not
mean it, and a 10-bit SDR file put through a tonemap would be damaged by it just
as surely as an HDR file is damaged by not being. Primaries are the weaker
signal for the opposite reason — BT.2020 primaries appear on SDR content
occasionally, and the transfer curve is what actually determines whether the
code values need converting.

### Tonemap, and tag the output honestly — in that order of importance

When the source is HDR and the output is SDR, the filter chain converts to
linear light, tone maps, and converts to BT.709, and the output is tagged
`bt709` throughout.

The tagging is not a detail to be picked up later. It is cheaper than the
tonemap, it is independently correct, and without it the pipeline emits a file
that misdescribes itself — which is the defect that makes this bug
irreproducible across clients. A conversion that produced a slightly
disappointing picture *correctly labelled* would be a quality complaint. What
ships today is a correctness bug.

### Tonemapping happens on the CPU first, and that is a real cost

**There is no `tonemap_cuda` in stock ffmpeg**, and `scale_cuda` converts pixel
format only — it has no tonemapping option. Verified against the build this was
measured on (ffmpeg 8.1.2, `--enable-cuda-llvm --enable-nvdec`). The filters
that exist are `tonemap` (CPU, via `zscale` to linear light), `tonemap_opencl`,
`tonemap_vaapi`, and `libplacebo` (Vulkan).

This puts HDR in direct tension with [ADR 0032](0032-hardware-decode.md), whose
whole decision is that frames stay on the GPU. For an HDR file on the CUDA path
there are three options and none is free: round-trip through Vulkan for
`libplacebo`, map to OpenCL for `tonemap_opencl`, or download to system memory
and tone map on the CPU.

**HDR files take the CPU path initially.** Correctness first: what ships today
is wrong, and it is wrong on every HDR file, where a GPU-resident tonemap is a
speed optimisation on a minority of a library. Downloading frames for HDR
specifically costs roughly what ADR 0032 measured decode-only to cost — a real
regression against the GPU-resident path, but against a path that does not exist
yet, and still faster than today's fully-software decode.

Jellyfin patches ffmpeg to add `tonemap_cuda`. LANcast uses whatever ffmpeg is
on the machine ([ADR 0016](0016-packaging-and-distribution.md)), so a patched
build is not available to it and vendoring one is a decision several orders
larger than this. `libplacebo` is the path to revisit if the CPU cost proves
unacceptable — it is in common builds and does the highest-quality tonemapping
of the four.

### Which operator, and stated rather than defaulted

`hable` for the initial implementation, with the operator not exposed as a
setting.

There is no correct answer here, only preferences with different failure modes:
`clip` blows out highlights, `reinhard` flattens midtones, `mobius` and `hable`
trade off differently in the shoulder. `hable` preserves midtone contrast, which
is where faces are. Naming it in one place with a comment beats a magic default,
and exposing it as a user setting before anyone has complained would be a
control whose options cannot be explained to the person choosing between them.

## Consequences

**Every existing row needs re-probing before HDR is detectable.** Until then
`color_transfer` is `NULL` and HDR files behave as they do today — badly, but no
worse. A re-probe of a large library is not instant, and the fix therefore
arrives per-item rather than all at once. This is the same shape as the `pix_fmt`
migration (revision 12) that the 10-bit H.264 rule depends on, and it can follow
whatever that did.

**HDR transcodes get slower.** A tonemap in the filter chain is real work on top
of a decode and an encode, and on the CPU path it is not cheap. The alternative
is continuing to ship a file that misrepresents its own contents.

**The two ADRs must be implemented in awareness of each other.** Both rewrite
the same filter chain in `transcode.Args`, and a tonemap inserted into a
GPU-resident pipeline is a different filter from one inserted into a CPU one.
Neither should land without the other having been read; whichever goes first
should leave the chain in a shape the other can extend rather than replace.

**Dolby Vision profiles beyond the PQ base layer are out of scope.** Profile 5
in particular is not PQ and will not be detected by the rule above. Treating it
as SDR is what happens today and will continue to; getting it right needs
dynamic-metadata handling that is a much larger piece of work, and pretending
otherwise in this ADR would be scope that never gets built.

## Amendment, 2026-08-17 — tagging alone is not achievable, and is harmful

This ADR said the output tagging is "cheaper than the tonemap, independently
correct", and should therefore happen whether or not the conversion runs.
Implementation measured that and the second half is wrong.

`-colorspace` / `-color_primaries` / `-color_trc` are not sufficient on their
own. x264 writes its VUI from the **frame properties** it is handed, which the
decoder sets from the source, and those flags do not override them. Running
LANcast's own arguments against a real HDR10 clip, reading the result back with
ffprobe:

| Path | `color_space` / `color_transfer` / `color_primaries` |
| --- | --- |
| Tone mapped | `bt709` / `bt709` / `bt709` |
| Tags only | `bt709` / **`smpte2084`** / **`bt2020`** |
| Untouched (before this work) | `bt2020nc` / `smpte2084` / `bt2020` |

The middle row is not a correctly-labelled file. It is one whose matrix and
transfer disagree — a *third* wrong state, and the most incoherent of the three,
in a decision whose whole purpose was to stop the output being differently wrong
on different displays.

So the tagging is not independent of the conversion after all. It is independent
of the *tonemap*, but it requires something to rewrite the frame properties.
`setparams` does exactly that and nothing else: core libavfilter, no libzimg, so
it is available in builds where `zscale` is not.

**There are therefore three states, not two:**

| ffmpeg build | Output |
| --- | --- |
| `zscale` + `tonemap` | converted, tagged `bt709` |
| `setparams` only | not converted, tagged `bt709` — coherent, flat |
| neither | left exactly as before this work |

The third is deliberate. Emitting the tags with nothing to back them produces
the middle row above, which is worse than the self-consistent HDR tags that
shipped before. Where coherence is unreachable, the least wrong action is none.

Relabelling without converting is still a claim about pixels that were never
converted, and that is accepted knowingly: every client then renders the picture
the same flat way, where the hybrid renders differently on each. A consistently
disappointing picture is a quality complaint; an incoherent file is a bug report
nobody can reproduce.

### Also settled on implementation

**The tension with [ADR 0032](0032-hardware-decode.md) did not arise.** That ADR
was read as putting frames on the GPU, but nothing decodes on the GPU today —
`EncoderArgs` states it outright, and there is no `-hwaccel` anywhere in the
package. Frames are already in system memory, so the CPU tonemap is a plain
filter insertion rather than the download this ADR warned it might cost. The
tension returns whenever GPU-resident decode does.

**The filter chain had to become a single `-vf`.** ffmpeg keeps only the last
`-vf` and silently discards the others, so a tonemap added as a second flag
would have replaced the quality-ceiling scale rather than composing with it — a
cap that stops being honoured with nothing in the output to say why. Scale is
emitted before the tonemap: the same conversion on fewer pixels.

**Verified on real HDR content**, as step 5 required. A 4-second clip from a
2160p HDR10 file through LANcast's own arguments: `SATAVG` 2.6 → 4.0 and `YAVG`
40.6 → 35.0, with highlights visibly better controlled and colour restored in a
side-by-side frame comparison. The direction and the visible result are
confirmed; the magnitudes differ from the 28.3 → 105.1 measured in the Context
above, which was a different sample and scene, and the two should not be read as
the same measurement repeated.

## Amendment, 2026-09-10 — the CPU cost proved unacceptable, so tone map on the GPU when the service can

The Decision said "`libplacebo` is the path to revisit if the CPU cost proves
unacceptable". It has, and the tension with GPU decode that the first amendment
said "returns whenever GPU-resident decode does" has returned: decode is on the
card now (`-hwaccel cuda`), so an HDR file decodes on the GPU, downloads every
10-bit frame to system memory, converts it to 32-bit float RGB on the CPU, tone
maps, converts back, and uploads to NVENC.

**Measured on a 3840×1606 HDR10 film** (`The Fifth Element`, BT.2020 PQ), 15–30
seconds from the same offset, with the server's own filter chain:

| Tone map | Throughput |
| --- | --- |
| CPU `zscale` + `tonemap` (this ADR's chain) | **0.41–0.44× realtime** |
| CPU chain, scaled to 1080p first | 0.51× |
| GPU `libplacebo` (Vulkan) | 2.74× |
| GPU `tonemap_opencl` | **3.71×** |

Below 1× a film cannot be converted as fast as it plays, so no buffer or quality
ceiling rescues it: 1080p barely moves the number, because the cost is the
download and the float conversion, not the pixel count. Reported as stutter on
exactly this title, and as an audio-track change that takes uncomfortably long,
since every new stream pays the slow start again. It had previously been
diagnosed as a supply problem that did not exist, from a benchmark that left the
tone map out — the measurement that matters is the one using the real chain.

### Decision

**Tone map with `tonemap_opencl` when the running server can prove OpenCL works;
otherwise keep the CPU chain exactly as it is.**

- **OpenCL over `libplacebo`**: fastest of the two here, and closer to the look
  this ADR already shipped. Against the CPU output over three scenes (bright,
  mid, dark), SSIM and mean-luma difference:

  | Variant | SSIM | Luma delta |
  | --- | --- | --- |
  | `tonemap_opencl` default | 0.935 / 0.870 / 0.962 | −5.6 / −3.3 / −6.2 |
  | `tonemap_opencl`, `peak=10` | **0.955 / 0.889 / 0.975** | **−1.0 / −0.9 / −1.3** |
  | `libplacebo` default | 0.955 / 0.857 / 0.957 | +4.2 / −2.3 / +8.7 |

  `peak=10` is the chosen parameter: the CPU chain linearises with `npl=100`, so
  signal 1.0 is 100 nits and 10 is the 1,000-nit mastering peak of ordinary
  HDR10 — the brightness then agrees with the shipped look to about one level.
  `libplacebo` is the higher-quality operator in principle, but its default peak
  detection moved brightness in both directions between scenes, which is a look
  that changes with the shot. The frames were also compared by eye: all three are
  correct pictures, differing mainly in overall brightness.

- **`hable`, unchanged**, for the reason this ADR chose it.

- **Proven by a real encode at startup, never by a filter listing.** The first
  implementation of this ADR decided capabilities from `ffmpeg -filters` with the
  reasoning "a filter has no hardware behind it to be absent". That is true of
  `zscale` and false of `tonemap_opencl`, which needs an OpenCL device — and the
  server runs as a Windows service in session 0, the environment where v0.8.0's
  `-hwaccel auto` chose DXVA2 and ffmpeg exited before writing a byte. Being
  listed is not being usable there. So the capability is set only by running the
  exact device initialisation and filter on a generated HDR frame, the same way
  `DetectEncoders` refuses to trust `-encoders`.

- **A failed probe costs speed, never the stream.** If OpenCL is absent in the
  service's session, the capability is false and HDR takes the CPU chain — today's
  behaviour, slower and correct. Nothing about a GPU tone map may make a file that
  plays today stop playing.

### Consequences

**Verification must be as the service.** Every number above was measured from an
interactive shell, where OpenCL is certain to initialise. That proves the path is
worth having and says nothing about session 0 — which is precisely the gap that
cost v0.8.0. The startup probe's log line is how the running service reports
which chain it chose, and that line from the installed service is the
verification, not these benchmarks.

**The look of HDR output changes slightly** on machines where the probe passes:
brightness within about one level of today's, structure agreeing at SSIM
0.89–0.98. Accepted knowingly, against a film that cannot currently be watched.

**A quality ceiling is applied after the GPU tone map rather than before it.**
There is no OpenCL scaler in the builds this runs on, so the scale runs on the
downloaded 8-bit frame. The CPU chain keeps scaling first, as the first amendment
recorded.

## Work breakdown

1. Probe `color_transfer`, `color_primaries`, `color_space`; migration adding
   three nullable columns to `media_stream`; store and expose them.
2. `probe.IsHDR(stream)` — the transfer-function rule, with a test per real
   value including the ones that must *not* match (10-bit SDR, BT.2020 SDR).
3. The tonemap in `Args`, gated on HDR source and SDR output, as a pure
   argument change with table tests. It is the same testability rule the rest of
   this package follows: no encode required to assert the command line.
4. Output colour tagging, asserted independently of the tonemap — the two
   should not be able to regress together.
5. A visual check against a real HDR file, because saturation statistics
   confirm a conversion happened and cannot confirm it looks right.
