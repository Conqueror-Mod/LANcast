# butterchurn — vendored, reproduced, pinned, and cut down

This directory holds the MilkDrop engine that [ADR 0091](../../../docs/adr/0091-milkdrop-presets.md)
chose, checked in as a built artefact. It follows the rule
[ADR 0013](../../../docs/adr/0013-transcode-pipeline.md) set for hls.js, and
[web/vendor/hls.js/README.md](../hls.js/README.md) is the model this file copies.
That rule is *reproduce the bundle*: build it here from a pinned commit, prove
the build matches what upstream published, check in what was built, review it,
and load it as its own chunk.

It is not a precedent. CLAUDE.md's rule that hls.js is for live TV only, and
that its presence is no licence for a second player library, applies here too.
butterchurn is here because ADR 0091 argued for this one library, for this one
use.

Nothing here updates itself. Updating it means repeating the reproduction
below, re-reading the risk paths, and replacing this record — by a person,
deliberately.

## What is here

| | |
| --- | --- |
| library | butterchurn |
| version | 3.0.0-beta.5 (npm, published 2025-07-13) |
| commit | `63e587a7ba3093382e456cce4538e70babcd7197` (npm `gitHead`; 2025-07-12, "Upgrade eel-wasm to emit ES module and avoid need for patch (#96)") |
| upstream | https://github.com/jberg/butterchurn |
| licence | MIT (`LICENSE`) |
| artefact | `butterchurn.min.js`: upstream's production build **with `no-js-presets.patch` applied** |
| sha256 (vendored) | `130e4f3fdd103b80c64bc3404057ff73670229a2b035a82f88beaa80fd53036f` |
| size | 233,490 bytes |
| sha256 (stock build, unpatched) | `2e1914624666361a06b4aac4df8475dd53d59eac3425bb680c4b3529b2a4e401` |
| sha256 (npm `dist/butterchurn.min.js`) | `b5353af542b95cb5264069a0a4f52a23174e45745b58c48e245a3557d229b640` — **not** reproducible; see below |

Everything else in `butterchurn.min.js` is other people's code, inlined at
build time from the versions butterchurn's `pnpm-lock.yaml` pins:

| bundled | version | licence | file here |
| --- | --- | --- | --- |
| eel-wasm (the EEL → WebAssembly compiler) | 0.0.16, commit `f7394d36640a8238e89e211ca7bbad73b34d7936` | MIT, declared in `package.json` only | `LICENSE.eel-wasm` |
| @assemblyscript/loader | 0.17.14 | Apache-2.0 | `LICENSE.assemblyscript`, `NOTICE.assemblyscript` |
| ecma-proposal-math-extensions | 0.0.2 | MIT, declared in `package.json` only | `LICENSE.math-extensions` |
| butterchurn's `presetFunctions.ts`, compiled to WebAssembly by assemblyscript 0.17.14 and embedded as base64 | — | MIT (butterchurn) plus the AssemblyScript runtime it links | `LICENSE`, `LICENSE.assemblyscript` |

`butterchurn.d.ts` is written by hand, from the source at the commit above;
upstream ships no types. `no-js-presets.patch` is LANcast's, and is the whole
of LANcast's change to upstream.

### eel-wasm is 0.0.16, not 0.0.17

ADR 0091 says eel-wasm 0.0.17. That is the version the spike's *Node* scripts
used. butterchurn 3.0.0-beta.5's lockfile pins **0.0.16**, and 0.0.16 is what
is inside this build. There is no separate copy of eel-wasm here: two copies
of the compiler would be two attack surfaces for one job.

What 0.0.17 adds, read from its commits: debug names in the emitted module, and
#62, *"Handle out of range values for mod operator"*, which makes `%` mimic
x87's behaviour instead of trapping on a value outside the 32-bit range. So on
0.0.16 a preset doing `%` on a huge or NaN value throws a
`WebAssembly.RuntimeError` — the same class of failure as the two trapping
presets the spike found (bitwise operators and buffer indexes, which 0.0.17
does not change). The wrapper step d builds must catch it either way.

Between 0.0.15 and 0.0.16 the compiler's source changed only its import paths
(`./parser` → `./parser.js`, for ES modules); every line that emits WebAssembly
is the same. That matters for the next section.

## Reproducing it

On Windows, use a short directory (pnpm's store paths exceed `MAX_PATH` under a
deep one), and run pnpm from PowerShell or cmd, not Git Bash (its junctions
point at `/tmp/...` paths that Node cannot follow).

```powershell
git -c core.autocrlf=false -c core.longpaths=true clone --no-checkout https://github.com/jberg/butterchurn.git
cd butterchurn
git -c core.autocrlf=false -c core.longpaths=true checkout 63e587a7ba3093382e456cce4538e70babcd7197
npx pnpm@10.6.5 install --frozen-lockfile --ignore-scripts
$env:NODE_ENV = "production"; npx rollup -c
Get-FileHash dist\butterchurn.min.js     # stock: 2e191462...a4e401
git apply <LANcast>\web\vendor\butterchurn\no-js-presets.patch
npx rollup -c
Get-FileHash dist\butterchurn.min.js     # vendored: 130e4f3f...53036f
```

Node 20.11.1 and pnpm 10.6.5 (the `packageManager` butterchurn names).
`--ignore-scripts` keeps puppeteer, a test-only devDependency, from
downloading a browser. Verified 2026-10-18, twice, in two fresh clones: both
hashes came out the same each time.

And the compiler it bundles:

```powershell
git clone https://github.com/captbaritone/eel-wasm.git
cd eel-wasm
git checkout f7394d36640a8238e89e211ca7bbad73b34d7936   # npm gitHead of 0.0.16
npx pnpm@10.6.5 install --frozen-lockfile --ignore-scripts
cd packages\compiler
npx pnpm@10.6.5 run build
Get-FileHash dist\index.js   # 4c3eef09dacd560e8509eab86d230ed1c483a5cd92499f7bc6a9ba50d26077e3
```

That `dist/index.js` (and its `.map`) is **byte-identical** to the published
npm package `eel-wasm@0.0.16`, and `build/parser.js` regenerates identical to
the committed file. So the compiler inside the vendored build is reproduced
from source, not taken on trust.

`@assemblyscript/loader` and `ecma-proposal-math-extensions` ship their source
as their published files (`index.js` and `reference-implementation/index.js`;
there is no build step), and pnpm checks each tarball against the sha512 in the
lockfile. They were read in full rather than rebuilt.

### Why the vendored build is not byte-identical to npm's

The hls.js rule asks for a build *proven byte-identical to what upstream
published*. For butterchurn 3.0.0-beta.5 that is not possible, and this is
why, measured rather than guessed:

- The stock build from the pinned commit and its own lockfile is 234,681
  bytes. npm's `dist/butterchurn.min.js` is 243,799. `isSupported.min.js`
  *is* byte-identical, which shows the toolchain is the same.
- Diffing the unminified bundles (`dist/butterchurn.js`, the same build without
  terser) shows **exactly two differences**, and both are eel-wasm:
  1. npm's bundle inlines eel-wasm as *CommonJS* source (`requirePreProcessor`,
     `requireEnvParser` and the rest, 2,327 lines). The lockfile build inlines
     eel-wasm 0.0.16's published ES module.
  2. The one call into it is `srcExports.loadModule(...)` in npm's,
     `loadModule(...)` (minified `Vt`) in the lockfile build.
  Every other line of butterchurn — the renderer, the shaders, the equation
  runner, the AssemblyScript module's base64 — is identical.
- So npm's file was built against an eel-wasm that the lockfile does not name.
  eel-wasm 0.0.16 was published 75 minutes before butterchurn beta.5, by the
  same author, and the CommonJS form is eel-wasm as it was built *before* its
  ES-module commit. Building with eel-wasm 0.0.15 in its place comes within
  13 lines of npm's bundle; the rest is in the jison-generated parser wrapper
  (`require.main === module` and `module.exports` vs `exports`), which points
  at an unpublished local checkout of eel-wasm. That is where the trail stops:
  the exact input is not public.

The term here is therefore: **reproduce from the pinned commit and its
lockfile, and prove each inlined dependency from its own source.** That is
stronger evidence than a matching hash of npm's file would have been, because
npm's file contains a compiler nobody can rebuild.

One consequence for ADR 0091's numbers: the spike's browser run (504 presets
under the strict CSP, `bc3.html`) used npm's file, so it measured the
pre-0.0.16 eel-wasm. Since that compiler's emitting code is identical to
0.0.16's (above), those numbers stand for this build — but step d should
re-run a sample against the vendored file rather than assume it.

## The patch: the JavaScript preset path is removed

Upstream's `Visualizer` has two ways to load a preset. `loadWASMPreset`
compiles a preset's EEL through eel-wasm. `loadJSPreset` takes a preset's
`*_eqs_str` fields — JavaScript that butterchurn 2's converter generated from
EEL — and runs them through `new Function`. `onlyUseWASM` only gates the call
*from `loadPreset`*: `loadJSPreset` is a public method, so anything holding the
visualizer could call it directly, and `this.opts` is a plain object anybody
could flip.

So rather than a guard and a test, the method is gone. `no-js-presets.patch`
(6 lines in, 80 out, one file):
- deletes `loadJSPreset`, which held all eight `new Function` calls in the
  bundle;
- makes `loadPreset` compile `init_eqs_eel` through WebAssembly or throw
  `"butterchurn (LANcast build): preset has no EEL to compile"`. Upstream
  logged a warning and silently did nothing, which would have left a viewer on
  the previous preset with no reason given.

A patched build cannot be compared with anything upstream publishes. The
honest form, as the hls.js README puts it, is to reproduce the stock artefact
first, proving source and toolchain, and treat the patched build as derived
from inputs already proven. Both hashes are in the table.

`loadButterchurn` (web/src/playback/loadButterchurn.ts) sets
`onlyUseWASM: true` anyway, after the caller's options. It guards nothing in
this build. It is there because an upgrade that forgot to re-apply the patch
would otherwise reopen `new Function` with nothing to say so —
`loadButterchurn.test.ts` would say so first, because it fails on any
`Function(` in the file.

## How it is loaded

`web/vite.config.ts` and `web/vitest.config.ts` alias `butterchurn` to
`butterchurn.min.js` here, and `tsconfig.json` points the name at
`butterchurn.d.ts`. Neither butterchurn nor eel-wasm is in `package.json`, so
the name has nothing else to mean. `loadButterchurn.test.ts` holds all of that
in place: both aliases, the absence from `package.json`, this file's sha256,
and the absence of any `Function(`.

The only importer is `loadButterchurn()`, a dynamic `import("butterchurn")`,
and as of this step nothing calls it. So a production build is unchanged by
vendoring: `internal/web/dist` came out byte-identical. A throwaway build with
a probe entry that does call it (2026-10-18) put butterchurn in a chunk of its
own, `butterchurn.min-<hash>.js`, 232,812 bytes (60.9 kB gzip), reached only by
that `import()`. Vite re-minifies a vendored file as it bundles it, exactly as
it does `hls.min.js` (618,156 bytes in, 617,969 out), so the shipped chunk is not
byte-identical to this file. It was checked again for `Function(` and has
none.

## What was read, and what was found

The review is scoped to the paths that carry risk, as the hls.js one was,
rather than to a line count. Read: butterchurn's preset loading
(`loadPreset`, `loadWASMPreset`, `loadJSPreset` in `src/visualizer.js`, and
`src/index.js`); `src/equations/presetEquationRunnerWASM.js`, searched for
any object, DOM or global access; eel-wasm's `compileModule` and `loadModule`
as they sit in the bundle (what a module may import, and which names it may
call); the math-extensions polyfill in full. Then the stock and patched
artefacts themselves were searched for every pattern below, because the
artefact is what ships.

**No string becomes code.** In the vendored file there is no `Function(`, no
`eval(`, no `setTimeout`/`setInterval` at all, no `importScripts`, no
`innerHTML`/`outerHTML`/`document.write`. Upstream's stock bundle has eight
`new Function` calls, all in `loadJSPreset`; the patch removed every one. One
`.prototype.constructor(` remains: `@assemblyscript/loader` wrapping an
AssemblyScript class, which constructs a wrapper object, not a function.

**What a preset becomes.** eel-wasm parses EEL with a jison-generated parser
(no `eval`; checked) into a syntax tree and emits WebAssembly bytes directly.
The module's imports are exactly:
- one mutable `f64` `WebAssembly.Global` per variable, in the pools butterchurn
  creates (`perFrame`, `perVertex`, `shapePerFrameN`, `wavePerFrameN`);
- `shims`: `sin`, `cos`, `tan`, `asin`, `acos`, `atan`, `atan2`, `rand`,
  `pow`, `log`, `log10`, `exp`, `sigmoid` — numbers in, a number out.

A pool may not be named `shims` (eel-wasm refuses it), so a preset cannot
substitute its own imports. A function name starting `_` is refused, so a
preset cannot call the compiler's internal helpers. The module has its own
memory (`megabuf`/`gmegabuf`) and nothing else. It can compute; it cannot reach
a JavaScript object, the DOM, the network or storage.

**Limits, confirmed in the code.** `loop` and `while` are capped at 1,048,576
iterations (`MAX_LOOP_COUNT`). Each preset module's memory is fixed at 2,048
pages, min and max: **128 MiB reserved per preset**, as ADR 0091 says.

**No network, no storage.** No `fetch`, `XMLHttpRequest`, `WebSocket`,
`EventSource`, `sendBeacon`, `localStorage`, `sessionStorage`, `indexedDB` or
`document.cookie`. No URL in the artefact; the five in the unminified source
are comments crediting three.js and glsl-fxaa. `@assemblyscript/loader`
exports an `instantiateStreaming` that accepts a `Response` it is *given*;
butterchurn never calls it (it uses `instantiate` on an embedded buffer).

**What loading the chunk does to the page.** Stated so nothing comes to
depend on it by accident:
- It assigns about twenty functions onto `window` at import time — `sqr`,
  `sqrt`, `log10`, `sign`, `rand`, `randint`, `bnot`, `pow`, `div`, `mod`,
  `bitor`, `bitand`, `sigmoid`, `bor`, `band`, `equal`, `above`, `below`,
  `ifcond`, `memcpy`. They existed for butterchurn 2's generated JavaScript,
  which called them as globals. With that path removed they are dead weight,
  but removing them is a larger patch than the risk is worth.
- It adds `DEG_PER_RAD`, `RAD_PER_DEG`, `scale`, `fscale`, `clamp`,
  `radians` and `degrees` to the global `Math` (the math-extensions
  polyfill). The two constants are non-writable and non-configurable once set.
- It creates canvases with `document.createElement("canvas")`: its own WebGL 2
  canvas, one for the song-title text, and scratch ones inside
  `toDataURL`/`warpBufferToDataURL`. It reads
  `window.devicePixelRatio` and checks `window.OffscreenCanvas`.
- The embedded AssemblyScript module (butterchurn's own, not a preset's) is
  given `console.logi`/`logf` imports that print to the console. A preset's
  module is not given them.

**Failure modes the caller must own** (they are step d's, recorded here so
they are not rediscovered):
- `loadPreset` rejects on EEL eel-wasm cannot parse, and now on a preset with
  no EEL at all.
- `render` can throw `WebAssembly.RuntimeError` ("float unrepresentable in
  integer range") from inside a preset's equations; on 0.0.16 that includes
  `%` (see above). Catch it and skip the preset.
- A broken *shader* does not throw: butterchurn never checks
  `COMPILE_STATUS`, so the preset draws black (ADR 0091 §4).

## Signed off

The term exists so that a person looked. Reproduction and the notes above were
prepared with Claude Code; the signature below is the human review, and it is
not a formality.

- Reviewed by:
- Date:
- Reproduction confirmed independently:
