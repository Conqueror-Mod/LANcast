# ADR 0042 — Two files, one work

Date: 2026-08-17 · Status: proposed

## Context

A file named `Spider-Man Into the Spider-Verse (Alternate Cut) (2018).mkv` sat
unmatched at 0% beside the same film's theatrical file. Fixing the parser
([ADR 0041](0041-a-misplaced-file-is-corrected-on-disk.md)'s neighbouring work)
makes it match — and the moment it does, the library holds **two rows with the
same title, the same year, the same poster and the same provider id**, which is
the "duplicate The Batman" complaint that started this investigation.

So the parser fix cannot land alone. It converts a visible failure into an
invisible one.

Then the file turned out not to be an alternate cut at all:

```
size    2832374353   2832374353
head 1MB   F4839821…     F4839821…
mid 4MB    4CB76636…     4CB76636…
tail 1MB   8FCBB2BE…     8FCBB2BE…
```

Byte for byte the theatrical file, copied and renamed. **The filename asserted an
edition that does not exist**, and no amount of parser work would have found that
out, because parsing a name is exactly the thing that believes the name.

### This is not one file

Surveying the live library — 1,209 films — **thirteen pairs already share a
provider id**. They are five different situations:

| What it is | Pairs | Evidence |
| --- | --- | --- |
| Redundant copy, identical bytes | 7 | same size, sometimes same folder, sometimes two index folders |
| Same cut, different encode | 3 | e.g. 787 MB mp4 beside a 14.6 GB mkv |
| A genuine second edition | 1 | a 2005 animated feature beside its "Complete Edition" re-release |
| Two halves of one film | 1 | `… CD1.avi` and `… CD2.avi` |
| **A misfile** | 1 | a 1989 film wearing a 2022 film's identity, from a stale `.nfo` |

The last row is the one that settles the design. A shared provider id is **not**
evidence of duplication — it is evidence that *something* wants a human. Two of
these thirteen are not duplicates at all; one is a single film in two parts and
one is simply wrong.

### The two discriminators that do not work

**The filename.** The motivating file called itself an alternate cut and was a
copy. A rule that trusted the marker would have created a permanent second tile
for a file with nothing in it to distinguish.

**Duration.** The obvious tiebreak, and useless here: `media_item.duration_ms` is
overwritten with the **provider's** runtime on match
(`fetchMovie` sets it from TMDB's `runtime`). Two rows matched to one id
therefore always report identical durations, whatever the files contain. Every
pair above shows equal durations, including the misfile — where one film is 126
minutes and the other 177. Anything built on that column would agree with itself
and be wrong.

What is left that is real: `size_bytes`, the path, and a byte comparison.

## Decision

**LANcast reports the collision. It does not resolve it.**

1. **Report, do not reconcile.** A second file claiming a work already in the
   library is surfaced with its evidence — both paths, both sizes, and whether
   the bytes match — and no action is taken. This is the `shape_warning`
   posture ([shapecheck.go](../../internal/scan/shapecheck.go)) applied to items
   rather than libraries: be loud at the moment it happens, and let the person
   decide.

2. **Keep the edition marker instead of discarding it.** `stripEditionSuffix`
   already finds `(Alternate Cut)`, `Director's Cut`, `DC`, `SE` — and then
   throws the finding away, keeping only the shortened title. Storing it in a
   nullable `edition` column is the smallest change that lets two editions of one
   work be told apart in a grid, and it costs one migration and no new
   heuristics. The marker becomes a *label*, never a grouping key.

3. **Never merge, rank, or delete.** Two files stay two rows. LANcast does not
   pick a best copy, does not hide the smaller one, and does not remove anything
   from disk.

The reason is the one the motivating case demonstrates and which this project's
user put better than the code does: being told about a flaw in your own files is
more valuable than having it smoothed over. A server that silently merged these
would have hidden a mislabelled duplicate, a stale `.nfo` pointing a 1989 film at
a 2022 one, and a film split across two discs that nothing had grouped. All three
are worth knowing. None of them is the server's decision to make.

## Alternatives considered

**Merge into one work with several files.** The Plex model, and the one a user
asks for first. Rejected for now on two counts. `media_item.path` is UNIQUE and
is the row's identity — every containment check, every playback session and every
watch position resolves through it — so one row with several files is a change to
what an item *is*, not a feature added beside it. And on this library it would
have been actively harmful: merging by provider id would have folded a 1989 film
into a 2022 one and called the result a version. Worth revisiting once the
reporting above has shown how often the collisions are genuinely the same work.

**Deduplicate automatically by hash.** Tempting for the seven identical pairs,
and wrong. A byte-identical second copy is sometimes deliberate — a file kept on
two drives, or in two index folders, which is exactly what two of these pairs
are. "Identical" is a fact worth reporting; "redundant" is a judgement, and this
server does not delete media on a judgement.

**Pick the best copy and hide the rest.** Requires ranking 787 MB against
14.6 GB with no knowledge of why both exist, and produces a library that quietly
disagrees with the filesystem. The 4K remux is usually the one to keep and
sometimes the one that will not direct-play on the client asking.

**Discriminate editions by duration.** Would work if `duration_ms` were the
file's. It is the provider's — see above.

**Use the edition marker as a grouping key.** The motivating file proves the
marker can be a lie. It is a label the user wrote; it is displayed, not trusted.

## Consequences

The library gains a report that will initially be full, and that is the point:
thirteen pairs exist today and none of them is visible. Two of the thirteen are
outright errors — the misfile and the mislabelled copy — that had gone unnoticed
for years, and one is a film that has never played correctly because nothing
knows its two halves belong together.

The `edition` column is additive and nullable, so existing rows read as "no
edition stated" and behave exactly as they do now.

**Multi-part works are adjacent and not solved here.** `PartOf` and `ChapterOf`
exist in `internal/media` and [ADR 0017](0017-collections-and-multi-part-works.md)
covers the model, but a `CD1`/`CD2` pair in this library parsed as two whole
films. Whether that is a parser gap or a scanner one is its own question; this
decision only ensures the pair is *reported* rather than sitting silently as two
identical tiles.

**The misfile category argues for a second report.** A stale `.nfo` overrode a
correct parse, and the next enrichment pass then searched the provider using the
NFO's title and year and confirmed the wrong film at 0.964 confidence. Local
sources outranking providers is deliberate ([ADR 0008](0008-field-level-locking.md))
and is not reopened here — but an NFO that disagrees with both the filename and
the containing folder is a detectable condition, and it is the same shape of
answer as this ADR: report it, do not overrule it.
