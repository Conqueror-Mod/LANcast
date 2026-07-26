# Security posture

> **Status: single-password authentication.** One password guards the whole
> instance; an unsecured server binds to loopback only. This document describes
> what is protected, what is not, and what remains open.

## Current model

**One password guards everything.** Set on first run, verified with bcrypt
(cost 12), exchanged for a server-side session stored as a SHA-256 of the
token. The schema is keyed by `user_id` already
([ADR 0006](adr/0006-playback-state-keyed-by-user.md)), so real multi-user
accounts can arrive without a migration.

**An unsecured server is loopback-only.** With no password set, LANcast binds
`127.0.0.1` rather than all interfaces — the port is not open on the network at
all, not merely refusing. Rejecting requests after accepting them would still
mean a listening socket answering strangers. Setting a password and restarting
enables LAN binding.

**Why sessions live server-side.** They are revocable. Changing the password
deletes every session, including the caller's own — a password change that
leaves old sessions alive has not locked anyone out. A self-contained signed
cookie cannot express that without rotating a signing key.

**Why only the token hash is stored.** The database is the easiest thing to
walk off with: it is one file, and backups of it exist by design. A stolen
copy yields no usable sessions.

## What requires a session

Everything under `/api/` except four endpoints:

| Public | Why |
|---|---|
| `GET /api/health` | A monitor should not need credentials |
| `GET /api/auth/status` | The client must know whether to show setup or login |
| `POST /api/auth/setup` | Only works while unconfigured, which is loopback-only |
| `POST /api/auth/login` | Obviously |
| The web assets | The login form lives in them — gating the page behind a session nobody can obtain yet is a locked door with the key inside |

**Streaming is gated.** `GET /api/stream/{id}` requires a session. A public
stream URL would make the password decorative.

## CSRF

Two independent defences, because either alone leaves a gap:

- **`SameSite=Strict` on the session cookie.** A cross-site request does not
  carry it.
- **An origin check on every state-changing method.** `POST`, `PUT`, `PATCH`,
  and `DELETE` must present an `Origin` or `Referer` matching the request host.

A request with neither header is allowed: non-browser clients (curl, a TV app,
a script) legitimately send neither, and they are not the CSRF threat. Reads
are not origin-checked — the session gate covers them, and blocking
cross-origin `GET` would break embedding a stream URL.

This mattered specifically because adding a session cookie to an API that
previously had nothing worth stealing is what *creates* CSRF exposure. Before
auth, a malicious page could already reach `localhost:8080`; it just had
nothing to gain.

## Brute force

Login attempts are throttled per client IP — 10 per 5 minutes, decaying on
their own. A single shared password is one guessable secret, so unlimited
attempts against it is the entire attack.

The key is the remote address only. Forwarded headers are attacker-controlled
unless a trusted proxy sets them, and honouring them here would let anyone
reset their own counter. **If you deploy behind a reverse proxy, throttling
will see the proxy as one client** — the proxy should rate-limit instead.

There is deliberately no lockout: an attacker who could lock the owner out of
their own server has achieved denial of service for free.

## Transport security

**A server reachable beyond loopback serves HTTPS**
([ADR 0014](adr/0014-transport-security.md)). The password on login and the
session cookie on every request are therefore encrypted on the wire rather than
readable by anyone on the network path — which, on a semi-trusted LAN, is the
gap that mattered most.

**A loopback-only server stays plain HTTP.** When no password is set the server
binds `127.0.0.1` only; nothing on that wire is worth protecting, and a
certificate warning on `localhost` is pure setup friction. TLS turns on at the
same boundary that enables LAN binding.

**The certificate is either supplied or self-signed.** Set `tls_cert_file` and
`tls_key_file` to serve a certificate you trust — from an internal CA, `mkcert`,
or copied in — and clients connect with no warning. This is the recommended
configuration. With neither set, LANcast generates a self-signed certificate on
first LAN-bound run, persists it `0600` under `<data>/tls/`, and reuses it
across restarts. A self-signed cert **encrypts the wire but does not
authenticate the server**: the first HTTPS visit shows a browser warning until
the cert is trusted or replaced.

**Enabling TLS does not break `http://` bookmarks.** The listening port answers
a plaintext request with a permanent redirect to the same address over HTTPS,
so an old bookmark upgrades transparently instead of failing a TLS handshake.

**No ACME / Let's Encrypt.** Automatic public certificates require the server to
be internet-reachable and to contact an external CA — public exposure and
phone-home, both of which LANcast rejects. For a publicly trusted certificate,
terminate TLS at a reverse proxy.

## Still unprotected

- **One password, no accounts.** No per-user watch state, no revoking one
  person's access without changing it for everyone.
- **No audit trail.** Nothing records who changed what.
- **No API rate limiting** beyond login.

## Standing risks that authentication does not remove

**Adding a library is arbitrary read access.** `POST /api/libraries` accepts
any path that exists and is readable. An authenticated client can point it at
`C:\Users`, scan, and stream every video underneath. `GET /api/browse` makes
finding such a path trivial. That is acceptable when the only authenticated
party is the owner — and it is exactly why the password matters.

## What is protected

These hold regardless of authentication and should not regress.

**Stream containment.** `GET /api/stream/{id}` re-resolves the item path with
`filepath.Abs` and verifies it still falls inside the owning library root
before opening anything. The database is trusted, but a hand-edited or
corrupted row must not become arbitrary file read access. Covered by tests
asserting both an outside path and a traversal path are refused *and* that the
file contents are not served.

**Artwork hashes are validated.** A hash arrives from a URL path and becomes a
filesystem path, so it is checked for length and hex-only characters first.
Traversal attempts are a cache miss, never a file read.

**Filesystem paths are never serialized.** `Item.Path` is `json:"-"`. Clients
have no use for server paths, and withholding them keeps the layout private
even when the API is reachable.

**The TMDB key is write-only.** Stored `0600` in `config.json`, never in the
database, and `GET /api/settings` reports only `configured: true`. A secret
readable back out of an API leaks through screenshots, logs, and shared
sessions.

**Browse lists directories only.** Never files, never contents, and hidden or
system directories are omitted.

## What is not protected

- **No authentication or authorization**, anywhere.
- **No CSRF protection.** A page in your browser can issue requests to
  `localhost:8080` and to LAN addresses. With no auth there is nothing to
  steal, but the moment a session cookie exists this becomes urgent — the same
  request that creates a library from a malicious page would then carry your
  session.
- **No rate limiting on the API.** Only outbound provider calls are limited.
- **No audit trail.** Nothing records who changed what.

## Deployment guidance

**Do not port-forward LANcast directly.** There is a password and TLS now, but a
self-signed certificate cannot prove the server's identity to a client that has
never seen it, and exposing the login endpoint to the whole internet invites
credential-guessing that per-IP throttling only blunts. A VPN keeps the server
off the public internet entirely.

**Recommended: a VPN that puts your device on the LAN.** Tailscale and
WireGuard both do this well. Nothing is exposed publicly, traffic is encrypted
by the VPN, and LANcast needs no configuration at all. This keeps the
no-phone-home principle intact and avoids reimplementing solved problems badly.

**Alternative: a reverse proxy terminating TLS.** Caddy or nginx in front of
LANcast, with a publicly trusted certificate — the way to get a real certificate
without LANcast contacting a CA itself. Forward the original client IP only if
you also rate-limit at the proxy — LANcast's throttle will otherwise see every
request as coming from one client. Point the proxy at LANcast over HTTPS, or run
LANcast loopback-only behind it.

Built-in TLS ([ADR 0014](adr/0014-transport-security.md)) closes the plaintext
hole on the LAN; it is not a substitute for either of the above when reaching
LANcast from outside the network.

Treat the LAN itself as semi-trusted. A compromised phone, TV, or IoT device on
your network can reach the login endpoint and is inside the boundary LANcast
relies on.

## Threats explicitly out of scope

- **A user with filesystem access to the data directory.** They can read the
  database and the TMDB key directly; the application cannot defend against
  its own host.
- **Malicious media files.** LANcast serves bytes and does not parse container
  internals. This changes at M3, when ffmpeg begins processing untrusted input.
- **Denial of service.** A large enough scan or enough concurrent streams will
  exhaust resources. Acceptable for a household server.

## Open decisions

Tracked in [roadmap.md](roadmap.md):

1. ~~**Transport security.**~~ **Resolved** — built-in TLS with bring-your-own or
   self-signed certificates ([ADR 0014](adr/0014-transport-security.md)). A VPN
   or reverse proxy remains the path for reaching LANcast from outside the LAN.
2. **Multi-user accounts.** The schema is ready. Worth doing when more than one
   person in the house wants their own watch state.
3. **Session management UI.** Listing and revoking individual sessions, rather
   than the all-or-nothing password change.
4. **ffmpeg and untrusted input.** From M3, LANcast will parse media files
   rather than only serving bytes. That is a genuinely new attack surface and
   deserves its own review.
