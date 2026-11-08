# ADR 0116 — An audiobook is its own library, and most of it is built already

Date: 2026-11-03 · Status: **accepted** · Related: [ADR 0024](0024-music-libraries.md), [ADR 0028](0028-pictures-library.md), [ADR 0073](0073-a-retro-game-is-a-file-the-server-owns.md), [ADR 0101](0101-a-comic-is-read-out-of-its-archive.md)

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

## Decision

**Accepted by the owner on 2026-11-03**, with the answers to its open
questions recorded below in place of the questions. The library to build
against is a set of 115 Star Trek audiobooks, kept as a test library like
the others (`TEST AUDIOBOOK LIBRARY`). **Its tags are read and reported
before step 1 is built**, as the matcher was measured before ADR 0115's
screens: author, narrator and series are assumed below to sit in particular
tags, and that assumption is checked against real files first.

### Its own library kind, `audiobooks`, holding `book` items

For the reasons the backlog entry gives, and one more. The model is different:
a book has an author and a narrator, not an album artist; a series and a
position in it, not a year to browse by; and its chapters are inside one file
far more often than they are separate files. A mode of the music kind would
make every music screen check which mode it is in, and every book screen work
around artist → album → track. Retro and comics showed that a kind of its own
costs little here: a kind in `internal/media`, a scan rule, a library config in
the client.

A book is **one item**, whatever it is made of, and **always a `book` row over
one or more `book_part` rows** (amended at step 1, below), so a one-file book
and a forty-part rip play, resume and count the same way:

- **One file** (`.m4b`, `.m4a`, `.mp3`): a book of one part, and its chapters
  are the file's chapters.
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
  ("Discworld/01 The Colour of Magic"). **One series and one number per
  book.** Star Trek nests (*The Next Generation*, then #12 within it, and
  sometimes a sub-series such as *Destiny*); the finer level is left for a
  later step, if the tags carry it.
- **Abridged and unabridged** recordings of one book are **two books**, shown
  side by side with "Abridged" marked the way a film's edition is ([ADR 0042](0042-two-files-one-work.md)).
  They are different recordings, not copies, and are never grouped.
- **A full-cast audio drama lists every narrator**, and the narrator filter
  finds the book under each of them.
- **Cover**: embedded art, else `cover.jpg` beside it, which is the existing
  cover pass unchanged.

#### What the test library's files hold, read 2026-11-03, and what changed

The rules above were assumptions. Every file in `TEST AUDIOBOOK LIBRARY` was
read with ffprobe before step 1 was built, and most of them did not hold:

- **12 books, not 115**: 37 MP3s, 33 hours. No `.m4b`, **no embedded chapters
  and no embedded art** in any file.
- **The album tag is on 9 of 37 files, and wrong where present**: "performed
  live", "Star Trek", "Ferengi". Title tags read "Emissary 1of4" and "DS9 -
  Fallen Heroes". **The folder name is the only reliable title.**
- **The author is tagged once**: album artist "Peel, John" on *Objective
  Bajor*. The artist tag is "Star Trek" or "Alien Voices" everywhere else —
  a franchise or a label, not a writer.
- **No file names a narrator.** Two put "by L. Nimoy and J. de Lancie" in a
  comment, which is not safe to parse; the other comments are volume
  normalisation data.
- **Series and number are folders**: `Deep Space Nine/03 - Warped - Abridged`.
  Gateways is the exception, numbered only in its filename (`_Bk7_`).
- **"Abridged" is only ever in a folder name**, on 7 of the 12.
- **Covers are loose images named after the book** (`Emissary1.jpg`,
  `rule1.jpg`, `Objective Bajor.JPG`), never `cover.jpg`.

So step 1 reads the details this way instead:

- **Title**: the book folder's name, with a leading "NN - " and an
  abridgement mark taken off, and the series' name taken off the front when
  it is followed by a separator ("Gateways - What Lay Beyond" is *What Lay
  Beyond*). For a book of one loose file, its title tag, else its filename.
- **Series**: the folder above the book's, when that is not the library's
  root. **Number**: the folder's leading number, else "Book N" / "BkN" in
  the folder or first part's name. A sub-series number ("Millennium, Book 1")
  stays in the title, as answer 4 allows.
- **Author**: the album artist the parts agree on, else the artist tag, with
  "Last, First" turned round; else, in an `Author/Series/Book` layout, the
  folder above the series. The artist fallback gives "Star Trek" for 11 of
  the 12 test books; the owner chose it over leaving the author empty.
- **Abridged** from the folder or file name, stored as the book's `edition`.
- **Narrator: deferred to step 3.** Nothing in the test library carries one,
  so nothing at step 1 could be checked against a real file. Step 3 reads
  the composer and `narrator` tags, stores every narrator as a credit, and
  builds the filter on them.
- **Part order**: by track number when every part has a different one, else
  by filename in natural order ("disc 2" before "disc 10").
- **Cover**: the album cover pass, then **any picture in the book's own
  folder**, first in natural order. Never the series folder's, and never in a
  folder whose audio belongs to other books too.

**Which files make one book is the folder, and only the folder.** A folder
holding audio and no folders of audio is one book. Files at the library's
root, or beside folders of books, are each a book of their own. A folder is
never split because its files' titles differ, because the ordinary rip names
its files after chapters. The cost is visible in the test library:
`Alien Voices/` holds two separate recordings loose in one folder and reads as
one two-part book, whose parts keep their own titles. Moving each into a folder
of its own makes them two books.

Books need **no schema change**. The wide table's columns already fit: the
author is `artist`, as an album's maker is; the series is `series` and the
number `episode`, as an episode's are; "Abridged" is `edition`. A part's place
in its book is its `episode`.

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
  speed. **A book never played starts at 1×**, not at the last speed used
  elsewhere. Pitch is kept, which `playbackRate` does by default in browsers and
  mpv does with its `scaletempo` default.
- **Skip back 15 s and forward 30 s**, the convention every book player shares,
  in place of seeking by track.
- **A sleep timer**: 15, 30, 45 or 60 minutes, or **end of chapter**. A book
  is listened to in bed, and finding the place again in the morning is half of
  why resume matters. **It fades out over its last few seconds** rather than
  stopping dead.
- **Resume is the default, not an offer.** Pressing Play on a started book goes
  to where it stopped, with no "resume or start over?" question, minus a few
  seconds so the last sentence is heard again. Start over sits in the menu.

### Browsing

Books by title, author, narrator and series, with filters for author,
**narrator** and series. The narrator filter earns its place here: books read
by a cast member repeat across a Star Trek library. Shelves for Continue
Listening and Recently Added. **Continue Listening is the audiobooks
library's own**, separate from music's, so a half-finished book does not push
songs off the shelf or the other way round. The tiles are square like albums:
book covers are square more often than not.

**Books stay on this server**, like comics (ADR 0101): an audiobooks library
is not offered to paired servers until somebody asks for it.

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

1. The kind, the scan rules (one file or a folder of parts), tags and folders
   into title, author and series, and a browse grid with covers. **Built**;
   the narrator moved to step 3, for the reason above.
2. Playing a book: resume by default, chapters list and chapter skip, 15/30
   skips, speed per book. **Built**; see "Step 2, as built" below.
3. The sleep timer, the narrator read from tags, and the author, narrator and
   series filters. **The sleep timer is built** (2026-11-08), in the client
   alone: a menu beside the speed. Minutes count only while the book plays, so
   a pause does not spend them. "End of chapter" is a place on the book's
   clock, checked on every time update, and a skip into another chapter moves
   it to that chapter's end. The last ten seconds fade, then the book pauses,
   which writes the place, and the listener's volume is put back. **The
   narrator and the filters are built too** (2026-11-08), with **no schema
   change**: a narrator is a row in the existing `credit` table with role
   `narrator`, under a person keyed by name, so one reader across forty books
   is one person. Read from a `narrator` tag where any part has one, else the
   composer tag; every name is kept, a full cast included, split on `;`, `/`,
   `&`, "and", and a comma between two whole names (one comma between single
   words is "Nimoy, Leonard"). A `credits` lock keeps them. The book pass
   records the revision of its tag rules per library in `meta`, so a library
   read before narrators existed is read once more at its next scan, changed
   or not. The listing takes `narrator=` (a person id, like `director=`) and
   `author=` / `series=` (names), and the facets list the authors and series.
   A book's page says "Read by …".

Before step 1: `devseed` creates `TEST AUDIOBOOK LIBRARY` like the other
test libraries, and the 115 books' tags are read and reported. Whether they
are `.m4b` files or folders of parts, and where author, narrator and series
really sit, decides the details of step 1, not its shape: both forms of book
are planned.

## Step 2, as built (2026-11-03)

**A book plays in a small player of its own, beside the main one, not as a
mode of it.** "The music player, plus" above assumed the music player could
carry a book. It cannot without bending everything else: the main player is
built around one file per item, and its position, resume, progress writes,
queue and the desktop's mpv path all take the element's clock to be the
item's clock. A book of seven parts breaks that in every one of those places
at once. So the book player is one audio element, one clock laid over all the
parts, and a bar along the foot of the window that stays while you browse. It
holds no queue, which answers this ADR's trap by construction: there is
nowhere for a chapter to be queued. The two players never sound together:
starting a book stops the main player, and the main player starting pauses
the book.

- **The server lays a book out** (`GET /api/items/{id}/book`): its parts end to
  end with each one's start on the book's clock, its chapters, and the
  caller's place and speed. Chapters are a one-file book's own when it has
  them, else one per part.
- **Progress is written against the book**, on the book's clock, never against
  a part. The progress route accepts a book (it refused every container
  before) and calls it **finished within its last minute**, not at a film's
  percentage: 90% of a ten-hour book is an hour unheard.
- **Speed is per book and per person**, a nullable `speed` on
  `playback_state` (schema 90). Absent until chosen, which plays at 1×.
  Choosing a speed is not a place, so it puts nothing on the Continue shelf.
- **Resume is the default**: Play carries on from five seconds before the
  saved place. **Start over sits beside it** as a second button, the way a
  comic's does, rather than in the menu as written above; it is still never a
  question.
- **Continue Listening for books is the audiobooks library's own shelf**, at
  the top of the library. Home's shelves and its spotlight leave books out.
- **Not in step 2**: the desktop app plays books through its web view, not
  mpv; there is no volume control on the bar (the system's is used); media
  keys are the main player's only. None of these is needed to listen.

## Answered by the owner, 2026-11-03

1. **Is there a library?** Yes: 115 Star Trek audiobooks, for testing.
   Format to be read from the files when they arrive.
2. **Narrator as a filter?** Yes.
3. **Offered to paired servers?** No, kept local like comics, until asked.
4. **Series?** One series and one number per book; sub-series later.
5. **Abridged and unabridged?** Both shown, "Abridged" marked like an edition.
6. **Full-cast dramas?** Every narrator listed, and found by the filter.
7. **Continue Listening?** The audiobooks library's own shelf.
8. **Sleep timer?** 15/30/45/60 minutes and end of chapter, fading out.
9. **Default speed?** 1× for a book never played; each book then keeps its own.

## Not in this ADR

- A metadata provider (Open Library or another): its own ADR, when needed.
- EPUB ebooks, which are a reader, not a player (the roadmap's next entry).
- Bookmarks and notes inside a book.
- Syncing a position with Audible, Libby or anything else.
