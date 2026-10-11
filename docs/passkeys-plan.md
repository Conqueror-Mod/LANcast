# Passkeys, and the certificate they need: design plan

*Written 2026-10-11. A plan for two ADRs: proposed 0081 revisits
[ADR 0014](adr/0014-transport-security.md)'s "no ACME", and proposed 0082 adds
passkeys. Decided so far: revisit ACME, opt-in. Nothing is built yet.*

## Why now

Aither is publicly reachable. Every account on it is now one password away from
anybody on the internet, and the only brake is a per-IP throttle
([`auth.ClientKey`](../internal/auth/auth.go)). An attacker with many
addresses does not slow down. Passkeys remove the guessable secret entirely,
and they cannot be phished, because the browser binds each one to the site's
name.

## The two constraints that shape everything

Both are platform rules, not LANcast choices, and both were confirmed before
writing this:

1. **A passkey is bound to a domain name, never an IP address.** The WebAuthn
   RP ID must be a registrable domain (or `localhost`). A page at
   `https://203.0.113.5:8080` cannot create or use a passkey at all.
   ([web.dev, RP ID deep dive](https://web.dev/articles/webauthn-rp-id))
2. **Chromium refuses WebAuthn on any page with a certificate error** (since
   M110), using the same test as its "Not secure" warning. LANcast's
   self-signed certificate is exactly that error in every browser that has not
   been told to trust it.
   ([Chromium announcement](https://lists.w3.org/Archives/Public/public-webauthn/2022Nov/0135.html))

So **passkeys need a hostname and a publicly trusted certificate**, or they work
nowhere except the desktop client (if at all; see the spikes). That is why this
plan is two ADRs and the certificate comes first.

**ACME alone is not enough.** It provides the trusted certificate, but the
passkey still needs a *name*. Aither needs a DNS name pointing at its static
address before any of this works.

## Part 1: a name and a certificate (ADR 0081, revisits 0014)

### What ADR 0014 said, and what has changed

0014 rejected ACME for two reasons: it needs public reachability, and it means
talking to an external CA. The first reason no longer applies, because the
server is already public. The second is still a principle, and the answer is to
make it a choice the operator makes, not something LANcast does unasked:

- **Off by default.** Nothing contacts a CA until an admin types a hostname
  and turns it on in Settings.
- **The only outbound call is to the CA the admin chose.** It happens only to
  issue and renew, it is logged, and Settings names it. That is stricter than
  the update check, which is a switchable outbound call that defaults on.
- **No email and no account metadata unless the admin supplies it.** ACME
  allows an account with no contact address.
- **Bring-your-own-certificate stays** for anybody who would rather run
  certbot or win-acme themselves.

### The name

*Revised 2026-10-11.* The first recommendation was a domain the operator owns.
That is still the strongest choice, but it costs money every year, and the
free route turned out to be good enough, with one rule:

**A free name must be under a domain on the Public Suffix List.** The PSL is
how browsers and Let's Encrypt decide where one site ends. Under an unlisted
shared domain (sslip.io, nip.io, most afraid.org domains), every user of the
provider is one "site": they share one Let's Encrypt rate limit, and any of
them can set cookies on the parent domain. Checked against the list on
2026-10-11:

| On the PSL | Notes |
|---|---|
| `duckdns.org` | **failed in practice (2026-10-11):** Let's Encrypt's secondary validation could not resolve the name, four times in a row. A/AAAA/TXT only, so no CAA record |
| `dynv6.net` | **chosen for Aither.** Supports CAA; production issued on the first attempt |
| `dynu.net`, `freeddns.org` | fine |
| `ddns.net` (No-IP) | free names must be reconfirmed every 30 days or they are lost; avoid |
| `dedyn.io` (deSEC) | new registrations suspended as of 2026-10 |

**Losing a free name is an inconvenience, not a lockout.** The RP ID is
permanent, so passkeys made under a name die with it. But the password stays
as the recovery path, so the remedy is to sign in with it and register
passkeys under the new name. A later move to an owned domain is the same
re-registration.

**No CAA on DuckDNS.** The "only Let's Encrypt may issue for this name" lock
is lost. It is a hardening step, not a requirement.

### The challenge type

Let's Encrypt proves control on fixed ports, not 8080:

| Challenge | Needs | Fit |
|---|---|---|
| HTTP-01 | inbound **80** | another port forward, and a plaintext listener |
| **TLS-ALPN-01** | inbound **443** | **best fit:** forward external 443 to 8080. LANcast already routes by ALPN (peers use it), so `acme-tls/1` is one more branch on the same listener. It also drops the `:8080` from the URL. |
| DNS-01 | API credentials for the DNS host | no ports, but a stored secret and provider-specific code |

**Recommend TLS-ALPN-01, with external 443 forwarded to 8080.**

### Keep the key across renewals: the desktop client depends on it

The desktop client pins the **serving key's SPKI** for a remote server
(`internal/knownserver`, ADR 0070), and a changed key leads to the refusal
path. Let's Encrypt certificates last 90 days now and will get shorter.
`autocert` generates a fresh key per certificate, so every renewal would
present every remote desktop client with "this server's key changed".

So: use `golang.org/x/crypto/acme` directly (already a dependency through
`x/crypto`), and sign each order's CSR with **one long-lived serving key**
kept beside the current self-signed one. The pin then survives renewal.
Federation is unaffected either way: peers are answered with the ADR 0044
identity certificate, selected by ALPN, never the serving one.

### Inside the house

A phone on the LAN must also reach the server *by name*, or it gets a
certificate for a name it did not ask for, and no passkey. Three options:

- **Hairpin NAT** on the AT&T gateway, so the public `A` record works from
  inside. **Measured 2026-10-11: it does, over IPv4, but unreliably.** From
  Aither itself the public name answered with the same serving key as
  loopback, but only 3 of 8 attempts succeeded on either port, against 8 of 8
  on loopback. The gateway's NAT loopback is flaky, so **split DNS (the name
  pointing at the LAN address inside the house) is the real answer** for
  passkeys at home. IPv6 is untested, because there is no `AAAA` record yet.
- **IPv6:** with no NAT, an `AAAA` record that resolves to Aither's global
  address works on the LAN as-is, if the gateway is not filtering it.
- **Split DNS:** the router or a hosts entry maps the name to the LAN address.

Typing the IP keeps working everywhere, with the self-signed certificate and a
password. It just never offers a passkey.

## Part 2: passkeys (ADR 0082)

### Scope

- **Sign in with a passkey** as an alternative to the password, for any
  account.
- **Discoverable credentials** (no username needed), with conditional UI so
  the browser offers saved passkeys in the username field.
- **Manage passkeys** under Account: add, rename, remove, and see when each
  was last used.
- **The password stays.** It is the recovery path, and
  `LANcast.exe resetauth` stays the last resort. "Passkey only" is a later,
  separate decision.

### Only where it can work

`GET /api/auth/status` gains `passkeys: {available, rp_id}`. `available` is
true only when the request's `Host` matches the configured RP ID **and** the
connection is TLS with the trusted certificate. The page shows passkey UI only
then. On `https://<ip>:8080` the option is simply absent, so it never shows a
button that cannot work. This is the same feature-detection rule the desktop
settings use.

### Data (migration + ADR: a new table)

```sql
CREATE TABLE passkey (
    id             TEXT    PRIMARY KEY,           -- opaque, like user ids
    user_id        TEXT    NOT NULL,
    credential_id  BLOB    NOT NULL UNIQUE,
    public_key     BLOB    NOT NULL,              -- COSE key, verbatim
    sign_count     INTEGER NOT NULL DEFAULT 0,
    aaguid         BLOB,
    transports     TEXT,                          -- JSON array, as given
    backup_eligible INTEGER NOT NULL DEFAULT 0,
    backup_state   INTEGER NOT NULL DEFAULT 0,
    name           TEXT    NOT NULL,              -- "Chris's phone"
    rp_id          TEXT    NOT NULL,              -- the name it was made for
    created_at     INTEGER NOT NULL,
    last_used_at   INTEGER
);
CREATE INDEX idx_passkey_user ON passkey(user_id);
```

- **No foreign key to `user`.** This follows `session`'s reasoning in
  `migrate.go`: lookups join `user`, so a deleted account's passkeys stop
  resolving. Deleting an account also deletes its passkeys explicitly.
- **`rp_id` is stored** so a passkey made for an old name is shown as stale
  instead of failing silently.
- **Challenges live in memory:** single-use, about 5 minutes, keyed by an
  opaque id, the same shape as guest tickets. A restart invalidates in-flight
  ceremonies, which is harmless.
- Signing in with a passkey creates an ordinary row in `session`. Sessions,
  CSRF and `SameSite=Strict` are unchanged.

### API (contract change: `docs/api.md` + `docs/openapi.json` in the same commit)

| Route | Auth | Notes |
|---|---|---|
| `POST /api/auth/passkey/login/options` | public | returns a challenge; added to `isPublicPath` |
| `POST /api/auth/passkey/login` | public | verifies; throttled like password login |
| `POST /api/account/passkeys/options` | session + **recent re-auth** | registering a passkey is as sensitive as changing the password |
| `POST /api/account/passkeys` | session | finishes registration |
| `GET /api/account/passkeys` | session | the caller's own passkeys |
| `PATCH/DELETE /api/account/passkeys/{id}` | session | rename / remove |

Not available to API keys (ADR 0061) and not to remote guests (ADR 0046).
Each change goes in the audit log (ADR 0026). **Changing the password does not
remove passkeys**, but it does end every session, as it does today.

### Library or hand-rolled

- **`github.com/go-webauthn/webauthn`** is the maintained Go implementation.
  Before choosing it, measure its module graph (`go mod graph`) against the
  project's dependency line. It brings at least a CBOR library, and attestation
  and metadata support LANcast does not need.
- **Hand-rolled** "none"-attestation verification (clientDataJSON,
  authenticatorData flags, rpIdHash, ES256/EdDSA/RS256 signatures, CBOR/COSE
  decode) is a few hundred lines, but it is security-critical parsing.
- **Recommend the library**, unless the measured graph is heavy. Either way,
  wrap it behind one `internal/auth/passkey` package, so the choice can be
  undone.

## Independent of all of this: ship first

**A per-account login throttle** beside the per-IP one: exponential backoff on
the target account, which costs an attacker time without being a lockout they
can trigger against the owner. It needs no hostname, it helps today, and it is
small.

## Spikes before building (each a question with an answer)

1. **Does WebView2 do WebAuthn at all** (Windows Hello) against a trusted
   certificate? If not, the desktop client keeps the password and the page's
   feature detection must say so honestly.
2. **Does the client's pinned self-signed certificate count as a certificate
   error** for WebAuthn? It uses `--ignore-certificate-errors-spki-list`. This
   decides whether passkeys could ever work on the LAN without a public name.
3. **Does the AT&T gateway hairpin?** IPv4: **yes** (2026-10-11). IPv6:
   untested until an `AAAA` record exists.
4. **The module graph of go-webauthn.**

## Build order

1. Per-account throttle (independent, ship now).
2. ~~Get a name pointing at Aither and forward 443 → 8080~~ (done
   2026-10-11: a free dynv6 name with CAA, `A` only).
3. ~~ADR 0081 + opt-in ACME~~ (built; **verified as the installed service**
   2026-10-11: production issued for the dynv6 name; SNI selection, the
   unchanged SPKI and the peer path all checked). Let's Encrypt **staging**
   failed secondary validation for three unrelated free DNS providers while
   production succeeded, so staging is not a reliable rehearsal for names on
   shared DNS hosts.
4. Run spikes 1–3 against the real certificate.
5. ADR 0082 + migration + passkey package + API + spec.
6. Login and Account UI, with feature detection.
7. Verify on real devices: a phone on cellular, a phone on Wi-Fi, the desktop
   client, and a browser by IP (which must show no passkey option).
