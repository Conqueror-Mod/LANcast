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
<lancast generated="2026-07-22T14:02:11Z" hash="sha256:v1:9f2c…"/>
```

The `v1` names the hashing scheme. See the 2026-08-08 amendment for why it is
there — briefly, the set of hashed fields is not fixed forever, and a digest
whose scheme this build cannot compute is not evidence of anything.

On read, LANcast recomputes the hash over the file's current field values:

| Condition | Interpretation | Treatment |
|---|---|---|
| Marker present, hash matches | LANcast's own unmodified output | **Mirror** — a cache, not a source. Ignored for precedence. |
| Marker present, hash differs **under a scheme this build can compute** | Someone edited it after we wrote it | **Authoritative** local source |
| Marker present, scheme unknown or unparseable | We cannot tell — but it is ours | **Mirror**, conservatively |
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


## Amendment — 2026-08-08

Two changes, both from watching a wrong title outlive three databases.

### An edit must be proven, not assumed

The original rule read "hash differs ⇒ a human edited it". That is only sound
while every LANcast build computes the same digest over the same fields, and
nothing was keeping that true. `FieldsHash` covers a fixed list; adding one
field to it — which is an ordinary thing to do when a new piece of metadata gets
stored — would make **every sidecar LANcast has ever written** stop matching its
own hash. Each one would then be read as a file a human edited, and its contents
promoted to authority over every provider. Silently, on every machine, in one
release.

That is the same shape as `?profile=` and `containerFromExtension`: a mechanism
that keeps running while quietly ceasing to apply.

So the digest now carries its scheme, and the read side asks whether an edit can
be **proven** rather than whether the hash matched. A digest this build cannot
compute is treated as ours.

The asymmetry is deliberate. Wrongly ignoring an edit costs the user a change
they can make again in the UI — where it locks the field, which is the stronger
mechanism anyway. Wrongly treating our own stale output as authority re-pins an
identity to a file, which is exactly what this ADR exists to prevent and what
took a morning to diagnose.

Files written before this carry `sha256:<hex>` with no version. They are read as
v1, because v1 *is* the scheme that produced them — a statement of fact, not a
compatibility guess.

### No sidecar for an identity we never established

LANcast wrote a sidecar for every enriched item, including ones the matcher had
declined to match. An `unmatched` item has a title from its filename at a
confidence the scorer itself rejected, and writing that into the user's media
folder under LANcast's own provenance stamp turns a guess into a durable local
fact — one that survives the database and is inherited by the next.

Sidecars are now written only for `matched` and `local` identities. `review` is
excluded for its own reason: it means the matcher wants a human to look, and
writing the candidate it was unsure about would pre-empt that answer with a file
on disk.

**Not decided here:** whether an edit should make the *whole* file authoritative
or only the fields that actually changed. Today one corrected title promotes the
entire sidecar — plot, cast, rating — including parts LANcast got wrong that
nobody touched. Per-field provenance would fix it and needs its own decision;
it changes what this ADR promises rather than tightening it.

## Verification

- Write an NFO, rescan, confirm provider updates still apply (mirror detected).
- Hand-edit that NFO, rescan, confirm the edit now wins.
- Confirm unknown elements survive a rewrite untouched.
- Confirm a marker whose scheme is from a newer build is treated as a mirror,
  not as an edit.
- Confirm an unmatched item produces no sidecar at all.
