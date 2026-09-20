# ADR 0070 — The desktop client can trust a server it did not install

Date: 2026-09-20 · Status: accepted as amended — see [Amendment: identity, not the serving certificate](#amendment-identity-not-the-serving-certificate)

## Context

LANcast's desktop client can only ever open the server on its own machine.
That is not a missing screen. It is three separate assumptions, each of which
made sense when the client and the server were installed together by the same
installer:

1. The address is a flag. `-addr` defaults to `:8080` and is never surfaced in
   the app, so there is nothing to type and nowhere to type it.
2. The trust is read off local disk. `serverCertPin()` gets the pin by reading
   the server's certificate out of the **local data directory**
   ([`internal/certpin`](../../internal/certpin/certpin.go)). A server on
   another machine has no certificate on this disk, so there is nothing to pin.
3. The lifecycle assumes ownership. `ensureServer()` starts a local `lancastd`,
   or waits for the installed service, before opening anything.

Of those, only the second is interesting. The first two are plumbing; the
second is a trust decision that has never been made.

This came out of a real household. A second person has an account on the
server, and on her own PC she has no way to reach it: her copy of the desktop
client opens her own empty server, and the shared library is simply not
available to her in the app.

### The browser is not an answer

She can open `https://<server>:8080` in a browser today. The server already
binds every interface once a password is set, and its certificate already
carries SANs for the LAN addresses, so this works after one click through an
unknown-issuer warning.

It is not good enough, and the reason is sharper than polish. A browser cannot
play through libmpv, so every file it cannot decode natively is converted by
the server — which is precisely the work [ADR 0067](0067-the-desktop-client-plays-through-libmpv.md)
removed. The household member most likely to be watching a 4K HEVC file with
TrueHD audio would be the one person for whom the server still transcodes it.
A second viewer would cost more server CPU than the owner does, permanently.

### What is already right

Two things make this smaller than it looks.

**The pin is over the public key, not the certificate.** `certpin.SPKI` hashes
the SubjectPublicKeyInfo specifically so that the server regenerating its
certificate as expiry approaches does not break the pin. That property is what
makes a stored pin safe to keep for years, and it is what gives a *changed* pin
a precise meaning — see the decision below.

**The server needs no change whatsoever.** The window navigates to the server's
own origin, so the session cookie is first-party and
`auth.SameOriginRequest` compares the request's `Origin` against `r.Host` and
matches. `SameSite=Strict` behaves the same way. There is no new endpoint, no
new header and no API change, so [ADR 0018](0018-api-contract-and-versioning.md) is untouched
and `docs/api.md` and `docs/openapi.json` do not move.

`certpin.SPKIFromPEM` already exists as "the entry point for a certificate that
did not come from the default location". The seam was left open.

## Decision

**The desktop client may connect to a LANcast server it did not install, and
learns to trust that server's public key on first connection, with the person
looking at the key when it does.**

### Choosing a server

The client keeps a list of known servers in its own client data directory —
beside the window profile, not in the server's data directory, because this is
a fact about this installation of the client and not about any server. Each
entry holds the address, the pinned SPKI, a name the person gave it, and when
the pin was accepted.

The local server keeps its privileged position. When a server is installed on
this machine it remains the default and its pin is still read from disk, which
is a strictly stronger check than anything over the network: the key is read
from the same filesystem that the server writes it to. Remote entries are
additional, never a replacement.

The address is typed by a human. **No discovery, no directory, no rendezvous
service** — the no-phone-home principle is not relaxed by this ADR, and LAN
discovery is deliberately left out of it (see Rejected).

### First connection

On connecting to an address with no stored pin, the client fetches the
certificate, computes the SPKI, and **shows it to the person and waits**. The
dialog names the address, the fingerprint, and says plainly that accepting
means this key and no other will be trusted at that address.

It is not silent, and it is not a checkbox that defaults to accepting. A
trust-on-first-use prompt that nobody reads is `-k`, which is what this ADR
exists to avoid.

### A changed pin is refused

> **Amended.** This section was wrong, and the amendment at the end of this
> document replaces it. The rule below treats a changed *serving* key as
> evidence of an attack; [ADR 0044](0044-server-identity-and-peering.md) had
> already established that a serving certificate legitimately rotates and that
> identity is a separate, longer-lived key. Read the amendment for the rule
> that is implemented.

If the key at a known address does not match the stored pin, **the client
refuses to connect** and says what happened. It does not offer a "continue
anyway" button in the same breath as the warning.

This rule is strict because the pin is over the public key. Routine
certificate rotation does not change it. Adding a network interface does not
change it — the SANs change, the key does not. So a pin that has changed means
the key has genuinely been replaced: the server was reinstalled, its data
directory was recreated, or something on the network is not the server. Two of
those three are things the owner did deliberately and can confirm out of band;
the third is the attack TLS exists to stop. Re-accepting is possible, but it is
an explicit act on the entry — forget this server, then add it again — rather
than a button on the error.

### Lifecycle

When pointed at a remote server the client does not start a local `lancastd`,
does not wait for the installed service, and does not pin the local data
directory. A remote server that is not answering is reported as not answering;
it is not something this machine can fix by starting a process.

### What follows from it

Native playback needs no separate decision. `nativePlayer` already takes an
origin and a pin, and `mpv.NewRelay(origin, pin)` already carries the pinned
TLS to the relay, so a remote server with an accepted pin gives libmpv
playback the same way a local one does — including stream tickets
([ADR 0068](0068-a-native-player-streams-with-a-ticket.md)), which are minted
by whichever server the session belongs to. This must be *verified* during
implementation rather than assumed; it is stated here as the expected shape,
not as a result.

## Rejected

**Ignore certificate errors for a chosen address.** This is the whole attack
surface TLS is here to close, and `internal/certpin` opens with an explicit
refusal to do it: "Not 'ignore certificate errors', which would accept anything
on the LAN pretending to be the server." A per-address exception is the same
decision with a smaller blast radius and the same failure.

**Install the server's certificate into the OS trust store.** It needs
elevation, it is invisible afterwards, it trusts that key for the whole machine
rather than for one app pointed at one address, and removing it is a manual act
in a tool most people have never opened. A pin the app owns can be forgotten by
the app.

**A browser-only answer.** Costs native playback and puts the server back to
converting for the household's second viewer. Discussed above.

**LAN discovery (mDNS or a broadcast).** Tempting, and genuinely nicer than
typing an address. It is left out of *this* ADR because it is a separate
decision with its own privacy surface — a server announcing itself on a
semi-trusted LAN is a choice the owner should make, not a default — and because
it does not remove the need for anything here: a discovered address still has
to be trusted, which is the part this ADR is about. Discovery can be added
later on top of this, and cannot be a substitute for it.

**A relay or rendezvous service for connecting over the internet.** Phone-home.
Out of scope, and remains so.

## Consequences

A second person in a household gets the real app, with native playback, and the
server stops converting for them — which is the point.

The client grows a trust store it did not have, and with it the obligation to
be understandable when trust fails. The refusal path is the part of this
feature most likely to be met by somebody who is not the owner of the server,
possibly with no way to reach the owner at that moment, so its wording matters
more than the happy path's.

Nothing about the server's security posture changes. A server with no password
still binds loopback only and is still unreachable from another machine, so
this feature cannot expose an unsecured server — it can only reach one that was
already reachable.

The client becomes able to be wrong about which server it is talking to in a
way it previously could not, because previously there was only one answer. Any
display of server-specific state — library counts, the current user, Watch
Together rooms — must be clear about *which* server it belongs to, and a stale
cache from one server must never be shown while connected to another.


---

## Amendment: identity, not the serving certificate

Date: 2026-09-20 · Amends the decision above before any of it shipped.

### What was wrong

This ADR pinned the **TLS serving key** and treated a change in it as evidence
that the server had been replaced. [ADR 0044](0044-server-identity-and-peering.md)
had already considered that exact design, under the heading *"Pin the TLS
certificate and skip the second keypair"*, and rejected it on two grounds that
apply here unchanged:

- The serving certificate is **designed to regenerate silently.** `tlscert`
  treats a missing file, a corrupt PEM or an aging certificate as a cache miss
  rather than an error, so that one bad file cannot stop a server starting.
  Correct for a serving certificate; fatal for an identity.
- Under the **bring-your-own-certificate** path — ADR 0014's *recommended*
  production configuration — the operator supplies and rotates the
  certificate. Rotation is routine maintenance.

There is a third, specific to this project: the known fix for a certificate
whose SANs predate a new network interface is to **delete the certificate and
key and restart**. That is a documented repair. Under the original rule it
would have shown the other household the sentence reserved for an attacker.

A rule that fires on routine maintenance is worse than no rule, because the
refusal it produces is the one people learn to click past.

LANcast already had the right anchor. `internal/identity` is an Ed25519 keypair
that is generated **only** when none exists and is an error in every other case,
precisely so that it cannot quietly become somebody else.

### The corrected rule

**Two keys, two lifetimes, two questions.**

| | answers | lifetime |
|---|---|---|
| TLS serving key (SPKI) | *is this connection private* | rotates; may be replaced at any time |
| Identity key (ADR 0044) | *is this the server I know* | generated once, never regenerated |

The client stores both against an address, and judges on the identity.

**First contact** cannot use the identity, and that is a constraint rather than
a choice: `GET /api/identity` is session-gated by ADR 0044 §7, and there is no
session before there is a trusted transport. So first contact is still
trust-on-first-use over the serving key, with the fingerprint shown and a person
looking at it — compared against the same value on the server's own screen. That
is what the serving key is good for: it is the only thing both ends can see
before anyone has logged in.

**Once connected and signed in**, the page reports the server's identity
fingerprint and the client records it against that address. The record then has
an anchor that does not rotate.

**When the serving key changes** at a known address, the client no longer calls
it an attack. What it does depends on what it knows:

- **An identity is on record.** This is what a rotation looks like. The client
  says the connection key changed, shows the identity fingerprint it has, and
  asks the person to confirm out of band that the server is still that one.
  Confirming accepts the new serving key. Nothing is sent to the new key before
  that, so a server answering in the impostor's place receives no session.
- **No identity on record**, because nothing ever got as far as signing in.
  Then the two cases genuinely cannot be told apart, and the refusal stands.

**When the identity changes**, the refusal stands absolutely, and it is the
stronger statement of the two: the serving key may rotate under a server that
is still itself, but an identity that has changed means the data directory was
recreated or something else is answering. ADR 0044 §6 is why: identity belongs
to the data directory, so restoring a backup onto new hardware keeps it.

### What the server shows

`GET /api/settings` reports `certificate_fingerprint`, and Settings › General
shows it. That stays, because it is what the two ends compare at first contact
— but it is labelled as the **connection** key, and it sits beside the identity
fingerprint that `GET /api/identity` already served, so the durable one is the
one a person sees first.

### What this does not change

The trust record, address canonicalisation, the picker, the one-key-per-window
constraint and the relaunch are unaffected. This amendment changes which key is
authoritative and what a change in each one means — not how a server is chosen
or how a window is pinned.

### The process lesson

`CLAUDE.md` says to read the decision records before re-deciding, and this is
what it is for. ADR 0044 had already had this argument, written down the
answer, and named the losing option. The cost of not reading it was a security
rule that would have fired on maintenance, and a second fingerprint concept
alongside one that already existed.
