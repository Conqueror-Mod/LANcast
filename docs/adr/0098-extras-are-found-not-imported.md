# ADR 0098 — Extras are found, not imported

Date: 2026-10-26 · Status: **accepted** · Amends [ADR 0038](0038-extras-are-not-works.md)

## Context

ADR 0038 kept a film's trailers, featurettes and deleted scenes out of the
library, because importing them as films had put a real library at 1,381 films
against a true 1,192. It called that the honest first step and named what
playing them would need: a kind, a place on the detail page, and a decision
about whether a trailer plays through the same routes as a film.

The obvious build is the one Plex has: import each extra as a row, a child of
its film. Here that would mean a new kind that every listing must leave out —
the grid, every count, Recently Added, Continue Watching, search, duplicates,
collections, trending, the random pick. 138 queries in 42 files read the item
table. Missing one is the quiet failure this project keeps a rule about (a write
that changes what a list holds must invalidate it), and it is exactly the failure
0038 was written to undo: an extra appearing as a film.

## Decision

**Extras are found in the film's folder when its page asks, and never imported.**

- **Which files.** The rules 0038 already uses to exclude them, so the two can
  never disagree. For a film in a folder of its own: the marked files beside it
  (`-trailer`, `-featurette`, …) and every video in the extras folders directly
  inside it (`Trailers/`, `Featurettes/`, `Behind The Scenes/`, …). For a film
  sharing a folder with other films, only the marked files that name it —
  `Alien-trailer` is Alien's and not Aliens' — because an extras folder there
  cannot say whose it is. A show's extras are found the same way in the show's
  folder. A library root is never a film's own folder: `Trailers/` directly under
  a root is a category (0038). Samples are left out, as from the library.
- **What they are called.** Kind and title come from `internal/media`
  (`ClassifyExtra`), because all filename guessing lives there. The folder wins
  over a suffix: the folder is what a person organised.
- **How they play.** Straight from the file, with range requests, in a player on
  the film's page. No conversion: the desktop window plays nearly everything, a
  browser plays what it can, and a file it cannot play says so with its format.
  Nothing is recorded — an extra has no progress, is not watched, and is not in
  history.
- **How a request names one.** By an opaque key, looked up against a fresh
  listing on every request and never decoded into a path. A key names one of
  this film's extras or nothing. Each file is also held to the library root with
  links followed (ADR 0094).
- **Access.** Through the film: a film an account may not see has no extras for
  it either.
- **The Trailer button.** When the film has a trailer of its own, it plays that
  instead of the online one. The online trailer remains for films without.

## Consequences

- 0038 stands: an extra still never appears as a film, in the grid or a count,
  because it is still never a row.
- The cost is a directory listing per detail page view. Folders are small; this
  is the same order of work as finding a film's subtitles.
- No resume, no watched mark and no conversion for extras. Those are what rows
  would buy, and nobody has asked for them; if they do, this ADR is what to
  revisit, with the 138 queries as the price.
