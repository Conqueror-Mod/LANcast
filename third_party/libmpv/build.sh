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

cmake_build() { # dir [extra cmake args…]
	local dir=$1
	shift
	cmake -S "$dir" -B "$dir/build-lancast" -G Ninja \
		-DCMAKE_TOOLCHAIN_FILE="$work/toolchain.cmake" \
		-DCMAKE_INSTALL_PREFIX="$prefix" \
		-DCMAKE_BUILD_TYPE=Release \
		-DBUILD_SHARED_LIBS=OFF \
		"$@" >/dev/null
	cmake --build "$dir/build-lancast" -j "$jobs" >/dev/null
	cmake --install "$dir/build-lancast" >/dev/null
}

meson_build() { # dir [extra meson args…]
	local dir=$1
	shift
	meson setup "$dir/build-lancast" "$dir" --cross-file "$work/cross.ini" \
		--wipe >/dev/null 2>&1 ||
		meson setup "$dir/build-lancast" "$dir" --cross-file "$work/cross.ini" "$@" >/dev/null
	meson configure "$dir/build-lancast" "$@" >/dev/null
	ninja -C "$dir/build-lancast" -j "$jobs" >/dev/null
	ninja -C "$dir/build-lancast" install >/dev/null
}

# ---- zlib ------------------------------------------------------------------
fetch zlib https://github.com/madler/zlib.git "$ZLIB_TAG"
say "build zlib"
cmake_build "$src/zlib" -DZLIB_BUILD_EXAMPLES=OFF

# ---- dav1d -----------------------------------------------------------------
fetch dav1d https://code.videolan.org/videolan/dav1d.git "$DAV1D_TAG"
say "build dav1d"
meson_build "$src/dav1d" -Denable_tools=false -Denable_tests=false

# ---- SPIRV-Cross -----------------------------------------------------------
fetch spirv-cross https://github.com/KhronosGroup/SPIRV-Cross.git "$SPIRV_CROSS_TAG"
say "build SPIRV-Cross"
cmake_build "$src/spirv-cross" \
	-DSPIRV_CROSS_SHARED=OFF -DSPIRV_CROSS_CLI=OFF \
	-DSPIRV_CROSS_ENABLE_TESTS=OFF -DSPIRV_CROSS_ENABLE_C_API=ON

# ---- shaderc (with its own pinned dependencies) -----------------------------
fetch shaderc https://github.com/google/shaderc.git "$SHADERC_TAG"
say "build shaderc"
( cd "$src/shaderc" && python3 ./utils/git-sync-deps >/dev/null )
cmake_build "$src/shaderc" \
	-DSHADERC_SKIP_TESTS=ON -DSHADERC_SKIP_EXAMPLES=ON \
	-DSHADERC_SKIP_COPYRIGHT_CHECK=ON -DSHADERC_ENABLE_SHARED_CRT=OFF \
	-DENABLE_GLSLANG_BINARIES=OFF -DSPIRV_SKIP_EXECUTABLES=ON

# ---- libplacebo ------------------------------------------------------------
fetch libplacebo https://code.videolan.org/videolan/libplacebo.git "$LIBPLACEBO_TAG"
say "build libplacebo"
meson_build "$src/libplacebo" \
	-Dvulkan=disabled -Dopengl=disabled -Dd3d11=enabled \
	-Dshaderc=enabled -Ddemos=false -Dtests=false

# ---- FFmpeg (LGPL: no --enable-gpl, no GPL externals) ----------------------
fetch ffmpeg https://github.com/FFmpeg/FFmpeg.git "$FFMPEG_TAG"
say "build FFmpeg"
(
	cd "$src/ffmpeg"
	./configure \
		--prefix="$prefix" \
		--arch=x86_64 --target-os=mingw32 --enable-cross-compile \
		--cross-prefix="$target-" --pkg-config=pkg-config --pkg-config-flags=--static \
		--enable-runtime-cpudetect \
		--enable-zlib --enable-libdav1d \
		--enable-d3d11va --enable-dxva2 \
		--disable-programs --disable-doc --disable-encoders --disable-muxers \
		--disable-debug --disable-autodetect >/dev/null
	make -j "$jobs" >/dev/null
	make install >/dev/null
)

# ---- mpv --------------------------------------------------------------------
fetch mpv https://github.com/mpv-player/mpv.git "$MPV_TAG"
say "build libmpv"
meson_build "$src/mpv" \
	-Dgpl=false -Dlibmpv=true -Dcplayer=false \
	-Dlibass=disabled -Dlua=disabled -Djavascript=disabled \
	-Dd3d11=enabled -Dspirv-cross=enabled -Dshaderc=enabled \
	-Dvulkan=disabled -Dopengl=disabled -Dwasapi=enabled \
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

sha="$(sha256sum "$out/libmpv-2.dll" | cut -d' ' -f1)"
say "built $out/libmpv-2.dll"
printf 'size   %s bytes\nsha256 %s\n' "$(stat -c%s "$out/libmpv-2.dll")" "$sha"
printf '\nRecord this hash in PROVENANCE.md before shipping it.\n'
