# ADR 0015 — Multi-user accounts

Date: 2026-07-26 · Status: accepted · shipped by v0.3.2

## Context

LANcast has always been single-password: one secret guards the instance, and
every request acts as the same identity, the constant `local`
([ADR 0011](0011-single-password-with-server-sessions.md)). The schema was built
for more from the start — `playback_state` and `session` are keyed by a
`user_id TEXT` column, defaulting to `'local'`
([ADR 0006](0006-playback-state-keyed-by-user.md)) — precisely so that real
accounts could arrive without discarding watch history. This ADR cashes that in.

Two things make it worth doing now rather than later. A household has more than
one viewer, and a shared watch history is the single most-felt limitation of the
current model: everyone's resume points and "watched" flags collide. And the
security posture has a standing hole that accounts are the right tool to close —
`POST /api/libraries` accepts any readable path, so *any* authenticated caller
can scan and stream `C:\Users`. Today that is acceptable only because the sole
authenticated party is the owner. The moment a second, less-trusted person has a
login, "everyone is the owner" stops being tenable.

## Decision

**Users become rows in a `user` table**, added by migration 7:

```
user(
  id            TEXT PRIMARY KEY,   -- opaque; the migrated owner keeps 'local'
  name          TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,      -- bcrypt, cost 12
  role          TEXT NOT NULL,      -- 'admin' | 'member'
  created_at    INTEGER NOT NULL
)
```

**The existing password becomes the first admin, with `id = 'local'`.** Every
`playback_state` and `session` row already carries `user_id = 'local'`, so the
owner's resume points, watched flags, and current login survive untouched — no
rewrite, no rescan, no lost history. The migration adds the table; a startup
step seeds the `local` admin from the password hash currently in `config.json`,
because SQL cannot read that file. After seeding, the password hash lives only in
the database and is removed from settings — one home for the user directory, not
two.

**Two roles, and the distinction is a real privilege boundary, not a label.**

- **admin** — manages users, creates and deletes libraries, edits server
  settings (TMDB key, encoder, TLS). Adding a library is arbitrary filesystem
  read access, so it is an admin-only power.
- **member** — browses, plays, and owns their own watch state and subtitle
  preferences. A member cannot create a library, cannot reach settings, and
  cannot see or manage other users.

This is what makes a second account safe to hand out: a member is scoped to
watching, not to the filesystem.

**"Secured" now means at least one user exists.** The loopback-until-secured
guarantee is unchanged in spirit — an instance with no accounts still binds
`127.0.0.1` only. `Settings.Secured()` moves from "a password is set" to "a user
row exists," and first-run setup creates the first admin instead of setting a
bare password.

**Changing your own password revokes only your own sessions.** Under one shared
password, a change revoked *every* session because there was one identity to lock
out. With accounts, that behaviour would let one person log everyone else out.
Deleting a user cascades to their sessions and playback state; an admin resetting
another user's password revokes that user's sessions, not the whole instance.

**CSRF, throttling, and the origin check are unchanged.** They already operate
below identity. The login throttle stays keyed on remote IP.

## Consequences

**Good — the arbitrary-read-access hole becomes bounded.** The standing risk
that [security.md](../security.md) documents — any authenticated client can
point a library at any readable path — shrinks to *any admin*, which is the
owner and whoever they deliberately promote. This is the first time that risk
has an answer better than "only give the password to people you'd give the
filesystem to."

**Good — no history is lost.** Keying the migrated owner to the pre-existing
`'local'` id means the entire point of ADR 0006 pays off exactly as intended.

**Good — the change is mostly deletion of a constant.** The API already threads
`localUser` through every per-user call; multi-user replaces that constant with
the session's real `user_id`. The plumbing is in place, which is why this is a
feature and not a rearchitecture.

**Cost — password hashes now live in the database.** ADR 0011 deliberately kept
the single hash in `config.json` and out of the DB, on the reasoning that the
database is the easiest artifact to walk off with. A user *directory* has no
natural home except the database, so this reverses that specific choice. It is
an acceptable reversal: bcrypt at cost 12 exists precisely to be stored and to
resist offline cracking, and the property that actually mattered — that a stolen
database yields no usable *sessions* — is untouched, because only token *hashes*
are stored and a bcrypt hash is not a login.

**Cost — first-run and setup change shape.** Setup creates an admin (name +
password) rather than a lone password, and the client needs a minimal user-
management surface for admins. That is new UI, scoped to admins.

**Cost — a migration that a startup step completes.** Migration 7 is pure SQL
and cannot read `config.json`, so the owner is seeded in code at startup when the
`user` table is empty but a legacy password hash is present. This split is
called out because a migration that looks complete in SQL but depends on a
startup step is exactly the kind of thing that rots silently — the seeding step
must be covered by a test that starts from a revision-6 database plus a
`config.json` password and asserts a `local` admin appears.

## The thing that is easy to get wrong

Authorization checks must live on the server, keyed off the session's role —
never on the client hiding a button. A member who crafts `POST /api/libraries`
by hand must be refused by the handler, not merely never shown the form. The
privilege boundary is only real if the admin-only endpoints check the role of
the calling session on every request, the same way stream containment is
re-verified on every request rather than trusted from an earlier check.

## Revisit if

Per-user library *visibility* is wanted — hiding a library from some members
rather than the current all-members-see-all-libraries model. That is a genuinely
larger design (an access-control list per library) and is deliberately out of
scope here: this ADR establishes identity and a coarse admin/member split, which
is the prerequisite for any finer-grained sharing later.
