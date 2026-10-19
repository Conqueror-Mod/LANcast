#!/bin/sh
# SPIKE (ADR 0091 step g). Build milkdrop-shader-converter for WASI.
#
#   $1  a clone of jberg/milkdrop-shader-converter @ 291c885 with its two
#       submodules (glsl-optimizer @ b8eba1e, hlsl2glslfork @ fb01237), and
#       hlslang's parser already generated (see README: flex + bison)
#   $2  wasi-sdk (34.0 was used)
#   $3  output directory
#
# No cmake: the file lists below are the ones the two CMakeLists.txt glob,
# minus hlsl2glslfork's OSDependent layer, which wasi/ replaces.
set -e
S="$1"; SDK="$2"; O="$3"; H="$(cd "$(dirname "$0")" && pwd)"
mkdir -p "$O/obj"
# Include paths cannot carry spaces through the word-split below, so the
# shim is copied next to the objects first.
rm -rf "$O/wasi"; cp -r "$H/wasi" "$O/wasi"; W="$O/wasi"
CC="$SDK/bin/clang --target=wasm32-wasip1 --sysroot=$SDK/share/wasi-sysroot"
CXX="$SDK/bin/clang++ --target=wasm32-wasip1 --sysroot=$SDK/share/wasi-sysroot"
# -fno-strict-aliasing is required, not tuning. Without it, at any -O level
# above -O0, glsl-optimizer never returns: Mesa's exec_list of this era
# reads its head and tail sentinels through overlapping struct pointers,
# which clang may assume cannot alias. Mesa's own builds of the time passed
# the same flag. Measured: -O0 ok, -O1 and -Os hang, -Os with this flag ok.
OPT="${OPT:--Os} -fno-strict-aliasing -DNDEBUG -w"
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
# directory, since glsl-optimizer and hlsl2glslfork both have an ir/ast-ish
# file or two of the same basename.
xargs -P "${JOBS:-8}" -I{} sh -c '
  kind=$(echo "{}" | cut -d"|" -f1); inc=$(echo "{}" | cut -d"|" -f2); f=$(echo "{}" | cut -d"|" -f3)
  obj="$O/obj/$(basename "$(dirname "$f")")_$(basename "$f").o"
  if [ "$kind" = c ]; then $CC $OPT -std=gnu99 $inc -c "$f" -o "$obj"
  else $CXX $OPT -std=gnu++11 -fno-exceptions $inc -c "$f" -o "$obj"; fi
' < "$O/units.txt"

$CXX $OPT -fno-exceptions -o "$O/msc.wasm" "$O"/obj/*.o -Wl,--strip-all -Wl,-z,stack-size=1048576
ls -l "$O/msc.wasm"
