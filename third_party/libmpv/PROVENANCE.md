# libmpv — LANcast's own LGPL build

The desktop client plays through libmpv ([ADR 0067](../../docs/adr/0067-the-desktop-client-plays-through-libmpv.md)),
and LANcast builds that library itself rather than shipping a published one,
because every published Windows build of mpv is **GPL** and the commercial
licence cannot distribute a GPL library ([ADR 0069](../../docs/adr/0069-lancast-builds-its-own-lgpl-libmpv.md)).

## What ships

| | |
|---|---|
| File | `libmpv-2.dll`, beside `LANcast-Client.exe` |
| Licence | **LGPL-2.1-or-later** (mpv built `-Dgpl=false`, FFmpeg without `--enable-gpl`) |
| mpv | v0.41.0, FFmpeg n9.0.2 (see Builds) |
| SHA-256 | `1466ac41514fa50b0ea7e9be87e104bcb28c2ab0d9f8d26b31e83ddd8e5814a7` |

**The binary is not in this repository.** It is built by [`build.sh`](build.sh)
from the tags pinned in [`versions.env`](versions.env), verified against the
hash recorded here, and attached to the release. A multi-megabyte decoder
arriving in an installer with no account of where it came from is the thing
ADR 0013 refused when it refused unaudited JavaScript.

## What it contains, and what it does not

Built from: zlib, dav1d, SPIRV-Cross, shaderc, libplacebo, FFmpeg and mpv —
every one LGPL, BSD, MIT or Apache-2.0. **No GPL source is fetched at all**, so
a missing configure flag cannot quietly produce a GPL build.

Left out, because the LGPL build excludes them and LANcast does not use them:
the x264 and x265 **encoders**, and GPL-only filters such as `vf_delogo`. The
client decodes and never encodes; the server's conversion pipeline is a
separate ffmpeg process the user installs (ADR 0048) and is unaffected.

**libass and its text-shaping stack are in** — freetype, fribidi, harfbuzz and
libass, all permissive or LGPL. Not by choice: mpv 0.41 has no option to build
without libass. LANcast draws subtitles in the page today
(`web/src/playback/nativeTracks.ts`), so nothing uses it yet; having it is what
styled embedded subtitles will need later.

## The four things the LGPL asks, and where each is met

1. **Source for the library.** Pinned tags in `versions.env`; `build.sh` fetches
   exactly those and applies **no patches**.
2. **The user may replace it.** The client loads `libmpv-2.dll` by full path at
   runtime (`internal/mpv/player_windows.go`) — no link step, no import
   library, no headers compiled in. Dropping in another build replaces it.
3. **No undisclosed modifications.** There are none. If a patch ever becomes
   necessary it lives in this directory and is listed here.
4. **Say what it is.** This file, the entry in `NOTICE`, and the licence text
   shipped beside the DLL.

## Builds

Each row is a DLL that shipped. Verify a file with
`sha256sum libmpv-2.dll` (or `Get-FileHash`) before packaging it.

| date | mpv | FFmpeg | size | SHA-256 |
|---|---|---|---|---|
| 2026-09-19 | v0.41.0 | n9.0.2 | 46,655,395 | `1466ac41514fa50b0ea7e9be87e104bcb28c2ab0d9f8d26b31e83ddd8e5814a7` |

The first build, and 46 MB against the 115 MB of the GPL builds published for
Windows — the difference is the encoders and the scripting this one leaves out.
It depends on nothing but Windows' own DLLs, which the recipe now asserts:
anything else would be a file the installer does not place.

## Rebuilding

```bash
# once, on Ubuntu — the only step needing a password
sudo apt install -y build-essential mingw-w64 meson ninja-build cmake nasm pkg-config python3 git curl

third_party/libmpv/build.sh          # writes out/libmpv-2.dll and prints its hash
```

The script refuses to produce a DLL it cannot vouch for: it re-reads mpv's
configured `gpl` option, checks FFmpeg's `CONFIG_GPL`, and greps the built
binary for the names of GPL encoders. A GPL build is a licence violation that
otherwise looks exactly like a working file.
