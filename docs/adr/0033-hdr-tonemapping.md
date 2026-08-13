# ADR 0033 — HDR, and a file that lies about what it contains

Date: 2026-08-13 · Status: **proposed**

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
