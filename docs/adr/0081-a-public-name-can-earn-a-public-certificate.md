# ADR 0081 — A public name can earn a public certificate

Date: 2026-10-11 · Status: **proposed** · **amends [ADR 0014](0014-transport-security.md)**

The plan is [passkeys-plan.md](../passkeys-plan.md), Part 1. Chris decided on
2026-10-11 to revisit ADR 0014's "no ACME" as an opt-in. This record is the
shape of that opt-in, and it is not accepted yet.

## Context

ADR 0014 rejected ACME for two reasons:

> Automatic public certificates require the server to be reachable from the
> internet on port 80/443 and to talk to an external CA — public exposure and
> phone-home, the two things LANcast's deployment guidance and core principles
> reject.

Three things have changed since then.

**The first reason has been overtaken by events.** Aither has been publicly
reachable since 2026-10-10, on purpose, so that friends' servers can federate
with it. Exposure was decided by the operator. LANcast's job now is to make
that exposure as safe as it can, and its strongest available tool is
unavailable on a bare IP address with a self-signed certificate.

**Passkeys need a trusted certificate and a name.** Both requirements are
platform rules (WebAuthn's RP ID must be a domain; Chromium refuses WebAuthn on
any certificate error since M110). No amount of LANcast code works around
either. With the self-signed certificate, passkeys can never work in a phone's
browser.

**There is now a name.** Aither has a free DuckDNS name, under a domain on the
Public Suffix List, and it already resolves and hairpins from inside the house
(measured 2026-10-11). The missing piece is a certificate a browser trusts for
that name. The reverse proxy ADR 0014 pointed to is a second program to
install, configure and keep running, in front of a server whose whole pitch is
being one program.

The second reason, **no phone-home**, still stands, and this decision is built
around it. The principle, from the README, is that "the server does not do
network things you did not ask for". A certificate request is a network thing
the operator asks for, by name, in Settings. Until then, nothing happens.

## Decision

### 1. Opt-in, and off until an admin turns it on

A new **Public certificate** section in the server's Settings, admin-only.
With it off (the default, and the state of every existing install after
upgrade), LANcast makes no ACME request of any kind and behaves exactly as
under ADR 0014.

Turning it on takes three things from the admin:

- **The hostname**, such as `example.duckdns.org`. One name only; no wildcards.
- **The CA**, from a fixed list: Let's Encrypt, or Let's Encrypt **staging**
  (for testing, with generous rate limits and an untrusted root).
- **Agreement to the CA's subscriber agreement**, shown as a link with a tick
  box. LANcast does not agree to terms on anybody's behalf.

**No contact address is sent.** An ACME account needs none. If the admin wants
expiry emails from the CA, there is an optional field, and it is empty by
default.

**The directory URL is not settable through the API.** The fixed list maps to
known URLs. A custom ACME directory (a private CA, say) is a
`acme_directory_url` entry in the `0600` settings file, the same way ADR 0014
keeps certificate paths off the remote API. An admin session is not enough to
point the server's outbound requests at an arbitrary URL.

### 2. TLS-ALPN-01, on the listener LANcast already has

The CA proves control of the name by connecting to **port 443** of the address
it resolves to. Port 8080 cannot be used: the challenge ports are fixed by the
protocol. The operator forwards external 443 to LANcast's port on the gateway,
which also lets people drop `:8080` from the URL.

TLS-ALPN-01 rather than the alternatives:

- **HTTP-01** needs inbound port 80 and a plaintext listener answering it.
  LANcast's plain-HTTP side only redirects, and that should stay all it does.
- **DNS-01** needs write credentials for the DNS provider, stored on the
  server, plus provider-specific code. The DuckDNS token can repoint the name.
  Storing it would turn a server compromise into a name hijack.
- **TLS-ALPN-01** arrives as a TLS handshake carrying the ALPN protocol
  `acme-tls/1`. LANcast already sorts handshakes by ALPN: peers are diverted on
  `lancast-peer/1` in `peer.Attach`. The challenge is one more branch in that
  same `GetConfigForClient`. While a challenge is pending it answers with the
  validation certificate from `x/crypto/acme`'s `TLSALPN01ChallengeCert`;
  otherwise it falls through.

**The implementation is `golang.org/x/crypto/acme`**, already in `go.mod`
through `x/crypto`. No new module. Not `autocert`: it generates a new key per
certificate (see §4), and it owns the whole `GetCertificate` callback, which
LANcast needs for §3.

### 3. Two certificates, chosen by the name asked for

The public certificate covers one name. Everybody else keeps what they have.

| The client asked for (SNI) | It gets |
|---|---|
| the configured hostname | the public certificate |
| anything else: an IP, `localhost`, no SNI | the self-signed certificate, unchanged |
| ALPN `lancast-peer/1` (a peer server) | the ADR 0044 identity certificate, unchanged |
| ALPN `acme-tls/1` (the CA) | the challenge certificate, only while one is pending |

So nothing that works today changes. A browser that reached the server by IP
still gets the self-signed certificate. Federation never sees the public
certificate. The desktop client's pin keeps working however it connects (§4).

`tls.Config.Certificates` (fixed at startup) becomes a `GetCertificate`
callback reading an atomic pointer, so a renewal is served on the next
handshake without a restart.

### 4. The public certificate uses the serving key LANcast already has

The desktop client pins the **SPKI of the serving key** for a remote server
(`internal/knownserver`, ADR 0070), and a changed key leads to the refusal
path. Certificates from Let's Encrypt last 90 days today and will get shorter.
A new key per renewal would present every remote desktop client with "this
server's key changed" several times a year.

So the CSR for every order is signed with **the existing serving key**,
`<data>/tls/key.pem`, the same key behind the self-signed certificate. One
key, two certificates, one pin: a client that pinned the key when it saw the
self-signed certificate accepts the public one, and the reverse, across every
renewal.

That is a deliberate trade against key rotation. The serving key's job is
transport privacy for one server, its compromise is already a full compromise
of that server, and the identity that matters for federation is the separate
ADR 0044 key. If a serving key ever has to be replaced, the remedy is the one
that exists today: delete it, restart, and the clients re-accept.

The ACME **account key** is separate: `<data>/tls/acme/account.key`, `0600`,
generated on first enable. It is never the serving key, so losing one does not
compromise the other.

### 5. Renewal, failure, and saying so

- **Renew when a third of the lifetime remains.** That is 30 days for a 90-day
  certificate, and it scales down as lifetimes shrink.
- **A failed attempt** is retried with backoff (an hour, doubling, capped at a
  day), and the **current certificate stays in service**. A renewal failure
  must never take HTTPS down.
- **An expired public certificate** is not served. The configured name falls
  back to the self-signed certificate, which is no worse than an expired one
  and is what the server served before the opt-in.
- **Everything is said.** Settings shows the name, the CA, the expiry date,
  the last attempt and its error in the CA's own words. The log records every
  request to the CA (`msg="acme"`), so "what did my server contact, and why"
  has an answer.
- **Turning it off** stops all requests, deletes the public certificate and
  the account key, and the name falls back to self-signed on the next
  handshake.

### 6. Where the settings live

The hostname, CA choice, agreement, optional email and on/off go in
`config.Settings` and through `GET/PUT /api/settings`, admin-only like every
other field there. `acme_directory_url` is file-only (§1). Certificate state
(expiry, last error) is reported by a new read-only
`GET /api/settings/public-certificate`. That is an API contract change, so
`docs/api.md` and `docs/openapi.json` change in the same commit.

No schema change: the certificate, chain and account key are files under
`<data>/tls/`, like the self-signed ones.

## Consequences

**Good: passkeys become possible.** This is the prerequisite for ADR 0082
(passkeys), and the reason this is happening now.

**Good: browsers stop warning at the public name.** A phone opening the name
gets a padlock instead of an interstitial, and friends stop having to be told
"click through the warning", which is a habit that trains people to ignore the
one warning that matters.

**Good: nothing changes for anyone who does not opt in,** and for an opted-in
server, nothing changes for clients that never use the name.

**Cost: a new outbound connection, and a new party.** The server contacts
Let's Encrypt every couple of months. It is named, logged, chosen and
reversible. It is still a dependency on a party outside the house, and if
Let's Encrypt is unreachable for weeks, the public name reverts to the warning
it has today.

**Cost: port 443 is forwarded.** One more open port. It lands on the same
LANcast listener as 8080, with the same handlers and throttles, so the
attack surface does not grow. The number of ports a scan finds does.

**Cost: one serving key forever, unless replaced by hand** (§4). Accepted for
the pin's sake; written down so nobody "fixes" it into per-renewal keys.

**Cost: the name is public.** Every issued certificate is published in
Certificate Transparency logs, and scanners watch them. A name that was only
in DNS becomes one that appears on lists. This is why the name must not
identify the operator, and why the per-account throttle shipped first (#838).

## Alternatives considered

- **Keep ACME out; document a reverse proxy.** ADR 0014's position. Rejected
  for this deployment because it asks the operator to run and update a second
  internet-facing program, which is more risk than the feature, not less.
- **`autocert`.** Rejected: a fresh key per certificate breaks the desktop
  client's pin on every renewal (§4), and it wants to own `GetCertificate`.
- **DNS-01 with the DuckDNS token.** Rejected: it stores a credential that can
  repoint the name, so a compromised server could hijack its own name and its
  passkeys' RP ID.
- **Let's Encrypt IP-address certificates.** They exist now, but a passkey
  cannot use an IP address as its RP ID, so they solve the warning and not the
  reason for this ADR.
- **The public certificate for every connection.** Rejected: a client that
  connects by IP would get a certificate for a name it did not ask for, which
  is an error, and so is worse than the self-signed one it gets today.

## Revisit if

- **Let's Encrypt shortens lifetimes far enough** that renewals become a
  noticeable share of uptime, or adds a renewal-information API (ARI) to
  `x/crypto/acme`. Use it rather than the fixed third.
- **A second household wants this.** The settings page assumes the operator
  understands port forwarding. A guided version would need its own design.
