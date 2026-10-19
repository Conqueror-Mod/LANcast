# msc — the MilkDrop shader converter, vendored, reproduced, pinned

This directory holds the converter that turns a MilkDrop 2 preset's HLSL
shaders into the GLSL butterchurn runs, as [ADR 0091](../../../docs/adr/0091-milkdrop-presets.md)'s
amendment of 2026-10-19 decided (step g1). It is checked in as a built
artefact, `msc.wasm.gz`, with the few files LANcast wrote to build it and the
script that does. It follows the rule [ADR 0013](../../../docs/adr/0013-transcode-pipeline.md)
set for hls.js, as [web/vendor/butterchurn/README.md](../../../web/vendor/butterchurn/README.md)
does: build it here from pinned sources, prove the build reproduces, check in
what was built, and say what was read.

It is the same converter butterchurn's own preset packs were made with. Unlike
butterchurn, it runs on the server, inside wazero, and never natively: the
spike found memory-safety bugs in it (below). `msc.go` is the sandbox.

Nothing here updates itself. Updating it means repeating the build below,
re-reading the risk paths, and replacing this record — by a person,
deliberately.

## What is here

| | |
| --- | --- |
| converter | `milkdrop-shader-converter` 0.0.8, commit `291c8851d2ed0769fce5d06cf32b39d51d027875` ("bump version to 0.0.8") |
| upstream | https://github.com/jberg/milkdrop-shader-converter |
| ↳ submodule | `hlsl2glslfork` @ `fb0123780aca0be16b08b2d068095c84d1edf873` (https://github.com/jberg/hlsl2glslfork, branch `cmake-remove-tests`) — Unity's HLSL parser and GLSL generator, with a cut-down mojoshader preprocessor |
| ↳ submodule | `glsl-optimizer` @ `b8eba1e4c67b6c38c14c4a2cf473133181faca0a` (https://github.com/jberg/glsl-optimizer, branch `cmake-remove-tests`) — Unity's fork of Mesa's GLSL compiler |
| toolchain | wasi-sdk 34.0 (`wasi-sdk-34.0-x86_64-windows.tar.gz`, 619,003,408 bytes, sha256 `cccb5c323a9b34f0349a9b09e8804a0a7632c68c3310f4b5f437ed57d7e71d8f`, the digest GitHub publishes for the release): clang 23.1.0 (llvm `895aa2c896ad`), wasi-libc `2e6fb9d8ee0c` |
| parser generators | flex 2.6.4 and bison 3.8.2, Ubuntu's, in WSL |
| artefact | `msc.wasm`, gzipped as `msc.wasm.gz` |
| sha256 (`msc.wasm`) | `91babcc93ad5b5f90231087f966c6f5e36d182af886e1a6dde6c7a4b5aad1cf9` |
| size | 1,225,443 bytes; 340,265 gzipped (`gzip -9 -n`, gzip 1.14) |
| sha256 (`msc.wasm.gz`) | `3c5771001e67753a21ea2616728293c56f7633bd58e890da63a2f51b7d98a46a` |

`module_test.go` inflates the embedded file and holds it to the first hash and the
size, so a changed module cannot be checked in without this record changing
too.

LANcast's own files, in `build/`, are the whole of LANcast's change: there are
**no patches to the sources**.
- `convert.cpp` is the converter's `src/main.cpp` with its Node binding taken
  off: HLSL on stdin, GLSL on stdout, the info log on stderr, and its C++
  exceptions turned into exit codes (0 ok; 1 HLSL parse error; 2 translation
  error; 3 non-ASCII output; 4 glsl-optimizer refused it; 5 empty input). The
  calls into hlsl2glslfork and glsl-optimizer, the GLSL ES 3.00 header and the
  optimiser pass are unchanged.
- `osinclude.h` and `ossource.cpp` replace hlsl2glslfork's `OSDependent/Linux`
  layer, which keeps its pool allocator and "initialised" flag in pthread keys.
  A module here is single-threaded and lives for one shader, so an 8-slot
  table is all the thread-local storage there is.
- `build.sh` builds it, below.

Licences of what the module contains, all compatible with LANcast shipping it,
with notices:

| in `msc.wasm` | licence | file here |
| --- | --- | --- |
| milkdrop-shader-converter (the driver `convert.cpp` derives from) | MIT | `LICENSE.milkdrop-shader-converter` |
| hlsl2glslfork | BSD-3-Clause (Unity, BitSquid, ATI, 3Dlabs); its mojoshader preprocessor zlib (Ryan C. Gordon) — both in its `LICENSE.txt` | `LICENSE.hlsl2glslfork` |
| glsl-optimizer | MIT (Brian Paul, Unity) | `LICENSE.glsl-optimizer` |
| wasi-libc (with musl, cloudlibc, dlmalloc) | Apache-2.0 WITH LLVM-exception / Apache-2.0 / MIT, with musl's MIT and cloudlibc's BSD-2-Clause | `LICENSE.wasi-libc` |
| libc++, libc++abi | Apache-2.0 WITH LLVM-exception | `LICENSE.libcxx` |

And beside it, ported to Go rather than linked:

| in `internal/milkdrop/shader.go` | licence | file here |
| --- | --- | --- |
| `splitPreset`, `prepareShader`, `processOptimizedShader` from milkdrop-preset-utils @ `ba5a4e4` | MIT (Jordan Berg) | `LICENSE.milkdrop-preset-utils` |

The wasi-libc and libc++ texts were taken from those projects at the commits
wasi-sdk 34 names in its `VERSION` file; the SDK itself ships none.

## Building it

Use a short path without spaces for the clone and the output (the include
flags are word-split, and the submodules' paths are long). Clone with
`core.autocrlf=false`: the build below was made from an LF checkout. (The
spike's was CRLF, and gave the same hash; see the proof.)

```sh
git -c core.autocrlf=false -c core.longpaths=true clone --no-checkout https://github.com/jberg/milkdrop-shader-converter msc
cd msc
git -c core.autocrlf=false -c core.longpaths=true checkout 291c8851d2ed0769fce5d06cf32b39d51d027875
git -c core.autocrlf=false -c core.longpaths=true submodule update --init
cd ..

# in WSL (or any Linux with flex 2.6.4 and bison 3.8.2): generate hlslang's parser
sh <LANcast>/internal/milkdrop/msc/build/build.sh gen <the clone>
#   hlslang_tab.cpp  3127030ef2ca8f682ee30ab81421de594e8976a50fe3656bf7428967144b5c6b
#   hlslang_tab.h    62381c780009a46acf07db02f71274e3f21bc847a795b1e1acc34a3740dee5e3
#   Gen_hlslang.cpp  6a124566e60acf0f1d51f5a6ac984a8a82d78d57b7ebeecb162aab2b5817800e

# in Git Bash (or any POSIX shell) with wasi-sdk 34.0 unpacked
sh <LANcast>/internal/milkdrop/msc/build/build.sh wasm <the clone> <wasi-sdk-34.0> <out>
#   <out>/msc.wasm     91babcc9...5aad1cf9
#   <out>/msc.wasm.gz  3c577100...7d98a46a
cp <out>/msc.wasm.gz <LANcast>/internal/milkdrop/msc/
```

`build.sh` refuses to run against any other commit of the three repositories,
or any other wasi-sdk, and `gen` refuses any other flex or bison. The
repository's own `tools/bin/bison.exe`, `flex.exe` and Cygwin DLLs are never
run; nothing downloaded is run except the toolchain. There is no cmake: the
file lists are the ones the two `CMakeLists.txt` glob, minus hlsl2glslfork's
OS layer. It takes about 20 s with 16 jobs.

**One flag is required, not tuning: `-fno-strict-aliasing`.** Without it,
every optimised build (`-O1`, `-Os`) loops for ever inside glsl-optimizer,
while `-O0` works. Mesa's `exec_list` of this era reads its head and tail
sentinels through overlapping struct pointers, which clang may assume cannot
alias; Mesa's own builds of the time passed the same flag. The spike found it
by bisecting (ADR 0091 amendment §2).

## The reproducible-hash proof

`msc.wasm` came out `91babcc9…5aad1cf9` in every build:

| build | source | script | parser | result |
| --- | --- | --- | --- | --- |
| spike, three builds (2026-10-18), one from a copy at another path | CRLF clone | the spike's `build.sh` | flex/bison in WSL | `91babcc9…` |
| g1, 2026-10-19 | **fresh LF clone**, `D:/wasisdk-dl/g1/msc` | **this** `build/build.sh`, 16 jobs | regenerated by `build.sh gen` | `91babcc9…` |
| g1, 2026-10-19 | a copy of that clone at another path, `D:/wasisdk-dl/g2/other-src` | this `build.sh`, 4 jobs | the same | `91babcc9…` |

So the hash does not depend on the checkout's line endings, the directory, the
number of parallel jobs, or which script built it (the two differ only in
comments and pin checks). The generated parser is identical across the CRLF
and LF checkouts once carriage returns are removed, and `gzip -9 -n` of the
same module gave the same `.gz` both times.

**Not proved:** a build on another machine, or with the Linux or macOS
wasi-sdk. clang is deterministic for a given input and version, so another
host should agree; that is an expectation, not a measurement.

The module was also compared with the converter itself, in the spike: a
native mingw-w64 gcc build of the same sources agreed with it on 298 of 298
converted shaders and on all 304 exit codes; and its output, put through the
same string steps, is byte-identical to butterchurn's own packs on 388 of 390
(v3) and 1,033 of 1,043 (v2) shaders, every difference being a different
revision of the preset (ADR 0091 amendment §4).

## What was read, and what it can reach

**The module's whole reach is 13 WASI imports**, all `wasi_snapshot_preview1`:
`args_get`, `args_sizes_get`, `environ_get`, `environ_sizes_get`,
`clock_time_get`, `fd_close`, `fd_fdstat_get`, `fd_prestat_get`,
`fd_prestat_dir_name`, `fd_read`, `fd_seek`, `fd_write`, `proc_exit`. It
imports no memory and nothing outside WASI. There is no `path_open`, so it
could not name a file even if a directory were mounted; `msc.go` mounts none,
passes no environment, uses wazero's fake clock, and adds no host functions.
`module_test.go` fails if a rebuild changes that list.

**What was read.** `convert.cpp` and the OS layer in full (LANcast's). In the
converter, the paths that could reach outside it, found by searching every
compiled file for `fopen`, `open`, `popen`, `system`, `exec*`, `socket` and
`getenv`:
- glsl-optimizer's only `fopen` is in its command-line `main.cpp`, which
  `build.sh` leaves out; its only `getenv` is in `src/getopt`, which is not
  compiled either.
- hlsl2glslfork's mojoshader preprocessor has a default include reader,
  `MOJOSHADER_hlslang_internal_include_open`, that calls `stat` and `open`.
  Nothing calls it. `convert.cpp` passes no callbacks
  (`Hlsl2Glsl_Parse(…, NULL, …)`), so `PaParseString` hands the preprocessor
  a null include callback, and an `#include` in a shader fails with *"Saw
  #include, but no include callbacks defined"* (`mojoshader_preprocessor.cpp`
  line 809). The linker drops the unreferenced reader, which is why the module
  imports no `path_open`; the tests check both.

The rest of the C++ is not reviewed line by line — 234 files, 12 MB — and it
does not need to be for safety: the sandbox is the argument, not the code.

**What is known to be wrong in it.** The spike mutated 3,000 real shaders at
random. **2 trapped** with *invalid table access*, an indirect call through a
corrupted function pointer, which in a native build is a crash or worse. **1
never returned.** The host converted on through all three. That is why:
- it never runs natively in the server;
- every shader gets a fresh instance, so one shader's corruption cannot reach
  the next;
- there is a **3 s deadline** (seven times the slowest real shader with one
  worker, 434 ms) and a **32 MiB memory ceiling** (five times the largest
  instance, 5.9 MiB), and one conversion runs at a time.

It was not a coverage-guided fuzzer: it shows these bugs exist, not how many
there are. `msc_test.go` proves the deadline, the ceiling and trap
containment with modules of a dozen bytes built for it, since the real
trapping inputs are corpus shaders LANcast does not redistribute.

**What its output can do** is what a butterchurn JSON upload's GLSL could
already do (ADR 0091's security table): run on the GPU behind ANGLE, see only
butterchurn's textures, reach no network, DOM or JavaScript. It is bounded by
`MaxShaderBytes` (64 KB a shader), and LANcast's output pass runs after it.

## Cost

Measured by the spike on Chris's development machine (amd64), and again by
this build's tests:
- compiling the module, once per process, on the first upload that needs it:
  ~0.55 s; Go's `Sys` rises from ~10 to ~75 MiB and stays there;
- a shader: ~0.1 s (p50), ~52 ms of it each fresh instance building its tables;
- in the release binary: the 340 KB `.gz`, inflated in memory on first use.

## Signed off

The term exists so that a person looked. The build, the reproduction and these
notes were prepared with Claude Code; the signature below is the human review,
and it is not a formality.

- Reviewed by:
- Date:
- Reproduction confirmed independently:
