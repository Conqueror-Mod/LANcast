# ADR 0058 — A backup is the database

Date: 2026-09-03 · Status: **proposed**

The roadmap's *"backup and restore — rebuild a library without a full rescan"*,
unplanned since M3.

## What is actually at risk

A rescan can rebuild most of a library, given hours. What it can rebuild is
worth separating from what it cannot, because only the second kind makes this
urgent.

**Re-derivable, slowly.** On the library measured here: 26,342 probe results
(hours of ffprobe), metadata and artwork for 18,777 items (network, an API key,
and rate limits), 16,007 people and 26,990 credits.

**Not re-derivable at any price.** Watch history and positions, ratings,
playlists and their membership locks, collections, sensitive marks, and every
**locked field** — which is to say every correction a person made by hand. A
rescan reconciles *files*; ADR 0008 and the locked-fields rule exist precisely
so that it does not re-litigate identity, and the flip side of that guarantee
is that nothing can reconstruct those decisions from the media.

Today none of it is protected. There is no backup of any kind.

## Decision

**A backup is a snapshot of the database, taken with `VACUUM INTO`, and nothing
else.**

Measured on the real library, from a live server with no downtime:

```
source 104 MB -> snapshot 103 MB in 608ms
```

`VACUUM INTO` is the whole reason this is simple. It writes a consistent
snapshot of a database that is being used, which a file copy cannot do — the
WAL means copying `lancast.db` alone yields a file that is torn, plausible, and
wrong. Verified against `modernc.org/sqlite`, the driver LANcast actually
ships, rather than assumed from SQLite's documentation.

**Artwork is deliberately excluded.** The cache is **4.6 GB against the
database's 100 MB** — forty-six times the size — and it is content-addressed
and re-fetchable. Including it would turn a one-second operation somebody
might do daily into a multi-gigabyte one they do never, and a backup nobody
takes protects nothing.

The cost is stated rather than hidden: after a restore, posters arrive again
over the following hours through the enrichment worker, and any artwork a
provider has since withdrawn does not come back. That is the trade, and it is
the right one — a missing poster is a cosmetic loss, a missing watch history is
not.

## Consequences

**Restoring is offline, and that is not a limitation to smooth over.** The
server stops, the file is replaced, the server starts. A live restore would
mean swapping the database under open transactions, which is how a restore
becomes the incident.

**A backup from a newer build must be refused, loudly.** Migrations are
one-way: an older backup restored into a newer build migrates forward and is
fine, while a newer backup opened by an older build gets *"database is schema
version N but this build supports N-1"* — the failure that has already cost
this project once. A restore reads the snapshot's `schema_version` first and
says which build is needed rather than letting the server fail to start.

**Media that moved is a re-point, not a rescan.** A library restored onto a
machine where the files live at a different path has correct rows pointing at
the wrong places. Library locations are already editable (ADR 0034), so the
answer is to correct the location and let a scan reconcile — which is the whole
promise of the roadmap item, and it works because `probed_at` and
`metadata_updated_at` survive in the backup.

**The provider cache rides along.** It is 3,730 rows and expires in seven days
regardless, so excluding it would be a special case that saves nothing.

**Sessions ride along too, and should not.** A restored backup would carry
live session rows, so anyone holding a cookie from the backup's era is signed
in again. Sessions are server-side precisely so they can be revoked; a restore
handing them back undoes that. They are cleared on restore.

## Alternatives rejected

**Copy the database file.** The obvious approach and it is wrong: with WAL
enabled, a copy taken while the server runs is inconsistent, and the resulting
file is *plausible* rather than obviously broken — which is the worst failure a
backup can have, because it is discovered at restore time.

**Export to a portable format — JSON, or per-library sidecars.** Attractive
because it survives a schema change and can be read by other tools. Rejected
because it is a second representation of the whole data model that has to be
kept in step with the first, and every schema revision becomes two pieces of
work. The database already has a migration path; a JSON export would need its
own, and the one thing worse than no backup is a backup that silently drops the
column added last month.

**Include artwork.** Forty-six times the size for something re-fetchable. It
belongs behind an explicit "and the images too" option later, not in the
default that decides whether anybody backs up at all.

**Continuous replication to a second file.** Solves a problem nobody has: this
is a home server whose failure mode is a disk dying or a person moving
machines, not a transaction lost in the last second. A snapshot somebody can
copy to a USB stick is the shape of backup that gets used.
