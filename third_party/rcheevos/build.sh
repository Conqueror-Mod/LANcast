#!/usr/bin/env bash
#
# Build LANcast's rcheevos.dll: RetroAchievements' own library, MIT, for
# Windows x86_64, cross-compiled from Linux (ADR 0111).
#
#   ./build.sh [output-dir]      default: ./out
#
# rcheevos publishes no DLL; RetroArch and every other emulator compile it in.
# LANcast's player is Go and loads native code with syscall, not cgo, so the
# library is built here into a DLL with one file of LANcast's own beside it
# (lancast_rc.c), the only functions the DLL exports.
#
# Nothing in rcheevos is patched. Prerequisites, on Ubuntu:
#
#   sudo apt install -y mingw-w64 git curl
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
out="${1:-$here/out}"
work="${RCHEEVOS_WORK:-$HOME/lancast-rcheevos/work}"

# shellcheck source=versions.env
source "$here/versions.env"

cc=x86_64-w64-mingw32-gcc
mkdir -p "$work" "$out"

src="$work/rcheevos-$RCHEEVOS_TAG"
if [ ! -d "$src/.git" ]; then
	git clone -q --depth 1 --branch "$RCHEEVOS_TAG" https://github.com/RetroAchievements/rcheevos.git "$src"
fi
have="$(git -C "$src" rev-parse HEAD)"
if [ "$have" != "$RCHEEVOS_COMMIT" ]; then
	echo "rcheevos $RCHEEVOS_TAG is $have, not $RCHEEVOS_COMMIT; refusing it" >&2
	exit 1
fi

inc="$work/libretro-include"
mkdir -p "$inc"
if [ ! -f "$inc/libretro.h" ]; then
	curl -fsSL --retry 3 -o "$inc/libretro.h" \
		"https://raw.githubusercontent.com/libretro/libretro-common/$LIBRETRO_COMMON_COMMIT/include/libretro.h"
fi

# Every C file rcheevos has, except the RAIntegration bridge (a Windows-only
# developer tool that loads another DLL) and the external-client shim it uses.
sources=()
while IFS= read -r f; do sources+=("$f"); done < <(
	find "$src/src" -name '*.c' \
		! -name 'rc_client_raintegration.c' \
		! -name 'rc_client_external.c' | sort
)

# -fvisibility=hidden plus dllexport in lancast_rc.c: the DLL exports the lrc_
# functions and nothing else. -static-libgcc, and winpthread linked statically
# (rc_client's clock_gettime lives there), so it needs no mingw runtime DLL.
# -Wl,--no-insert-timestamp and a fixed -ffile-prefix-map keep the bytes the
# same from one build to the next, so the recorded hash can be re-proved.
# Symbols are stripped by the linker (-s), not by a separate strip, which
# writes the current time back into the PE header.
"$cc" -O2 -shared -std=c99 \
	-DRC_CLIENT_SUPPORTS_HASH -D_WIN32_WINNT=0x0601 \
	-fvisibility=hidden -static-libgcc \
	-ffile-prefix-map="$work"=/build -ffile-prefix-map="$here"=/lancast \
	-Wl,--no-insert-timestamp -s \
	-I"$src/include" -I"$src/src" -I"$inc" \
	-o "$out/rcheevos.dll" \
	"$here/lancast_rc.c" "${sources[@]}" \
	-Wl,-Bstatic -lwinpthread -Wl,-Bdynamic

cp "$src/LICENSE" "$out/rcheevos-LICENSE.txt"

# Proof the DLL exports only ours: the rows of objdump's name table, which
# look like "[   0] lrc_do_frame".
exports="$(x86_64-w64-mingw32-objdump -p "$out/rcheevos.dll" \
	| sed -n '/Name Pointer\] Table/,/^$/p' \
	| grep -E '^[[:space:]]*\[ *[0-9]+\]' | awk '{print $NF}')"
if echo "$exports" | grep -qv '^lrc_'; then
	echo "the DLL exports something other than lrc_ functions:" >&2
	echo "$exports" >&2
	exit 1
fi
echo "$exports" | sed 's/^/  export /'
sha256sum "$out/rcheevos.dll"
