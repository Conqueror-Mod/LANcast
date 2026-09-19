#!/usr/bin/env bash
#
# Build LANcast's libmpv-2.dll: LGPL, Windows x86_64, cross-compiled from
# Linux (ADR 0069).
#
#   ./build.sh [output-dir]      default: ./out
#
# Why a recipe of our own rather than the public mpv Windows builds: every
# published build is GPL, which a commercial licence cannot distribute. mpv and
# FFmpeg are LGPL when built without their GPL components, and what that costs
# is encoders and a few filters — x264, x265, delogo. LANcast decodes and never
# encodes in the client, so the LGPL build loses nothing it uses.
#
# Two rules hold the licence, and both are asserted at the end of this script
# rather than trusted:
#
#   1. FFmpeg is configured without --enable-gpl and mpv with -Dgpl=false.
#   2. No GPL source is fetched at all (versions.env), so a flag going missing
#      cannot quietly produce a GPL build from sources that were lying around.
#
# Prerequisites, on Ubuntu (this is the one step that needs a password):
#
#   sudo apt install -y build-essential mingw-w64 meson ninja-build cmake \
#        nasm pkg-config python3 git curl
#
# Nothing here is patched. If that ever changes, the patch belongs beside this
# file and in the provenance note, because "unmodified" is one of the things
# the LGPL asks us to be able to say.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
out="${1:-$here/out}"
work="${LIBMPV_WORK:-$HOME/lancast-libmpv/work}"
prefix="$work/prefix"
src="$work/src"
jobs="$(nproc)"

# shellcheck source=versions.env
source "$here/versions.env"

target=x86_64-w64-mingw32
export PKG_CONFIG_LIBDIR="$prefix/lib/pkgconfig"
export PKG_CONFIG_PATH="$prefix/lib/pkgconfig"
export PATH="$prefix/bin:$PATH"

mkdir -p "$src" "$prefix" "$out"

say() { printf '\n=== %s\n' "$*"; }

# Each component stamps the prefix when it installs, so a re-run after a failure
# picks up where it stopped. Shaderc alone is several minutes; losing it to a
# typo in a later step is how a one-command recipe becomes a thing nobody runs.
done_already() { [ -f "$prefix/.stamp-$1" ]; }
stamp() { touch "$prefix/.stamp-$1"; }

# fetch clones one pinned tag, shallow, and leaves it alone on a re-run.
fetch() { # name url tag
	local name=$1 url=$2 tag=$3
	if [ -d "$src/$name/.git" ]; then return; fi
	say "fetch $name $tag"
	git clone -q --depth 1 --branch "$tag" --recurse-submodules "$url" "$src/$name"
}

cat > "$work/cross.ini" <<EOF
[binaries]
c = '$target-gcc'
cpp = '$target-g++'
ar = '$target-ar'
strip = '$target-strip'
windres = '$target-windres'
# libplacebo looks for 'llvm-dlltool' then 'dlltool'; in a cross build it
# exists only under the target prefix, so meson is told where it is.
dlltool = '$target-dlltool'
llvm-dlltool = '$target-dlltool'
pkg-config = 'pkg-config'

[host_machine]
system = 'windows'
cpu_family = 'x86_64'
cpu = 'x86_64'
endian = 'little'

[built-in options]
prefix = '$prefix'
libdir = 'lib'
default_library = 'static'
buildtype = 'release'
# The MinGW runtime goes inside the DLL. Without this the build produces a
# libmpv-2.dll that needs libgcc_s_seh-1.dll and libstdc++-6.dll beside it, and
# Windows reports the absence as "The specified module could not be found" —
# naming the file that loaded fine rather than the ones that did not.
c_link_args = ['-static-libgcc', '-static-libstdc++', '-static']
cpp_link_args = ['-static-libgcc', '-static-libstdc++', '-static']
EOF

cat > "$work/toolchain.cmake" <<EOF
set(CMAKE_SYSTEM_NAME Windows)
set(CMAKE_SYSTEM_PROCESSOR x86_64)
set(CMAKE_C_COMPILER $target-gcc)
set(CMAKE_CXX_COMPILER $target-g++)
set(CMAKE_RC_COMPILER $target-windres)
set(CMAKE_FIND_ROOT_PATH $prefix;/usr/$target)
set(CMAKE_FIND_ROOT_PATH_MODE_PROGRAM NEVER)
set(CMAKE_FIND_ROOT_PATH_MODE_LIBRARY ONLY)
set(CMAKE_FIND_ROOT_PATH_MODE_INCLUDE ONLY)
EOF

# Output goes to a per-component log and is printed on failure. Sending it all
# to /dev/null cost an evening: the build stopped at libplacebo saying nothing,
# and the reason (a missing dlltool) was one line that had been discarded.
run() { # name command…
	local name=$1
	shift
	if ! "$@" > "$work/$name.log" 2>&1; then
		echo "--- $name failed; last 30 lines of $work/$name.log" >&2
		tail -30 "$work/$name.log" >&2
		return 1
	fi
}

cmake_build() { # name dir [extra cmake args…]
	local name=$1 dir=$2
	shift 2
	run "$name-configure" cmake -S "$dir" -B "$dir/build-lancast" -G Ninja \
		-DCMAKE_TOOLCHAIN_FILE="$work/toolchain.cmake" \
		-DCMAKE_INSTALL_PREFIX="$prefix" \
		-DCMAKE_BUILD_TYPE=Release \
		-DBUILD_SHARED_LIBS=OFF \
		"$@"
	run "$name-build" cmake --build "$dir/build-lancast" -j "$jobs"
	run "$name-install" cmake --install "$dir/build-lancast"
	stamp "$name"
}

meson_build() { # name dir [extra meson args…]
	local name=$1 dir=$2
	shift 2
	rm -rf "$dir/build-lancast"
	run "$name-configure" meson setup "$dir/build-lancast" "$dir" \
		--cross-file "$work/cross.ini" "$@"
	run "$name-build" ninja -C "$dir/build-lancast" -j "$jobs"
	run "$name-install" ninja -C "$dir/build-lancast" install
	stamp "$name"
}

# ---- zlib ------------------------------------------------------------------
fetch zlib https://github.com/madler/zlib.git "$ZLIB_TAG"
done_already zlib || {
	say "build zlib"
	# zlib's own mingw makefile rather than its CMake build: CMake installs the
	# static library as `zlibstatic`, and both FFmpeg (-lz) and meson look for
	# `libz.a`. FFmpeg's configure said only "zlib requested but not found",
	# which is true and says nothing about the name being the problem.
	(
		cd "$src/zlib"
		# zlib's CMake build renames zconf.h to zconf.h.included, which leaves
		# the makefile build with no header to compile against. Put it back.
		[ -f zconf.h ] || cp zconf.h.included zconf.h 2>/dev/null || cp zconf.h.in zconf.h
		run zlib-build make -f win32/Makefile.gcc -j "$jobs" PREFIX="$target-" libz.a
		mkdir -p "$prefix/lib" "$prefix/include" "$prefix/lib/pkgconfig"
		cp libz.a "$prefix/lib/"
		cp zlib.h zconf.h "$prefix/include/"
		sed -e "s|@prefix@|$prefix|; s|@exec_prefix@|$prefix|" \
			-e "s|@libdir@|$prefix/lib|; s|@sharedlibdir@|$prefix/lib|" \
			-e "s|@includedir@|$prefix/include|; s|@VERSION@|${ZLIB_TAG#v}|" \
			zlib.pc.in > "$prefix/lib/pkgconfig/zlib.pc"
	)
	stamp zlib
}

# ---- dav1d -----------------------------------------------------------------
fetch dav1d https://code.videolan.org/videolan/dav1d.git "$DAV1D_TAG"
done_already dav1d || {
	say "build dav1d"
meson_build dav1d "$src/dav1d" -Denable_tools=false -Denable_tests=false
}

# ---- SPIRV-Cross -----------------------------------------------------------
fetch spirv-cross https://github.com/KhronosGroup/SPIRV-Cross.git "$SPIRV_CROSS_TAG"
done_already spirv-cross || {
	say "build SPIRV-Cross"
cmake_build spirv-cross "$src/spirv-cross" \
	-DSPIRV_CROSS_SHARED=OFF -DSPIRV_CROSS_CLI=OFF \
	-DSPIRV_CROSS_ENABLE_TESTS=OFF -DSPIRV_CROSS_ENABLE_C_API=ON
}

# The C++ runtime by its static archive rather than -lstdc++: the linker
# answers -lstdc++ with libstdc++.dll.a, and the DLL then needs
# libstdc++-6.dll beside it. This one survived two rounds of -static flags,
# because those were on the link line while this arrived through pkg-config.
libstdcxx="$($target-g++ -print-file-name=libstdc++.a)"

# libplacebo asks pkg-config for `spirv-cross-c-shared`, which only a shared
# SPIRV-Cross installs. Shipping that would mean a second DLL beside libmpv for
# no benefit, so the static build gets an alias naming the same C API and the
# libraries it needs behind it. This is recipe glue, not a patch to anybody's
# source — the distinction PROVENANCE.md makes about being able to say
# "unmodified".
cat > "$prefix/lib/pkgconfig/spirv-cross-c-shared.pc" <<EOF
prefix=$prefix
exec_prefix=\${prefix}
libdir=\${prefix}/lib
includedir=\${prefix}/include/spirv_cross

Name: spirv-cross-c-shared
Description: C API for SPIRV-Cross (static build, aliased for libplacebo)
Version: 0.68.0
Libs: -L\${libdir} -lspirv-cross-c -lspirv-cross-glsl -lspirv-cross-hlsl -lspirv-cross-msl -lspirv-cross-cpp -lspirv-cross-reflect -lspirv-cross-util -lspirv-cross-core $libstdcxx
Cflags: -I\${includedir}
EOF

# ---- shaderc (with its own pinned dependencies) -----------------------------
fetch shaderc https://github.com/google/shaderc.git "$SHADERC_TAG"
done_already shaderc || {
	say "build shaderc"
( cd "$src/shaderc" && run shaderc-deps python3 ./utils/git-sync-deps )
cmake_build shaderc "$src/shaderc" \
	-DSHADERC_SKIP_TESTS=ON -DSHADERC_SKIP_EXAMPLES=ON \
	-DSHADERC_SKIP_COPYRIGHT_CHECK=ON -DSHADERC_ENABLE_SHARED_CRT=OFF \
	-DENABLE_GLSLANG_BINARIES=OFF -DSPIRV_SKIP_EXECUTABLES=ON
}

# shaderc installs a shared library as well as a static one, and its default
# pkg-config file names the shared. libplacebo and mpv then link it, and the
# result is a libmpv-2.dll that needs libshaderc_shared.dll beside it. Point
# the default file at the static build and remove the shared artefacts, so
# there is nothing left to link by accident.
if [ -f "$prefix/lib/pkgconfig/shaderc_combined.pc" ]; then
	cp "$prefix/lib/pkgconfig/shaderc_combined.pc" "$prefix/lib/pkgconfig/shaderc.pc"
	rm -f "$prefix/lib/libshaderc_shared.dll.a" "$prefix/bin/libshaderc_shared.dll" \
		"$prefix/lib/libSPIRV-Tools-shared.dll.a" "$prefix/bin/libSPIRV-Tools-shared.dll"
fi

# ---- libplacebo ------------------------------------------------------------
fetch libplacebo https://code.videolan.org/videolan/libplacebo.git "$LIBPLACEBO_TAG"
done_already libplacebo || {
	say "build libplacebo"
meson_build libplacebo "$src/libplacebo" \
	-Dvulkan=disabled -Dopengl=disabled -Dd3d11=enabled \
	-Dshaderc=enabled -Ddemos=false -Dtests=false
}

# Every -lstdc++ in a .pc file becomes libstdc++.dll.a on the link line, and one
# of them (FFmpeg's libavfilter) is enough to make the DLL need libstdc++-6.dll
# beside it. Rewrite them all to the static archive, once, after anything that
# installs a .pc has run. Linking both forms is a "multiple definition" wall,
# so this has to replace rather than add.
for pc in "$prefix"/lib/pkgconfig/*.pc; do
	sed -i "s#-lstdc++#$libstdcxx#g" "$pc"
done

# ---- libass and what it needs ----------------------------------------------
#
# Not optional: mpv 0.41 has no switch to build without libass, so "the page
# draws the subtitles so we need no text shaping" stops being a choice. All
# four are permissive or LGPL, so the licence position is unchanged — and
# having them is what lets styled embedded subtitles work later.
fetch freetype https://gitlab.freedesktop.org/freetype/freetype.git "$FREETYPE_TAG"
done_already freetype || {
	say "build freetype"
	# Without harfbuzz on the first pass: harfbuzz wants freetype and freetype
	# wants harfbuzz, and this is the usual way out of that circle.
	meson_build freetype "$src/freetype" -Dharfbuzz=disabled -Dbrotli=disabled \
		-Dbzip2=disabled -Dpng=disabled -Dtests=disabled
}

fetch fribidi https://github.com/fribidi/fribidi.git "$FRIBIDI_TAG"
done_already fribidi || {
	say "build fribidi"
	meson_build fribidi "$src/fribidi" -Ddocs=false -Dbin=false -Dtests=false
}

fetch harfbuzz https://github.com/harfbuzz/harfbuzz.git "$HARFBUZZ_TAG"
done_already harfbuzz || {
	say "build harfbuzz"
	meson_build harfbuzz "$src/harfbuzz" -Dfreetype=enabled -Dglib=disabled \
		-Dgobject=disabled -Dcairo=disabled -Dicu=disabled -Dtests=disabled \
		-Ddocs=disabled -Dutilities=disabled
}

fetch libass https://github.com/libass/libass.git "$LIBASS_TAG"
done_already libass || {
	say "build libass"
	meson_build libass "$src/libass" -Dfontconfig=disabled -Dlibunibreak=disabled
}

# ---- FFmpeg (LGPL: no --enable-gpl, no GPL externals) ----------------------
fetch ffmpeg https://github.com/FFmpeg/FFmpeg.git "$FFMPEG_TAG"
say "build FFmpeg"
done_already ffmpeg || (
	cd "$src/ffmpeg"
	run ffmpeg-configure ./configure \
		--prefix="$prefix" \
		--arch=x86_64 --target-os=mingw32 --enable-cross-compile \
		--cross-prefix="$target-" --pkg-config=pkg-config --pkg-config-flags=--static \
		--enable-runtime-cpudetect \
		--enable-zlib --enable-libdav1d \
		--enable-d3d11va --enable-dxva2 \
		--disable-programs --disable-doc --disable-encoders --disable-muxers \
		--disable-debug --disable-autodetect
	run ffmpeg-build make -j "$jobs"
	run ffmpeg-install make install
	stamp ffmpeg
)

# ---- mpv --------------------------------------------------------------------
fetch mpv https://github.com/mpv-player/mpv.git "$MPV_TAG"
say "build libmpv"
# Option names are mpv's own (meson.options): there is no -Dlibass, and the
# OpenGL switch is -Dgl. Anything not named here stays on meson's 'auto', which
# skips what was never built rather than failing the configure.
meson_build mpv "$src/mpv" \
	-Dgpl=false -Dlibmpv=true -Dcplayer=false \
	-Dlua=disabled -Djavascript=disabled -Dlibarchive=disabled \
	-Dd3d11=enabled -Dspirv-cross=enabled -Dshaderc=enabled \
	-Dvulkan=disabled -Dgl=disabled -Dwasapi=enabled -Dzlib=enabled \
	-Ddefault_library=shared

dll="$(find "$src/mpv/build-lancast" -name 'libmpv-2.dll' -o -name 'mpv-2.dll' | head -1)"
[ -n "$dll" ] || { echo "no libmpv-2.dll was produced" >&2; exit 1; }
cp "$dll" "$out/libmpv-2.dll"
"$target-strip" --strip-unneeded "$out/libmpv-2.dll"

# ---- the licence assertions -------------------------------------------------
#
# Checked rather than trusted: a GPL build is a licence violation that looks
# exactly like a working DLL.
say "verify"
grep -q 'gpl = false' "$src/mpv/build-lancast/meson-info/intro-buildoptions.json" 2>/dev/null ||
	python3 - "$src/mpv/build-lancast" <<'PY'
import json, sys, pathlib
opts = json.loads((pathlib.Path(sys.argv[1]) / "meson-info" / "intro-buildoptions.json").read_text())
gpl = next((o["value"] for o in opts if o["name"] == "gpl"), None)
if gpl is not False:
    sys.exit(f"mpv was configured with gpl={gpl!r}; refusing to ship it")
PY
if grep -q 'CONFIG_GPL 1' "$src/ffmpeg/config.h"; then
	echo "FFmpeg was configured with --enable-gpl; refusing to ship it" >&2
	exit 1
fi
for banned in x264 x265 libxavs libdavs2; do
	if "$target-strings" "$out/libmpv-2.dll" 2>/dev/null | grep -qi "$banned"; then
		echo "the DLL names $banned, which is GPL; refusing to ship it" >&2
		exit 1
	fi
done

# The licence text ships beside the DLL, so the copy a user has came with its
# own terms rather than a link to them.
cp "$src/mpv/LICENSE.LGPL" "$here/LICENSE.LGPL"

# Nothing may be needed beside it but Windows' own DLLs. A dependency we ship
# by accident is a file the installer does not place and a client that reports
# "module not found" about the one file that is present.
deps="$("$target-objdump" -p "$out/libmpv-2.dll" | sed -n 's/.*DLL Name: //p' | sort -u)"
strays="$(echo "$deps" | grep -iE '^(lib|.*mingw)' || true)"
if [ -n "$strays" ]; then
	echo "the DLL needs other files beside it:" >&2
	echo "$strays" >&2
	exit 1
fi

sha="$(sha256sum "$out/libmpv-2.dll" | cut -d' ' -f1)"
say "built $out/libmpv-2.dll"
printf 'size   %s bytes\nsha256 %s\n' "$(stat -c%s "$out/libmpv-2.dll")" "$sha"
printf '\nRecord this hash in PROVENANCE.md before shipping it.\n'
