#!/usr/bin/env bash
#
# Build lancast-cheevos.dll: rcheevos plus LANcast's shim, Windows x86_64
# (ADR 0112).
#
#   ./build.sh [output-dir]      default: ./out
#
# The sources are vendored in upstream/ (see PROVENANCE.md), so this fetches
# nothing. It runs anywhere a mingw-w64 gcc does: CI cross-compiles on Linux
# with x86_64-w64-mingw32-gcc, and a Windows machine with MSYS2 or WinLibs
# uses plain gcc. Set CC to choose.
#
# The DLL is plain C with no dependencies beyond the C runtime and kernel32,
# linked statically so nothing else has to ship beside it; the check at the
# end asserts that.
#
# -lwinpthread is named because rc_client.c calls clock_gettime, which
# mingw-w64 declares in <time.h> and implements in winpthread. A posix-threads
# gcc links it unasked; the win32-threads one CI and the release use does not,
# and fails with "undefined reference to clock_gettime64". Named, both build
# the same thing. winpthread is MIT (expat); its clock code is public domain.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
out="${1:-$here/out}"
up="$here/upstream"

if [[ -z "${CC:-}" ]]; then
	if command -v x86_64-w64-mingw32-gcc >/dev/null 2>&1; then
		CC=x86_64-w64-mingw32-gcc
	else
		CC=gcc
	fi
fi

# Everything in src/ except the RAIntegration bridge, which loads the
# RetroAchievements toolkit DLL for developers and has no place here.
mapfile -t sources < <(find "$up/src" -name '*.c' ! -name 'rc_client_raintegration.c' | sort)
sources+=("$here/shim/lancast_cheevos.c")

mkdir -p "$out"
"$CC" -shared -O2 -std=c99 \
	-DRC_CLIENT_SUPPORTS_HASH \
	-D_CRT_SECURE_NO_WARNINGS \
	-I"$up/include" -I"$up/src" \
	-static \
	-o "$out/lancast-cheevos.dll" \
	"${sources[@]}" \
	-lwinpthread

# The DLL must export exactly the shim, so a renamed function fails here
# rather than at a user's first game.
if command -v x86_64-w64-mingw32-objdump >/dev/null 2>&1; then
	OBJDUMP=x86_64-w64-mingw32-objdump
else
	OBJDUMP=objdump
fi
dump="$("$OBJDUMP" -p "$out/lancast-cheevos.dll")"
want="$(grep -oE 'LC_API [a-z_0-9 ]+\*? ?lc_[a-z_]+\(' "$here/shim/lancast_cheevos.h" | grep -oE 'lc_[a-z_]+' | sort)"
have="$(grep -oE '\+base\[ *[0-9]+\] +[0-9a-f]+ +[A-Za-z_0-9]+' <<<"$dump" | grep -v ' Export$' | awk '{print $NF}' | sort)"
if [[ "$want" != "$have" ]]; then
	echo "build.sh: exports differ from shim/lancast_cheevos.h" >&2
	diff <(echo "$want") <(echo "$have") >&2 || true
	exit 1
fi

# Only system DLLs: KERNEL32 and the C runtime (msvcrt, or the UCRT's
# api-ms-win-crt-* forwarders, depending on the toolchain).
extra="$(grep -oE 'DLL Name: .+' <<<"$dump" | sed 's/DLL Name: //' \
	| grep -viE '^(kernel32|msvcrt|ucrtbase|api-ms-win-crt-[a-z-]+-l1-1-0)\.dll$' || true)"
if [[ -n "$extra" ]]; then
	echo "build.sh: depends on a DLL that would have to ship beside it: $extra" >&2
	exit 1
fi

sha256sum "$out/lancast-cheevos.dll"
