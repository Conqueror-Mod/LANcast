# ADR 0100 — A code after the password, for whoever wants one

Date: 2026-10-26 · Status: **accepted**

## Context

The server can be reached from outside the house (a public name, ADR 0081), so
a password is all that stands between the internet and somebody's library.
Passkeys (ADR 0082) are stronger, but they only exist at the public name, and
not everybody has a device that makes them. The roadmap asked for time-based
codes from an authenticator app (RFC 6238), and three questions with them: who
decides that an account uses it, what happens when a phone is lost, and what a
restored backup does.

## Decision

**Each person turns it on for themselves.** It is off for every account until
that account turns it on. **An administrator cannot turn it on for somebody
else** — a second factor somebody else enrolled is a second factor somebody
else holds — but can take it off, which is the way back in for a lost phone
with lost recovery codes. That removal is audited.

**The password earns a challenge, not a session.** With it on, a right password
answers with an unguessable challenge and no cookie; a second request trades
the challenge and a six-digit code (or a recovery code) for the session. A
challenge lasts five minutes and five tries, in memory. A wrong password answers
exactly as it always did, so the response tells nobody without the password
that the account uses a code.

**The throttles clear only when the whole sign-in succeeds.** The per-address
and per-account limits on sign-in count wrong codes as they count
wrong passwords, and a right password alone does not reset them — otherwise
each challenge would be a fresh budget for guessing codes.

**A code works once.** The step before and after now are accepted, for clock
drift, and the step used is recorded in the same statement that checks it
(`totp_last_step`), so the same code — read over a shoulder, or two requests
racing — succeeds once.

**The standard parameters, written here.** HMAC-SHA1, six digits, thirty
seconds: what every app assumes. It is forty lines of the standard library,
tested against the RFC's own vectors, and it sits on the sign-in path, so it is
not a dependency.

**The secret is sealed, the recovery codes are hashed.** The secret is sealed
under the key kept beside the database (`internal/secret`, as RetroAchievements
keys are), so a copy of the database alone cannot produce codes. Ten recovery
codes are made when it is turned on, shown once, and stored as hashes. On a
backup restored to another machine the secret cannot be opened, so codes stop
working there and recovery codes are the way in — which is what they are for.
Four nullable columns on `user`, migration 82.

**Passkeys skip it.** A passkey is already two factors (the device, and what
unlocks it). API keys are unaffected: they are their own credential, made by
somebody already signed in.

**Weakening it asks for the password.** Turning it off and making new recovery
codes need the current password, on the sign-in throttle, as adding a passkey
does. Turning it on needs a code from the app, which proves the app has it.

## Consequences

- A third-party client that signs in with a password must handle
  `{authenticated: false, two_factor: true, challenge}` for an account that has
  turned it on. `user` is no longer always present on the login answer.
- Losing the phone and the recovery codes means asking an administrator. An
  administrator who loses both on the only admin account has the same recourse
  as a forgotten password: `reset-auth`, at the machine (docs/install.md).
- Turning it on does not sign out other sessions; it changes how the next
  sign-in works, not who is signed in now.
