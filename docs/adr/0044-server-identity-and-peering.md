# ADR 0044 — Server identity and peering

Date: 2026-08-19 · Status: accepted

Closes the half of [ADR 0014](0014-transport-security.md) that ADR 0014 named
and deliberately left open: *"It encrypts the wire; it does not authenticate the
server, and the docs say so plainly."*

First ADR of the work planned in
[the Watch Together federation plan](../watch-together-federation-plan.md).

## Context

LANcast has no notion of itself as a particular thing. It has a data directory,
a password, accounts, and a certificate that proves nothing. Ask a running
server who it is and there is no answer to give, because until now nothing has
ever needed to ask.

Two people each running LANcast need exactly that. Before a room can span two
households, three things have to exist:

1. each server must **be** somebody,
2. the two must be **introduced**,
3. and each must be able to **prove**, on every later connection, that it is the
   one that was introduced.

### The constraint removes most of the answers

The fourth principle is no phone-home. That deletes, in one line, the entire
category of solutions the rest of the industry uses: no directory to look a
server up in, no rendezvous service, no identity provider, no account system in
the middle. There is nobody to ask who somebody is.

What survives is the only family that works with no third party at all:
**identity is a keypair, introduction is out-of-band, and proof is possession of
the private key.** This is Syncthing's shape, and it is the shape because the
constraints are the same, not because it is fashionable.

### Where TLS leaves off

ADR 0014 gave the server a self-signed certificate when it binds beyond
loopback, and was explicit about what that does and does not buy. On a LAN, wire
encryption between machines you own is most of the value; the far end's identity
is established by the fact that you walked past the machine.

Between two households none of that holds. The far end is on somebody else's
network, reached over a link neither of you controls end to end, and its
*identity* is the entire security property. A certificate nobody can verify is
worth nothing there.

### The precedent is already in the tree

[`internal/certpin`](../../internal/certpin/certpin.go) solved a smaller version
of this for the desktop window, and its reasoning transfers whole:

> The trust here is deliberately narrow. Not "ignore certificate errors", which
> would accept anything on the LAN pretending to be the server — the exact
> attack TLS is here to stop. This pins **one public key** […] and every other
> certificate is validated normally.

Federation pins peers the way the desktop window already pins its own server.
This ADR is that idea pointed outward.

## Decision

### 1. A server's identity is an Ed25519 keypair

Generated on first run, persisted `0600` under `<data>/identity/`, stable for
the life of that data directory.

Ed25519 because there are no parameters to choose badly, the keys are small
enough to paste, and it is already the algorithm this project signs releases
with. One algorithm means one set of habits rather than two.

This is the first key that belongs to **an installation** rather than to the
project. The release-signing key and the plugin key are compiled into the binary
and are the same on every copy; this one is different on every copy, and that is
the point.

### 2. The identity is the public key; the fingerprint is for humans

The fingerprint is SHA-256 over the raw public key, rendered base32, uppercase,
unpadded — 52 characters, displayed in groups of four.

**Not truncated.** A shortened fingerprint is a smaller target to collide with,
and the whole security property rests on this value being hard to forge. It is
long because it has to be.

The fingerprint's job is **confirmation, not transport**. What actually moves
between two people is the invite blob below; the fingerprint is what you read
down the phone to check that the blob you pasted came from who you think it did.
Sizing it for reading aloud at the cost of collision resistance would be
optimising the wrong half.

### 3. Introduction is out-of-band, and mutual

An **invite blob** — a single copyable string carrying the fingerprint, a
display name, and one or more addresses — is exchanged however two people who
know each other already talk. LANcast provides no channel for this and should
not: any channel it provided would be a third party.

**Receiving an invite has no effect on its own.** Pairing exists when each side
has added the other, and not before. Two reasons, and the second is the one that
matters:

- a relationship one party can create alone is a relationship that can be
  created *at* you;
- and consent that one side can grant on behalf of both is not consent. Every
  later decision in this feature — presence, roster visibility, joining — is
  built on top of the pairing, so if the pairing itself is one-sided, everything
  above it inherits that.

### 4. Every peer connection is mutually authenticated TLS, with the peer's key pinned

Not a CA. Not a hostname. The certificate presented on a peer connection must
carry the public key that arrived in the invite, or the connection does not
happen. Every other TLS connection the server makes — metadata providers,
update checks — validates normally and is untouched by this.

The transport reuses ADR 0014's existing certificate machinery. What this adds
is the authentication ADR 0014 left out.

### 5. The address is a hint; the fingerprint is the identity

A peer that moves gets a new address and is still the same peer. Addresses are
overlay-network addresses (see the federation plan), which are stable in
practice and guaranteed by nothing.

This is why the invite carries addresses **plural**, and why a failed connection
to a known address is a reachability problem rather than an identity problem.

### 6. Identity belongs to the data directory

Not to the machine, not to the install. This is the correct behaviour and it
cuts both ways, so both are stated here rather than discovered:

- **Restoring a backup onto new hardware keeps the identity**, and every
  existing pairing survives. That is what somebody restoring a backup wants, and
  it is the reason the key lives beside the database rather than beside the
  executable.
- **Copying a data directory clones an identity.** Two servers then claim to be
  one server, and their peers cannot tell them apart — because by this ADR's
  definition they *are* the same server. Anybody duplicating a data directory to
  run a second instance has made two of the same thing.

### 7. `GET /api/identity` is session-gated

It answers with the fingerprint and display name, for the settings screen and
for building an invite. Behind a session, because it confirms a LANcast is here
and what it is called, and an unauthenticated endpoint that does that is a
scanning target for no benefit — the invite travels out-of-band anyway.

## Alternatives considered

**Pin the TLS certificate and skip the second keypair.** Tempting, since
`certpin` already does exactly this locally, and it is one fewer secret.
Rejected on two counts, both of which are ADR 0014 behaving correctly for its
own purpose:

- The serving certificate is **designed to regenerate silently**.
  `tlscert.loadValid` treats any problem — a missing file, a corrupt PEM, an
  aging cert — as a cache miss rather than an error, on the stated reasoning
  that a server should not die because of one. Right for a serving cert; fatal
  for an identity, where silent regeneration means the server has quietly become
  somebody else and every peer's pin breaks with nothing to explain it.
- Under ADR 0014's **bring-your-own-cert path** — the *recommended* production
  configuration — the operator supplies the certificate and can rotate it.
  Rotating a certificate is routine maintenance and must not destroy peerings.

A long-lived identity key that the serving certificate rotates underneath keeps
both properties. The two things have different lifetimes because they answer
different questions.

**A shared pairing passphrase.** Much nicer to type than a fingerprint. Rejected
because a symmetric secret is held by both sides, either can leak it, and it
cannot survive being said out loud in a room — which is precisely the channel
people will use. With an asymmetric identity the thing you exchange is public
and the thing that proves you never leaves the machine.

**A discovery or rendezvous service, even an optional one.** Phone-home. Even
opt-in, it becomes the path everybody uses and then the path everybody depends
on. Out.

**Transitive trust — a peer vouching for a third.** Solves a problem this
project does not have at a scale it will not reach. Two households is the case;
designing a web of trust for it would be designing for an imagined product.

## Consequences

**Good — it closes a gap ADR 0014 wrote down and could not close.** Not a new
security posture, the completion of a stated one.

**Good — revocation is deleting a row.** Unpair and the pinned key is gone;
nothing signed by it verifies, no session it vouched for can be minted, and
there is no per-person cleanup to forget.

**Good — nearly all of it is testable with no network.** Key generation,
fingerprint rendering, invite parsing and pin verification are pure functions
over bytes. The same split that keeps `probe.ParseJSON` honest applies here, and
for the same reason: a design that needs two live servers to test one
if-statement is a design that stops being tested.

**Cost — losing the data directory loses the identity**, and every peer must
pair again. Acceptable, and worth saying in the docs before somebody finds out:
losing the data directory already loses the library, the accounts and the
history, so identity is not the part that hurts.

**Cost — fingerprints get mistyped.** Mitigated by making copy the normal path
and typing the exception, and by grouping. It cannot be mitigated away entirely,
and shortening the fingerprint to help is the trade this ADR refuses.

**Cost — one more secret to keep out of everything.** The identity key must
never reach the API, the logs, or a crash report — this project ships crash
reporting, and a key that leaks into one is a key published to whoever reads it.
`0600`, in the data directory, and nowhere else.

## What this does not decide

**What a paired peer may do.** Nothing here grants any access at all. Pairing
establishes only that two servers know who each other are; what that permits is
ADR 0046 (remote guests), and the answer there is deliberately close to nothing.

**Who may see whose viewing across a pairing.** That amends
[ADR 0035](0035-who-may-see-whose-viewing.md) and is its own decision. Note that
0035's unit of consent is a person, and a pairing is between *servers* — this
ADR gives the amendment something to hang on and settles none of it.

**How the two servers reach each other.** The federation plan takes that as an
overlay network the owner runs. Nothing in this ADR depends on it: an identity
is an identity over any transport.

## Revisit when

Somebody needs a server to have two identities, an identity to outlive its data
directory, or a pairing to be established by anything other than two people
deliberately exchanging a string.
