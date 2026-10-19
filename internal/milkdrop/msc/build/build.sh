#!/bin/sh
# LANcast (ADR 0091 step g1). Build msc.wasm, milkdrop-shader-converter for WASI.
#
#   sh build.sh gen  <src>                  generate hlslang's parser (flex + bison)
#   sh build.sh wasm <src> <wasi-sdk> <out> compile and link <out>/msc.wasm
#
#   <src>       a clone of jberg/milkdrop-shader-converter at $MSC with its two
#               submodules at $HLSL and $GLSLOPT. Checked below: a build from
#               any other commit stops before compiling anything.
#   <wasi-sdk>  wasi-sdk 34.0, unpacked. Its VERSION file is checked too.
#   <out>       where objects and msc.wasm go. Keep <src> and <out> on paths
#               without spaces: the include flags are word-split.
#
# `gen` needs flex 2.6.4 and bison 3.8.2 (Ubuntu's, in WSL on Windows). The
# repository's own tools/bin/bison.exe, flex.exe and Cygwin DLLs are never run.
# `wasm` needs only the SDK and a POSIX shell (Git Bash will do).
#
# There is no cmake: the file lists below are the ones the two CMakeLists.txt
# glob, minus hlsl2glslfork's OSDependent layer, which this directory replaces
# with osinclude.h and ossource.cpp. convert.cpp is the converter's own
# src/main.cpp with its Node binding taken off.
#
# The README beside msc.wasm.gz records the hash this produces and how it was
# reproduced. Changing anything here — a flag, a pin, a file — changes that
# hash, and module_test.go fails until the README and the test agree again.
set -e

MSC=291c8851d2ed0769fce5d06cf32b39d51d027875     # milkdrop-shader-converter 0.0.8
HLSL=fb0123780aca0be16b08b2d068095c84d1edf873    # jberg/hlsl2glslfork
GLSLOPT=b8eba1e4c67b6c38c14c4a2cf473133181faca0a # jberg/glsl-optimizer
SDK_VERSION=34.0

mode="$1"; S="$2"
[ -n "$mode" ] && [ -n "$S" ] || { echo "usage: build.sh gen <src> | build.sh wasm <src> <wasi-sdk> <out>" >&2; exit 2; }

pin() {
  got=$(git -C "$1" rev-parse HEAD)
  [ "$got" = "$2" ] || { echo "$1 is at $got, want $2" >&2; exit 1; }
}
pin "$S" "$MSC"
pin "$S/hlsl2glslfork" "$HLSL"
pin "$S/glsl-optimizer" "$GLSLOPT"

MI="$S/hlsl2glslfork/hlslang/MachineIndependent"

if [ "$mode" = gen ]; then
  flex --version | grep -qx 'flex 2.6.4' || { echo "need flex 2.6.4" >&2; exit 1; }
  bison --version | head -1 | grep -qx 'bison (GNU Bison) 3.8.2' || { echo "need bison 3.8.2" >&2; exit 1; }
  cd "$MI"
  bison -o hlslang_tab.cpp -d -t hlslang.y
  cp hlslang_tab.hpp hlslang_tab.h
  flex hlslang.l                    # %option outfile: Gen_hlslang.cpp
  sha256sum hlslang_tab.cpp hlslang_tab.h Gen_hlslang.cpp
  exit 0
fi

[ "$mode" = wasm ] || { echo "unknown mode $mode" >&2; exit 2; }
SDK="$3"; O="$4"
[ -n "$SDK" ] && [ -n "$O" ] || { echo "usage: build.sh wasm <src> <wasi-sdk> <out>" >&2; exit 2; }
[ "$(head -1 "$SDK/VERSION")" = "$SDK_VERSION" ] || { echo "$SDK is not wasi-sdk $SDK_VERSION" >&2; exit 1; }
for f in hlslang_tab.cpp hlslang_tab.h Gen_hlslang.cpp; do
  [ -f "$MI/$f" ] || { echo "$MI/$f is missing: run build.sh gen first" >&2; exit 1; }
done

H="$(cd "$(dirname "$0")" && pwd)"
rm -rf "$O/obj" "$O/wasi"; mkdir -p "$O/obj"
# Include paths cannot carry spaces through the word-split below, and this
# directory sits under the LANcast checkout, which may have one, so the shim
# is copied next to the objects first.
mkdir -p "$O/wasi"; cp "$H/convert.cpp" "$H/osinclude.h" "$H/ossource.cpp" "$O/wasi/"; W="$O/wasi"
CC="$SDK/bin/clang --target=wasm32-wasip1 --sysroot=$SDK/share/wasi-sysroot"
CXX="$SDK/bin/clang++ --target=wasm32-wasip1 --sysroot=$SDK/share/wasi-sysroot"
# -fno-strict-aliasing is required, not tuning. Without it, at any -O level
# above -O0, glsl-optimizer never returns: Mesa's exec_list of this era reads
# its head and tail sentinels through overlapping struct pointers, which clang
# may assume cannot alias. Mesa's own builds of the time passed the same flag.
# Measured in the spike: -O0 works, -O1 and -Os hang, -Os with this flag works.
OPT="-Os -fno-strict-aliasing -DNDEBUG -w"
G="$S/glsl-optimizer"; L="$S/hlsl2glslfork"
GINC="-I$G/include -I$G/src/mesa -I$G/src/mapi -I$G/src/glsl -I$G/src"
LINC="-I$L/hlslang -I$L/hlslang/MachineIndependent -I$W"

{
  for f in "$G"/src/glsl/glcpp/*.c "$G"/src/util/*.c "$G"/src/mesa/program/*.c "$G"/src/mesa/main/*.c "$G"/src/glsl/*.c; do
    echo "c|$GINC|$f"; done
  for f in "$G"/src/glsl/*.cpp; do
    case "$f" in */main.cpp|*/builtin_stubs.cpp) continue;; esac
    echo "cxx|$GINC|$f"; done
  for f in "$L"/hlslang/GLSLCodeGen/*.cpp "$L"/hlslang/MachineIndependent/*.cpp "$L"/hlslang/MachineIndependent/preprocessor/*.cpp "$W"/ossource.cpp; do
    echo "cxx|$LINC|$f"; done
  echo "cxx|-I$S|$W/convert.cpp"
} > "$O/units.txt"

export CC CXX OPT O
# One compile per unit, in parallel. The object name keeps the parent
# directory, since glsl-optimizer and hlsl2glslfork both have a file or two of
# the same basename. The link takes the objects in sorted order (the shell's
# glob), so the parallelism cannot change the output.
xargs -P "${JOBS:-8}" -I{} sh -c '
  kind=$(echo "{}" | cut -d"|" -f1); inc=$(echo "{}" | cut -d"|" -f2); f=$(echo "{}" | cut -d"|" -f3)
  obj="$O/obj/$(basename "$(dirname "$f")")_$(basename "$f").o"
  if [ "$kind" = c ]; then $CC $OPT -std=gnu99 $inc -c "$f" -o "$obj"
  else $CXX $OPT -std=gnu++11 -fno-exceptions $inc -c "$f" -o "$obj"; fi
' < "$O/units.txt"

$CXX $OPT -fno-exceptions -o "$O/msc.wasm" "$O"/obj/*.o -Wl,--strip-all -Wl,-z,stack-size=1048576
# gzip -n: no name and no timestamp in the header, so the .gz is a function of
# the .wasm alone.
gzip -9 -n -c "$O/msc.wasm" > "$O/msc.wasm.gz"
sha256sum "$O/msc.wasm" "$O/msc.wasm.gz"
ls -l "$O/msc.wasm" "$O/msc.wasm.gz"
