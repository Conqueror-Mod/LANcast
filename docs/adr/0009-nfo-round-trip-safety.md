# ADR 0009 — NFO round-trip safety

Date: 2026-07-22 · Status: accepted

## Context

LANcast reads and writes Kodi-style `.nfo` sidecar files. Reading them is the
migration path for anyone arriving from Kodi; writing them keeps a library
portable and readable by Kodi, Jellyfin, and Emby, which is the practical
expression of "your data is yours" — the metadata lives in your folders, not
locked inside LANcast's database.

Reading and writing the same file format creates a hazard that is easy to miss
and unpleasant to debug:

1. LANcast writes `movie.nfo` from provider data.
2. A later scan reads `movie.nfo` as a **local source**.
3. Local sources outrank providers ([ADR 0008](0008-field-level-locking.md)).
4. The item is now pinned to its own past output. Provider updates never apply
   again, for no reason the user can see.

The failure is silent, permanent, and looks like "metadata just stopped working."

The precedence rule is not the problem — a human editing an NFO by hand *is*
making a deliberate statement about their library, and it should outrank a
provider. The problem is that LANcast cannot tell that statement apart from an
echo of its own voice.

## Decision

Every NFO LANcast writes carries a provenance marker containing a SHA-256 hash
of the field values written:

```xml
<lancast generated="2026-07-22T14:02:11Z" hash="sha256:9f2c…"/>
```

On read, LANcast recomputes the hash over the file's current field values:

| Condition | Interpretation | Treatment |
|---|---|---|
| Marker present, hash matches | LANcast's own unmodified output | **Mirror** — a cache, not a source. Ignored for precedence. |
| Marker present, hash differs | Someone edited it after we wrote it | **Authoritative** local source |
| No marker | Written by Kodi, another tool, or by hand | **Authoritative** local source |

## Consequences

**Good.** The precedence rule stays intact and means what it should. A human
edit wins; LANcast reading its own file back is not an edit and does not win.

**Good.** Migration works unchanged. An existing Kodi library has no LANcast
marker, so every `.nfo` is authoritative on first import — exactly right.

**Good.** Detection is content-based, not timestamp-based. File mtimes are
unreliable across sync tools, network shares, backup restores, and archive
extraction; a content hash is not.

**Good.** Editing an NFO by hand is a supported, working way to correct
metadata, on par with the UI. That matters for the Kodi audience specifically.

**Cost.** The hash must cover exactly the fields LANcast writes, computed
identically on write and read. A drift between the two implementations makes
every file look edited and re-freezes items — so the hash function is one
function used by both paths, never two.

**Cost.** A user who edits an NFO and happens to restore it to identical values
is back to mirror status. Correct behavior, since the file then says nothing
LANcast did not already say.

## Additional write rules

- **Opt-in per library.** Nothing is written into media folders without being asked.
- **Skipped silently on read-only mounts.** Not an error; many libraries are read-only by design.
- **Atomic.** Temp file plus rename, never a partial write over good data.
- **Non-destructive.** Unknown XML elements written by other tools are preserved
  untouched. LANcast is a guest in a file format it did not invent, and other
  tools' data is not ours to discard.

## Verification

- Write an NFO, rescan, confirm provider updates still apply (mirror detected).
- Hand-edit that NFO, rescan, confirm the edit now wins.
- Confirm unknown elements survive a rewrite untouched.
