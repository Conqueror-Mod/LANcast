# ADR 0038 — Extras are not works

Date: 2026-08-16 · Status: accepted

## Context

A real library reported **1,381 films against a true count of 1,192**. Roughly
120 of the difference was collections, which are tiles rather than films and are
a separate question. The rest was this: every playable video file in a movie
library became a movie.

A film's folder does not hold only the film. It holds `sample.mkv`, a
`Trailers/` folder, `Featurettes/`, `Behind The Scenes/`, and files named
`The Film-trailer.mkv`. Each of those was imported as a work — parsed for a
title, given a tile in the grid, sent to the metadata provider for matching, and
counted.

Nothing about this looked like a failure. The scan reported success, the count
matched the grid, and the extras were mixed in among real films sorted
alphabetically, so a five-second sample appeared beside a feature and read as
just another row somebody would have to scroll past.

## Decision

**A video library imports works, not the material that ships beside them.** The
scanner excludes extras during the walk, by two rules.

### Folders, with one crucial condition

The extras folder names are the ones Plex and Kodi both document, because those
are the conventions the tools that *produced* these folders were written
against: `Behind The Scenes`, `Deleted Scenes`, `Featurettes`, `Interviews`,
`Scenes`, `Shorts`, `Trailers`, `Extras`, `Other`. Compared after normalizing
away separators, so `Behind.The.Scenes` is the same name.

`Specials` is deliberately **not** on that list. In a shows library that is
season zero — real episodes, including the Christmas special somebody went
looking for.

The condition that matters: **a folder with one of those names sitting directly
inside a library root is a category, not an extras folder.** `Movies/Shorts/…`
is somebody's short-film collection and `Movies/Trailers/…` is a folder of
trailers they keep on purpose. An extras folder must have a film folder above
it, which is exactly where the convention puts it —
`Movies/The Film (2011)/Trailers/…`.

Getting that backwards would discard whole categories of real content, which is
a far worse bug than importing a featurette. The rule is written to be wrong in
the safe direction.

### Filenames

`sample`, `trailer`, anything ending `.sample`, and the `-trailer` family of
suffixes (`-featurette`, `-deleted`, `-behindthescenes`, and the rest). Suffixes
only, so *Free Samples* (2012) and *Trailer Park Boys* are films — they are
titles, not markers.

These carry no depth condition, because `sample.mkv` is junk wherever it sits.

### Video libraries only

The rule reads folder names that mean nothing in a music or picture library,
where `Interviews` is a perfectly ordinary album and `Scenes` a plausible photo
folder.

## An extra already imported is marked missing, not deleted

A file this rule newly excludes is not counted as *seen*, so the next scan marks
its row missing in the ordinary way. It is not deleted, and that follows the
standing rule directly: scanning marks missing, never deletes. A new exclusion
heuristic quietly destroying rows would be a worse thing to be wrong about than
the import it corrects.

Missing rows are excluded from the grid and the count, so the numbers correct
themselves on the next scan while the rows remain recoverable.

## The count and the grid, again

Fixing this surfaced a second defect in the same area. `topLevelPredicate` — the
constant that exists precisely so a library's count and its browse grid answer
the same question — did not carry `missing = 0`. The **count** filtered missing
rows and the **grid** did not, so a library with unreachable files listed tiles
it did not count, and offered them as playable.

That is the exact disagreement the constant was written to prevent, surviving in
the one place nobody looked because the duplication had been removed everywhere
else. `missing = 0` now lives in the predicate rather than in its callers.

## Consequences

- The scan reports `skipped_extras`, and the Libraries pane states it. Silence
  would be the worse failure here: somebody comparing this server's count
  against another has no way to discover where a difference went, and "189
  extras" is the entire explanation.
- Extras become invisible rather than browsable. Modelling them as children of
  their film — which is what Plex does — is a real feature and a larger one; it
  needs a kind, a place in the detail page, and a decision about whether a
  trailer is playable through the same routes. Excluding them is the honest
  first step, and it is reversible: the files are untouched and a later build
  can import them as something other than films.
- A library that keeps deliberate extras *inside* a film folder and wants them
  listed loses them from the grid. The ignore list already works in the other
  direction only; there is no per-library override for this, and adding one
  before anybody wants it would be inventing a setting.
