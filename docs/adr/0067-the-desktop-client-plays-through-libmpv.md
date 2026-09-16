# ADR 0067 — The desktop client plays through libmpv

**Status:** Proposed
**Date:** 2026-09-16

## Context

LANcast converts far more than it should. On one evening, three ordinary films
failed or stalled for reasons that all trace back to one cause:

| film | file | what the server had to do |
|---|---|---|
| Dogma (1999) | HEVC 4K + TrueHD 7.1, MKV | full video **and** audio encode |
| TMNT III (1993) | H.264 + AC-3 5.1, MKV | remux; 41 s before playback |
| Jay and Silent Bob Reboot (2019) | H.264 + DTS 5.1, MKV | remux **plus audio encode**; hit the 60 s cap and never played |

None of those files is unusual. All three would play instantly in Plex's
desktop app — and the reason is on this machine's own disk:

```
C:\Program Files\Plex\Plex\libmpv-2.dll
C:\Users\Chris\AppData\Local\Programs\Plezy\libmpv-2.dll
```

Plex's desktop client does not use a browser for video. It embeds **libmpv**,
which demuxes Matroska and decodes anything FFmpeg can. LANcast's window is
WebView2 — Chromium — and Chromium's `<video>` element cannot demux Matroska or
decode AC-3, DTS or TrueHD. Every one of those limits becomes a conversion on the
server.

**A codec pack does not help.** CCCP and its kind install DirectShow and Media
Foundation filters for native Windows players. Chromium does not consult system
codecs for `<video>`; it ships its own decoders and ignores what is installed.

## Decision

The **desktop client** plays through libmpv. Every other client — phones,
browser tabs, the television — keeps the HTML5 `<video>` element and the server's
conversion pipeline, unchanged.

This is a desktop-client promise, and the ADR says so up front because it is
the easiest thing to over-read: "direct play everything" means *in the LANcast
window*. The transcode pipeline is not retired. It stops being the desktop's
problem.

### The seam already exists

`videoRef` — the browser's media element — is referenced in **exactly one file**,
`PlaybackProvider.tsx`. Watch Together, intro and credit markers,
picture-in-picture and audio-device selection all reach playback through the
provider's context rather than the element.

So the provider talks to a **player backend** interface instead of a `<video>`
tag, with two implementations:

- **`html5`** — today's element, for every client that is a browser
- **`mpv`** — libmpv driven through window bindings, in the desktop client only

The React UI stays. The library, the detail pages, the player chrome, the queue,
Watch Together — all of it keeps working, because none of it knew there was a
`<video>` element to begin with.

### The server path mostly already exists

mpv reads over HTTP. The direct-play endpoint already serves raw bytes through
`http.ServeContent`, which handles `Range` — so mpv can open, stream and seek an
MKV with **no server change on the core path**. The desktop client stops asking
for conversions and asks for the file.

## What this fixes

- **Matroska** plays as-is. No remux, no wait.
- **AC-3, E-AC-3, DTS and TrueHD** decode locally, or pass through to a receiver.
  The whole class of audio conversions disappears for the desktop.
- **HEVC and AV1** decode on the GPU.
- **4K HDR** tone-maps on the GPU through libplacebo. The measured stutter on
  4K HDR was the *CPU* tone map running at 0.41× realtime; this removes it for
  the desktop rather than tuning it.
- **Subtitles** render natively — ASS styling and image-based PGS/VobSub — rather
  than being converted to WebVTT, which loses both.
- **The remux wait** stops mattering for the desktop, which makes the growing-
  playlist class of fault (Scream, Jay and Silent Bob) moot there.
- **Session reaping on a long pause** stops mattering too: there is no session.
  mpv holds an HTTP connection it can reopen at its own position.

## The decision that has to be made first

**How mpv's video appears under the React player chrome.** Everything else in
this ADR is known engineering; this is the one real unknown, and it is spiked
before anything else is built.

### Option A — mpv beneath a transparent WebView2 *(preferred)*

mpv renders into a native child window. The WebView2 above it has a transparent
background, so the player page is see-through where the picture is and draws its
own controls on top.

Keeps the React player chrome exactly as it is: subtitles settings, the queue,
Watch Together, the skip-intro button, all unchanged.

**Risk:** WebView2 transparency composited over a sibling native window is
supported but finicky — focus, DPI scaling, resizing and fullscreen transitions
all have to be right. This is what the spike proves or disproves.

### Option B — mpv owns the window during playback *(fallback)*

Playback switches to a native mpv window with mpv's own on-screen controls; the
React UI is for browsing.

Guaranteed to work and is what Plex's own player does. **Costs the React player
chrome**: every player feature LANcast has built — Watch Together sync, the
skip-intro button, subtitle styling, the queue panel — would need re-implementing
against mpv's OSC or a native overlay. That is most of the value of the player
work to date.

**Recommendation:** spike A. Fall back to B only if A cannot be made reliable.

## Constraints

### Licensing — must be decided before shipping, not after

LANcast is **AGPL-3.0 with a commercial licence available** (ADR 0053).

Official Windows builds of mpv are **GPL**, because they bundle GPL components.
GPLv3 and AGPLv3 are compatible, so shipping GPL libmpv beside an AGPL LANcast is
fine *for the AGPL distribution*. It is **not** fine for the commercial licence:
a proprietary build cannot link a GPL library.

libmpv can be built **LGPL** (`-Dgpl=false`), which a commercial build may link
dynamically. So either LANcast ships an LGPL libmpv it builds itself, or the
commercial licence has to exclude the desktop player. That is a decision for the
maintainer, and it gates distribution rather than development.

### No phone-home

mpv will happily run `yt-dlp`, load user scripts and read config from the user's
profile. Each of those is a network call or arbitrary code LANcast did not
choose. The embedded instance is started with, at minimum:

```
config=no  load-scripts=no  ytdl=no  input-default-bindings=no
```

and opens only URLs LANcast built — the same rule ADR 0066 applies to game
launches: the page names an item, never a URL.

### Session 0 does not apply

mpv runs in the **client**, in the user's session, where Direct3D exists. The
session-0 rule that governs anything touching ffmpeg on the server does not
reach it. GPU decoding and GPU tone mapping work as they do in any desktop
player.

### Distribution

`libmpv-2.dll` is large — tens of megabytes with FFmpeg linked in. It ships in
the installer, and it has to be added to `installFiles` for the in-app updater
to carry it. Its version is pinned, and its hash recorded, the same provenance
discipline the vendored WebView2 loader and hls.js already follow.

## Plan

**Phase 0 — spike the rendering question.** A throwaway window: WebView2 with a
transparent page over an mpv child window, playing one MKV from the direct-play
endpoint. Pass criteria: the picture shows through, controls draw on top, resize
and fullscreen are clean, focus returns to the page. Outcome decides Option A or B.

*In parallel, and independent of the decision:* ask WebView2 what it can already
decode (`canPlayType` for HEVC, AC-3, E-AC-3). Whatever it reports narrows the
conversions *browser* clients need — which this ADR does not otherwise touch.

**Phase 1 — the backend seam.** Extract a player-backend interface from
PlaybackProvider with no behaviour change. `html5` is today's code moved behind
it. The full client suite must pass unchanged; that is the proof the seam is
clean.

**Phase 2 — the mpv backend.** libmpv loaded by the desktop client, bindings for
load, play, pause, seek, track selection and volume, and mpv's property-change
events flowing back as the same events the provider already consumes.

**Phase 3 — parity.** Progress saving, resume, markers, subtitles and audio
tracks, Watch Together sync, and the display a game-style window move already
solved. Each is checked against the `html5` backend's behaviour rather than
re-specified.

**Phase 4 — the server stops converting for the desktop.** The desktop client
declares that it plays anything, so the decision path returns direct play and
the conversion pipeline is left to the clients that need it.

**Phase 5 — ship.** Licensing settled, the DLL in the installer and in
`installFiles`, provenance recorded.

## What would make this the wrong decision

- **The spike fails for Option A and Option B's cost is judged too high.** Then
  the right move is the cheaper half on its own: raise the remux cap, fix audio
  conversion speed, and use whatever WebView2 turns out to decode natively.
- **The commercial licence matters more than desktop direct play**, and an LGPL
  libmpv cannot be built or maintained.
- **Watch Together sync cannot reach parity** through mpv's position events
  closely enough for a shared room to stay in step.

## Rejected

**A codec pack.** Does not reach Chromium. See Context.

**Replacing WebView2 wholesale with a native UI.** Discards the entire React
client for a problem that lives only in the `<video>` element.

**Vendoring a second browser player library on the file path.** CLAUDE.md
refuses it, and it would not help anyway: a JavaScript player still ends at a
Chromium decoder that cannot decode DTS.
