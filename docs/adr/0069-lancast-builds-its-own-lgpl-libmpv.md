# ADR 0069 — LANcast builds its own LGPL libmpv

**Status:** Accepted
**Date:** 2026-09-19

Amends the licensing section of
[ADR 0067](0067-the-desktop-client-plays-through-libmpv.md), which left this
open and gated shipping on it.

**This is not legal advice**, the same caveat [ADR 0053](0053-the-licence-lancast-ships-under.md)
carries. It is the engineering shape of the decision, with the facts checked.

## Context

The desktop client plays through libmpv (ADR 0067). LANcast ships under
**AGPL-3.0 with a commercial licence available** (ADR 0053), and those two
licences do not agree about what mpv may be:

- **mpv's core is LGPLv2.1+.** It becomes GPL only through components enabled
  at build time — and every published Windows build, including the ones on this
  machine from Plex and Plezy, is a **GPL** build.
- GPL and AGPL are compatible, so a GPL libmpv beside the AGPL distribution is
  fine. A **proprietary build may not distribute a GPL library with itself**,
  which is the commercial licence, and that is the whole problem.

The alternative considered seriously was **not shipping it at all** and
fetching it on demand, the way ADR 0048 fetches ffmpeg rather than bundling it.
That is legally clean and nearly free to build, and it was rejected for one
reason: it turns the desktop player into something a person has to go and
enable, on every machine, for the feature that is supposed to be the reason the
desktop client exists. ffmpeg is a *server* tool an administrator installs once;
this is the thing playback depends on for every viewer at the window.

## Decision

**LANcast builds libmpv itself, LGPL, and ships it in the installer.**

`-Dgpl=false` for mpv and an FFmpeg configured without `--enable-gpl` produce a
library under LGPLv2.1+. What that excludes is GPL-only **encoders and
filters** — x264, x265, `vf_delogo` and the like. LANcast only ever decodes, so
the LGPL build loses nothing this project uses. That is what makes this
decision cheap in capability and expensive only in build infrastructure.

### What the LGPL asks of us, and how each is met

| Obligation | How |
|---|---|
| Distribute the library's own source and licence | The exact source revisions are pinned in the build recipe, the `LICENSE` files ship beside the DLL, and the recipe itself is in this repository |
| Let the user replace the library | The client **loads `libmpv-2.dll` by full path at runtime**. There is no link step, no import library, no headers compiled in — replacing the file replaces the library |
| Do not modify it, or publish the modifications | The recipe applies **no patches**. If it ever does, the patch lives in this repository beside it |
| Say what it is | The About pane and `third_party/libmpv/PROVENANCE.md` name the version, the licence and the build |

The runtime loading is not a licence trick built for this ADR; it is how the
player was written (`internal/mpv`, ADR 0067), because a missing DLL had to
leave the browser player working. It happens to be the arrangement the LGPL
asks for.

### Provenance

The same discipline the vendored WebView2 loader and hls.js already follow:
**a pinned version, a recorded SHA-256, and a reproducible recipe**. A binary
this size — tens of megabytes of decoder — arriving in the installer with no
account of where it came from would be the thing ADR 0013 refused when it
refused 300 KB of unaudited JavaScript.

The DLL is **not committed to this repository**. It is built by the recipe,
checked against the recorded hash, and attached to the release; `third_party/libmpv/`
holds the recipe, the hash and the provenance note, not the binary.

### Shipping

- The **installer** places it beside `LANcast-Client.exe`.
- `installFiles` in `internal/update/download.go` carries it, so the in-app
  updater replaces it with the rest (the updater already covers the client and
  the WebView2 loader).
- A machine without it keeps the browser player, unchanged. That is the
  fallback on any build where the file is missing, and it is what the client
  does today.

## Consequences

- A Windows cross-build of mpv and FFmpeg becomes something this project
  maintains: pinned, rebuilt when a security fix lands, and hashed.
- The commercial licence and the AGPL distribution ship the **same** artefact,
  which is the point — one player, one build, both licences satisfied.
- Updating libmpv is now a release decision rather than a user's problem.

## Rejected

- **Fetch it on demand (ADR 0048's pattern).** Clean and cheap; makes the
  desktop player opt-in per machine. Kept as the fallback if maintaining the
  build ever stops being worth it.
- **GPL libmpv in the AGPL build only.** Splits the product in two, and the
  half without a native player is the half somebody paid for.
- **libVLC, or FFmpeg's libraries with our own output.** libVLC's plugin
  licensing is its own mixed question; raw FFmpeg means writing the video
  output, the HDR tone mapping and the timing that mpv already has, all of
  which ADR 0067 chose mpv to avoid.
