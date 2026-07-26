# ADR 0014 — Transport security: built-in TLS, bring-your-own-cert, no ACME

Date: 2026-07-26 · Status: accepted

## Context

Everything LANcast serves is plaintext HTTP. Since [ADR 0011](0011-single-password-with-server-sessions.md)
that includes the password on login and the session cookie on every subsequent
request. Anyone on the network path — a compromised phone, TV, or IoT device on
the same LAN, which [security.md](../security.md) already treats as
semi-trusted — can read both and impersonate the owner. The roadmap and the
security posture both name this **the largest remaining gap** between LANcast
and real-world use.

Built-in TLS was previously considered and deferred (see the old "Deployment
guidance" note): certificate management is an ongoing burden, and self-signed
certificates were expected to break TV clients in ways that are miserable to
debug. Two things have changed. The gap is now concrete rather than
hypothetical — there are real credentials on the wire — and the objection about
TV clients is premature: the only client today is a browser, and TV clients are
M4. Deferring encryption of a live password until a client that does not exist
yet might dislike self-signed certs is the wrong trade.

The constraint that does not change is **no phone-home**. Whatever we choose
must not require LANcast to reach an external service or be publicly reachable
to function on a home LAN.

## Decision

**LANcast serves HTTPS when given a certificate and key**, configured the same
way as the TMDB key: `tls_cert_file` and `tls_key_file` in the `0600` settings
file. It is deliberately *not* exposed through the settings API — certificate
paths are a filesystem/deployment concern, take effect only on restart, and
keeping them off the remote API keeps that surface small. This is the
bring-your-own-cert path and the recommended production configuration — a cert
from an internal CA, `mkcert`, or one copied in from elsewhere is trusted by
clients with no warning.

**When the server binds beyond loopback and no certificate is supplied, it
generates a self-signed certificate on first run**, persists it under
`<data>/tls/` (`cert.pem`, `key.pem`, both `0600`), and serves HTTPS with it.
The certificate is stable across restarts — an unstable cert is precisely what
makes trust-on-first-use miserable — and covers `localhost`, `127.0.0.1`, and
the machine's LAN IPs. This closes the plaintext gap out of the box with no
external tooling. It encrypts the wire; it does not authenticate the server, and
the docs say so plainly.

**An unsecured, loopback-only server stays plain HTTP.** There is nothing on the
wire worth protecting when the only peer is the same machine, and a certificate
warning on `localhost` is pure friction during first-run setup. TLS turns on
exactly when the server becomes reachable by anyone else — the same boundary
that already gates LAN binding.

**When TLS is active, the plain HTTP port answers only with a redirect** to the
HTTPS URL, so a bookmarked `http://` address does not silently fail.

**No ACME / Let's Encrypt.** Automatic public certificates require the server to
be reachable from the internet on port 80/443 and to talk to an external CA —
public exposure and phone-home, the two things LANcast's deployment guidance and
core principles reject. Anyone who genuinely wants a publicly trusted cert can
put a reverse proxy in front, which is already the documented path for public
exposure.

**VPN remains the recommended way to reach LANcast remotely.** Built-in TLS
closes the on-LAN plaintext hole; it is not an invitation to port-forward.
Tailscale or WireGuard still give encrypted remote access with nothing exposed
publicly, and that guidance stands.

## Consequences

**Good — the password stops travelling in plaintext by default.** The moment a
server is reachable by anyone other than localhost, its traffic is encrypted,
without the operator having to obtain a certificate first. The posture improves
for the default install, not only for the operator who reads the manual.

**Good — no new external dependency and no phone-home.** Self-signed generation
uses the standard library (`crypto/tls`, `crypto/x509`). Bring-your-own-cert is
a file path. Neither reaches the network. The no-phone-home principle is intact.

**Good — the loopback-until-secured guarantee is untouched.** TLS is orthogonal
to binding. `bindAddr` still forces an unsecured server onto `127.0.0.1`; the
only addition is that a secured, LAN-bound server now also gets a certificate.

**Cost — the default self-signed cert produces a browser warning.** A first
visit to `https://<lan-ip>:8080` shows "not secure" until the operator either
trusts the cert or replaces it with a real one. This is the honest state of a
server that authenticates its clients but cannot prove its own identity, and it
is strictly better than plaintext. The setup docs explain both the warning and
the two ways out (trust it, or drop in a real cert).

**Cost — certificate lifecycle is now partly ours.** A generated cert has an
expiry; an expired one must be regenerated. We mitigate by issuing a long
validity and regenerating automatically when the persisted cert is missing or
expired, so the failure mode is "a new warning to re-accept," never "the server
will not start."

**Cost — streaming and range requests must be re-verified over TLS.** Long
video responses, `Range` seeks, and HLS segment delivery all now run over a TLS
listener. The existing `http.Server` handles this, but the "no write timeout for
long streams" note in `main.go` and the seek/resume paths get one more pass
under HTTPS before this is called done.

## The thing that is easy to get wrong

Serving HTTPS on the same port that used to serve HTTP silently breaks every
client that still requests `http://`. The redirect listener is not optional
polish — without it, turning TLS on looks like "the server went down" to anyone
with an existing bookmark or a client hardcoded to the old scheme. The redirect
is what makes enabling TLS a transparent upgrade rather than a breaking change.

The second trap is a self-signed cert that regenerates on every start. Each new
cert is a fresh "do you trust this?" prompt and invalidates any trust the
operator already granted. Persisting the cert to the data dir and reusing it is
what turns trust-on-first-use from an infuriating daily ritual into a
one-time step.

## Revisit if

A TV or embedded client that cannot be taught to trust a private cert becomes a
real target — at which point bring-your-own-cert from an internal CA the client
already trusts, not ACME, is the answer. Or if a first-class reverse-proxy
deployment (with forwarded-header handling and proxy-side rate limiting) becomes
common enough that terminating TLS at the proxy is the norm and built-in TLS
becomes the fallback rather than the default.
