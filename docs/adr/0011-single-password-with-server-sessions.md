# ADR 0011 — Single password, server-side sessions, loopback until secured

Date: 2026-07-22 · Status: accepted

## Context

Through M2, LANcast had no authentication. Anyone who could reach the port
could stream the library, enumerate the filesystem via `GET /api/browse`, and
add a library pointing at any readable path — which together amount to
arbitrary filesystem read access. That was tolerable while the server was a
localhost experiment and untenable the moment it ran on a household network.

Three decisions had to be made together, because each constrains the others.

## Decision

**One password guards the instance.** bcrypt at cost 12, stored in
`config.json` at `0600` alongside the other secret rather than in the database.

**Sessions are server-side rows**, keyed by the SHA-256 of a 32-byte random
token. The plaintext token exists only in the caller's cookie.

**An unsecured server binds `127.0.0.1` only.** Setting a password and
restarting enables network binding.

**CSRF is defended twice:** `SameSite=Strict` on the cookie, plus an
`Origin`/`Referer` check on every state-changing method.

## Consequences

**Good — sessions are revocable.** Changing the password deletes every session,
including the caller's own. A password change that leaves old sessions alive
has not actually locked anyone out, and a self-contained signed cookie cannot
express revocation without rotating a signing key. This is the whole reason for
paying the storage cost.

**Good — a stolen database grants no sessions.** Only token hashes are stored.
The database is the easiest artifact to walk off with: it is one file, and
backups of it exist by design.

**Good — accidental exposure is impossible, not merely discouraged.** Refusing
LAN requests after accepting them would still mean a listening socket answering
strangers. Not binding at all is a different guarantee. Verified by checking
the actual listening address, not by trusting the code path.

**Good — multi-user remains cheap.** `playback_state` has been keyed by
`user_id` since revision 1 ([ADR 0006](0006-playback-state-keyed-by-user.md)),
and sessions carry a `user_id` too. Real accounts need new rows, not a
migration.

**Cost — a restart is required after first setup.** Rebinding a live listener
is fiddly, and the alternative (bind wide, reject non-loopback) gives up the
guarantee above. `setup` returns `restart_required` so the UI can explain it,
which turns a confusing dead end into a single clear instruction.

**Cost — one password, no per-person state.** Everyone in the house shares
credentials and a watch history. Accepted for now; the schema means changing
it later is additive.

**Cost — throttling sees a reverse proxy as one client.** `ClientKey` uses the
remote address and deliberately ignores forwarded headers, since those are
attacker-controlled unless a trusted proxy sets them — honouring them would let
anyone reset their own counter. Behind a proxy, rate-limit at the proxy. This
is documented rather than papered over with a config flag nobody would set
correctly.

**Cost — no lockout.** Throttle windows decay on their own. An attacker who
could lock the owner out of their own server would have denial of service for
free.

## The thing that is easy to get wrong

Adding a session cookie to an API that previously had nothing worth stealing is
what *creates* CSRF exposure. Before auth, a malicious page could already reach
`localhost:8080`; it simply had nothing to gain. Introducing credentials
without `SameSite` and an origin check would have made the application less
safe than it was with no authentication at all.

## Revisit if

More than one person needs their own watch state, or the reverse-proxy step
proves to be enough friction that built-in TLS is worth its certificate
management burden.
