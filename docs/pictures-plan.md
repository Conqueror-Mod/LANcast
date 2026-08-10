# Pictures library — build plan

Decisions are in [ADR 0028](adr/0028-pictures-library.md). This is the order to
build them in, and what "done" means for each phase.

Five phases. Each is independently reviewable and leaves the app working — a
half-built picture library must never break the three libraries that already
work.

## Phase 1 — the server knows what a picture is

`internal/media`
- `LibraryPicture = "picture"`, and `KindGallery = "gallery"` / `KindPhoto = "photo"`.
- `IsImage(path)`: jpg, jpeg, png, webp, gif, bmp, tiff, tif, heic, heif.
- `IsScannable` grows a third branch. It currently reads "music takes audio,
  everything else takes video" — that shape does not extend, so it becomes an
  explicit switch on library kind. **One normalizer**: the extension sets stay in
  `parse.go` beside the existing two.

`internal/store`
- Migration to revision **16**: `width`, `height`, `taken_at`, all nullable.

`internal/scan`
- A picture library reconciles folder → `gallery`, file → `photo`, parented by
  `parent_id`. The existing show/season and artist/album grouping is the model;
  this is the simplest of the three because the folder is taken literally.
- Title from filename, unchanged from disk. No guessing, no cleanup, no year
  extraction — `media.Clean` exists for filenames that mean something, and these
  do not. A UUID is a worse title than a UUID that has been "tidied".

**Done when:** a scan of the test library produces 3 galleries and 46 photos,
the `.7z` is ignored, and a rescan changes nothing. Tests mirror
`scanner_test.go`'s existing per-case style.

## Phase 2 — thumbnails

`internal/photo` (new)
- Decode: stdlib jpeg/png/gif, `golang.org/x/image` webp/bmp/tiff, **ffmpeg for
  heic/heif**. The ffmpeg path reuses `internal/coverart`'s extractor pattern —
  process execution stays split from decoding so the format table is testable
  against fixtures with no ffmpeg installed.
- EXIF orientation and date-taken. Orientation is applied to derivatives;
  date-taken is stored, falling back to file mtime.
- Dimensions recorded while decoding, since the image is already open.

Worker
- Modelled on the coverart worker: its own goroutine, its own context, batched,
  cancellation between items, reported into `/api/activity`.
- Content-addressed, so a rescan re-does nothing and a partial run resumes.

**Done when:** the test library generates 46 thumbnails, a second run generates
none, and a file with a corrupt header records a scan issue rather than
vanishing or aborting the run.

## Phase 3 — the API

- `GET /api/items/{id}/photo` serves the original file. **Containment
  re-verified after `filepath.Abs`** against the owning library root, immutable
  cache headers, correct content-type from the real format rather than the
  extension.
- Existing item endpoints carry `width`, `height`, `taken_at` where present.
- `sort=taken` on `/api/items`.
- **`docs/api.md` in the same commit.** Additive throughout (ADR 0018).

**Done when:** the endpoint serves a photo, refuses a row whose path escapes its
library root, and the refusal has a test.

## Phase 4 — browse

- `libraryConfig` gains a picture config: sorts by date taken, date added, title.
- Picture library view: gallery tiles, each wearing one of its photos.
- Gallery view: uniform photo grid, reusing `PosterTile`'s focus and elevation
  behaviour rather than growing a second tile component.
- Home: a **Recently Added Photos** row and a per-library shelf. Photos are
  excluded from the hero pick — the rule is already written as "the hero is for
  something you watch", and this is the second media type to test it.

**Done when:** all three libraries plus pictures browse correctly, and the Home
rows appear in the right order with nothing duplicated between them.

## Phase 5 — the banner and the viewer

The screen from the brief, and the largest single piece.

**Banner.** Top of the picture library, cycling randomly through the library.
Pauses on hover and focus; `prefers-reduced-motion` stops it entirely. Selecting
a photo replaces the banner with that photo. A control at the lower right
expands.

**Viewer.** Full screen, over everything:
- arrow keys and on-screen chevrons move through the gallery
- escape leaves, returning focus to the tile that opened it (ADR 0004 — focus is
  never invisible, and a viewer that dumps focus on `body` breaks the keyboard
  model for the whole page)
- zoom by scroll and pinch, pan by drag, double-click or `0` resets
- slideshow with play/pause, and a reduced-motion path that does not auto-start
- registers with the focus controller rather than growing its own key handling

**Done when:** it survives a real session against the test library, keyboard-only
navigation works end to end, and the viewer never leaves focus nowhere.

## Verification

The unit that catches this class is the installed artifact on a real desktop —
the lesson from v0.4.1 that the roadmap already carries. At minimum, before
calling it done:

- a scan of the real test picture library, timed
- one HEIC file, because it is the format most likely to be broken and the one
  a real user has most of
- one deliberately corrupt image, to confirm it is reported rather than silent
- the viewer driven by keyboard alone
- screenshots at 1580×1000 and 1920×1080, as with the home page

## Deliberately not in this plan

Masonry layout, an all-photos view, GPS, any editing, sharing or export, and
face or object detection. The first four have reasoning in ADR 0028; the last is
a different product.
