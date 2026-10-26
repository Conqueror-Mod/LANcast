# ADR 0058 — A backup is the database

Date: 2026-09-03 · Status: **accepted**

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
handing them back undoes that. They are cleared — see the amendment below,
which moves *when*.

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

## Amendment — sessions are cleared at snapshot time, 2026-09-03

Status: **accepted**, amending the consequence above.

The original said sessions are cleared **on restore**. That is the wrong end,
and this ADR's own last paragraph is what gives it away: the shape of backup
this decision is built around is *"a snapshot somebody can copy to a USB
stick"*. A file copied to a stick and put back by hand never executes restore's
code at all — so a restore-time rule would be absent in exactly the case the
whole decision was designed for.

Worse, it would be absent silently. The backup would look identical, restore
cleanly by hand, and hand back every login it was carrying, with nothing
anywhere reporting that it had.

**Sessions are therefore cleared in the snapshot itself, immediately after
`VACUUM INTO`, before the file is reported as good.** A backup carries records,
not credentials, and that is now a property of the *file* rather than of the
code that happens to read it. It survives being copied, renamed, moved between
machines, and restored by somebody who never ran `lancastd restore`.

Three things make this cheap rather than a second mechanism to maintain.

**The snapshot is already delete-journalled.** `VACUUM INTO` produces a
database in `delete` mode, not WAL — checked, not assumed, the same way the
snapshot timing was. So writing to it leaves no `-wal` sidecar, and a backup
whose contents depend on a sidecar is a backup somebody copies half of. The
write is stated as `journal_mode(DELETE)` anyway, because relying on an
inherited default is how that stops being true later.

**A failure takes the snapshot with it.** A backup that still held sessions
must never be handed back as good, so anything going wrong in the clearing
removes the file. There is no state in which the operation reports success and
the property does not hold.

**The live server is untouched.** Taking a backup does not sign anybody out of
the server they are using — the deletion happens in the copy.

The restore path still clears sessions as well. That is deliberate and is not
redundancy for its own sake: backups written before this amendment carry
sessions, and they stay restorable for as long as the schema allows, which is
the entire point of having backups. The restore-time clearing is what covers
them.

Nothing else in the decision changes. Restoring is still offline, artwork is
still excluded, and a backup from a newer build is still refused by name.

## Amendment — a backup before every migration, 2026-10-26

Migrations only go forward, so the file that would undo a bad one is the one
taken just before it ran, and that is the backup nobody takes: an update
arrives from the tray or the in-app updater, and the migration runs on the way
back up with nobody watching. Now the server takes it.

**When.** At startup, in `store.OpenWith`, when a migration is pending on a
database that existed before the open. A new install also starts at revision 1
and migrates all the way up, and a snapshot of an empty database protects
nothing, so a missing or zero-byte file is skipped. A start that changes
nothing takes nothing.

**Where and what.** In the ordinary backups folder, by the ordinary snapshot,
so it is listed, downloadable and restorable like any other, with sessions
cleared. Named `lancast-backup-<time>-schema<N>-premigrate.db`, `N` being the
revision it holds, and listed with `pre_migration: true`.

**How many.** The newest three. Older ones are deleted only after the next one
is written. They are never counted toward the schedule's `backup_keep`, and the
schedule never deletes them; a backup taken by hand is still never deleted by
anything.

**When it fails, the server starts anyway.** Refusing to start would leave the
service down after an update with nobody at the machine, for a migration that
ran without a safety copy on every update before this one. The failure is
logged as `backup before the database update failed`.

**One thing it found.** Clearing sessions from the copy assumed a `session`
table, and a database old enough to predate it made the snapshot fail. A
database without the table has no sessions to remove, and is now treated so.

## Amendment — restore from Settings, 2026-10-26

The decision above made restoring an offline command because a server cannot
replace the database it is reading. That stays true; what changes is who runs
the offline step. **Restore** on a backup in Settings stages it, and the server
restarts itself; the next start applies it before anything opens the database,
with the same `RestoreSnapshot` the command uses.

- **A backup of the database as it is now is taken first**, by hand, so it is
  never rotated away. Restoring the wrong backup is undone from the same list,
  not from the `.replaced-…` file beside the database that only somebody at the
  machine can reach. The restore still leaves that file too.
- **One attempt.** The staged marker is removed before the restore is tried, so
  a backup that fails to restore cannot hold the server in a loop of failing
  starts. The server starts with the database that was there, and the outcome
  is written down for Settings to show after the restart.
- **Only where the server can restart itself**: as a service, or as a process
  the relaunch helper can start again. Anywhere else the button is not offered
  and the command is shown, as before. The command is shown beside the button
  in every case, because it is the way back when the server will not start.
- Everyone is signed out, as with the command.

**One thing it found.** The relaunch helper copied `os.Args`, from which `Main`
had already stripped `serve` (ADR 0080), so a server the LANcast window started
as `LANcast.exe serve …` came back as the window. Finishing an update on an
install that is not a service had the same fault from v0.9.79; a service never
takes that path. The relaunch now puts `serve` back.
