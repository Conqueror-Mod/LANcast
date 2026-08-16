# ADR 0037 — A show is a series title, not a directory

Date: 2026-08-16 · Status: accepted · Amends [ADR 0010](0010-shows-as-media-items.md)

## Context

[ADR 0010](0010-shows-as-media-items.md) built the show → season →
episode hierarchy and made a show's identity **its directory**. That is the
obvious choice, it is what `tvshow.nfo` is keyed on, and on the layout it was
designed against it is correct:

```
TV/
  Andor/
    Season 01/
      Andor.S01E01.mkv
```

`ShowDir` walks up from the episode, skipping folders whose name *is* a season
marker, and stops at `Andor`. One show, two seasons, done.

It fails completely on a layout that is at least as common in the wild:

```
TV/
  It's Always Sunny in Philadelphia S01/
  It's Always Sunny in Philadelphia S02/
  … S03 … S04 … through S20
```

`reSeasonDir` is anchored — `^(season|series|s)[\s._-]*(\d{1,2})$` — so a folder
called `It's Always Sunny in Philadelphia S01` is not a season folder, because
its name is not *only* a season marker. `ShowDir` therefore stops at it, and
every season becomes its own show.

Reported from a real library on v0.6.27: twenty tiles for one series, each
reading "1 season", each independently matched by the metadata provider so they
even shared a poster. `Blue Mountain State` appeared twice and `BMS S01` sat
beside it as a third series. The library count said 60 and was *correct* about
the rows — the rows were wrong.

Two things made this hard to see from the inside. The count and the grid agree,
because `topLevelPredicate` is shared between them (which is a fix from an
earlier bug of exactly this family), so nothing was inconsistent. And
enrichment *succeeded*: each pseudo-show matched the right series, so the screen
was full of correct-looking data.

## Decision

**A show is identified by its normalized series title within a library.** The
directory becomes evidence about where the files are, not about what the show
is.

The parsed series name already exists on every episode row — the filename
scanner writes it — and it is the one thing that stays the same across every
layout a series can be stored in. `media.SortTitle` normalizes it, reusing the
single normalizer rather than growing a second opinion.

### Where the filename says nothing, the directory still decides

An episode whose filename yields no series name is keyed on its directory
exactly as before. That is not a fallback so much as a refusal: with no title
there is nothing to group *on*, and folding such an episode in with a neighbour
would be a guess of the kind this codebase declines to make elsewhere.

The same limit is why `BMS S01` stays separate from `Blue Mountain State`.
Nothing in a filename resolves an abbreviation to the series it abbreviates, and
the only honest fix for that row is a metadata match, not a cleverer parse.

### The show's `path` stays a real directory whenever one exists

This is the part that took the most care. The show row's `path` is where
`tvshow.nfo` is written, so a fix that made every show identity synthetic would
have quietly stopped sidecar writing for every ordinary library in order to
repair an unusual one.

So `path` is:

- **the show directory**, when every episode agrees on one — the ADR 0010 case,
  behaving exactly as it did before, sidecars included; or
- **`lancast:show:<sort title>`**, when the episodes are spread across sibling
  folders. There is no directory that *is* the show in that layout, and writing
  a series-level file into whichever season folder was scanned first would put
  `tvshow.nfo` inside one season of the series it describes.

The synthetic form is the same shape collections already use, and it is chosen
deterministically rather than from scan order — a series must not change
identity because a directory walk returned folders in a different sequence.

`nfo.Write` now refuses a `lancast:` path outright. That guard is load-bearing
and platform-dependent in a way worth stating: `lancast:show:andor` +
`/tvshow.nfo` is a *relative* path on Linux, so without it the write lands
beside the server's working directory and looks like nothing happened, while
Windows rejects the colon and produces an error instead.

### Identity moves, rows do not

When a show's `path` changes — because a series gained a second folder, or
because an upgrade regrouped it — the existing row is **repointed in place**
rather than replaced. The row carries the show's artwork, its match state, and
its **locked fields**, and a re-organisation on disk is not a reason to lose any
of them. Creating a new row and pruning the old one would have been simpler and
would have violated the rule that locked fields survive everything.

### Upgrading is a rescan, not a migration

No schema change. On the first scan after this build:

- episodes regroup onto one show per series;
- seasons keyed on a real season directory are **re-parented** to it — this
  needed its own fix, because `EnsureSeason` inserts-or-does-nothing, so an
  existing season silently kept its old parent and the regrouping would have
  appeared to do nothing at all for the layout that already had season folders;
- the emptied per-folder show rows are swept by `PruneEmptyContainers` in the
  same pass, which already ran there for exactly this class of reinterpretation;
- where several show rows share a title, the **oldest wins**, deterministically
  by id, so the surviving row is the one most likely to carry the metadata and
  locks somebody has already curated.

## Consequences

- Two genuinely different series with byte-identical titles in one library now
  merge, where directory keying kept them apart. This is the real cost of the
  change. It is judged the better failure: it is visible on screen the moment it
  happens, where the old behaviour produced twenty plausible-looking tiles that
  nobody would read as a bug.
- Watch state, resume positions and ratings are untouched — all of them hang off
  episode rows, which keep their identity throughout.
- A library scanned by an older build shows its old shape until it is rescanned.
  Nothing repairs itself in place, and that is consistent with everything else
  here: a rescan reconciles files, and this is a file-reconciliation outcome.
- `nfo.IsSyntheticPath` is exported because the property belongs to the path
  rather than to the NFO package — anything turning a row into a file needs the
  same test, and a second spelling of it would eventually disagree.
