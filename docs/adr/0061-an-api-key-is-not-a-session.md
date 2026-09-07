# ADR 0061 — An API key is not a session

Date: 2026-09-06 · Status: **accepted** 2026-09-06

The admin restriction was the part put up to be argued with, and it was
accepted as drawn.

**Extends:** ADR 0014 (TLS and the loopback rule), ADR 0015 (accounts),
ADR 0018 (API versioning). Does not supersede anything.

## Context

`docs/openapi.json` describes 141 operations and the build refuses to let it
drift from the router. A third party can now know exactly what this server
offers.

They still cannot authenticate against it in any way meant for them. The only
credential is a session cookie obtained by posting a password to
`/api/auth/login`, which means a script must store somebody's password, and a
long-running integration must keep a cookie alive against a `SessionTTL` that
was chosen for a person watching a film. The one memory this project keeps about
third-party clients is about fighting the cookie's `Secure` attribute across a
scheme change, which is the shape of a workaround rather than a design.

The Immich study put API keys fourth on its list and called them "small,
unblocks third-party clients and any future CLI". They are small. The parts that
are not small are the two below, and they are the reason this is an ADR rather
than a table and a handler.

## Decision

### A key is presented in a header, never a query parameter

`Authorization: Bearer <key>`.

A credential in a URL is a credential in the server log, the browser history,
the `Referer` of the next request, and any proxy in between. The convenience of
`?key=` is real and it is not worth any of that.

### A key resolves into the same shape a session does

The middleware turns a valid key into the `store.Session` value that
`requireAuth` already stashes, and every handler downstream continues to read
`s.userID(r)` and `adminOnly` without knowing which kind of caller it has.

The alternative — a second authorization path beside the first — is how one of
the two paths ends up missing a check that the other has. There is exactly one
place that decides who the caller is.

### The CSRF check is skipped for key-authenticated requests, and only those

`requireAuth` refuses any state-changing request whose `Origin` or `Referer`
names another host. That check exists because **a browser attaches cookies by
itself**: the whole attack is a third-party page causing the victim's browser to
send a request that carries the victim's ambient credential.

Nothing attaches an `Authorization` header by itself. A cross-origin page cannot
make the browser add one, so a key-authenticated request cannot be forged in the
way the check defends against, and a legitimate browser-based client using a key
would otherwise be refused on every write it makes.

So the skip is conditional on the request being *authenticated by the key*, and
the cookie path keeps both of its defences exactly as they are. CLAUDE.md says
CSRF is defended twice and to keep both; this does not spend either of them, it
declines to apply one to a caller it was never about.

The failure to avoid is a blanket skip — "if an `Authorization` header is
present, skip CSRF" — which lets any page turn the check off by adding a
meaningless header while the cookie still does the authenticating.

### A key never grants admin, whoever owns it

This is the decision most likely to be argued with, so it is the one with the
most reasoning.

Admin surfaces in LANcast are not "more of the same". Adding a library is
**arbitrary filesystem read access at a path the request chooses**; that is the
sentence CLAUDE.md uses about why the loopback rule exists at all. Restoring,
resetting auth and managing users sit beside it.

A session is bounded by a person sitting in front of the app: it expires, it
dies when the password changes, and it exists because somebody typed a password
minutes ago. A key is the opposite of all four — long-lived, stored in a config
file on another machine, used unattended, and often committed to something by
accident. Giving that credential the authority to mount `C:\` and read it back
over HTTP is a different risk class from giving it the authority to list films.

So: **admin-gated routes require a session.** A key authenticates as its owner
for everything else, and `adminOnly` refuses it even when the owner is an
administrator, with a message that says why rather than a bare 403.

If a CLI later needs to drive administration, that is a separate decision with
its own reasoning, and it should arrive as an explicit capability rather than as
a quiet consequence of this one.

### Keys are stored hashed and shown once

The same treatment sessions already get: `auth.HashToken` on the way in, the
plaintext returned exactly once at creation and never retrievable afterwards. A
stolen database yields no usable keys.

The screen has to be honest that the value will not be shown again, because the
alternative is somebody closing the dialog and needing a new key — which is a
worse outcome than a clear warning, but a much better one than a database full
of usable credentials.

### Changing a password does not revoke keys

Changing a password deletes **every** session including the caller's own, and
that is deliberate (CLAUDE.md: "that is the point of server-side sessions, not a
bug to smooth over").

Keys are not swept up in it. A person changing their password because it is
their turn to rotate it should not silently break the machine that has been
copying their photographs for a year, and a person changing it because they
believe it was stolen needs to revoke keys **deliberately**, which means a list
with a revoke button and a last-used column that tells them which is which.

Stated here because the existing intuition is "changing the password logs
everything out", and this is a documented exception rather than an oversight.

### A key request does not make its owner appear online

`requireAuth` calls `presence.Seen` on every authenticated request, which is
what makes "online" a fact rather than a guess. A cron job polling every minute
is not a person being present, and letting it say so would make the one signal
this project has about who is actually here permanently wrong for anybody with
an integration.

## Consequences

**Schema revision 42** — an `api_key` table holding the token hash, the owning
user, a name, created and last-used timestamps. Additive, so a downgrade
survives it.

**Additive API only** (ADR 0018): new routes under `/api/keys`, and no change to
any existing response. Both halves of the contract in the same commit.

**The loopback rule is untouched.** A server with no password still binds
`127.0.0.1` and nothing else; a key is not a way to widen that, and creating one
requires being signed in first.

**Rate limiting is not in this.** It is a real gap for a credential meant to be
used unattended, and bolting a limiter onto one route while the rest of the API
has none would be security theatre. Noted as its own decision.

**What would make this wrong:** if the admin restriction turns out to block the
first real thing somebody wants a key for. That would mean the boundary is drawn
in the wrong place, and the honest response is another ADR moving it — not a
flag that quietly grants admin.
