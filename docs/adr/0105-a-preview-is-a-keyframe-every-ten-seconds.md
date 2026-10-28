# ADR 0105 — A preview is a keyframe every ten seconds

Date: 2026-10-27 · Status: **accepted**

## Context

Chapters (#949) put ticks and names on the scrub bar. The other half of that
roadmap entry is a picture: hover or drag along the bar and see the moment you
would land on. Plex and Jellyfin call it trickplay. It needs frames made ahead
of time, because cutting one from a film while the pointer moves is a seek and
a decode per pixel.

Measured on this machine before deciding, with the real ffmpeg:

| Film | Codec | Length | Time (4 threads) | Sheets | Size |
|---|---|---|---|---|---|
| Shrek | HEVC 10-bit 1080p | 90 min | 42 s | 6 | ≈ 2.0 MB |
| A Beautiful Day in the Neighborhood | H.264 1080p | 109 min | 42 s | 7 | 2.3 MB |

`-skip_frame nokey` is what makes it that cheap: only keyframes are decoded,
so the cost is a small fraction of playing the film. The frames came out right
and in order. One run on Shrek exited 0 and wrote nothing, and every run after
it worked, so "ffmpeg succeeded" is not trusted on its own.

## Decision

**A frame every 10 seconds, 240 pixels wide, tiled 10 × 10 to a JPEG sheet.**
One sheet is 1,000 seconds of film, about 330 KB. The client shows a window
onto a sheet with `background-position`, so moving the pointer moves the
window instead of fetching an image.

**Keyframes, not exact frames.** `-skip_frame nokey` then `fps=1/10`: each
frame is the nearest keyframe at or before its moment, and where keyframes
are further apart than 10 seconds a frame repeats. On a scrub bar that is
right to within a few seconds, which is what a scrub bar is for. Decoding
every frame would cost the whole film's decode for a difference nobody sees
at 240 pixels.

**Made in the background, off by default.** Settings › Playback & progress ›
*Show a picture over the scrub bar* (`scrub_previews`). It reads every film and
episode once, newest first, which on a library of ~1,250 films is a night's
work and ~3 GB. Credit detection (ADR 0054) is off by default for the same
kind of cost. Turning it off stops the pass and keeps what was made.

**Kept beside the database, keyed to the file.** `<data>/previews/<item id>/`
holds the sheets and a `manifest.json`. `media_item.preview_key`
(revision 85) records `size:mtime:rev` for the file a pass looked at:
- A replaced file no longer matches, so it is made again.
- A manifest whose key does not match is not served.
- Raising `preview.Rev` remakes everything.
- A failed file still records its key, so it is tried again when it changes,
  not on every pass.
- Previews of deleted items, and anything a pass cut short, are removed at the
  start of each pass.

**When it runs:**
- at startup;
- when the setting is turned on;
- after every probe pass. A film a scan adds becomes eligible once ffprobe has
  measured it, and the scan's own kick would have come too early.

**Gentle, like the marker worker.** One file at a time, ffmpeg capped at a
quarter of the cores, and the setting checked before every file. Nothing
waits on a preview.

**Nothing asks the GPU for anything.** There is no `-hwaccel`, so the run is
the same as a service in session 0 as from a shell. It was still checked as
the installed service before release (CLAUDE.md, the v0.8.0 rule).

**Served like any item route.** `GET /api/items/{id}/previews` describes the
sheets, and `/previews/{n}` is one sheet. Both look the item up through the
usual visibility checks, so a ceiling or a hidden library is a `404`. A sheet
is a number, and the files are under the data directory, so nothing a client
sends becomes a path.

## Consequences

- An HDR film's previews are not tone mapped, so they look flat. That is
  acceptable at 240 pixels for finding a place. A tone-mapped pass is a `Rev`
  bump.
- Not offered to paired servers or guests yet: a friend browsing your library
  sees chapters and no pictures. That can come with federation's next change.
- The per-file cost is a decode of keyframes, so a scan does not get slower.
  The work happens after it, off the request path.
