# ADR 0087 — An optimized copy is converted once

Date: 2026-10-13 · Status: **accepted** 2026-10-13

The plan is [settings-plan.md](../settings-plan.md), "Optimized versions,
briefly". Chris decided on 2026-10-13:
- **One preset:** 1080p H.264 at about 8 Mbps.
- **Chosen per title**, from its menu.
- **Stored in a folder the administrator chooses.**
- **Under a size limit**, at which new copies **wait rather than delete
  anything**.

## Context

A film a device cannot play as it is gets converted live, every time it is
watched, on every device that needs it. On a phone or a browser that is most
4K HDR, most HEVC, and most surround audio. Live conversion costs CPU or GPU
for the whole film, and a 4K HDR title on Aither converts at 0.41× real time
with CPU tone mapping, which no amount of buffering
fixes.

An optimized copy is that conversion done once, ahead of time, and kept.
Afterwards, a device that would have needed converting plays the copy
directly.

It touches four existing rules, which is why it needs an ADR:
- the playback decision;
- scans (a copy must never be taken for a library file);
- deletion;
- verifying ffmpeg work as the installed service (CLAUDE.md).

## Decision

1. **The copy is the decision a universal device would get.**
   - The worker runs the existing `probe.DecideTrack` against a fixed profile:
     H.264 and AAC in MP4, at most 1080p, at most 8 Mbit/s, stereo.
   - It converts with the same pipeline and the same encoder the live path
     proved at startup (`transcode.Args`, a third output, `File`).
   - HDR is tone-mapped exactly as live playback does it.
   - It is written to a temporary name and renamed when complete, so a
     half-written copy is never served.

   No second set of conversion rules exists to disagree with the first.
2. **Chosen per title** (Chris's decision). "Make optimized version" is in
   More Options. On a show, a season or a collection it queues every episode
   or film inside it, in watching order. Nothing is converted that nobody
   asked for.
3. **One at a time, in the background.**
   - A single worker converts the queue in order.
   - It appears on the Scheduled tasks board as "Optimized versions", and its
     progress in Status › Conversions.
   - It yields to playback: while any live conversion is running, it waits,
     because a person watching now outranks a copy for later.
4. **Stored outside every library.**
   - Copies live in `optimized_dir`, which defaults to `optimized` in the data
     folder. A folder inside any library location is refused, so a scan can
     never pick a copy up as media.
   - Each copy is named by its item id, `<id>.mp4`.
   - Every path is re-verified to sit inside the folder before it is opened,
     as for every file route (CLAUDE.md).
5. **Under a size limit, waiting rather than deleting** (Chris's decision).
   `optimized_limit_gb`, default 200.
   - Before a conversion starts, its size is estimated from the film's length
     at the preset's bit rate.
   - If the estimate would pass the limit, the request stays queued as
     "waiting for space" and says so.
   - Nothing is ever deleted to make room. Removing copies is a person's
     decision.
6. **Playback prefers the copy only when it helps.** The playback answer
   changes only when both are true:
   - the device's own decision for the original is to convert;
   - a finished copy is something that device can play directly, judged by
     the same rules against the copy's own probe, which is stored when the
     copy is finished.

   Then the answer is direct play of the copy (`version: "optimized"`), and
   the stream route serves it for `?version=optimized`. A device that plays
   the original directly keeps playing the original, and so does a request
   for a particular audio track: the copy has one audio track.
7. **Deleting the original deletes the copy.** The row cascades from
   `media_item`, and a sweep removes any copy file with no row. Removing a
   title from the library says, in its dialog, that its optimized copy goes
   too.
8. **Verified as the service.** This is ffmpeg work, so it is proven by
   swapping the build into the installed service and converting a real HEVC
   or HDR title, then reading `lancastd.log`. A shell is not enough
   (CLAUDE.md).

## Consequences

- **Schema 76** adds `optimized_version(item_id, state, size_bytes,
  probe_json, error, requested_at, finished_at)`. It is a new table, which is
  why this ADR exists.
- **Settings:** `optimized_dir` and `optimized_limit_gb`, in Server ›
  Transcoding.
- **Routes:**
  - `GET /api/optimized`: the queue, sizes and the limit;
  - `PUT` and `DELETE /api/items/{id}/optimized`: request, or remove the copy
    (a container acts on every title inside it);
  - `?version=optimized` on `GET /api/stream/{id}`.
- **The client** adds the version to the stream URL when the playback answer
  says so, and offers the menu entries. The desktop app's own player plays
  everything directly and never asks for a copy.

## Alternatives rejected

- **Several presets.** One preset keeps the decision and the disk use
  predictable. A second can be added as a profile later without changing the
  shape.
- **A per-library "optimize everything" rule.** It could queue days of
  conversion behind one click. Chris chose per title.
- **Making room by deleting the least-watched copies.** A copy someone asked
  for would disappear behind their back. Chris chose waiting.
- **Storing copies beside the media.** Writing into media folders is what NFO
  writing asks permission for, and a scan would find them.
