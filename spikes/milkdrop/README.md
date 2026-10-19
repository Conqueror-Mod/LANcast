# SPIKE — MilkDrop presets without eval (ADR 0091)

**Throwaway.** This directory is evidence for
[ADR 0091](../../docs/adr/0091-milkdrop-presets.md), not product code.
- Nothing in `internal/` or `web/src` imports it.
- No Go or vitest suite runs it.
- It should be deleted once the ADR's build steps exist.

## What is here

| File | What it does |
|---|---|
| `eel.mjs` | The interpreter: EEL text → tokens → AST → JavaScript closures. No `eval`, no `new Function`. Semantics follow butterchurn 2's converter. |
| `milk.mjs` | Reads a `.milk` file into base values, EEL blocks and HLSL shaders. Wrapped equation lines are rejoined with **no** separator, as MilkDrop does. |
| `run.mjs` / `run-lib.mjs` | Bulk pass: parse, compile and run every preset under a directory (60 frames, 48×36 mesh, waves and shapes). |
| `diff3.mjs` | The three-way comparison: butterchurn 2's `new Function` output, the interpreter, and eel-wasm, on identical inputs, compared after every frame. |
| `wasm-bulk.mjs` | Does eel-wasm compile every preset? |
| `godiff.mjs` | Written after the spike, for step c: does the Go validator (`internal/milkdrop/eel`) accept exactly the blocks eel-wasm accepts? Reads the blocks `internal/milkdrop/corpus_test.go` writes. |
| `shaders.mjs` | What the HLSL shaders depend on: textures, `tex3D`, loops. |
| `pick.mjs`, `prep-browser.mjs` | Choose the named dozen and the random sample, and build the browser JSON. The JSON keeps the EEL and drops every `*_eqs_str`. |
| `browser/bc3.html` | butterchurn 3, `onlyUseWASM`, under `script-src 'self' 'wasm-unsafe-eval'`. Records shader-compile failures, frame time, flashes (raw and limited) and gold. |
| `browser/bc2.html` | butterchurn 2 handed the interpreter's closures, under `script-src 'self'`. |
| `browser/common.js` | Synthetic music, readback, the flash detector, the limiter simulation and the gold sampler. |
| `avs/*.mjs` | The AVS corpus: decode with webvsc, check webvs coverage, parse AVS expressions with `eel.mjs`. |
| `results/` | Summaries of the runs quoted in the ADR. |

## Reproducing

The inputs are **not** in the repository. Preset licences are uncertain (see
the ADR), and the libraries are third-party.

- **Presets:** `git clone --depth 1` of
  `projectM-visualizer/presets-milkdrop-original` (`e03b83e`),
  `presets-cream-of-the-crop` (`0180df2`) and `presets-projectm-classic`
  (`14a6244`).
- **Libraries:** `npm pack` `butterchurn@2.6.7`, `butterchurn@3.0.0-beta.5`,
  `butterchurn-presets@2.4.7` and `@3.0.0-beta.4`, `eel-wasm@0.0.17`,
  `milkdrop-preset-converter@0.1.2` and `milkdrop-shader-converter@0.0.8`,
  unpacked as `x/<name>-<version>/package`.
- **Browser pages:** copy `butterchurn3.js` and `butterchurn2.js` from those
  packages beside the pages. Copy `eel.mjs` as `eel.js` (a static server may
  serve `.mjs` as text/plain). Serve the folder with any static server.
- **AVS:** `7z x` the VISBOT installer into an empty directory. Do not run
  it. Then `npm install @visbot/webvsc@2.3.0 webvs@3.0.0 --ignore-scripts`.

Every script takes its input paths as arguments. None of them downloads
anything.

## Mistakes the harness made, so nobody repeats them

Each of these produced a confident wrong result before it was caught:

1. **Wrapped lines joined with `\n`.**
   - MilkDrop rejoins `per_frame_22=k1 = is_` and `per_frame_23=beat*…`
     with nothing between, so the name is `is_beat`.
   - Joining with a newline failed 11 of the original pack's 434 presets.
2. **Base values seeded under `.milk` names.**
   - butterchurn's equations read `decay`, `echo_zoom`, `zoomexp`. The
     `.milk` file writes `fDecay`, `fVideoEchoZoom`, `fZoomExponent`.
   - The converter also emits only the non-default values. Without
     butterchurn's `baseValsDefaults`, half the presets "diverged" on NaN.
3. **Timing inside `node:vm`.**
   - Cross-realm calls made the `new Function` reference look *slower* than
     the interpreter (0.71×).
   - In the same realm it is 3–6× *faster*. `diff3.mjs` compiles the
     reference in the main realm.
4. **Frame variables.**
   - `run.mjs` lets the previous frame's `time`, `bass` etc. override the
     new frame's. Its timings are therefore only indicative.
   - `diff3.mjs` follows butterchurn's `PresetEquationRunner` exactly, and
     the agreement figures come from it.
5. **Per-vertex variables in the wasm driver.**
   - butterchurn clones the frame's variables once per frame for the vertex
     pass, so pixel-only variables restart each frame.
   - Letting them persist made eel-wasm "diverge" on 19 of 40 presets.
6. **The flash detector counted nothing.**
   - With no direction set, it followed every value and never saw a 0.1
     swing, so every preset read 0 flashes.
   - A synthetic 5 Hz strobe reading 0 exposed it. Positive controls (5 Hz,
     2 Hz, steady) now run first.
7. **Infinity − Infinity is NaN.** The comparison counted two equal
   infinities as a divergence.

## The named dozen

Chosen by `pick.mjs` with a fixed seed: three MilkDrop 1 and three MilkDrop 2 presets from the original pack; from cream, one each with `loop`+`megabuf`, `while`, `tex3D`, the `worms` texture and a 50+-instance shape; one MilkDrop 1 per-point wave from classic. Shaders here went through the **JS** converter (the broken one); the 504-preset browser run used natively converted shaders.

| Preset | MD | shaders | interp vs ref | eel-wasm vs ref | JS-converted shaders compile |
|---|---|---|---|---|---|
| Rovastar - Destiny Star (Starbrust Mix) | 1 | none | identical | identical | — |
| Phat_Eo.S. Eyes_spiral_mix | 1 | none | identical | identical | — |
| Idiot24-7 - Ascending to heaven 2 | 1 | none | identical | identical | — |
| fiShbRaiN + Flexi - witchcraft 2.0 | 2 | warp comp | identical | identical | no (2 failed) |
| ORB - Acid Cycle [flexi composite] | 2 | warp comp | identical | identical | no (1 failed) |
| martin - pendulum in brass ps3 | 2 | warp comp | identical | identical | no (2 failed) |
| $$$ Royal - Mashup (430) | 2 | warp comp | identical | differs (reg04) | no (2 failed) |
| Martin - Pixies Party (Hakan mash-up) 6-6 | 2 | warp comp | converter threw; interp accepts | compiled | converter failed |
| catch 333 magick relayed to unbeknownsth more FOTIS | 2 | warp comp | converter threw; interp accepts | compiled | converter failed |
| afro-legionican | 2 | warp comp | identical | identical | no (2 failed) |
| suksma - geiss + dotes roams proph | 2 | warp comp | identical | identical | no (2 failed) |
| shifter - tadpole evolution (static mix) | 1 | none | identical | identical | — |
