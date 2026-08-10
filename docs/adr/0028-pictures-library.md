# ADR 0028 — Pictures library

**Status:** accepted
**Date:** 2026-08-09

## Context

[ADR 0024](0024-music-libraries.md) added music and deliberately deferred
photos, leaving "Photo library with a built-in image viewer" in the roadmap's
backlog with a note that it needs its own decision. This is that decision.

Music was the first test of [ADR 0002](0002-one-wide-media-item-table.md)'s
claim that a new media type needs no new tables. It held. Pictures are the
second test, and they push differently: a photo is not a thing you *play*, it
has no duration, no playback state worth keeping, and — this is the part that
actually matters — **the file is its own artwork**. Every other media type in
LANcast points at an image that represents it. A photo *is* the image.

The reference library on hand is instructive and not what a specification would
assume: 46 files in three flat folders (`AI Art`, `Copilot Images`,
`Wallpaper`), jpg/png/webp, 214 MB, averaging 4.5 MB each. The filenames are
UUIDs and `openart-f81b7650ced542cdb5b37d8916f0bc92_raw.jpg`. **Nothing is
recoverable from the filename**, and AI art and wallpapers carry no EXIF. That
is the opposite of the video case, where the filename is the primary guess, and
of the music case, where the tags are authoritative.

## Decision

### Kind, and no new tables

A `picture` library holds two new `media_item` kinds: **`gallery`** (a folder)
and **`photo`** (a file), related by `parent_id` — the same shape as
artist → album → track and show → season → episode.

A folder becomes a gallery because in this library the folder is the *only*
grouping that carries meaning. The filename says nothing and there is no
provider to ask. Deliberately not `album`: that kind is music's, and two media
types sharing one kind string would collide in every helper that switches on
kind — square artwork, child-count nouns, the lot.

ADR 0002's claim survives a second media type. No new tables.

### The photo is its own artwork

Derivatives go through `internal/artwork`'s existing content-addressed cache,
keyed by the hash of the file. The existing widths already fit — `thumb` (185)
for grids, `poster` and `poster2x` for larger tiles, `fanart` (1280) for the
banner — because they resize by width and preserve aspect. Full screen serves
the original file.

Generation runs in **its own worker**, not inside the scan, for the reason
probing runs in its own worker: a scan that resizes 50,000 images inline is a
scan that appears to hang. It reports into the activity panel like every other
worker, and because the cache is content-addressed, a rescan re-does nothing.

### Decoding, and the format that will otherwise bite

> **Amendment, 2026-08-09 (Phase 2).** The rule below named an extension list —
> HEIC and HEIF go to ffmpeg, everything else does not. The first run against a
> real library disproved it: eight of the reference photographs are valid BMPs
> that Go's decoder does not read and ffmpeg decodes without complaint. They
> were reported as failures because `.bmp` was not on a list. The rule is now
> **whatever the in-process decoders refuse is offered to ffmpeg** — no list to
> go stale, and it covers HEIC anyway. With no ffmpeg available the two outcomes
> are still told apart: `image.ErrFormat` means this build cannot read the
> format, any other decode error means the file is broken.

Go's standard library covers jpeg, png and gif; `golang.org/x/image` — already a
dependency — covers webp, bmp and tiff. **HEIC/HEIF goes through ffmpeg**, which
is already a hard requirement for probing and transcoding. This matters more
than it looks: a phone backup is mostly HEIC, and a picture library that shows
a wall of placeholders for the user's actual photos would be a feature that
looks finished and is useless.

A file nothing can decode is still scanned and listed, with a **recorded scan
issue** naming why. Hiding it would be the silent-failure shape this project
keeps finding: the file is on disk, the user can see it in Explorer, and a
library that omits it without explanation is a library that looks wrong.

### EXIF: two fields, and deliberately not a third

**Orientation** is applied when derivatives are generated. A phone photo shown
unrotated is visibly wrong and reads as our bug, not the camera's.

**Date taken** is stored, falling back to file mtime when absent — which is the
entire reference library. It is what makes "recently added" mean the picture
rather than the copy, and it is the natural sort for a photo library.

**GPS is not read.** It is location data about the user, LANcast has no use for
it here, and the safest way to never leak it is to never load it. Reading it
later is a decision with its own consequences; not reading it costs nothing now.

Schema revision 16 adds `width`, `height` and `taken_at` as **nullable
columns** — additive, no reshaping of the model.

### Serving the original

A new endpoint serves the photo file itself. It **re-verifies containment**
within the owning library root after `filepath.Abs`, per the standing rule: the
database is trusted, and this is the boundary where a bad row becomes arbitrary
file read.

### The screens

**Home** gets a Recently Added Photos row *and* a per-library shelf, consistent
with every other library. **Photos never appear in the hero** — the hero is for
something you watch, and a photo has no backdrop to fill it.

**The picture library** opens on a banner cycling randomly through the library.
Selecting a photo replaces the banner with it; a control at its lower right
expands to full screen, where arrow keys move through the gallery, escape
leaves, and zoom, pan and a slideshow are available.

The banner auto-advances, which is the thing this project turned down for the
Home hero last night. That is not a contradiction: the Home hero is a *decision
surface* — resume this, or not — and a moving target makes a decision harder. A
picture library's banner is the content itself, and cycling through it is the
point. It pauses on hover and focus, and `prefers-reduced-motion` stops it
entirely.

## Consequences

**The gold rule is untouched.** Nothing here needs an accent; photographs bring
their own colour, and the focus ring stays the only thing gold means.

**A video dropped in a picture library is already counted.** The kind-mismatch
counter added for music counts media of the other sort, and video in a picture
library qualifies without further work.

**Thumbnail generation is the one real cost.** 46 files is nothing; 50,000 is a
long first run. It is incremental, resumable, and visible in the activity panel,
which is the difference between slow and broken.

**Rejected: a masonry grid.** Uniform tiles first. Masonry needs dimensions
before layout and a second implementation for the TV client, and it is a
refinement of a screen that does not exist yet. The dimensions are stored, so
it stays available.

**Rejected: any editing.** No rotate, crop, delete-from-viewer or export.
LANcast reads libraries; a picture viewer that writes to the user's photos is a
different product with a different risk profile.

**Rejected: flat browse in v1.** Galleries only. An "All photos" view is a small
addition once the grid exists, and building both now means keeping two browse
surfaces consistent while neither has been used.
