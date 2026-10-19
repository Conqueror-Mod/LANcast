# Step g — MilkDrop 2 shaders, converted on the server (ADR 0091)

**Evidence, not product code**, like the rest of `spikes/milkdrop`. Nothing in
`internal/` or `web/src` imports it. The two Go programs carry the `spike`
build tag, so `go build ./...` and `go test ./...` never see them. The
decision is in ADR 0091's amendment of 2026-10-19.

The question: can the converter butterchurn's own preset packs were made
with run inside the LANcast server, under wazero, at upload, and are its
results worth having?

## The converter chain, exactly

butterchurn's packs were made by `milkdrop-preset-converter-node`. Per shader:

| step | what | where | licence |
|---|---|---|---|
| `splitPreset` | pull `warp_N=` / `comp_N=` lines out of the `.milk` | `milkdrop-preset-utils` @ `ba5a4e4` (JS) | MIT |
| `prepareShader` | wrap the body in MilkDrop's header: samplers, `q1`–`q32`, `GetBlur1`… macros | same | MIT |
| `convertHLSLString` | **HLSL → GLSL ES 3.00, then optimised** | `milkdrop-shader-converter` @ `291c885` (0.0.8), a Node addon | MIT |
| ↳ | HLSL (D3D9) parser and GLSL code generator | `jberg/hlsl2glslfork` @ `fb01237` (Unity's fork, with mojoshader's preprocessor) | BSD-3 (`LICENSE.txt`: Unity, BitSquid, ATI, 3Dlabs); its stripped-down mojoshader preprocessor was zlib |
| ↳ | GLSL optimiser (Mesa's GLSL compiler front end) | `jberg/glsl-optimizer` @ `b8eba1e` (Unity's fork of Mesa) | MIT (`license.txt`: Brian Paul, Unity) |
| `processOptimizedShader` | strip the GLSL back to the `shader_body { }` that butterchurn wraps again at runtime | `milkdrop-preset-utils` (JS) | MIT |

Linked into the module as well: wasi-sdk's wasi-libc (MIT / Apache-2.0) and
libc++ (Apache-2.0 with LLVM exception). All of it is compatible with LANcast
shipping it, with notices.

`milkdrop-preset-converter` (the browser one, hlslparser via emscripten) is
**not** in this chain. It is the broken converter ADR 0091 §4 measured.

Only the middle row is native. The rest is string work: `pipeline.mjs` is a
transcription of it, checked byte for byte against the shipped packs below.

## What is here

| file | what |
|---|---|
| `wasi/convert.cpp` | `milkdrop-shader-converter`'s `main.cpp` as a WASI command: HLSL on stdin, GLSL on stdout. The Node binding is gone, exceptions are exit codes, nothing else changed. |
| `wasi/osinclude.h`, `ossource.cpp` | hlsl2glslfork's OS layer for a single-threaded module: an 8-slot table where Linux uses pthread keys. |
| `build.sh` | builds `msc.wasm` with wasi-sdk, no cmake: the two CMakeLists' file lists, compiled in parallel. |
| `pipeline.mjs` | the string steps either side of the native one. |
| `jobs.mjs` | corpus → one job per non-empty warp or comp shader. |
| `host/main.go` | runs `msc.wasm` under wazero, one fresh instance per shader, the way an upload would. Times, exit codes, memory. |
| `fidelity.mjs` | does the WASI build reproduce the native addon? Compares with butterchurn's packs. |
| `post.mjs` | results → converted shaders per preset, failure reasons, texture needs. |
| `bundle/main.go` | the browser set: reads each `.milk` with `internal/milkdrop`, adds the GLSL, and passes it back through `milkdrop.Parse` as butterchurn JSON. |
| `browser/render.{html,js}` | butterchurn 3 (LANcast's vendored build) renders each preset twice, without and with its shaders, and records compile and link failures, GL errors, frame time and the difference. |
| `results/` | the summaries quoted in the ADR. |

## Building it

Build from source only. Nothing downloaded was run except the toolchain:
- **wasi-sdk 34.0** (`wasi-sdk-34.0-x86_64-windows.tar.gz`, 619,003,408 bytes,
  sha256 `cccb5c32…71d8f`, matching the digest GitHub publishes for the
  release);
- **flex 2.6.4 and bison 3.8.2** from Ubuntu's archive, in WSL. They generate
  hlslang's lexer and parser. The repository ships a `tools/bin/bison.exe`,
  `flex.exe` and Cygwin DLLs for this, and they were not run.

```sh
git clone https://github.com/jberg/milkdrop-shader-converter && cd milkdrop-shader-converter
git checkout 291c885 && git -c core.longpaths=true submodule update --init
cd hlsl2glslfork/hlslang/MachineIndependent
bison -o hlslang_tab.cpp -d -t hlslang.y && cp hlslang_tab.hpp hlslang_tab.h
flex hlslang.l                                   # -> Gen_hlslang.cpp
cd -
sh <this dir>/build.sh <that clone> <wasi-sdk-34.0> <out dir>   # -> out/msc.wasm
```

Paths with spaces break the include flags, so keep the clone and the output
directory on a path without one (the spike used `D:/wasisdk-dl`).

## Running it

```sh
node jobs.mjs run/jobs.jsonl <corpus>/presets-milkdrop-original <corpus>/presets-cream-of-the-crop <corpus>/presets-projectm-classic
go run -tags spike ./spikes/milkdrop/shaders/host -wasm out/msc.wasm -in run/jobs.jsonl -out run/results.jsonl
node fidelity.mjs run/results.jsonl run/fid.json <butterchurn-presets>/presets
node post.mjs run/results.jsonl run/jobs.jsonl.meta.json run/converted.json run/summary.json
go run -tags spike ./spikes/milkdrop/shaders/bundle -conv run/converted.json -every 4 -out serve/presets.json <the three pack dirs>
# serve/: browser/render.*, ../browser/common.js, web/vendor/butterchurn/butterchurn.min.js
# open render.html (?set=controls.json for the positive controls)
```

## Mistakes the harness made, so nobody repeats them

1. **Optimised builds never returned.** Every `-O1`/`-Os` build hung inside
   glsl-optimizer; `-O0` worked. It is Mesa's `exec_list` of 2014 reading its
   head and tail sentinels through overlapping struct pointers, which clang
   may assume cannot alias. `-fno-strict-aliasing` fixes it (Mesa's own
   builds of the time passed it). Found by bisecting: trace points in the
   driver, then a native gcc build that worked, then optimisation levels.
   The deadline the host sets is what turned a hang into a measurement.
2. **A `#` in a preset's file name.** Job ids are `<file>#warp`; splitting on
   the first `#` mislaid shaders. Split on the last.
3. **Native output had CRLF.** Windows' text-mode stdout made every native
   result "differ" from the WASI one until line endings were normalised.
4. **Node will not import `D:/…` as a module path.** Use a `file:///` URL.
