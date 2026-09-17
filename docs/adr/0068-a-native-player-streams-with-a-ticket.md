# ADR 0068 — A native player streams with a ticket

**Status:** Accepted
**Date:** 2026-09-16

## Context

The desktop client is moving playback to libmpv (ADR 0067). mpv fetches the
file itself, over its own HTTP stack, and that stack is not the browser's:

- **It cannot carry the session.** The session cookie is `HttpOnly` by design
  (ADR 0011), so neither the page nor the client process can read it to hand
  over.
- **It cannot pin the certificate.** A LAN-bound server has a self-signed
  certificate the client pins by public key. mpv's TLS can verify against a
  CA or not at all; it has no pinning.

The certificate is solved in the client: a loopback relay inside the desktop
client makes the upstream request with the pinned check, and mpv reads from
`127.0.0.1`. That leaves the question of what credential the relay presents.

## Decision

A **stream ticket**: `POST /api/items/{id}/stream-ticket` returns a secret the
caller presents as `Authorization: Ticket <ticket>`.

- It opens `GET`/`HEAD /api/stream/{id}` for **its own item** and is not a
  credential anywhere else, including the transcode and download paths for the
  same item.
- It records the hash of the session or API key that minted it and **re-checks
  that credential on every request**. Sign-out, a password change and key
  revocation end it; the password change's promise about "every session" holds.
- It lasts **twenty-four hours**. The Dogma report was a film paused overnight;
  a shorter ticket would recreate that failure on the path built to remove it.
- It lives **in memory**, bounded. A restart costs a re-mint and a ticket is
  never on disk.
- It is a **header**, never a query parameter, for the reason API keys are
  (ADR 0061).

## Rejected

- **Reading the cookie out of WebView2** (intercepting a page request, or the
  cookie manager). Works, but defeats `HttpOnly` from inside our own client and
  depends on WebView2 internals; and a future native client on a TV has no
  WebView2 to read from.
- **An API key for the player.** A key opens the whole non-admin API until
  revoked. The player needs one file.
- **A query-parameter token.** A credential in a URL is a credential in logs.

## Consequences

- One new endpoint and one new security scheme in the contract.
- The desktop client needs a loopback relay; it is the only component that
  holds a ticket, and it hands mpv a loopback URL with no credential in it.
- Any future native client (a TV app, a phone app with its own player) gets the
  same narrow path rather than a broader one.
