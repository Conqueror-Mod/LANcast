# ADR 0101 — A comic is read out of its archive

Date: 2026-10-26 · Status: **accepted**

## Context

The roadmap asked for a comics library and a reader: the Kodi-shaped
"everything is a library" principle applied to the one common kind of media
LANcast could not show at all. Comic books are kept as archives of page images,
almost always `.cbz` (a zip) and sometimes `.cbr` (RAR) or `.cb7` (7-Zip).

## Decision

**`.cbz` only.** A zip is in the standard library; RAR and 7-Zip are not, and
neither is worth a dependency before somebody has a library of them. A `.cbr`
in a comics library is not indexed.

**A library kind of its own, `comics`, holding `comic` items.** Spelled
differently, as `picture` and `photo` are, so a helper switching on one cannot
match the other. Its files take none of the video passes: "Saga 012" is an
issue, not a season and an episode. The title and year come from the filename
— the bracketed tags at the end are release notes, the four-digit one is the
year — and the series is the folder the file sits in, when that is not the
library's root, because comics are kept a folder per series. No metadata
provider knows comics, so none is asked, and ffprobe is never handed a zip.

**Pages are read out of the archive when asked for, never unpacked.** The pages
are the archive's images in natural name order (`p2` before `p10`), folder by
folder; `ComicInfo.xml`, `__MACOSX/` and hidden files are not pages. A page is
addressed by its number in that order, so nothing a client sends becomes a path
inside the zip, and the archive itself is held to its library's root like every
other file a row names (ADR 0094). A page over 64 MB, or an archive claiming
more than 5,000 pages, is refused rather than read into memory.

**The cover is the first page**, made into the item's poster by a small worker
after each scan, the way a photo's thumbnail is, so every grid tile and detail
page shows it with no new code. An archive that cannot be read is tried once.

**A place is not progress.** Where somebody is in a comic is a page, in its own
table (`comic_place`, migration 83), not a position in milliseconds in
`playback_state`. A comic there would have joined Continue Watching with a bar
drawn from a duration it does not have. Reaching the last page marks it
`watched`, so the tick, Mark as unwatched and the watched filter work on
comics as on films.

**The reader is LANcast's own.** A page at a time, fitted to the screen; the
arrow keys, a tap on either side, or a swipe turn it; the place is kept as it
goes. Nothing about a comic goes to the desktop's mpv.

## Consequences

- **An account with a content limit sees no comics** until an administrator
  rates one by hand on its page, the control every unrated title already has.
  Nothing rates them otherwise, and an unrated title is kept from a limited
  account, as an unrated film is (ADR 0071). `ComicInfo.xml` carries an
  `AgeRating` that could fill this in; reading it is a later decision.
- A comics library is not shared with paired servers and is not offered over
  DLNA: a friend's server and a television have no reader for it.
- No two-page spreads, right-to-left reading or `ComicInfo.xml` metadata yet.
  Each is additive.
