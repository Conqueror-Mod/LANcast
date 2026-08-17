# ADR 0026 — Audit log

Date: 2026-08-08 · Status: accepted · shipped in v0.6.1

## Context

During v0.4.x testing a library lost its contents and the question "what
emptied this library" had no answer anywhere. Not in the database, which holds
current state and no history; not in `lancastd.log`, which by then recorded
startup and failure but not mutations; not in the client, which had already
navigated away. The library was rebuilt by rescanning and the cause was never
established.

That is the gap. LANcast has a growing set of destructive and semi-destructive
operations — removing a library, deleting an item, overriding a match, resetting
a password, creating and deleting accounts, granting a plugin a capability — and
none of them leave a trace that outlives the request.

Two forces make this more pressing than it was.

**Multi-user accounts shipped** ([ADR 0015](0015-multi-user-accounts.md)). While
a server had one password, "who did this" had one answer. With admin and member
roles and several named accounts, an unattributed mutation is genuinely
ambiguous, and the person who has to answer the question is the owner rather
than the actor.

**Plugins execute third-party code** ([ADR 0020](0020-plugin-isolation-boundary.md),
[ADR 0021](0021-plugin-distribution-and-trust.md)). The capability grant is an
explicit act with a trust decision behind it. A record of when a capability was
granted, and by whom, is part of what makes that decision reviewable rather than
merely made.

The roadmap states the constraint that shapes the whole design: **an audit trail
a client writes is forgeable by the client it is auditing.** This has to live
where the mutation is authorised, not where it is requested.

Today's session added a third motivation. Twice in one day, "what happened here"
took extended forensics against the live database — once for a title that
reverted, once for a scan that failed — and in both cases the answer would have
been one query against a table that did not exist. The absence is not
hypothetical.

## Decision

**A server-side, append-only audit log in the existing database, written inside
the same transaction boundary as the mutation it records where practical, and
readable only by administrators.**

### Where it is written

In `internal/api`, at the handler, *after* authorisation succeeds and the
mutation returns without error. Not in `internal/store`, and not in the client.

The store is the wrong layer because it cannot see the actor: `Store` methods
take a `context` and typed arguments, not a session. Threading a session through
every store method to satisfy auditing would make the audit requirement leak
into every signature in the package, which is exactly the sort of pervasive
change that gets reverted the first time it is inconvenient.

The handler is the right layer because it is where authorisation already
happens. `adminOnly` and `s.userID(r)` are both there. An action that was
authorised is an action that can be attributed, and those are the same place.

### Shape

One table, revision 15:

```sql
CREATE TABLE IF NOT EXISTS audit_event (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    at          INTEGER NOT NULL,          -- unix seconds
    actor_id    TEXT    NOT NULL,          -- users.id, or 'local' when unsecured
    actor_name  TEXT    NOT NULL,          -- denormalised on purpose, see below
    action      TEXT    NOT NULL,          -- 'library.delete', 'item.match', ...
    target_kind TEXT,                      -- 'library' | 'item' | 'user' | 'plugin'
    target_id   TEXT,                      -- id as text: not every target is an int
    summary     TEXT    NOT NULL,          -- one human sentence, resolved server-side
    detail      TEXT                       -- optional JSON, for what a sentence cannot carry
);
CREATE INDEX IF NOT EXISTS idx_audit_at ON audit_event(at DESC);
```

**`actor_name` is denormalised deliberately.** A foreign key to `users` would
either cascade-delete the evidence when an account is removed, or block the
removal. "Who deleted this library" must survive the deletion of the account
that did it — that is precisely the case an audit log exists for. The id is kept
alongside for joining while the account still exists.

**`target_id` is TEXT** because targets are not uniformly integers: plugins are
named, users have string ids, items and libraries are integers. One column that
holds all of them beats four nullable typed columns.

**`summary` is resolved at write time**, so a deleted library still reads
"Removed library Films (14 items)" rather than "library 3", which would be
unresolvable the moment the row it names is gone. This is the same rule the
activity view follows for scan titles.

### What is recorded

Everything that changes state a user could later be surprised by:

| Action | Why |
|---|---|
| `library.create`, `library.delete` | The v0.4.x question, exactly |
| `item.delete` | Removal, whether ignore or delete-from-disk |
| `item.match` | Identity override — the correction that outranks providers |
| `item.edit`, `item.lock`, `item.unlock` | Field edits and locks (ADR 0008) |
| `user.create`, `user.delete`, `user.password_reset`, `user.role_change` | Account changes an owner must be able to review |
| `auth.password_change` | Own-password change; revokes sessions |
| `plugin.install`, `plugin.grant`, `plugin.enable`, `plugin.disable`, `plugin.remove` | The trust decisions of ADR 0021 |
| `settings.update` | Provider keys, NFO writing, LAN binding |

**Reads are not recorded.** Browsing, playback and progress are not audit
events: they are the normal operation of a media server, they would swamp the
table, and per-user watch history already exists as product surface rather than
as an audit trail. This is an audit log, not telemetry, and the distinction is
what keeps it readable.

**Scans are not recorded** beyond the library they targeted, because the
activity view already reports scan progress and outcome, and a scan is not a
decision anyone needs attributed.

### What is not decided here

**Retention.** The table grows without bound. Given the recorded set excludes
reads, a busy household generates events at the rate of deliberate acts — tens
per week, not thousands per day — so a cap is not needed to make this shippable.
When it is needed, the choice is between a row cap and an age cap, and it should
be made against a real table rather than guessed at now.

**Whether identity should live in its own store.** The roadmap pairs this with
the audit log: if losing the library database means losing password hashes, that
is a different blast radius than losing the library. It is a real question and it
is not this one. Recorded here so it is not lost:
the audit log does not make it worse, because the log holds no secrets.

## Consequences

**Every mutating handler gains one line.** That is the cost, paid once per
handler, and it is visible in review — an audit call missing from a new handler
is a missing line in a diff, not a silent failure of an abstraction.

**A failed audit write must not fail the request.** The mutation already
happened and already returned; refusing it after the fact would be worse than
losing the record. Audit failures are logged at `ERROR` and swallowed. This is a
real weakening — an attacker who can reliably make audit writes fail can act
unlogged — but the alternative, refusing the user's deletion because a log write
failed, is a denial of service triggered by a full disk.

**The log is admin-only.** It names filesystem paths, library roots and account
changes, which is operator information for the same reason `GET /api/logs` is.

**`GET /api/audit`**, paginated, newest first, filterable by `action` and
`actor_id`. A new endpoint, additive, so `docs/api.md` gains a section and the
contract stays version 1 ([ADR 0018](0018-api-contract-and-versioning.md)).

## The thing that is easy to get wrong

**Writing the audit row where it can lie.** Two ways this happens. Recording
intent before the mutation, so a failed delete is logged as a delete — the log
must record what happened, not what was attempted. And resolving names at read
time, so a row whose target is gone renders as an id or as nothing, which is
exactly the case the log exists to survive.

**Making it an interceptor.** Middleware that audits by inspecting method and
path looks tidier and cannot produce a good `summary`: it does not know that
`DELETE /api/items/42` removed *Antz (1998)* and left the file on disk. A
generic layer would record the shape of the request and lose the meaning, which
is the part a human reads.

**Auditing reads.** It is tempting because it is easy, and it would bury the
eight events a month that matter under a million that do not.

## Revisit if

- **The table outgrows one query.** Then retention becomes a decision, and
  pagination alone stops being enough.
- **A second writer appears** — a plugin with a capability to mutate library
  data would need its own attribution, and `actor_id` would need to express
  "plugin X acting under grant Y" rather than a user.
- **Tamper-evidence is wanted.** Append-only by convention is not append-only by
  construction: anyone with the database file can edit it. Hash chaining is the
  standard answer and is deliberately not built now, because the threat model
  here is "which of my housemates did this", not "an attacker with filesystem
  access is covering their tracks" — that attacker can already edit the media
  library itself.
