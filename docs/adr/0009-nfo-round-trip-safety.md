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

### Per-field provenance — decided, and built

The question left open above is now answered: **an edit authors only the field
that changed.**

The marker gained a second attribute, and the scheme became v2:

```xml
<lancast generated="…" hash="sha256:v2:9f2c…" fields="title:ab12…,year:cd34…,…"/>
```

The whole-record hash still answers *did anything change*. The per-field digests
answer *what changed*, and that is the difference between honouring a title
correction and promoting the plot, cast and rating that happened to sit beside
it. On read, a file whose marker carries per-field digests yields a record
containing only the fields whose digests no longer match — everything else is
absent, so providers keep filling it exactly as before.

This falls out of the existing precedence rather than complicating it: locals
already outrank providers per field, so returning fewer fields is all that was
needed.

Three cases kept deliberately:

- **A cleared field is not an instruction.** A title emptied in the file reads
  as no opinion, not as "blank this". A provider filling an empty title is a
  better failure than a parser disagreement silently erasing one.
- **A marker with no per-field digests keeps the old behaviour.** Files written
  before v2 make the whole file authoritative, which is blunter and still
  correct.
- **A foreign sidecar is wholly authoritative**, unchanged. No marker means
  another tool wrote it and none of it is ours to second-guess.

**An older scheme stays verifiable.** v1 and v2 digest the record identically —
only the marker's shape grew — so a v2 build still checks a v1 marker properly.
Getting this wrong was caught by a test: an early version treated any
non-current scheme as unverifiable, which would have silently stopped honouring
edits on every sidecar already on disk. That is a worse bug than the one
versioning was introduced to prevent, and it is the reason the rule is "a
version we do not implement", not "a version that is not current".

## Verification

- Write an NFO, rescan, confirm provider updates still apply (mirror detected).
- Hand-edit that NFO, rescan, confirm the edit now wins.
- Confirm unknown elements survive a rewrite untouched.
- Confirm a marker whose scheme is from a newer build is treated as a mirror,
  not as an edit.
- Confirm an unmatched item produces no sidecar at all.
- Edit one field of a LANcast sidecar by hand; confirm that field wins and the
  others stay open to providers.
- Confirm a v1 marker with a hand edit is still honoured by a v2 build.
