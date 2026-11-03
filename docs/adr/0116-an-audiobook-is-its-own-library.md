# ADR 0116 — An audiobook is its own library, and most of it is built already

Date: 2026-11-03 · Status: **proposed** · Related: [ADR 0024](0024-music-libraries.md), [ADR 0028](0028-pictures-library.md), [ADR 0073](0073-a-retro-game-is-a-file-the-server-owns.md), [ADR 0101](0101-a-comic-is-read-out-of-its-archive.md)

## Context

Audiobooks are the first line of the roadmap's feature backlog. The backlog
entry names what a book needs that music does not: resume by position,
chapters, playback speed, and author, narrator and series in place of artist
and album. It asks one question first: is a book its own library kind, as
music, pictures, retro and comics each became, or a mode of the music kind?

Checked against the code on 2026-11-03, the entry is partly out of date, and
in the direction that makes this smaller:

- **Resume already works for audio.** `playback_state.position_ms` is written
  for every item, and the client's `resumeSeconds` resumes any item past its
  started floor (`web/src/playback/resumePoint.ts`). A track already comes back
  where it stopped, and Continue Listening exists. A ten-hour book needs
  nothing new here.
- **Chapters are already read.** `internal/probe/chapters.go` (#949) asks
  ffprobe for a file's chapters on demand and the API caches them per file.
  An `.m4b` carries its chapters exactly as an MKV does.
- **Speed exists in both players.** The film player has a speed setting, and
  the music deck and the mpv backend both carry `playbackRate`. Nothing in the
  music UI offers it.
- **An `.m4b` is not indexed at all.** The scanner's audio list
  (`media.audioExts`) has `.m4a` and not `.m4b`; only the stream handler knows
  the type. The entry's "a book dropped into a music library is one very long
  track" is wrong: today it is not there.

So most of what a book needs is in the tree. What is missing is a place for
books to live, a model that is not artist → album → track, and a player that
offers what a listener expects.

## Decision (proposed)

### Its own library kind, `audiobooks`, holding `book` items

For the reasons the backlog entry gives, and one more. The model is different:
a book has an author and a narrator, not an album artist; a series and a
position in it, not a year to browse by; and its chapters are inside one file
far more often than they are separate files. A mode of the music kind would
make every music screen check which mode it is in, and every book screen work
around artist → album → track. Retro and comics showed that a kind of its own
costs little here: a kind in `internal/media`, a scan rule, a library config in
the client.

A book is **one item**, whatever it is made of:

- **One file** (`.m4b`, `.m4a`, `.mp3`): the book is the file, and its
  chapters are the file's chapters.
- **A folder of parts** (`01.mp3`, `02.mp3`, … the common rip): the book is
  the folder, a container like an album, and its parts are children played in
  order. Each part is a chapter, named from its title tag or else its file.

`.m4b` joins the scanner's audio list for audiobooks libraries **only**. In a
music library it stays unindexed, because a book among songs is the problem
this ADR exists to avoid.

### Where the details come from: the files, and nothing else at first

All filename and tag guessing lives in `internal/media`, as ever.

- **Title**: the album tag, else the title tag, else the folder or file name.
- **Author**: the album artist tag, else the artist tag.
- **Narrator**: the composer tag, which is where most rippers and Audible
  exports put it. A `narrator` tag wins where present.
- **Series and number**: the grouping tag or `series` / `series-part` tags,
  else a folder named after the series holding numbered books
  ("Discworld/01 The Colour of Magic").
- **Cover**: embedded art, else `cover.jpg` beside it, which is the existing
  cover pass unchanged.

**No provider in the first version.** No phone-home, and no key. Audible has
no public API. Open Library is a candidate for later. Like MusicBrainz for
albums, that belongs in a decision of its own, made once untagged books prove
to be a real problem.

### The player: what a listener expects

The music player, not the film player, plus:

- **Chapters** as a list in Now Playing, and next/previous chapter in place of
  next/previous track when the book is one file. Built on the chapters
  endpoint that exists.
- **Speed**: 0.75× to 2× in 0.25 steps, remembered **per book**, because
  somebody who listens to one narrator at 1.5× may not want another at that
  speed. Pitch is kept, which `playbackRate` does by default in browsers and
  mpv does with its `scaletempo` default.
- **Skip back 15 s and forward 30 s**, the convention every book player shares,
  in place of seeking by track.
- **A sleep timer**: 15, 30, 45 or 60 minutes, or **end of chapter**. A book
  is listened to in bed, and finding the place again in the morning is half of
  why resume matters.
- **Resume is the default, not an offer.** Pressing Play on a started book goes
  to where it stopped, with no "resume or start over?" question, minus a few
  seconds so the last sentence is heard again. Start over sits in the menu.

### Browsing

Books by title, author, narrator and series, with filters for author,
narrator and series. Shelves for Continue Listening and Recently Added. The
tiles are square like albums: book covers are square more often than not.

## The thing that is easy to get wrong

**Do not let a book become a playlist of chapters.** If the queue holds
chapters, "next track" skips a chapter, shuffle shuffles a book, gapless
crossfade blurs a chapter break, and the Continue shelf lists chapter 7
instead of the book. The book is the playing item. Chapters are positions
inside it, as they are in a film, even when they are files on disk.

The same applies to scrobbling, play counts and "played 3 times": a book
counts once when it finishes, not once per part.

## Consequences

- One new kind, `book`, and one new library kind, `audiobooks`. A new nullable
  column or two for narrator and series number may be needed (author fits the
  existing `artist`; series fits `series`). The schema change gets a migration
  and is named here when it is built.
- Speed per book is a per-user setting, so it belongs beside the play state,
  not on the item.
- A folder book needs a play cursor that spans files, which the queue already
  has for albums. A one-file book needs none.
- Nothing about film, music or any other kind changes.

## Steps, each mergeable on its own

1. The kind, the scan rules (one file or a folder of parts), tags into title,
   author, narrator and series, and a browse grid with covers.
2. Playing a book: resume by default, chapters list and chapter skip, 15/30
   skips, speed per book.
3. The sleep timer, and the author, narrator and series filters.

## Open questions for the owner

- **Is anybody waiting for this?** The rule this project has followed is to
  plan an area right before building it, and to build it when somebody has
  the files. Is there an audiobook library to point it at, and roughly what
  shape is it in: `.m4b` files, or folders of MP3 parts?
- **Narrator as a filter, or only shown on the book?** A filter is cheap, but
  only worth it if narrators repeat in the library.
- **Should books be offered to paired servers** like films and music, or kept
  local like comics (ADR 0101) until somebody asks?

## Not in this ADR

- A metadata provider (Open Library or another): its own ADR, when needed.
- EPUB ebooks, which are a reader, not a player (the roadmap's next entry).
- Bookmarks and notes inside a book.
- Syncing a position with Audible, Libby or anything else.
