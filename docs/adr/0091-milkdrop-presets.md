# ADR 0091 — A MilkDrop preset is data, never code

Date: 2026-10-18 · Status: **accepted** 2026-10-18 · Amended 2026-10-18 (step f′: operator precedence settled, eel-wasm patched)

Chris asked whether LANcast could accept uploaded Winamp visualizations, so
fewer would need building by hand. Now Playing shipped in v0.9.89. Its own
visualizers shipped in v0.9.90: Spectrum, Scope, and five WebGL feedback
**Trails** presets, each a plain object of numbers. The plan behind them
([now-playing-plan.md](../now-playing-plan.md), "Not in this plan") declined
butterchurn, the web MilkDrop, for one reason: *"its presets compile to JS run
with `new Function`"*. It said taking butterchurn on would need its own ADR.
This is that ADR.

Winamp had two visualizer formats, and this record covers both:
- **MilkDrop** (`.milk`): text, with an expression language (EEL) and, in
  MilkDrop 2, HLSL shaders.
- **AVS** (`.avs`): binary, with its own EEL-like expressions.

The question was never "can a browser draw MilkDrop". butterchurn does that.
The question is whether an admin can upload a preset and have every viewer's
browser play it **without any text from that file ever running as
JavaScript**. A spike measured the answer instead of assuming it
([spikes/milkdrop/](../../spikes/milkdrop/README.md)).

## Recommendation

**Yes, in a narrower form than "upload any .milk".**
- **Engine:** vendor **butterchurn 3** and always run it with `onlyUseWASM`.
  That path compiles a preset's EEL into a WebAssembly module whose only
  imports are numeric variables and maths functions. It never reaches
  `new Function`, and the spike proved that under a CSP that forbids
  `unsafe-eval`.
- **No interpreter of our own.** The spike also built one, and it works.
  It is not slower than eel-wasm at the median. But eel-wasm already exists,
  is what butterchurn 3 is built around, and has the flatter worst case. So
  the interpreter stays as the fallback and as the basis of the server's
  upload check.
- **Shaders are the hard part.** The only *correct* HLSL→GLSL converter is
  native C++, and the browser-side converter produced shaders that failed to
  compile on every MilkDrop 2 preset tried. So shaders are a separate,
  second step with an open question of its own.
- **First step:** accept a `.milk` **without** its shaders, plus butterchurn's
  own JSON format with shaders already converted.
- **Photosensitivity and gold:** a luminance limiter and a hue guard on the
  output are what LANcast can enforce. A preset's own promises are not.
- **AVS: no.** The converter decodes all 15 test presets, but the only web
  renderer covers none of them in full, and has been unmaintained since 2018.

## What the spike measured

Everything below was run on 2026-10-18 on Chris's development machine, against
three public preset collections from the projectM project, pinned by commit:
- `presets-milkdrop-original` @ `e03b83e`: 434 presets, the pack that shipped
  with MilkDrop;
- `presets-cream-of-the-crop` @ `0180df2`: 5,236 presets, projectM's current
  default pack;
- `presets-projectm-classic` @ `14a6244`: 3,817 presets.

That is **9,487 presets** in all, from about 20 years of authors. The task
asked for about ten. Ten would not have found the failure rates below,
because those rates are tenths of a percent.

The spike ran three engines on the same preset:
- **ref**: butterchurn 2's converter turns EEL into a JavaScript string, and
  `new Function` runs it. This is what LANcast must not ship. Here it is only
  the yardstick.
- **interp**: the spike's interpreter. It reads EEL into a syntax tree and
  turns the tree into ordinary JavaScript closures. No string ever becomes
  code.
- **wasm**: eel-wasm 0.0.17, the compiler butterchurn 3 uses. It turns EEL
  into a WebAssembly module.

### 1. The expression language can be handled without `eval`

| | interp (spike) | eel-wasm |
|---|---|---|
| Presets whose every EEL block is accepted | **9,461 / 9,487** (99.7%) | **9,476 / 9,486** (99.9%)¹ |
| Runtime failures | 0 in 60 frames, all 9,461 | 2 of 504 in the browser (WebAssembly trap, §3) |
| Identical to `ref`, frame by frame, on a 162-preset sample | **156 / 156** | **153 / 156** |

¹ One file named `EOE125~1.MILk` was missed by this
script's file filter.

"Identical" is a strict test:
- each engine runs 120 frames of synthetic music;
- after every frame, and at sampled vertices of the 48×36 mesh, every
  variable is compared with the reference;
- a difference over 1e-9 counts as a divergence;
- the variables are carried between frames exactly as butterchurn's
  `PresetEquationRunner` carries them.

The sample was the named dozen plus 150 drawn at random (50 original, 75
cream, 25 classic). Six of the 162 could not be compared, because
butterchurn 2's *converter* failed on them (below).

The comparison found several bugs in the spike's harness before it found
agreement. They are noted in the README, so nobody repeats them.

**eel-wasm's three differences are real, and they are about `&`.**
- One is a harness artefact: eel-wasm keeps `reg00`–`reg99` outside the
  variable pools, where the driver could not read them.
- The other two presets use a bare `&` (`bass&bass_att`,
  `bass-new_bass&time*.63`).
- Run on single lines, the engines disagree on three rules:

  | | interpreter and butterchurn 2 | eel-wasm |
  |---|---|---|
  | `5 - 3 & 6` | 2 (`&` looser than `-`) | 3 (`&` tighter) |
  | `-2 ^ 2` | 4 | −4 |
  | `2 ^ 3 ^ 2` | 512 | 64 |

- Across all 9,487 presets, **312 (3.3%) use a bare `&`**. None uses `|`
  or `^`, so only `&` matters.
- Which engine matches Winamp's MilkDrop is **not established**.
  - butterchurn 2's converter agrees with the interpreter.
  - eel-wasm's author checked his compiler against real MilkDrop with a
    test preset, which may mean eel-wasm is the faithful one.
  - Settling it needs MilkDrop itself, and is a step in the build.
  - *Settled by the amendment below (step f′).* On `&` the interpreter is
    right and eel-wasm wrong. On `^` eel-wasm is right and the interpreter
    wrong. `!` and `&&` matter too, so "only `&` matters" does not hold.
    eel-wasm is now patched.

The presets that fail are almost all typos in the preset itself:
- `above(x,0.99l)`;
- `0 = 0.01*rand(100)`;
- `.-53`;
- a stray `\\`.

Two real corners of EEL account for the rest:
- `assign()` with a buffer slot as its target;
- `_aboeq()`, an ns-eel2 internal name.

Both engines reject the same files, which says the files are at fault.

butterchurn 2's own converter is *worse* than either engine. Of the 162
sampled presets:
- **5 failed to convert**, including `rot=.3*Sin(rad-+ang*…)` from the
  original MilkDrop pack.
- **1 converted into JavaScript that does not parse.** *Unchained – Beat
  Demo (Demonology Mix)* has a bare expression as a statement
  (`ob_bob_b+ob_b*sin(q2+time*1.73);`, a no-op). The converter emitted it
  with no separator before the next statement: `(…) a['ob_g']=…`.
- The spike's interpreter and eel-wasm accepted all 6.

That last case is the hazard this ADR exists to avoid, in its harmless
form. When code is generated from untrusted text, the text decides the
shape of the program. Here it only broke the program.

One behaviour of the format was rediscovered the hard way, and both engines
need it. MilkDrop wraps a long equation across numbered lines and rejoins
them with **nothing** between, so `k1 = is_` followed by `beat*…` means
`is_beat`. Joining with a newline made 11 of the original 434 presets fail.

### 2. Speed: both cost about 4× JIT JavaScript, in different places

These are equation times per frame in Node, with JIT, on one core. A frame
here is per-frame equations plus per-vertex equations over butterchurn's
default 48×36 mesh, for the 156 presets all three engines ran:

| 156 presets | ref (`new Function`) | interp | wasm |
|---|---|---|---|
| p50 | 0.67 ms | 1.46 ms | 3.60 ms |
| p90 | 1.14 ms | 5.72 ms | 4.14 ms |
| p99 | 1.87 ms | 10.99 ms | 4.79 ms |
| total vs ref | 1× | **3.9×** | **4.2×** |

What this means:
- **Not running preset code as JavaScript costs about 4× in equation time.**
  That holds whichever safe engine is used.
- **The interpreter is cheaper at the median. WebAssembly is cheaper at the
  tail.**
  - The wasm column has a floor of about 3 ms even for trivial presets. That
    floor is mostly the driver writing variables into `WebAssembly.Global`
    objects from JavaScript, about 13 writes per vertex across 1,813
    vertices.
  - butterchurn 3's runner does the same kind of marshalling, so the
    in-browser figure below is the one that counts.
  - The interpreter's cost grows with the preset. Over the whole cream pack,
    its p99 preset spends **42 ms a frame** in equations: two and a half
    frames of budget at 60 Hz, before the GPU does anything.
- **Two earlier readings were wrong, and are recorded so nobody quotes
  them.**
  - On 40 presets, wasm looked faster than the interpreter. 162 presets
    reversed the median.
  - Before that, the interpreter looked faster than `new Function`. That
    run had the reference inside `node:vm`, where every call crosses realms.
- **Compiling a preset to WebAssembly** took a median of 24 ms and a p99 of
  141 ms in Node. That happens at preset change, while butterchurn blends,
  so the slow tail could show as a stutter at the switch.

In the browser, under the strict CSP, butterchurn 3 rendered the 504 presets
of its own v3 pack at a median **1.8 ms** per frame on the same machine,
in the desktop app's built-in Chromium pane:
- that covers equations, all GL submission, and the readback the
  measurement adds;
- the worst preset's p95 was 16 ms;
- loading a preset, which includes compiling it to WebAssembly, took a
  median of 20 ms.

A TV or a phone will be several times slower. Nothing here measured either.

### 3. butterchurn takes functions, and its WASM path never reaches `new Function`

Read from the vendored source, then proved in a browser:

- **butterchurn 2.6.7.** `loadPreset` compiles only
  `if (typeof preset.init_eqs !== 'function')`. Hand it functions and it uses
  them.
  - `bc2.html` loads it under `script-src 'self'`: no `unsafe-eval`, and
    also no `wasm-unsafe-eval`.
  - The page feeds butterchurn 2 the interpreter's closures, and it renders.
  - On the shader-less presets, its mean luminance matches butterchurn 3's
    to two decimal places (0.018/0.081/0.163 against 0.018/0.080/0.165).
  - The only CSP violation recorded was the page's own deliberate probe,
    which checks that `new Function` really is blocked.
- **butterchurn 3.0.0-beta.5.** Given a preset with `init_eqs_eel` and
  `onlyUseWASM: true`:
  - it calls `eel-wasm`'s `compileModule`, then `WebAssembly.compile`;
  - the module's imports are `WebAssembly.Global`s for the variable pools,
    plus `shims` (`sin`, `cos`, `rand`, `pow`, `log`…);
  - the `new Function` branch (`loadJSPreset`) is unreachable while
    `onlyUseWASM` is set.
  - `bc3.html` ran all 504 presets of butterchurn's v3 pack under
    `script-src 'self' 'wasm-unsafe-eval'`. The page's probe confirmed
    `new Function` was blocked, and **no other violation occurred**.

Three smaller facts about butterchurn 3 go into the vendoring review:
- **Two of the 504 presets trap.** WebAssembly throws *"float unrepresentable
  in integer range"* when eel-wasm converts NaN or Infinity to an integer,
  for a bitwise operator or a buffer index. butterchurn lets the exception
  escape to its caller. LANcast's wrapper must catch it and skip the preset,
  not stop the visualizer.
- It **adds properties to the global `Math`** (`ecma-proposal-math-extensions`).
- It reserves **128 MiB of WebAssembly memory per preset module**:
  eel-wasm's memory section is fixed at 2048 pages, holding `megabuf` and
  `gmegabuf`. Desktop browsers reserve that lazily. A 32-bit TV browser may
  refuse it. That is unmeasured, and it is the first thing to try on a TV.

### 4. Shaders: the browser converter is broken, the native one is not

MilkDrop 2 shaders are HLSL (Direct3D 9) and must become GLSL ES 3.0 for
WebGL 2. butterchurn's author has two converters:

| | `milkdrop-preset-converter` 0.1.2 (JS) | `milkdrop-shader-converter` 0.0.8 (native) |
|---|---|---|
| Inside | hlslparser compiled with emscripten | hlsl2glslfork (Unity, BSD-3) + glsl-optimizer (Mesa, MIT): 234 C/C++ files, 12 MB |
| Runs in | a browser, or Node with three stubs | Node, as a `.node` native addon |
| Result on the sample | **every MilkDrop 2 shader tried failed to compile** in Chrome | correct: it is what butterchurn's own preset packs were made with |

The JS converter's failure is systematic, not occasional. MilkDrop's
`GetBlur1(uv)` macro expands to `tex2D(sampler_blur1, uv).xyz*scale1 + bias1`.
The converter emits `… && bvec3(bias1)`, which no GLSL compiler accepts.
`GetBlur` is in a large share of MilkDrop 2 presets. butterchurn never checks
`COMPILE_STATUS`, so a broken shader does not raise an error: the preset just
renders black. The spike had to wrap `compileShader` to see it at all.

Shaders made by the native converter compile well:
- In butterchurn's v3 pack, **22 of 504** presets had a shader that
  failed.
- About half of those failures name a texture that was not loaded
  (`texsize_smalltiled_electric_nebula`, `texsize_rand00`…). The other half
  are undeclared identifiers the converter left behind (`xlat_mutablen`,
  `uv3`, `tele`).
- Twelve presets rendered black. That is the visible cost of a shader that
  fails to compile without anyone checking.

What the shaders in the packs depend on:

| | original (434) | cream (5,236) |
|---|---|---|
| has a warp or comp shader | 186 | 4,012 (77%) |
| needs a named image texture | 53 | 793 |
| needs a random texture (`rand00`…) | 2 | 94 |
| uses `tex3D` | 10 | 851 |
| has a loop | 31 | 440 |

So about a fifth of the presets that carry shaders also need image files
from a texture pack. The provenance of those images is murkier than the
presets' own.

**Where a native converter could run.**
- In the browser: no. The only browser converter is the broken one.
- On the LANcast server: not as shipped. The server is Go, and a Node native
  addon would need cgo.
- The plausible route is to compile hlsl2glslfork + glsl-optimizer to
  **WASI** and run it under **wazero**. LANcast already uses wazero, for
  plugins, and it is deny-by-default.
- The conversion would then happen once, at upload, inside a sandbox, and
  the client would receive finished GLSL.
- The spike did **not** try to build that. Whether 12 MB of 2014-era C++
  builds cleanly with wasi-sdk is the open question. It is why shaders are a
  second step and not part of the first.

**What a shader can do once it reaches WebGL:**
- It runs on the GPU, behind ANGLE's validator. That is the same surface
  every web page's shaders reach.
- It can read only the textures butterchurn binds.
- It cannot reach the network, the DOM, or JavaScript.
- Its realistic harm is a very long-running shader. WebGL 2 allows unbounded
  `while` loops, and 440 cream presets have loops. The browser's GPU watchdog
  answers that with a lost context: the visualizer goes black, the tab
  survives.
- Admin-only uploads keep that a household's own problem.

### 5. Photosensitivity: the presets ignore the rule, and LANcast can enforce it

The plan's rule is WCAG 2.3.1: **no more than three general flashes a second.**
Presets were written with no such rule.

The spike measured flashing over 5 seconds of a synthetic 120 bpm track:
- The screen was split into 25 overlapping windows, each a third of the
  width by a third of the height. That is about 11% of the screen, roughly
  WCAG's "25% of a 10° field" at living-room distance.
- A flash is two opposing changes of 0.1 relative luminance where the darker
  state is below 0.8.

The detector's first version counted **zero** flashes in every preset. A
synthetic 5 Hz strobe also read 0, which exposed it as broken: it followed
every value while no direction was set, so it never saw a swing. Corrected,
the controls read as they should: 5 Hz → 5, 2 Hz → 2, steady → 0.

| butterchurn v3 pack, 502 presets | count |
|---|---|
| any flash at all | 451 |
| **over 3 flashes a second** | **98** (worst 30/s) |
| over 3 a second **after the limiter** | **0** (worst 2.5/s) |

Treat the 98 as an **upper bound**.
- The detector judges a window by its mean luminance. A small, bright,
  fast-flickering detail inside a dark window moves that mean by 0.1 and
  counts.
- The worst readings, 30 a second, are a window changing state every frame.
  *martin – test* reads 30, yet the limiter needed to pull it only 0.038.
  That fits a small detail, not a full-field strobe.
- The detector errs towards caution, which is the right way round for a
  gate. But "98 presets strobe" should not be quoted without looking at
  some of them.
- What does not depend on the detector's calibration is that **after the
  limiter, no preset exceeds 3**.

**The limiter LANcast can own** sits after any preset. It is a post-pass in
which each region of the picture may change luminance by at most 0.6 a
second.
- That number follows from the rule. Three flashes need six transitions of
  0.1, which is 0.6 of movement in a second.
- No window's mean can move faster than its fastest cell, so more than 3 a
  second becomes *impossible*, not merely unlikely.
- The cost is that the limited picture drifts from what the preset drew. The
  median preset is pulled by 0.028 in luminance. The presets that
  actually flashed are pulled by 0.091, and a 5 Hz full-field strobe by
  0.29, where it is held close to grey.
- In practice, bright beat-driven presets look softer and smear on hard
  cuts. That is the right trade.

It would be built as one fragment shader:
- butterchurn 3 draws into its own WebGL 2 canvas and copies it to a 2D
  output canvas;
- LANcast hands it an offscreen canvas instead, and uploads each frame as a
  texture into a small WebGL pass of LANcast's own;
- that pass keeps the previous output and clamps each region's change.

The spike simulated the limiter on the measured luminance. It did not build
the GPU pass.

What LANcast **cannot** promise:
- **Red flashes.** WCAG's saturated-red rule is separate, and the limiter
  only sees luminance.
- **Pattern flicker.** Stripes and checkerboards that pulse without changing
  mean luminance are the "spatial pattern" hazard, and a mean-luminance
  limiter does not see them.
- **Small fast strobes** below the window size. These are allowed by WCAG
  but can still bother someone.
- **A flag measured at upload** cannot be trusted as a gate.
  - The server has no GPU and cannot run a preset.
  - The admin's browser could measure one at upload, but that is a claim
    from a client, measured on one synthetic track. Real music can drive a
    preset harder.
  - Such a flag is worth showing, as "this preset flashed in testing". It is
    not a substitute for the limiter.

**Calm** (the existing setting) should lower the limiter's rate. Under
`prefers-reduced-motion`, MilkDrop presets should not start on their own.

### 6. Gold: the presets ignore it too

Gold means *where you are* and nothing else ([design.md](../design.md)).
MilkDrop presets are full of amber.

Measured with the spike's sampler, using the same band `vizMath` uses
(hue 20–75°, saturation ≥ 0.12, not near black), over the v3 pack:
- 245 of 502 presets have more than 5% gold pixels on average;
- 87 have more than 20%;
- the median preset is 4.8% gold.

LANcast *can* enforce the rule. The limiter pass can carry `awayFromGold`
as a per-pixel hue shift, exactly the one `vizMath.ts` applies to the
cover's palette. That changes how presets look: a fire preset becomes
red-orange or rose. The alternatives are worse:
- let a MilkDrop preset be the one place gold means nothing;
- refuse half the presets.

The hue guard belongs in the same pass as the limiter, and it is not
optional.

### 7. AVS: the expressions would be fine, the renderer does not exist

Chris supplied a real pack: VISBOT's *VC017 The least random number* (2009).
It is an NSIS installer (sha256 `3a0610df…bb5e`). The spike extracted it with
7-Zip into an empty directory and never ran it. It holds:
- 15 `.avs` presets;
- 8 bitmaps;
- 6 `.ape` files: `buffer`, `convolution`, `eeltrans`, `FramerateLimiter`,
  `globmgr` and `triangle`. These are **native Windows DLL effect plugins**
  that the presets call into. A browser can never run them; each one would
  have to be reimplemented.

How the tools fared:

- **webvsc** (`@visbot/webvsc` 2.3.0, MIT, last published 2023-12) decoded
  **all 15** presets to JSON. The npm build exports `convertFileSync`, not
  the `convertPreset` its README documents.
- **webvs** (`webvs` 3.0.0, MIT) is the only web AVS renderer. Its last
  release was 2018-04 and its repository is untouched since 2018-06.
  - It registers 18 components. The 15 presets use 34 kinds.
  - Rendered as webvsc names them, **no preset is fully covered**. Coverage
    per preset runs from 5 of 19 components to 26 of 31.
  - Three gaps are only renames (`ConvolutionFilter`, `GlobalVariables`,
    `TexerII`), but `Movement` alone is in **all 15** and is missing. So is
    much else: `SetRenderMode` (9), `CustomBPM` (8), `AVSTransAutomation`
    (5), `Water`, `Blur`, `Bump`, `Grain`, `VideoDelay`, `Triangle`…
  - webvs compiles AVS expressions with `new Function` (`compileExpr.ts`),
    so it would need the same surgery as butterchurn 2, without an upstream
    that has already done it.
- **The expressions themselves** are the ns-eel2 family:
  - The spike's MilkDrop interpreter already parses **317 of 382** code
    blocks (83%) unmodified.
  - The rest are AVS's own builtins (`getosc`, `getspec`, `gettime`,
    `mb`/`gmb`), which are a few lines each.
  - Also left over is **AVSTrans**'s `#define` and `$dt` macro layer, which
    comes from the native `eeltrans.ape`.
  - Two `EffectList` blocks decoded as stray bytes.

So interpret-don't-eval fits AVS's expressions. What AVS lacks is a
renderer. Supporting even these 15 presets means building and maintaining an
AVS engine: 34 effects, several of them native plugins, in WebGL. That is a
project the size of butterchurn itself, with no upstream. **The decision
should be MilkDrop only.**

## Security argument

What an uploaded preset becomes on a viewer's machine:

| Part of a preset | becomes | can reach |
|---|---|---|
| base values (`zoom=1.01`) | numbers | nothing |
| EEL equations | a WebAssembly module from eel-wasm | its own linear memory, f64 globals LANcast creates, and `Math` functions. No JS objects, DOM, network or storage. |
| shaders (step 2) | GLSL, already converted on the server | the GPU, through ANGLE, with only butterchurn's textures bound |
| textures (step 2) | images | nothing; decoded by the browser |

Things that are **never** in a stored preset, and that the server refuses at
upload:
- `*_eqs_str` (butterchurn 2's JavaScript);
- any unknown key;
- HTML.

Even if one slipped through, butterchurn 3 with `onlyUseWASM` would not run
it. The CSP the client would carry, `script-src 'self' 'wasm-unsafe-eval'`,
would also throw on any `new Function`. LANcast already needs
`'wasm-unsafe-eval'` for the browser game player (ADR 0077), so it costs
nothing new.

The remaining risks, plainly:
- **The compiler is the attack surface.** eel-wasm is 27 KB of minified
  JavaScript turning untrusted text into bytes. A bug in it could, at worst,
  emit an invalid module, which `WebAssembly.compile` rejects, or a module
  that computes nonsense. A WebAssembly module cannot escape its imports.
  The interpreter would have had the same property, with a larger surface.
- **Denial of service on the viewer.** EEL `loop`/`while` is capped at
  1,048,576 iterations, the same cap as ns-eel2. A preset with nested
  loops can still take seconds per frame. Shaders can trip the GPU watchdog.
  Both hurt only the tab playing that preset, and only a preset an admin
  chose to upload.
- **Memory.** 128 MiB reserved per module. Discussed above.
- **Supply chain.** butterchurn 3 is a beta (`3.0.0-beta.5`, 2025-07),
  though maintained (commits to 2026-04). eel-wasm is `0.0.17` (2025-12).
  Vendoring follows ADR 0013's hls.js rule: build from a pinned commit,
  prove it byte-identical to the published package, check it in, review it,
  load it as its own chunk, keep it off by default.

## Licences

- **butterchurn:** MIT (Jordan Berg).
- **eel-wasm:** MIT per its `package.json`. The repository has no LICENSE
  file, so the vendoring README should record that and quote the field.
- **@assemblyscript/loader:** Apache-2.0.
- **ecma-proposal-math-extensions:** MIT.
- **Step 2 adds** hlsl2glslfork (BSD-3, ATI/3Dlabs/Unity) and glsl-optimizer
  (MIT, Mesa/Unity).
- **All of these are compatible** with LANcast shipping them.

- **Presets.** Almost no MilkDrop preset carries a licence. projectM's
  collection says so, then concludes that they are *"safe to assume … in the
  public domain"* and offers removal on request. That is a reasonable
  position for projectM, which redistributes them. **LANcast should not
  redistribute any.** It ships no presets: an admin uploads presets they
  obtained, into their own server, which is the same footing as their own
  media files.
- **The spike** committed no preset text. It reads the collections from a
  local clone.
- **butterchurn's own preset packs** (`butterchurn-presets`, MIT as a
  package) contain other authors' presets under the same uncertain terms.
  They are not bundled either.
- **Textures** are the murkiest part. Many come from MilkDrop's own
  distribution. They are an upload, like presets.

## Decision

1. **MilkDrop only.** AVS is declined on the evidence in §7.
2. **Engine:** butterchurn 3, vendored per ADR 0013's rule, with
   `onlyUseWASM: true` hard-coded. The client never constructs a preset
   containing `*_eqs_str`. It is a fourth tier in Now Playing, below Trails,
   off by default, and its chunk loads only when picked.
3. **Uploads are admin-only.** They go into `<data>/visualizers/milkdrop/`.
   The server owns them: it parses the `.milk` INI and stores a normalised
   preset (base values, EEL text, shaders when converted). It serves the
   preset same-origin to signed-in clients. No client fetches anything from
   anywhere else.
4. **The server checks EEL at upload** with a Go port of the spike's parser
   (parse only, no evaluation). A preset that will not parse is refused with
   the line and the reason, instead of failing silently on every screen. The
   port is a few hundred lines. It must follow MilkDrop's join-without-
   newline rule. It is tested against the same 9,487-preset corpus, kept
   outside the repo.
5. **Step 1 has no shaders.**
   - A `.milk` whose warp or comp shader is present plays with butterchurn's
     default shaders, and is labelled *"shaders not converted"*.
   - butterchurn's v3 JSON (`*_eqs_eel` plus GLSL) is accepted as-is, after
     key validation.
   - Textures are not accepted yet.
6. **Step 2, its own spike first:**
   - build hlsl2glslfork + glsl-optimizer for WASI;
   - run it under wazero at upload;
   - accept textures beside presets.
   - If that build is impractical, MilkDrop stays at step 1, and this ADR
     says so.
7. **The output pass is mandatory.** One fragment shader after butterchurn:
   the luminance limiter (0.6/s, lower in Calm) and the gold guard.
   `prefers-reduced-motion` keeps MilkDrop from auto-starting. A per-preset
   "flashed in testing" note may be shown. It never replaces the pass.

## Build breakdown

| Step | What | Size |
|---|---|---|
| a | Vendor butterchurn 3 + eel-wasm (pinned build, byte-identical proof, review, README) | M — mostly the review |
| b | Server: upload/list/delete API for presets (admin), `.milk` INI reader, JSON validator, storage in data dir; `docs/api.md` + `openapi.json` | M |
| c | Server: Go EEL parser for upload validation, corpus test outside the repo | S–M |
| d | Client: MilkDrop tier in the visualizer picker, chunk loading, preset list, `onlyUseWASM` | M |
| e | Client: output pass (limiter + gold guard), Calm and reduced-motion wiring, vitest for the maths the way `trailMath` is tested | M |
| f | Verify on a TV and a phone: memory (128 MiB modules), frame time | S, but must happen before release |
| f′ | Settle `&` precedence against Winamp's MilkDrop (eel-wasm's test-preset method); if eel-wasm is wrong, patch upstream or carry a patch | S — **done**: wrong, patch carried (amendment below); upstream issue drafted, not filed |
| g | **Step 2 spike:** hlsl2glslfork + glsl-optimizer → WASI → wazero; texture uploads | unknown until spiked |

The plan's other open item, a visualizer as a wazero plugin, is unaffected.
A MilkDrop preset is data for a built-in engine, not a plugin.

## Alternatives considered

- **The spike's own interpreter in place of eel-wasm.**
  - It works: 99.7% coverage, 156/156 identical to the reference, and it
    runs under the strictest CSP (no WebAssembly at all).
  - It is cheaper than eel-wasm at the median and three times worse at p99.
  - It would mean forking butterchurn's runner, since butterchurn 3 is built
    around eel-wasm, and owning that code for good.
  - Keep the spike as the fallback if eel-wasm is abandoned, and as the
    basis of the Go validator.
- **butterchurn 2 fed interpreter closures.** Proven to work (`bc2.html`).
  It is the older engine and has no future upstream. Choose 3 unless 3
  fails on a TV.
- **Convert shaders in the admin's browser at upload.** The only browser
  converter is the broken one. Revisit if a correct one appears.
- **Trust the preset, skip the output pass.** Fails the plan's
  photosensitivity rule and the design system's gold rule by measurement.
- **AVS via webvs.** §7.

## Not in this decision

- Presets in the pop-out window: that is the same open item as for every
  visualizer.
- Editing presets in LANcast.
- Sharing presets between paired servers. A preset is data, so it could
  travel later, under the federation rules.

## Amendment, 2026-10-18 — step f′: eel-wasm groups operators unlike MilkDrop, so LANcast carries a patch

§1 left open which engine groups `&` and `^` the way Winamp's MilkDrop does.
Neither does throughout. eel-wasm was wrong for 110 presets of the 9,487,
and the vendored build now carries a one-file grammar patch that makes it
agree with MilkDrop in every case the corpus contains. The full record,
reproduction and hashes are in
[web/vendor/butterchurn/README.md](../../web/vendor/butterchurn/README.md)
("The second patch"). This is the decision and the evidence behind it.

**The reference is MilkDrop 2's own front end, not a reading of it.**
- MilkDrop 2's expression library is ns-eel2 (2004–2008, `code/ns-eel2` in
  the open MilkDrop 2 source). Four of its files were compiled unchanged:
  - `nseel-caltab.c`, the bison tables generated from its grammar `cal.y`;
  - `nseel-yylex.c` and `nseel-lextab.c`, the lexer;
  - `nseel-eval.c`, the name lookup;
  - and `preprocessCode()`, lines 1010–1455 of `nseel-compiler.c`, which
    rewrites `^`, `%`, `!`, the comparisons and `&&`/`||` as calls before
    parsing.
- Only the x86 code generator was replaced, with one that prints and
  evaluates the tree.
- That harness reproduces 122 of the 124 cases it can evaluate in eel-wasm's
  `testCases.ts`. Those are the cases eel-wasm's author confirmed in real
  MilkDrop with `buildTestPreset.js`, the "test-preset method" row f′ named.
  The two misses are not about grouping.
- The author's cases never combine `&`, `|`, `!` or `&&` with another
  operator. That is why his method did not catch this.
- MilkDrop 1 used Nullsoft's evallib, whose `cal.y` is public in the AVS
  source. Its declarations for `|`, `&`, `+ -`, `* /` and unary minus are the
  ones ns-eel2's tables encode. So MilkDrop 1 and 2 agree on everything here
  except `%`, which version 1 parsed at the `*` level. Version 1 also had no
  `^`, comparison operators or `&&`.

**MilkDrop's grouping, loosest first:** assignment; `&&` `||`; comparisons
(right-associative); `|`; `&`; `+` `-`; `*` `/`; unary `-`; `%` `^`
(left-associative); `!`. eel-wasm 0.0.16 had `^` and `%` right. It put `&`,
`|` and `&&`/`||` tighter than everything, and `!` and unary minus at the level
of binary `+`.

**What it changed, measured on every compiled block of the corpus.** One
evaluator ran both parses, so only grouping can differ:

| | presets |
|---|---|
| parse differs at all | 2,733, nearly all a sign moving through `*` or `/` (no value change) |
| **computed value differs** (stock eel-wasm) | **110** (1.16%): `&` 105, `!` 4, `&&` 1 |
| computed value differs (patched) | 14, none of it grouping: integer literals over 2³¹ that MilkDrop's `atoi` saturates (11), and `_aboeq()` called by name (3, already refused by the validator) |

The examples are real and visible:
- `bass-new_bass&time*.63` means `(bass-new_bass) & (time*.63)` to MilkDrop.
  eel-wasm read it as `bass - (new_bass&time)*.63`.
- `!q3*(instance%4==1)` was negated whole by eel-wasm.
- `rg4>1.2 && change2==1` became `rg4 > ((1.2 && change2) == 1)`.

**The spike's interpreter is not a reference either.** It gets `&` right but
`^` wrong (`-2^2` = 4, `2^3^2` = 512; MilkDrop gives −4 and 64). It also gets
`%` after `/` wrong, and comparison chains (it groups them to the left). It
remains the fallback named in "Alternatives", with those fixes owed if it is
ever used. The Go validator never models grouping, and agrees with the
patched compiler on all 73,013 blocks (`godiff.mjs`).

**Decision.**
- Carry `web/vendor/butterchurn/eel-wasm-precedence.patch`. It is
  eel-wasm's operator table plus 16 test cases (10 fail before it, all pass
  after; eel-wasm's own suite passes).
- Rebuild butterchurn with it, reproducibly: `a932cffc…` in two fresh clones.
  `loadButterchurn.test.ts` pins both that hash and the patch's.
- Fix it upstream too, but that is Chris's to file:
  - the issue and PR text are drafted in
    [spikes/milkdrop/precedence/UPSTREAM.md](../../spikes/milkdrop/precedence/UPSTREAM.md);
  - nothing has been posted.
  - Once eel-wasm releases the fix and butterchurn takes it, drop the patch
    and re-vendor.

**Not settled by this amendment:**
- the 16 new cases have not yet been run as a test preset in real Winamp
  (`buildTestPreset.js` generates it; a green screen confirms);
- `?:`, which MilkDrop 2 parses and eel-wasm does not;
- integer literals above 2³¹.
