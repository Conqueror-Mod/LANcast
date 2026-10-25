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
| artefact | `butterchurn.min.js`: upstream's production build **with `no-js-presets.patch` applied, and eel-wasm rebuilt with `eel-wasm-precedence.patch`** |
| sha256 (vendored) | `a932cffc715509b0403e288a3a7b49a46bcacda73a0282430192695ba39715de` |
| size | 233,406 bytes |
| sha256 (stock build, unpatched) | `2e1914624666361a06b4aac4df8475dd53d59eac3425bb680c4b3529b2a4e401` |
| sha256 (no-js-presets.patch only) | `130e4f3fdd103b80c64bc3404057ff73670229a2b035a82f88beaa80fd53036f` (the build vendored before the precedence patch) |
| sha256 (eel-wasm-precedence.patch) | `7f93cd28a65c1d84d5cf20b0709aad4787aeb55ba401ae248ef335c88918b0d7` |
| sha256 (npm `dist/butterchurn.min.js`) | `b5353af542b95cb5264069a0a4f52a23174e45745b58c48e245a3557d229b640` — **not** reproducible; see below |

Everything else in `butterchurn.min.js` is other people's code, inlined at
build time from the versions butterchurn's `pnpm-lock.yaml` pins:

| bundled | version | licence | file here |
| --- | --- | --- | --- |
| eel-wasm (the EEL → WebAssembly compiler) | 0.0.16, commit `f7394d36640a8238e89e211ca7bbad73b34d7936`, **rebuilt with `eel-wasm-precedence.patch`** | MIT, declared in `package.json` only | `LICENSE.eel-wasm` |
| @assemblyscript/loader | 0.17.14 | Apache-2.0 | `LICENSE.assemblyscript`, `NOTICE.assemblyscript` |
| ecma-proposal-math-extensions | 0.0.2 | MIT, declared in `package.json` only | `LICENSE.math-extensions` |
| butterchurn's `presetFunctions.ts`, compiled to WebAssembly by assemblyscript 0.17.14 and embedded as base64 | — | MIT (butterchurn) plus the AssemblyScript runtime it links | `LICENSE`, `LICENSE.assemblyscript` |

`butterchurn.d.ts` is written by hand, from the source at the commit above;
upstream ships no types. The two patches are LANcast's, and are the whole of
LANcast's change to upstream:
- `no-js-presets.patch`, to butterchurn: removes the JavaScript preset path
  (below).
- `eel-wasm-precedence.patch`, to eel-wasm: gives the EEL operators the
  precedence MilkDrop's own compiler gives them, so a preset computes what its
  author saw. Without it 110 of the 9,487 corpus presets compute something else
  (below, and ADR 0091's amendment of 2026-10-18).

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

That is the build with `no-js-presets.patch` alone. The vendored file also
has eel-wasm rebuilt with `eel-wasm-precedence.patch`. Build that compiler
first ("Then the patched compiler", below), then put it in place of the npm
copy and build butterchurn again:

```powershell
# in the butterchurn clone, after the no-js-presets build above:
$ew = "node_modules\.pnpm\eel-wasm@0.0.16\node_modules\eel-wasm\dist"
Remove-Item "$ew\index.js", "$ew\index.js.map"   # delete first: pnpm hard-links
                                                  # these into its global store
Copy-Item <eel-wasm>\packages\compiler\dist\index.js, <eel-wasm>\packages\compiler\dist\index.js.map $ew
npx rollup -c
Get-FileHash dist\butterchurn.min.js     # vendored: a932cffc...9715de
```

The copy is deleted before it is replaced because pnpm hard-links package
files from its content store: writing through the link would change eel-wasm
0.0.16 for every project on the machine. The store's copy was checked after
the build and is still `4c3eef09…`. Verified 2026-10-18 in two fresh clones of
each repository, with the same hashes both times.

And the compiler it bundles:

```powershell
git -c core.autocrlf=false clone https://github.com/captbaritone/eel-wasm.git
cd eel-wasm
git -c core.autocrlf=false checkout f7394d36640a8238e89e211ca7bbad73b34d7936   # npm gitHead of 0.0.16
npx pnpm@10.6.5 install --frozen-lockfile --ignore-scripts
cd packages\compiler
npx pnpm@10.6.5 run build
Get-FileHash dist\index.js   # 4c3eef09dacd560e8509eab86d230ed1c483a5cd92499f7bc6a9ba50d26077e3
```

That `dist/index.js` (and its `.map`) is **byte-identical** to the published
npm package `eel-wasm@0.0.16`, and `build/parser.js` regenerates identical to
the committed file. So the compiler inside the vendored build is reproduced
from source, not taken on trust. (Use the same short directory, and keep
`core.autocrlf=false`: a CRLF checkout builds a different `dist/index.js`, and
under a long path the build fails outright.)

Then the patched compiler, in the same clone:

```powershell
cd ..\..
git apply <LANcast>\web\vendor\butterchurn\eel-wasm-precedence.patch
cd packages\compiler
npx pnpm@10.6.5 run build    # regenerates build\parser.js from the patched grammar
Get-FileHash dist\index.js   # 4acb6f347ba1dd5808508518d15a361005a5eb02e75f980eaf5a64b3fe6af22a
npx vitest run               # eel-wasm's own suite, 16 new precedence cases included: 255 pass
```

`build/parser.js` comes out `f316085c9561c2dac1dec49fabaa80d63fd5037dcbf49aa609306c4cad04c077`,
and differs from the committed one only in jison's LR table (the `$V…`
constants and `table`). The lexer, the productions and every semantic action
are untouched, so the patch cannot change what a preset may say. It changes
only how an expression groups. This `dist/index.js` is the one the butterchurn
step above copies in.

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

## The first patch: the JavaScript preset path is removed

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

## The second patch: operators group as they do in MilkDrop

ADR 0091 left one question open, step f′: does eel-wasm group `&` and `^` the
way Winamp's MilkDrop does? It does not, and nor do several other operators.
`eel-wasm-precedence.patch` (37 lines in, 6 out, two files) fixes the grammar's operator
table and adds the cases that prove it to eel-wasm's own test list.

**The reference.** MilkDrop 2's expression library, ns-eel2 (2004–2008, as
in the open MilkDrop 2 source; read in the MilkDrop3 repository,
`code/ns-eel2`), groups operators in two ways:
- Its grammar is a bison grammar, `cal.y`. It ships only as the generated
  `nseel-caltab.c`, whose actions still carry `//#ln … "cal.y"`. The same
  grammar is in AVS's evallib, the library MilkDrop 1 used, as source
  (`avs/vis_avs/evallib/cal.y`, lines 22–27 of the 2013 open release):
  `%left '|'`, `%left '&'`, `%left '+' '-'`, `%left '*' '/' '%'`,
  `%left UMINUS`. So `|` is the loosest operator, `&` is next, and both are
  looser than all the arithmetic.
- Before parsing, `preprocessCode()` (`nseel-compiler.c`, lines 1010–1455)
  rewrites the other operators as function calls by scanning the text:
  - `a && b`, `a || b` and the comparisons become `_and(a,b)`, `_equal(a,b)`
    and so on. Each operand extends to the nearest `,` `;` `)` (or, for a
    comparison, to a `&&` or `||`). So these are looser than any arithmetic,
    and `&&`/`||` are looser than the comparisons.
  - `a ^ b` → `pow(a,b)`, `a % b` → `_mod(a,b)` and `!a` → `_not(a)` take
    only the operand beside them. So these are tighter than anything, a
    unary minus on their left included: `-2 ^ 2` is −4.

MilkDrop 1 and 2 agree on `&` and `|`. MilkDrop 1 had no `^`, no comparison
operators and no `&&`, and its `%` was integer modulo at the `*` level.
Presets written for it use `pow()`, `above()` and `band()`, which group the
same way in either version.

**How it was checked.** MilkDrop 2's own parse tables, lexer, lookup and
`preprocessCode()` were compiled unchanged with gcc. Only the code generator
was replaced, with one that prints the tree and evaluates it, so nothing of
MilkDrop's was run as shipped. That harness reproduces 122 of the 124 cases in
eel-wasm's own `testCases.ts` it can evaluate. Those are the cases eel-wasm's
author checked in real MilkDrop with `buildTestPreset.js`, the test-preset
method ADR 0091 named. The other two are a `\` comment (stripped by
MilkDrop's preset loader, not by ns-eel2) and division by zero, a runtime
detail. The harness and its scripts are in
`spikes/milkdrop/precedence/`.

| expression | MilkDrop 2 | eel-wasm 0.0.16 | patched |
|---|---|---|---|
| `5 - 3 & 6` | 2 | 3 ✗ | 2 |
| `6 & 3 * 2` | 6 | 4 ✗ | 6 |
| `3 & 6 ^ 2` | 0 | 4 ✗ | 0 |
| `-3 & 5` | 5 | −1 ✗ | 5 |
| `!0 * 2` | 2 | 1 ✗ | 2 |
| `1 > 0 && 0 > 1` | 0 | 1 ✗ | 0 |
| `1 + 1 && 0` | 0 | 1 ✗ | 0 |
| `0 \|\| 3 > 2` | 1 | 0 ✗ | 1 |
| `-2 ^ 2` | −4 | −4 | −4 |
| `2 ^ 3 ^ 2` | 64 | 64 | 64 |
| `10 / 4 % 3` | 10 | 10 | 10 |
| `1 < 2 < 3` | 0 | 0 | 0 |

eel-wasm already had `^` and `%` right. Its grammar put `&`, `|` and `&&`/`||`
*tighter* than everything else, and `!` and unary minus at the level of
binary `+`. The patched table, loosest first: assignment, `&&` `||`,
comparisons (right-associative), `|`, `&`, `+` `-`, `*` `/`, unary `-` `+`,
`%` `^` (left-associative), `!`.

**What it changes in real presets.** Every compiled block of the 9,487-preset
corpus was parsed by both, and each pair of trees was evaluated by one
evaluator. Where they disagree, the difference is grouping and nothing else.
- Before the patch, the trees differ in 2,733 presets. In most of them the
  difference is invisible, because a sign moves through `*` or `/`
  (`-a*b` versus `-(a*b)`).
- The computed values differ in **110 presets** (1.16%), for at least one of
  three input ranges.
- After the patch, no precedence difference is left anywhere in the corpus.
  14 presets still compute differently, for reasons that are not precedence:
  - integer literals above 2³¹, which MilkDrop's `atoi` saturates (11);
  - a preset calling ns-eel2's internal `_aboeq()` by name (3), which the Go
    validator already refuses.
- The Go validator agrees with the patched compiler on all 73,013 blocks
  (`godiff.mjs`). It never modelled precedence, and does not need to.

eel-wasm does not support `?:`, which MilkDrop 2 does. That is a missing
feature, not a grouping, and it is outside this patch.

**Upstream.** The same change suits eel-wasm upstream, and butterchurn would
then take it by bumping its lockfile. A draft issue for
captbaritone/eel-wasm is in ADR 0091's amendment. Until it is merged and
released, this patch is carried here and re-applied on every update.
`loadButterchurn.test.ts` pins both the artefact's hash and the patch's, so an
upgrade cannot drop it without the suite saying so.

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

- Reviewed by: Conqueror
- Date: 10/25/2026
- Reproduction confirmed independently: N/A
