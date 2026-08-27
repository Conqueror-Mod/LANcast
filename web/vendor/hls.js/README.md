# hls.js — vendored, reproduced, pinned

This directory holds a third-party media player library, checked in as a built
artefact. [ADR 0013](../../../docs/adr/0013-transcode-pipeline.md) sets the terms
it is here under, and this file is the record those terms require.

Nothing here is updated automatically. Updating it means repeating the
reproduction below, re-reading the risk paths, and replacing this record — by a
person, deliberately.

## What is here

| | |
| --- | --- |
| library | hls.js |
| version | 1.7.1 |
| commit | `565f70ee8e074a0fbe82ed80dfb7fac0697bbb8a` |
| upstream | https://github.com/video-dev/hls.js |
| licence | Apache-2.0 (`LICENSE` in this directory) |
| artefact | `hls.min.js` |
| sha256 | `6cfad701a61fb8a99add5e84449e64661169b0652bf44ceb2a28465c8817b5f1` |
| runtime dependencies | none |

## Why a built artefact rather than the source

ADR 0013 originally asked for the source tree, reviewed as any other checked-in
code. Measuring that changed the term: hls.js is **54,255 lines across 138
files**, and building it needs 72 direct devDependencies that install as **993
packages**. Vendoring the source would have meant more third-party code in this
repository and a review nobody would honestly perform.

The term is now **reproduce the bundle**: build it here from the pinned commit,
prove it is byte-identical to what upstream published, and check in what was
built. That is provenance verified rather than trust extended, which is the
thing the original objection was protecting.

## Reproducing it

```bash
git -c core.autocrlf=false clone --depth 1 --branch v1.7.1 \
    https://github.com/video-dev/hls.js.git
cd hls.js
npm ci --ignore-scripts
# the repository omits "version"; it is injected at publish time
#   add  "version": "1.7.1"  to package.json
npx rollup --config
sha256sum dist/hls.min.js   # must match the table above
```

Verified 2026-08-27 against the published npm package `hls.js@1.7.1`. All five
comparable artefacts came out byte-identical: `hls.min.js`, `hls.min.mjs`,
`hls.light.min.js`, `hls.worker.js`, and the unminified `hls.js`.

Three things will make a reproduction *look* like it failed when it has not.
Each cost an hour to find:

- **`package.json` has no `version` field.** It is added at publish. Build
  without it and the version string compiles to `void 0`, which changes every
  bundle.
- **`npm ci` fails on Node 20** unless `--ignore-scripts` is passed: a
  devDependency's postinstall script is incompatible with the runtime. Skipping
  install scripts is better hygiene here anyway.
- **Git line-ending translation on Windows.** With `core.autocrlf` left at its
  default, the unminified bundle came out with 806 CRs against upstream's zero —
  a delta of exactly 806 bytes, identical after normalising. The minified
  artefact this directory ships was unaffected, but a raw byte comparison on a
  Windows checkout will report a false mismatch for ever.

## What was read, and what was found

The review is scoped to the paths that carry risk rather than to a line count.
Findings as of the commit above:

**No dynamic code execution.** No `eval`, no `new Function`, no `innerHTML` or
`outerHTML`, no `document.write`, no `importScripts`, no string-bodied
`setTimeout`. The library builds no code at runtime.

**No hardcoded network destination.** Every absolute URL in the source is a
comment linking to a specification or a bug report. The library fetches what it
is given — the playlist and segment URLs handed to it — and nothing else.

**Three outbound call sites exist, and two are off by default.**

- `utils/fetch-loader.ts` and the XHR loader fetch playlists and segments. This
  is the library's purpose, and under LANcast every such URL points at this
  server, because the channel proxy never discloses the provider's address.
- `controller/cmcd-controller.ts` implements **CMCD**, which reports playback
  telemetry to a CDN. Default is `cmcd: undefined` — off unless configured.
  **Do not configure it.** It is a phone-home by design, and the no-phone-home
  principle in the README is not negotiable.
- `controller/eme-controller.ts` makes DRM licence requests. Defaults are
  `emeEnabled: false` and `drmSystems: {}` — off unless configured.

**One qualification on "zero runtime dependencies".** That is true of what gets
installed: nothing comes along at runtime. But `@svta/cml-cmcd` is a
devDependency that is **inlined into the bundle at build time**, so the artefact
does contain a small amount of third-party code beyond hls.js itself. Stated
rather than glossed, because "no dependencies" and "no other people's code in
the bundle" are different claims.

**Worth knowing for later.** The build has feature flags — `__USE_CMCD__`,
`__USE_EME_DRM__`, `__USE_SUBTITLES__`, `__USE_ALT_AUDIO__` — so CMCD and EME
can be compiled *out* entirely rather than merely left off. That is a stronger
answer than a default, and it is available whenever it is wanted. It has a cost
worth naming: a custom build cannot be byte-compared against anything upstream
publishes. The honest form is to reproduce the stock artefact first, proving the
source and toolchain, and treat a trimmed build as derived from inputs already
proven.

## Signed off

The term exists so that a person looked. Reproduction and the notes above were
prepared with Claude Code; the signature below is the human review, and it is
not a formality.

- Reviewed by: _(unsigned — pending)_
- Date: _(pending)_
- Reproduction confirmed independently: _(pending)_
