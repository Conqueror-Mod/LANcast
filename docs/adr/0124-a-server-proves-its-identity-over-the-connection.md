# ADR 0124 — A server proves its identity over the connection

Date: 2026-11-15 · Status: accepted

Amends [ADR 0070](0070-the-desktop-client-can-trust-a-server-it-did-not-install.md)
(what the client does when a known server's certificate changes) and adds one
narrow exception to [ADR 0044](0044-server-identity-and-peering.md) §7.

## Context

ADR 0070, as amended, has two keys per remote server: the TLS serving key,
which rotates, and the ADR 0044 identity, which never does. When the serving
key at a known address changes and an identity is on record, the client was
to "ask the person to confirm out of band that the server is still that one".

The whole-app review found that this confirmation compares two copies of the
same number (finding R11-1). The rotation screen showed the identity *from the
client's own record* and asked the operator to read out theirs. The real
operator's screen shows the real identity, the record holds the same value,
and nothing in the comparison involves the key that has just appeared. So it
matched whoever was answering, and accepting it handed an impostor the pin and
then the session cookie.

A stopgap shipped first: the prompt now shows the new *certificate*
fingerprint, read from the connection, and asks the operator to read theirs
out. That comparison is honest, but it is a person reading 44 characters of
base64 over a phone, for an event that is usually routine maintenance. People
will stop reading it.

The identity can answer the question by itself, if the server is asked to use
it. Proof is possession of the private key — ADR 0044's own words — and
nothing has ever asked for that proof on this path.

## Decision

**When the serving key at a known address changes, the client asks the server
to sign a challenge with its identity key, bound to the TLS connection that
presented the new key, and verifies it against the identity on record.** A
valid proof accepts the new key with no comparison by a person. An invalid one
is refused outright.

### The endpoint

`GET /api/identity/proof?nonce=<n>&fingerprint=<f>`, with no session.

- `nonce` is 16 to 64 random bytes from the client, base64url without padding.
- `fingerprint` is the identity the client expects (ADR 0044 §2 form; case and
  grouping are ignored).
- The server answers only if `fingerprint` is its own. Anything else is the
  same `404` as a route that does not exist.
- It answers only over TLS. Over plain HTTP there is no connection key to bind
  to, and the answer is `404`.
- The answer is `{public_key, signature}`, both base64 (standard, padded):
  the identity's raw 32-byte Ed25519 public key, and its signature over

  ```
  "LANcast identity proof v1" || 0x00 || nonce || EKM
  ```

  where `EKM` is 32 bytes of TLS keying material exported from *this
  connection* (RFC 5705 / RFC 8446 §7.5) with the label
  `EXPORTER-LANcast-identity-proof` and no context.

### What the client checks

On one TLS connection, whose leaf certificate's SPKI is the key the client is
deciding about:

1. `SHA-256(public_key)` is the identity on record (ADR 0044 §2 fingerprint).
2. The signature verifies under that key, over the same message built from its
   own nonce and its own export of the same connection's keying material.

Both hold: the key is accepted and the record keeps its identity. Either
fails: refused, with the identity-changed sentence, because something at that
address answered for the identity and could not prove it.

### Why exported keying material and not the SPKI

The review's sketch signed `nonce || SPKI of the key serving this connection`.
That would also work, but it needs the server to know which certificate served
a given connection, and a LANcast server can be serving any of four: the
generated one, an operator's own (ADR 0088), the public one on 443 (ADR 0081),
or the loopback name's. The handler would have to reconstruct a choice the TLS
stack already made.

Exported keying material is that choice, already made: it is unique to one
TLS session and both ends derive it independently. Somebody in the middle runs
two sessions, one each way, with two different exports, so a signature the
real server makes over its session is wrong for the client's. It binds the
proof to the connection — and through the connection, to the certificate the
client saw on it — with no help from the handler. Go's TLS refuses to export
from a TLS 1.2 session without the extended master secret, which is the case
where an export could be shared between two sessions; a client that gets that
refusal treats it as no proof.

### No session, and the exception to ADR 0044 §7

ADR 0044 §7 keeps `GET /api/identity` behind a session because an
unauthenticated endpoint that confirms "a LANcast is here, and this is it" is
a scanning target. This endpoint has to be reachable before a session, because
the moment it is needed is the moment the client has not yet decided to send
one. That is the point: nothing is sent to an unconfirmed key.

It keeps §7's property by answering only somebody who already knows the
answer. A caller must name the fingerprint, and a wrong one gets the same 404
as no route. A scanner learns nothing it did not bring; a client that has the
fingerprint learns only that the server holds the key for it, which is the
question.

The cost of a request is one Ed25519 signature. It is not rate-limited: a
request that does not name the fingerprint costs a map lookup and a string
comparison, and one that does comes from somebody who already has it.

### A signing oracle, considered

The identity key also signs guest tickets (ADR 0046), its own peer
certificate, and peer TLS handshakes. This endpoint signs a message chosen
partly by the caller. The fixed prefix is the domain separation: a guest
ticket payload starts with its own `lancast-guest-ticket/1`, a certificate is
DER and starts with `0x30`, and a TLS 1.3 handshake signature covers 64 spaces
and a fixed context string. A message that starts `LANcast identity proof v1`
can be none of them.

### When there is no proof

An older server answers `404`. So does a server behind a proxy that terminates
TLS, because the server and the client then hold different sessions. So does
an impostor, who will simply decline.

In all three the client falls back to the stopgap: it shows the new
certificate fingerprint and asks the operator to read theirs out. A downgrade
lands on the honest manual check, never on acceptance. Refusing outright when
there is no proof is the stronger rule, and it is the right one once every
supported server answers — but not in the release that introduces the
endpoint, when every existing server would be refused.

## Rejected

**Keep the person comparing, and only fix what they compare.** That is the
stopgap. It is honest and it is a chore; a check that fires on routine
maintenance and needs two people on a phone is the one that gets skipped.

**Sign the SPKI.** See above: the same strength, with the handler re-deriving
a decision the TLS stack already made.

**Make `GET /api/identity` public.** It would hand every scanner the
identity, which is what §7 refused, and it would still prove nothing: an
impostor can repeat a public fingerprint. Possession is the proof, not the
value.

**Use the peer mTLS path (ADR 0044 §4).** A desktop client has no identity of
its own and no pairing; the federation listener is for servers.

## Consequences

A reissued certificate on a known, current server is accepted after the
server proves itself, and the rotation screen says that it did. Nobody has to
read anything aloud.

An impostor at a known address cannot pass: it does not hold the identity key,
and it cannot relay the real server's proof because the proof is bound to a
session it is not party to.

A remote server must be this release or later to prove itself. Until then the
manual comparison remains, and it is the honest one.

The endpoint is public in the narrow sense above, and the API contract
documents it as such.
