# Security posture

> **Status: no authentication exists.** This document describes what LANcast
> protects today, what it does not, and what an attacker on your network can
> currently do. It is deliberately written before auth is designed, so the
> design starts from an accurate picture rather than an optimistic one.

## Current model in one sentence

LANcast trusts everyone who can reach the port.

There is no login, no token, no session. Every endpoint — including the ones
that enumerate directories and stream files — answers any request that arrives.
The only thing standing between your library and a stranger is that the server
binds to a LAN address and your router does not forward the port.

## What an unauthenticated client on your LAN can do today

| Action | Endpoint |
|---|---|
| List and stream every file in every library | `GET /api/stream/{id}` |
| Enumerate your filesystem, directory by directory | `GET /api/browse` |
| Add a library pointing at **any** readable path | `POST /api/libraries` |
| Read and overwrite metadata, locks, and watch state | `PATCH`, `PUT`, `DELETE` |
| Toggle NFO writing into your media folders | `PUT /api/settings` |
| Trigger scans and metadata refreshes | `POST .../scan`, `.../refresh` |

The two worth dwelling on:

**Adding a library is arbitrary read access.** `POST /api/libraries` accepts any
path that exists and is readable. Point it at `C:\Users`, scan, and every video
file underneath becomes streamable. `GET /api/browse` makes finding such a path
trivial. Neither grants anything the other did not already imply, but together
they turn "reachable on the LAN" into "readable filesystem".

**Settings are writable.** Anyone who can reach the server can enable NFO
writing, which causes LANcast to write files into media folders.

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
- **No transport security.** Everything is plaintext HTTP. Credentials do not
  exist yet, but watch history, library contents, and the TMDB key in transit
  during a settings write are all readable by anyone on the path.
- **No CSRF protection.** A page in your browser can issue requests to
  `localhost:8080` and to LAN addresses. With no auth there is nothing to
  steal, but the moment a session cookie exists this becomes urgent — the same
  request that creates a library from a malicious page would then carry your
  session.
- **No rate limiting on the API.** Only outbound provider calls are limited.
- **No audit trail.** Nothing records who changed what.

## Deployment guidance until auth exists

**Do not port-forward LANcast.** Not to test it, not briefly. There is no
authentication; exposing it publishes your library and hands out filesystem
enumeration.

For access away from home, use a VPN that puts your device *on* the LAN —
Tailscale and WireGuard both do this well — rather than exposing the port.
That is also the long-term recommendation: it keeps the no-phone-home
principle intact and avoids reimplementing solved problems badly.

Treat the LAN itself as semi-trusted. A compromised phone, TV, or IoT device on
your network is already inside the boundary LANcast relies on.

## Threats explicitly out of scope

- **A user with filesystem access to the data directory.** They can read the
  database and the TMDB key directly; the application cannot defend against
  its own host.
- **Malicious media files.** LANcast serves bytes and does not parse container
  internals. This changes at M3, when ffmpeg begins processing untrusted input.
- **Denial of service.** A large enough scan or enough concurrent streams will
  exhaust resources. Acceptable for a household server.

## Open decisions

Tracked in [roadmap.md](roadmap.md) under security and remote access:

1. **Auth model** — single shared password with a session cookie, or real
   multi-user accounts. `playback_state` is already keyed by `user_id`
   ([ADR 0006](adr/0006-playback-state-keyed-by-user.md)), so either can arrive
   without a migration.
2. **Behavior before a password is set** — binding to localhost until secured
   makes accidental exposure impossible, at the cost of a first-run step.
3. **CSRF defence** — required as soon as cookie sessions exist. `SameSite=Strict`
   plus an origin check is likely sufficient for a same-origin client.
4. **Transport** — reverse proxy with TLS is the recommendation; built-in HTTPS
   is possible but certificate management is its own burden and self-signed
   certificates break TV clients.
