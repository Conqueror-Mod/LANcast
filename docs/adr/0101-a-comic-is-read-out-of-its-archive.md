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

## Amendment, 2026-10-28: `.cb7`

A comics library now reads **`.cb7`**, a 7-Zip archive of page images. Its pages are found, filtered and ordered by the same rules as a `.cbz`'s (`isPageName`, `naturalLess`), and the reader is `github.com/bodgit/sevenzip`, already in the build for the retro cores, so it adds no dependency. `.cbr` stays unread: RAR has no reader here.

7-Zip archives are usually **solid**: every file is compressed as one stream, so a page can only be reached by decompressing everything stored before it. Opened afresh per request, as a `.cbz` is, a real 44-page, 48 MB issue repacked solid served page 0 in 71 ms, page 22 in 2.2 s and page 43 in 4.4 s. So a `.cb7` is **kept open between requests**:

- the two most recently read stay open, each closed after five idle minutes, or at once when its file changes (size or modification time) and nobody is reading it;
- a page is read under the archive's lock, so one request can't move the stream out from under another;
- pages the stream passes on its way to the one asked for are held, up to 96 MB, because the stored order need not be the reading order (`p10` is stored before `p2`).

On the same issue, one request per page, page 43 now takes 116 ms, and all 44 pages take 4.3 s together. Going back to the start restarts the stream, which costs the pages before the one wanted (84 ms for page 0). Jumping straight to the last page of an archive nobody has opened still decodes the whole stream once, about 4 s on that issue; the shelf makes every page after that cheap, it can't make the first long jump cheap.

On Windows the file is opened with `FILE_SHARE_DELETE`, which `os.Open` leaves out. Without it, a comic someone had read in the last five minutes couldn't be deleted or replaced in Explorer.
