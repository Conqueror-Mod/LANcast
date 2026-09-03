# ADR 0056 — A track's grouping key is worth storing

Date: 2026-09-02 · Status: **proposed**

Reverses a decision recorded in a comment in `internal/scan/tags.go`, which is
where it was made and where it has been correct until now.

## The fault

Reported as: adding tracks to a music library, the scan works, and what it
scanned does not appear for a long time.

The long time is the scan. Measured on the reporting library, from the log:

| files seen | changed | elapsed |
|---|---|---|
| 9,037 | **0** | **0.5s** |
| 9,054 | 17 | **92.0s** |
| 9,040 | 134 | 77.7s |
| 9,003 | 61 | 75.5s |

**Seventeen changed tracks cost more than a hundred and thirty-four.** The cost
has no relationship to what changed; it is a fixed price paid whenever anything
does.

Instrumenting the pass says where it goes:

```
music tag pass  tracks=9054  load_ms=134  read_tags_ms=129293  reconcile_ms=9612
```

| phase | share |
|---|---|
| load the track rows | 0.1% |
| **read tags from every file** | **92%** |
| group into albums and artists | 7% |

About 14ms per file, across all of them, every time.

## Why it reads every file

The tag pass already skips entirely when nothing changed — that is the 0.5s
row, and it is why this was never noticed. When something *has* changed it
loads `LibraryTracks` — the whole library — and reads tags from all of them,
because `reconcileMusic` rebuilds the album and artist hierarchy from a
`trackGroup` per track and a missing group is a missing album.

The group is not stored, deliberately, and the comment says why:

> It is not stored on the track — it belongs to the album, and persisting it
> per row to read it back one step later would be a column that exists only to
> survive a function boundary.

That was right. A field whose only purpose is to carry a value between two
functions in the same pass is a bad column, and when the pass always ran there
was nothing else to weigh against it.

**What changed is that the pass stopped always running.** Once the unchanged
case was made free, the grouping key stopped being a value in flight and became
the only reason to re-read files whose contents provably have not moved — the
walk has just established that no size and no mtime changed, and tags live
inside the file.

## Decision

**A track stores the grouping key its tags produced, and a scan reads tags only
from tracks that changed.**

Five values, all already computed and thrown away today:

| stored | why it cannot be derived later |
|---|---|
| `group_artist` | the **album** artist, which differs from the track's own performer on a compilation — that distinction is the reason a record does not shatter into one album per guest |
| `group_album` | the album as tagged, or as guessed from the folder |
| `group_dir` | the folder the file sits in |
| `group_album_from_folder` | whether the album name was a guess rather than a tag |
| `group_album_at_root` | whether that folder is a direct child of the library root |

The last three exist for `dropBucketAlbums`, which decides whether a folder is
really a record. Its two tells are **cohesion** — every track in a folder
agreeing on the artist — and **depth** — a real album living under an artist
folder rather than loose at the top. Neither is a property of one track, so
both need the whole library's groups present, which is exactly why the current
code must read every file to run it.

**The pass then reads tags only for tracks whose row changed**, and takes the
rest from storage. Every track still contributes a group, so `dropBucketAlbums`
and `reconcileMusic` see the same input they see today.

## Consequences

**A music scan becomes proportional to what changed.** Seventeen new tracks
should cost seventeen tag reads and the 9.6s reconcile, rather than 9,054 reads
and the same reconcile.

**The reconcile is then the floor**, at about 9.6 seconds, and this ADR does not
address it. It is a seventh of the cost and the wrong thing to optimise second.

**A stored group can go stale in one way the file cannot**, and it is worth
naming rather than discovering. If the *grouping rules* change — a new tell in
`dropBucketAlbums`, a different album-artist preference — stored keys were
computed under the old rules. `dropBucketAlbums` runs over the assembled groups
every scan, so changes to *it* take effect regardless; changes to how a key is
**extracted** do not. A build that changes extraction has to clear the stored
keys, the same way a build that learns a new probe field re-probes.

That is the same trade the tag pass already made when it learned to skip:

> The cost of this: improving the grouping rules no longer takes effect on a
> library where nothing has changed on disk.

This ADR does not widen that trade so much as move it from "nothing changed" to
"this track did not change", which is a strictly smaller exposure than the one
already accepted.

**The columns are not read by anything but the scanner.** They are not exposed
in the API and no client sees them. A grouping key is scanner working state
that happens to be worth keeping, and calling it anything else would invite it
being read as truth about the record — the album *item* is that truth.

## Alternatives rejected

**Rebuild the groups from the existing hierarchy.** The containers are already
in the database: a track's `parent_id` is its album, whose parent is the
artist. This looks like it needs no schema change at all, and it does not
survive contact with `dropBucketAlbums`, which needs `dir`,
`album_from_folder` and `album_at_root` — none of which are recoverable from
the hierarchy, because the hierarchy is the *output* of the decision they feed.
It is the same five values by a route that pretends not to store them.

**Read tags only for changed tracks and reconcile only those.** Cheapest, and
wrong. Cohesion and depth are properties of a *folder*, so a reconcile that
sees one new track in a folder cannot tell whether that folder is a record.
Adding one track to an eleven-track album would re-decide the album on evidence
of one track and split it.

**Cache the groups in memory between scans.** No schema change, and it fails
the only case that matters: the server restarts, or the pass runs for the first
time after an upgrade, and the next scan pays the full price anyway. A cache
that is empty exactly when somebody has just added music is not a cache.

**Make the tag read faster.** 14ms per file is not obviously wrong for opening
a file and parsing a header, and even a doubling leaves 65 seconds. The problem
is not that the reads are slow, it is that there are 9,054 of them to learn
about 17.
