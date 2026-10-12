# ADR 0082 — A passkey belongs to the public name

Date: 2026-10-11 · Status: **accepted** 2026-10-11 · builds on
[ADR 0081](0081-a-public-name-can-earn-a-public-certificate.md)

The plan is [passkeys-plan.md](../passkeys-plan.md), Part 2. Chris chose the
verification approach on 2026-10-11 (§3); the rest follows the plan.

## Context

The server is publicly reachable, and every account on it was one guessable
password away from anybody on the internet. #838 slowed guessing per account.
Passkeys remove the guessable secret: a key pair per device, the private half
never leaving it, and a signature the browser only produces for the site the
key was made for, so it cannot be phished.

Two platform rules decided where they can exist (ADR 0081 §Context): a passkey
is bound to a **domain name**, never an address, and Chromium refuses WebAuthn
on any **certificate error**. ADR 0081 provided both, a public name and a
trusted certificate for it. This record decides how passkeys use them.

Measured before deciding, 2026-10-11:

- **WebView2 (Edge 154) supports passkeys fully**: a platform authenticator
  (Windows Hello), autofill (conditional mediation), and a phone as the
  authenticator (hybrid). The desktop window's engine is not the obstacle; the
  address it loads is (§6).
- **go-webauthn v0.18.2 adds ten modules**: TPM, JWT, MessagePack,
  mapstructure, and the rest, almost all for attestation, which LANcast never
  checks.

## Decision

### 1. Passkeys exist only at the public name

The RP ID is the public certificate's hostname. A request may use passkeys
when it arrived **over TLS, for that host, while the certificate is valid**.
A failing renewal does not count against it until the certificate actually
expires. Everywhere else (by IP, on `localhost`, a server with no public
certificate) `GET /api/auth/status` reports `passkeys.available: false`, the
page shows no passkey button, and the ceremony routes answer `409`.

An origin is accepted when it is `https` and its host is the RP ID, **on any
port**. Both listeners belong to one server (ADR 0081 §2), and a passkey made
through 443 must work through 8080.

### 2. In addition to the password, never instead of it

The password stays. It is the recovery path, and it is how somebody signs in
anywhere passkeys cannot exist (§1). `LANcast.exe resetauth` remains the last
resort. "Passkey only" accounts are a later and separate decision.

Changing the password does not remove passkeys, but it ends every session, as
it always has.

### 3. Verified by LANcast's own code; CBOR by one library

`internal/auth/passkey` implements only what LANcast asks for:

- **`attestation: "none"`.** The attestation statement is not examined and the
  format is not judged. LANcast does not decide which makes of authenticator
  are good enough.
- **User verification required.** The UV flag must be set. A passkey stands in
  for the password, so it must have checked the person, not just their
  presence.
- **ES256, EdDSA and RS256**, which cover every platform passkey and security
  key in use. Anything else is refused **at registration**, so a key LANcast
  could never verify is never stored. RSA must be at least 2048 bits, an EC
  point must lie on P-256, and an Ed25519 key must be 32 bytes.
- **Client data:** type, challenge in constant time, allowed origin, and never
  cross-origin. **Authenticator data:** the RP ID hash, and the UP and UV flags.

**CBOR is the one thing not written here.** `fxamacker/cbor/v2` (plus its
`x448/float16`) is configured for input from a stranger: duplicate map keys
refused, no indefinite lengths, and nesting and sizes bounded.

Chris's choice, from three options (go-webauthn; this; or own code with a
hand-written CBOR parser). The library's weight is attestation LANcast never
uses. A hand-written parser for untrusted binary input is exactly the surface a
fuzzed decoder exists to remove.

### 4. Ceremonies

- **Challenges:** 32 random bytes, held in memory, single use (spent whether or
  not the ceremony succeeds), five minutes, bound to their purpose and, for a
  registration, to the account that asked. At most 1,000 outstanding, because
  the sign-in options route is public. A restart abandons ceremonies in
  flight, which is harmless.
- **Sign-in** uses discoverable credentials: the options name no account, and
  the passkey's user handle must name the account it was registered to.
  Throttled per address like password sign-in. Every refusal gets one answer
  ("that passkey could not sign you in"); the reason goes to the log.
- **Adding a passkey asks for the current password**, as changing the password
  does. Throttled with the same per-address and per-account counters. A
  session somebody left open is not enough to add a new way in. **Removing**
  one asks for nothing, because removing a way in only makes the account
  harder to enter.
- **The signature counter:** synced passkeys report zero forever, which says
  nothing. A counter that has ever been non-zero must go up; one that does not
  is the specification's sign of a cloned authenticator, and the sign-in is
  refused and logged.
- A successful sign-in issues an **ordinary session**. Sessions, CSRF and
  `SameSite=Strict` are unchanged.

### 5. Data

Schema revision 70 adds `passkey`: the credential id (unique, so one
credential belongs to one account), the COSE public key verbatim, the
algorithm, the counter, the AAGUID, the backup flags, a name, the `rp_id` it
was made for, and created and last-used times.

There is no foreign key to `user`, for `session`'s reason: lookups join
`user`, and `DeleteUser` removes passkeys in its transaction. A passkey made
for an earlier name stays listed as not usable here, so it can be recognised
and removed instead of failing with no explanation.

Routes: `POST /api/auth/passkey/login/options` and `/login` (public);
`GET /api/profile/passkeys`; `POST /api/profile/passkeys/options` and
`/api/profile/passkeys`; and `PATCH`/`DELETE /api/profile/passkeys/{id}`.
They are the caller's own and session-only, so not available to API keys
(ADR 0061) or remote guests (ADR 0046). Adding, renaming and removing are
audited (ADR 0026).

### 6. The desktop window, later

The window loads the server by address, so its origin can never match the
RP ID, and it shows no passkey button (§1). Teaching it to load the public
name when there is one is a follow-up. Its engine already supports
everything this needs.

## Consequences

**Good:** an account can be entered without a guessable secret, from a
phone, a laptop or a security key, and the credential cannot be phished.

**Good:** no new attack surface off the public name. The routes refuse, and
the page shows nothing.

**Cost:** passkeys depend on the public name. If the name changes, every
passkey must be made again (the old ones are listed as made for the old
name). If the certificate lapses, passkeys stop until it is renewed, and the
password still works.

**Cost:** security-critical verification is LANcast's own code. It is small,
covers one narrow mode, and is tested by a simulated authenticator for every
algorithm and every tampered field. It is still code that has to stay right.

**Cost:** one new module, `fxamacker/cbor`, and its float16 dependency.

## Revisit if

- **"Passkey only" is wanted** for some accounts, which would need a recovery
  story that is not the password.
- **Attestation becomes a requirement**, for example admin accounts restricted
  to hardware keys. The library then earns its weight.
