# Security posture

> **Status: multi-user accounts, an audit log, paired servers, and an opt-in
> public name.** Each person has an account with a role (admin or member), and
> an administrator can narrow what a member sees; an unsecured server binds to
> loopback only; authorised mutations are recorded; a server can pair with
> another and admit its people as guests; and a server with a public name can
> earn a publicly trusted certificate and offer passkeys. This document
> describes what is protected, what is not, and what remains open.
>
> Checked against the code on 2026-11-16 (`main` at `b6eaa2cb`), after the
> whole-app review in [review/](review/). Where a section names a review
> finding (R01-4 and so on), its ledger in [review/findings/](review/findings/)
> has the detail.

## Current model

**Accounts with roles.** Each user has a name and password (bcrypt, cost 12),
exchanged for a server-side session stored as a SHA-256 of the token. Every
account is an **admin** or a **member** ([ADR 0015](adr/0015-multi-user-accounts.md)):
an admin manages users, creates libraries, and reaches settings; a member
browses, plays, and owns their own watch state. The role split is a real
privilege boundary enforced on the server, not merely hidden in the client.

**An unsecured server is loopback-only.** With no account created, LANcast binds
`127.0.0.1` rather than all interfaces — the port is not open on the network at
all, not merely refusing. Rejecting requests after accepting them would still
mean a listening socket answering strangers. Creating the first account (an
admin) and restarting enables LAN binding.

**Loopback keeps out other machines, not other origins.** Any web page open in
the owner's browser can send a request to `127.0.0.1`, and a page that rebinds
its own hostname to `127.0.0.1` can read the answers. So while no account
exists, two checks still hold even though no session is asked for:

- **The `Host` must name this machine** — `localhost`, `127.0.0.1` or `[::1]`,
  or the server's public name while its certificate is valid (the desktop
  window opens that name, mapped to `127.0.0.1`, and after `reset-auth` a server
  with no account can still have one). A rebinding page's requests carry its
  own name in `Host` and are refused with `403`.
- **The CSRF origin check below applies**, exactly as on a configured server,
  so no other page can `POST /api/auth/setup` and become the first admin.

**Why sessions live server-side.** They are revocable. Changing your own
password, or an admin resetting yours, deletes your sessions — a password change
that leaves old sessions alive has not locked anyone out. A self-contained
signed cookie cannot express that without rotating a signing key. (One shared
password once revoked *every* session; with accounts, a change revokes only that
user's, so one person cannot log everyone else out.)

**Why only the token hash is stored.** The database is the easiest thing to
walk off with: it is one file, and backups of it exist by design. A stolen
copy yields no usable sessions.

**Signed-in devices can be seen and signed out.** Everybody can list the
devices signed in as them and sign out one, or all but the one they are using;
an administrator can do the same for every account, and that is audited
(`/api/profile/sessions` and `/api/sessions` in [api.md](api.md)).

**A member can be kept from a library, and held under a rating.** An
administrator can hide a library from a member and set the content rating a
member may not exceed ([ADR 0084](adr/0084-a-library-can-be-kept-from-an-account.md)).
Both are one access rule enforced in the store: an item it refuses answers
exactly as a missing one does, and the rule also reaches the lists that gather
items from across libraries (Continue Watching, history, people, photo
timelines). It applies to members only; an administrator could lift it in a
click, so it would protect nothing there. A new library is visible to everyone
until somebody narrows it.

**API keys are not sessions.** A third-party client can authenticate with a
long-lived key sent as `Authorization: Bearer`, stored as a hash like a session
token ([ADR 0061](adr/0061-an-api-key-is-not-a-session.md)). A key never reaches
an administrator route, whoever owns it, and cannot mint, list or revoke keys,
or change how its account signs in (two-factor, below).

- **Minting a key needs the account's password**, not only a session (R01-4).
  A key outlives every session, so a session left open, or a stolen cookie,
  must not be enough to turn into one.
- **Changing your own password keeps your keys.** That is deliberate: the
  machine that copies photographs should not break because somebody rotated a
  password. The keys are listed under Security and can be revoked one by one.
- **An administrator's reset of somebody's password revokes that account's
  keys**, signs it out everywhere, and says how many keys went, in the answer
  and the audit log. A reset is the "this account was compromised" action,
  and a key minted while it was compromised must not survive it.

**Two-factor sign-in is each person's own choice**
([ADR 0100](adr/0100-a-code-after-the-password-for-whoever-wants-one.md)).
With it on, the right password earns a challenge, not a session: good for five
minutes and five tries, traded for a session by a six-digit code from an
authenticator app or by one of ten single-use recovery codes. Turning it on or
off needs the password, and an API key cannot do either. An administrator can
only take it off somebody's account (a lost phone), never put it on. A wrong
code counts against the same throttles as a wrong password, so knowing the
password does not buy a fresh budget for guessing codes. Passkey sign-in skips
the code, because a passkey is already two factors. The TOTP secret is sealed
in the database (*Where secrets are kept*, below), and recovery codes are
stored as hashes.

**A native player borrows the session with a stream ticket**
([ADR 0068](adr/0068-a-native-player-streams-with-a-ticket.md)). libmpv cannot
carry the window's `HttpOnly` cookie and should not carry an API key, so the
window mints a ticket for one item. It opens `GET /api/stream/{id}` for that
item and nothing else (for a retro game, also that game's files, the minting
person's saves for it, and its RetroAchievements requests). It is sent as a
header, never in a URL, lives only in memory, and re-checks the credential that
minted it on every use, so signing out, a password change or revoking the key
ends the ticket too.

**An invite link makes one member account, once**
([ADR 0104](adr/0104-a-friend-is-invited-by-a-link-that-works-once.md)). Only an
administrator can make one. It lasts seven days, carries the content-rating
ceiling and hidden libraries the administrator chose, and always makes a
**member**. The secret is in the link's fragment, which a browser never sends,
so it reaches neither the request log nor a `Referer`. The two public routes
take it in a `POST` body. Only its hash is stored. Redeeming deletes the invite
in the same transaction that makes the account, so two people racing one link
get one account. A wrong, spent, withdrawn or expired secret all get the same
`404`, and guesses are throttled per address before any lookup or password
hashing is done (R01-8).

## What requires a session

Everything under `/api/` except these:

| Public | Why |
|---|---|
| `GET /api/health` | A monitor should not need credentials |
| `GET /api/auth/status` | The client must know whether to show setup or login |
| `POST /api/auth/setup` | Only works while unconfigured, which is loopback-only |
| `POST /api/auth/login` | Obviously |
| `POST /api/auth/login/two-factor` | The second half of a two-factor sign-in: the caller holds a challenge, not a session. Throttled with the password login |
| `POST /api/auth/passkey/login/options`, `POST /api/auth/passkey/login` | Passkey sign-in: whoever is signing in has no session yet. Throttled with the password login, and the challenge store is capped per address (R01-6) |
| `GET /api/auth/accounts` | The sign-in page's account picker. Names only, and only to the home network; anybody else gets an empty list. An administrator can turn it off |
| `POST /api/invites/check`, `POST /api/invites/redeem` | Joining with an invite link: whoever holds it has no account yet. The secret is the credential, and guesses are throttled |
| `GET /api/identity/proof` | A server proving its identity to a desktop window deciding whether to trust a new certificate (*Transport security*). Answers only a caller who already names this server's fingerprint, over TLS; anybody else gets `404` |
| `POST /api/guest/session` | Redeeming a guest ticket a paired server signed (*Another server*). The handler is the gate: signature, audience, pairing, expiry and nonce |
| The web assets | The login form lives in them — gating the page behind a session nobody can obtain yet is a locked door with the key inside |

The `/api/federation/` routes skip the session gate too, but are not public:
they are for paired servers, authenticated by the peer's pinned certificate
instead, and each one refuses a caller that did not present one. They are a
fixed list rather than a prefix, so a new route under that path is gated until
somebody adds it deliberately, and a test fails if the router has a federation
route the list does not.

Besides a session, the gate accepts four narrower credentials, each in an
`Authorization` header and never in a URL: an API key (anything but
administration and key management), a stream ticket (one item's stream), a
redeemed guest session (its own allow-list, below), and the metrics token
(`GET /api/metrics` and nothing else, below).

**Streaming is gated.** `GET /api/stream/{id}` requires a session (or a ticket
for that item). A public stream URL would make the password decorative.

**Every request body has a ceiling, before the gate.** 1 MiB for every route,
applied in front of the router and the session check, so the routes that
answer before sign-in cannot be made to hold an arbitrarily large body in
memory. A body that declares a larger `Content-Length` is refused with `413`
unread, and one that does not say is cut off at the ceiling. Four routes that
take a file have their own, larger ceilings: a profile picture, a game save, a
problem report and a plugin bundle. A handler may set a smaller limit of its
own, and the smaller one wins.

## CSRF

Two independent defences, because either alone leaves a gap:

- **`SameSite=Strict` on the session cookie.** A cross-site request does not
  carry it.
- **An origin check on every state-changing method.** `POST`, `PUT`, `PATCH`,
  and `DELETE` must present an `Origin` or `Referer` matching the request host.

A request with neither header is allowed: non-browser clients (curl, a TV app,
a script) legitimately send neither, and they are not the CSRF threat. A
request authenticated by an API key or a guest session skips the origin check,
but only when that credential actually resolved: a browser never attaches an
`Authorization` header by itself, so such a request cannot be forged the way
a cookie can, while a cookie request that merely *carries* a junk header keeps
both defences. Reads
are not origin-checked — the session gate covers them, and blocking
cross-origin `GET` would break embedding a stream URL.

This mattered specifically because adding a session cookie to an API that
previously had nothing worth stealing is what *creates* CSRF exposure. Before
auth, a malicious page could already reach `localhost:8080`; it just had
nothing to gain.

## Brute force

Login attempts are throttled per client IP — 10 per 5 minutes, decaying on
their own. A password is one guessable secret, so unlimited attempts against it
is the entire attack. Passkey sign-in, the two-factor code, and every place a
signed-in person re-enters their password share the same counter: changing it,
adding a passkey, minting an API key, and turning two-factor on or off all go
through one check that applies both this throttle and the per-account wait
below. Invite links have a counter of their own, per address.

The key is the remote address. Forwarded headers are attacker-controlled
unless a trusted proxy sets them, and honouring them from anyone would let
anyone reset their own counter. So `X-Forwarded-For` is read only from the
proxies named in **Server › Network › Trusted proxies** (`trusted_proxies`),
which is empty by default. It is read right to left, and the first address
that is not one of those proxies is the caller; anything further left is the
caller's own claim and is ignored. **If you deploy behind a reverse proxy, add
it there**, or every request looks like the proxy.

There is deliberately no lockout: an attacker who could lock the owner out of
their own server has achieved denial of service for free.

**Per account, from outside.** Per-IP throttling does nothing against guessing
spread over many addresses, which became the real threat once a server is
publicly reachable. So failures from outside the local network are also counted
against the account name tried: five free, then a wait that doubles from 30
seconds to a ceiling of 15 minutes — about a hundred guesses a day against one
account, however many addresses they come from. It is shaped to stay on the
right side of the no-lockout rule:

- Attempts made during the wait are refused **without being counted**, so the
  owner retrying never extends it.
- It does **not apply from loopback or a private address**. An attack from the
  internet can slow a remote sign-in; it can never lock the owner out at home.
  A home device on a global IPv6 address is treated as outside — the safe
  direction.
- Names that match no account are counted too, so a `429` does not reveal
  which accounts exist.

Behind a reverse proxy on the same machine that is not in the trusted list,
every caller looks local, so the per-account wait does not apply and the
sign-in page lists account names to anybody. Trusting the proxy fixes both.

## Losing the password

There is no password reset over HTTP, and there will not be one. A recovery
endpoint reachable by an unauthenticated caller *is* the authentication bypass
— it does not matter what it asks for first.

Recovery is local instead. Stop the server and run:

```
LANcast.exe reset-auth         # reports what it would remove
LANcast.exe reset-auth -yes    # removes every account and credential
```

(`lancast reset-auth` on Linux. Since [ADR 0080](adr/0080-one-executable-two-roles.md)
the server is the same executable as the app; see [install.md](install.md).)

The authority this requires is "can run a program on the server", which is the
same authority that could read the database file directly — so it grants an
attacker nothing they did not already have, and grants the owner a way back in.

Every credential goes with the accounts, not only passwords: sessions,
passkeys, API keys, two-factor secrets and recovery codes, pending invites,
and a connected RetroAchievements key. The replacement admin takes the old
first admin's user id, so anything left keyed on it would be inherited — and
the case this recovers from includes somebody else having taken the admin
account and added a passkey of their own.

Watch history survives: those rows are the library's data, not the account's,
and the replacement admin takes the same user id, so resume points reconnect.
Libraries, artwork, and settings are untouched. Afterwards the instance is in
the state a fresh install is in — unconfigured, and loopback-only until an
account exists.

On Windows the data directory belongs to SYSTEM and Administrators (*The data
directory*, below), so this needs an elevated shell; the command says so rather than reporting a bare SQLite
`readonly database` error.

## Transport security

**A server reachable beyond loopback serves HTTPS**
([ADR 0014](adr/0014-transport-security.md)). The password on login and the
session cookie on every request are therefore encrypted on the wire rather than
readable by anyone on the network path — which, on a semi-trusted LAN, is the
gap that mattered most.

**A loopback-only server stays plain HTTP.** When no password is set the server
binds `127.0.0.1` only; nothing on that wire is worth protecting, and a
certificate warning on `localhost` is pure setup friction. TLS turns on at the
same boundary that enables LAN binding.

**The certificate is self-signed unless you give it another.** With nothing
configured, LANcast generates a self-signed certificate on first LAN-bound run,
persists it `0600` under `<data>/tls/`, and reuses it across restarts. A
self-signed cert **encrypts the wire but does not authenticate the server**:
the first HTTPS visit shows a browser warning until the cert is trusted or
replaced.

**A certificate you bring is named by path** in Settings › Server › Network —
from an internal CA, `mkcert`, or a renewal tool
([ADR 0088](adr/0088-a-certificate-you-bring-is-followed.md)). The pair is
checked before it is saved, applied without a restart, and re-read when the
files change, so a renewal tool rewriting them in place keeps working. No
private key crosses the connection: the setting is a path on the server, not an
upload. A pair that will not load never stops the server; LANcast serves the
generated certificate instead and says why.

**Enabling TLS does not break `http://` bookmarks.** The listening port answers
a plaintext request with a permanent redirect to the same address over HTTPS,
so an old bookmark upgrades transparently instead of failing a TLS handshake.

**The LANcast window pins the key rather than trusting the store.** A self-signed
certificate is the case a browser cannot resolve — it either warns, or, against
a LAN-bound server, fails the handshake and retries so the app never loads. The
desktop client reads the server's own `cert.pem` from local disk and pins that
**one** public key; every other certificate is validated normally. This is the
security property the native window was built for
([ADR 0023](adr/0023-native-desktop-client.md)).

**A window trusts a changed key only on proof over that connection**
([ADR 0124](adr/0124-a-server-proves-its-identity-over-the-connection.md)). A
window that already knows a server by its identity
([ADR 0070](adr/0070-the-desktop-client-can-trust-a-server-it-did-not-install.md))
and finds a different serving key at the address asks
`GET /api/identity/proof` with a fresh nonce. The server signs the nonce and
keying material exported from *that* TLS session with its Ed25519 identity
key. Somebody in the middle necessarily runs a different TLS session on each
side, so the signature cannot be relayed. It replaced a check that asked a
person to compare two copies of an identity the window already held, which
matched whoever was answering (R11-1). The route answers only over TLS, only
to a caller who already names this server's fingerprint, and never for a TLS
1.2 session without the extended master secret, the one case where two
sessions could share an export.

**A public name can earn a public certificate, only if an administrator asks**
([ADR 0081](adr/0081-a-public-name-can-earn-a-public-certificate.md), amending
the "no ACME" of ADR 0014). It is off by default, and off on every upgraded
install: until an administrator turns it on in Settings, giving a hostname,
choosing Let's Encrypt (or its staging CA) and ticking the CA's subscriber
agreement, LANcast makes no ACME request of any kind. A certificate request is
a network thing the owner asked for by name, which is what the no-phone-home
rule permits. No contact address is sent unless one is entered, and the CA
directory is not settable through the API, so an admin session cannot point
the server's outbound requests at an arbitrary URL.

The CA proves the name with TLS-ALPN-01 on port 443, answered by LANcast's own
TLS listener; there is no plaintext challenge listener. While the certificate
is on, LANcast also listens on 443 itself, because IPv6 has no port
translation; that listener closes when the certificate is turned off. The
public certificate is served **only to a client that asks for that name**: by
IP address, on `localhost` or with no name, clients get the self-signed
certificate as before, and paired servers keep their identity certificate. It
uses the serving key LANcast already has, so the desktop window's pin survives
renewals.

**Passkeys exist only at the public name**
([ADR 0082](adr/0082-a-passkey-belongs-to-the-public-name.md)). Two platform
rules decide that: a passkey is bound to a domain name, and browsers refuse
them on a certificate error. Over TLS, for that name, while its certificate is
valid, an account can add passkeys and sign in with one; everywhere else the
sign-in page offers no passkey and the routes answer `409`. A passkey is **in
addition to the password**, never instead of it: the password stays the
recovery path and the way to sign in by address. Changing the password does not
remove passkeys, and still ends every session.

### What is quantum-resistant

**The key exchange is hybrid post-quantum; nothing that proves identity is.**

- **Key exchange: X25519MLKEM768, by default.** LANcast never sets
  `CurvePreferences`, so Go (1.24 and later) offers and prefers the hybrid
  X25519 + ML-KEM-768 group on every TLS 1.3 connection whose other end offers
  it too. Traffic recorded today cannot be decrypted later by somebody who
  gets a quantum computer, as long as ML-KEM or X25519 holds. Checked with
  real handshakes on 2026-11-16 (Go 1.26.11), against a listener built with
  the server's own TLS calls on loopback:

  | Client | Negotiated |
  |---|---|
  | Go, default `tls.Config` (any Go client of the API) | TLS 1.3, X25519MLKEM768 |
  | `certpin.TLSConfig` (libmpv's relay, the retro player) | TLS 1.3, X25519MLKEM768 |
  | `peer.ClientConfig` (one paired server to another) | TLS 1.3, X25519MLKEM768 |
  | Edge 157, headless (the Chromium major version of the installed WebView2 runtime, which is the desktop window) | TLS 1.3, X25519MLKEM768 |
  | The Claude desktop app's Chromium pane (ClientHello only; it refused the self-signed certificate) | offered X25519MLKEM768 first |
  | OpenSSL 3.5.6 `s_client` | TLS 1.3, X25519MLKEM768 |
  | curl 8.19 with Schannel (Windows' own TLS, as PowerShell and .NET use) | TLS 1.3, **X25519** |

  The last row is the limit: a client whose TLS library does not offer ML-KEM
  gets classical X25519, and nothing on the server can change that. Windows'
  own Schannel offered no ML-KEM group on this machine. A TLS 1.2 client gets
  classical ECDHE. The browser rows are Chromium builds, not the LANcast
  window itself driven against a server: WebView2 shares Edge's network stack,
  so it is expected to match, but that row was not captured from the window.

  `internal/peer/pqkex_test.go` holds this: one test fails if any Go source
  in the repository names `CurvePreferences` or turns the hybrid off with
  `tlsmlkem=0` / `tlssecpmlkem=0` (in a `//go:debug` line or `go.mod`), and
  three run the handshakes in the first three rows and require the hybrid.
- **Signatures and identity: not quantum-resistant.** The server identity, the
  guest tickets it signs, the identity proof above and the update signature
  are Ed25519. The TLS certificates are ECDSA (self-signed) or whatever the CA
  issues. A quantum computer that could break these could impersonate a
  server, but only while it is actively in the connection. It cannot decrypt
  recordings, because the key exchange above does not depend on them. Moving
  them is for when the platform offers post-quantum signatures in TLS and in
  WebAuthn, not something LANcast can do alone.
- **Sealed secrets: fine.** AES-256-GCM with a 256-bit key (*Where secrets are
  kept*). Grover's algorithm halves the effective strength of a symmetric key
  at best, which leaves 128 bits. Session and key hashes are SHA-256, and
  passwords are bcrypt, for the same reason.

## The audit log

Every authorised mutation is recorded with who did it and to what — libraries,
titles, matches, accounts, add-on trust ([ADR 0026](adr/0026-audit-log.md)).
Readable by admins from Settings.

**Browsing and playback are deliberately excluded.** An audit log that also
records what people watched is a surveillance log wearing an operations name,
and this project decides who may see whose viewing elsewhere and much more
narrowly ([ADR 0035](adr/0035-who-may-see-whose-viewing.md)).

## Another server, and its people

Two LANcast servers can pair, see each other's presence, and admit each
other's users as guests. This is the largest security surface added since this
document was first written, so the properties are stated rather than implied.

**A server has an identity.** A keypair and a fingerprint
([ADR 0044](adr/0044-server-identity-and-peering.md)), which closes the half of
ADR 0014 that ADR 0014 named itself: TLS encrypts the wire, it does not
authenticate the server. Only the public half and the fingerprint are ever
exported — LANcast writes crash reports to its own data directory (nothing is
sent anywhere), and a private key that reaches a crash report is a key
published to whoever later reads or shares that file.

**Pairing takes both sides.** Parsing an invite is not pairing; it exists when
each side has added the other. A relationship one party can create alone is one
that can be created *at* you.

**Peer connections are mutual TLS 1.3, pinned both ways.** Each server
presents a certificate for its Ed25519 identity key, and each side checks the
other's key against the fingerprint recorded at pairing. A certificate a CA
issued for the right name would still fail that check.

**Presence is never written down.** Who is watching what, right now, is shared
only under a per-account, per-peer grant, and there is no history and
deliberately no *last seen watching*
([ADR 0045](adr/0045-live-presence-between-paired-servers.md)). The harm ADR
0035 names is a record that accumulates; this one does not exist to accumulate.
Each account's **visible to peers** switch is off by default, and it outranks
the grants (R04-8): with it off, no paired server is sent that person's
presence whatever grants exist, and the person is not named in the roster a
paired server receives (R02-4). The grants are kept, so turning visibility
back on restores them.

**A remote guest is a principal, not an account.** Admitted by a ticket their
own server signs, default-deny in middleware — because `requireAuth` grants by
default and withdraws by exception, which is the wrong direction for somebody
who is not a user here ([ADR 0046](adr/0046-remote-guests.md)).

- **The ticket.** Ed25519-signed by the guest's own server, naming the person
  (a server can only mint one for the signed-in person asking), the host's
  fingerprint as audience, a random nonce, and an expiry two minutes out, with
  a minute's allowance for a clock that is ahead. The host verifies it against
  the key it pinned at pairing and spends the nonce, so a ticket works once.
  Every refusal looks the same from outside; the reason goes to the host's log.
- **The session it buys** lasts fifteen minutes and is renewed by presenting a
  fresh ticket. At most 512 exist at once, and a full book refuses rather than
  evicting somebody.
- **What a guest may reach** is one allow-list in `guestgate.go`, every entry a
  `GET`: who they are, the libraries shared with their server and their items,
  and the stream, conversion and subtitle routes. Every route that names an
  item is checked against that item, and a refusal is the same `404` as an item
  that does not exist. Allow-listing the routes alone would hand over the
  library, since the stream handler streams whatever id it is handed.
- **Which items.** Those in a library this server has shared with the guest's
  server ([ADR 0071](adr/0071-a-shared-library-is-a-standing-grant.md)), under
  the content-rating ceiling set on that share. (Before ADR 0071 a guest could
  stream only the item a Watch Together room was playing; a share is now a
  standing grant to the other household.) The share is looked up on every
  request, so un-sharing or unpairing takes effect on the next one, not when
  the session lapses.
- **Nothing else.** A guest writes no state at all, and what they may be sent
  is capped by the host rather than by their own capability
  ([ADR 0047](adr/0047-remote-streaming-is-capped-by-the-host.md)).

## Your other devices

**Play on another device**
([ADR 0117](adr/0117-play-on-another-device.md)) lets one of your windows send
*play*, *pause* and the like to another window signed in as **the same
account**. Every route is scoped to the caller's own account. There is no
administrator view, and a command to somebody else's player is a `404`, the
same answer as a player that does not exist. The receiving window plays
through its own session, so library access and the rating ceiling apply as if
it had been pressed there. The desktop window offers itself by default. A
browser or a phone does so only once switched on in Settings › Playback.
Players are held in memory only.

## What leaves the server, when an administrator asks

None of these does anything until an administrator sets it up, which is what
the no-phone-home rule permits.

**Webhooks** ([ADR 0097](adr/0097-a-webhook-goes-where-the-owner-points-it.md)).
Administrator-only, at most ten, `http` or `https` with no credentials in the
URL. LAN addresses are allowed, because that is where Home Assistant lives, so
an administrator can point the server's requests at any address the server can
reach; that is accepted as an administrator's power. Each delivery is signed
with HMAC-SHA256 under the webhook's own secret
(`X-LANcast-Signature: sha256=…`). The secret is shown once, when the webhook
is made. Deliveries never follow a redirect, time out after ten seconds, and a
failure is logged without the URL, which can carry a token. The events
are playback started or stopped, a scan finished, and a backup taken.
**Playback events name an account and a title, so they are sent only for
accounts that switched that on themselves** (off by default, and an
administrator cannot switch it on for somebody). Webhooks live in
`config.json`, not the database, so restoring a backup does not bring back one
that was deleted.

**The metrics token** ([ADR 0096](adr/0096-metrics-have-their-own-key.md)).
`GET /api/metrics` (Prometheus text) answers an administrator's session or a
token an administrator generates in Settings. The token is shown once, stored
only as a hash, compared in constant time, and is a credential for that one
route and method only. Presented anywhere else it is simply an unknown key. It
is not an API key, because the numbers (accounts, libraries, running
conversions) are an administrator's view, and API keys never reach those.

**Provider keys a release carries**
([ADR 0110](adr/0110-lancast-carries-its-own-provider-keys.md)). Release builds
carry a TMDB and an OpenSubtitles key, set at build time from repository
secrets and never committed, so a fresh install matches films with no
sign-up. Anybody with the binary can read them out; that is true of every
client that ships a key, and a revoked one is replaced by shipping a release.
A key the person enters always wins. **Settings can switch the built-in keys
off** (`builtin_keys_off`), and then a server with no key of its own fetches
nothing from either service. Plugins are never handed a built-in key, only one the
person entered, because an abusive plugin could get the built-in key revoked
for every install. A provider's key is stripped from any network error before
it reaches the log (R08-6), since the log is what *Report an issue* attaches.

**Report an issue** ([ADR 0109](adr/0109-a-report-goes-only-when-sent.md))
sends nothing until somebody presses Send: there is no background report and
no crash upload. It posts to the developer's own webhook, whose URL is a build
secret like the provider keys. What goes with it is said beside the button,
never an account name, address or the server's identity unless typed, and
only an administrator can attach the server log, after reading it. A failed
send is logged without that URL (R07-9).

## DLNA

A TV on the LAN can browse and play through DLNA
([ADR 0086](adr/0086-a-tv-browses-as-somebody.md)). Off by default. It runs only
when it is switched on, an account to browse as is chosen and still exists,
and the server is LAN-bound (a password is set), and it stops within seconds
of any of those ceasing to hold.

DLNA has no authentication: renderers cannot sign in. So it is bounded in
other ways:

- **Its own plain-HTTP listener** on port 8200, serving the DLNA routes and
  nothing else. No LANcast API is reachable through it.
- **Local network only.** Requests and discovery from anything but a private,
  loopback or link-local address are refused, judged on the connection rather
  than a header, so port forwarding or a public IPv6 address cannot make it an
  open media server (R10-8).
- **It sees what its account sees.** Every lookup is made as the chosen
  account, with that account's hidden libraries and rating ceiling. Choose an
  account limited to what anything on the LAN may play.

## Still unprotected

- **No API rate limiting** beyond the places a password, code or secret is
  guessed: sign-in and its second factor, passkey sign-in, re-entering a
  password while signed in, and invite links. Signed-in traffic is not
  limited, and neither is DLNA on the LAN.

## Standing risks that authentication does not remove

**Adding a library is arbitrary read access — now bounded to admins.**
`POST /api/libraries` accepts any path that exists and is readable, so it can be
pointed at `C:\Users` to scan and stream everything underneath, and `GET
/api/browse` makes finding such a path trivial. Both are **admin-only**
([ADR 0015](adr/0015-multi-user-accounts.md)), so this power belongs to the
owner and whoever they deliberately promote — not to every account. A member
cannot reach either endpoint. This is the first version where handing someone a
login does not hand them the filesystem.

## What is protected

These hold regardless of authentication and should not regress.

**Stream containment.** Every handler that turns a database row into a file
path (the stream, and the other routes that open a library file) re-checks
that the path still falls inside the owning library root before opening
anything, through `internal/contain`. The check **follows links**
([ADR 0094](adr/0094-containment-follows-links.md)): the lexical
`filepath.Abs` + `filepath.Rel` test it replaced passed a `film.mkv` that was a
link to anywhere on the machine. The database is trusted, but a hand-edited or
corrupted row must not become arbitrary file read access. Covered by tests
asserting that an outside path, a traversal path and a link out are refused
*and* that the file contents are not served.

**Artwork hashes are validated.** A hash arrives from a URL path and becomes a
filesystem path, so it is checked for length and hex-only characters first.
Traversal attempts are a cache miss, never a file read.

**Filesystem paths are never serialized.** `Item.Path` is `json:"-"`. Clients
have no use for server paths, and withholding them keeps the layout private
even when the API is reachable.

**Secrets are write-only.** No API returns a secret after the answer that
created it. A secret readable back out of an API leaks through screenshots,
logs, and shared sessions. For a provider key, `GET /api/settings` reports
whether one is set and which key is in use (`configured`, `source`), never the
value; for the metrics token, whether one exists and since when.

**Where secrets are kept.** Three places, each chosen for whether a backup
should carry it (a backup is the database, [ADR 0058](adr/0058-a-backup-is-the-database.md)):

| Secret | Where | In a backup? |
|---|---|---|
| Provider keys (TMDB, OMDb, OpenSubtitles) and the OpenSubtitles login, webhook URLs and secrets, the metrics token's hash | `config.json`, written `0600` | No |
| Two-factor (TOTP) secrets, the RetroAchievements key | The database, **sealed** with AES-256-GCM under `secrets.key`, a key file beside the database ([ADR 0090](adr/0090-retroachievements-are-yours-to-connect.md)) | Sealed, and unopenable without that machine's `secrets.key`: restored elsewhere, people enter them again and two-factor accounts sign in with a recovery code |
| Session, API key, invite and recovery-code hashes; password hashes (bcrypt) | The database | Hashes only |
| A plugin's own credentials | The database, **not sealed** | Yes, in the clear |

On Windows the `0600` modes mean nothing; what protects all of these on disk
is the data directory's own permissions (*The data directory*, below). The
last row is the exception to the sealing rule: a plugin credential someone
entered travels with every copy of the backup.

**Browse lists directories only.** Never files, never contents, and hidden or
system directories are omitted.

**An update is proved twice.** The downloader checks the release's signature
(a key compiled into the binary) and the archive's digest before anything is
staged. The service then checks all of it again when it moves the staged files
into the install directory at shutdown. It checks the signature over the
checksum list, the archive against it, the archive's name against the staged
version, and every staged file's SHA-256 against the archive. It refuses any
name outside the install set, and any version not newer than the one running.
Each file is checked once more where it lands, and a mismatch puts the install
back. Before this (R10-1), a staging that any local account could write in the
data directory was moved into Program Files as SYSTEM unchecked. It no longer
depends on the data directory's permissions for this.

**A restore does not bring back revoked credentials** (R04-3, ADR 0058
amended). A restored database has its sessions, **API keys and passkeys**
cleared, because a key revoked after a leak, or a passkey deleted for a lost
phone, would otherwise come back with the backup. Everyone signs in again with
their password, and re-issues keys and re-adds passkeys, and the restore's
result says how many of each went. **Recovery codes are kept**: only unused
hashes are stored, so a restore cannot tell which were spent after the backup,
and clearing them could lock a two-factor account out on a machine where the
TOTP secret cannot be unsealed. The result tells two-factor accounts to make
new codes if they used one since the backup.

**A restore runs only from files LANcast wrote.** A restore staged from Settings
is a marker in `backups\` that the next start acts on before it opens the
database. As the service, that start asks the tools guard (*Which binary runs*)
about the marker and the backup it names. It refuses either one if it is owned
by anyone but SYSTEM or Administrators, or if anyone else can write it. Before
this (R10-2), any local account could stage its own database and sign in as an
administrator after the next restart. A backup copied into the folder by hand
from an unelevated account is refused for the same reason. Copy it from an
elevated shell instead, so Administrators own it.

## Deployment guidance

**Do not port-forward a server that only has its self-signed certificate.** A
self-signed certificate cannot prove the server's identity to a client that has
never seen it. A VPN keeps the server off the public internet entirely, and is
still the simplest safe choice.

**Recommended: a VPN that puts your device on the LAN.** Tailscale and
WireGuard both do this well. Nothing is exposed publicly, traffic is encrypted
by the VPN, and LANcast needs no configuration at all. This keeps the
no-phone-home principle intact and avoids reimplementing solved problems badly.

**Alternative: a public name, directly.** Give the server a hostname, forward
port 443 to it, and turn on the public certificate (above). Clients then get a
certificate their browser trusts, accounts can add passkeys, and failed
sign-ins from outside the home network are slowed per account however many
addresses they come from. Nothing is relayed through anybody else. It is also
the internet reaching your sign-in page, so use strong passwords and add
passkeys.

**Alternative: a reverse proxy terminating TLS.** Caddy or nginx in front of
LANcast, with a publicly trusted certificate. Have the proxy set
`X-Forwarded-For`, and add its address under **Trusted proxies** in Server ›
Network — otherwise LANcast's throttle sees every request as coming from one
client, the per-account wait treats every caller as local, and the sign-in
page's account picker is shown to the internet. Point the proxy at LANcast over HTTPS, or run
LANcast loopback-only behind it. In the second arrangement have the proxy also
set `X-Forwarded-Proto: https`: the hop to LANcast is plain HTTP, and from a
trusted proxy that header is what marks the session cookie `Secure`, so a
later plain `http://` address for the same name cannot carry it in the clear.

Built-in TLS with a self-signed certificate
([ADR 0014](adr/0014-transport-security.md)) closes the plaintext hole on the
LAN; on its own it is not a substitute for any of the above when reaching
LANcast from outside the network.

Treat the LAN itself as semi-trusted. A compromised phone, TV, or IoT device on
your network can reach the login endpoint and is inside the boundary LANcast
relies on.

## The data directory

As a Windows service the data directory is `C:\ProgramData\LANcast`, and until
the review (R10-2, R10-3) it kept ProgramData's default permissions. Those let
every local account read every file in it (the identity and TLS private
keys, `secrets.key`, the database, the backups) and create files and folders in
every folder of it. Creating was the worse half: a planted `update-staged`, a
planted `restore-pending.json` or a planted `faces\lancast-faces.exe` was each
a way to SYSTEM, or to being a LANcast administrator, that skipped UAC. The
`0600` modes the code writes its secrets with mean nothing on Windows.

The folder now has its own descriptor (`internal/dataperm`):

| | |
|---|---|
| Owner | Administrators |
| DACL | protected: nothing is inherited from ProgramData |
| SYSTEM, Administrators | full control, inherited by everything inside |
| Users | list the top folder only, inherited by nothing |
| `tls\cert.pem` | Users may also read this one file |

`icacls C:\ProgramData\LANcast` shows it as `BUILTIN\Users:(RX)` with no
`(OI)(CI)` and no `(I)`.

Users keep two reads, because the desktop window and the tray run as the
person, not as the service:

- **Whether `lancast.db` exists.** That is how the window knows the library is
  the service's, and waits for the service instead of starting a second server
  on an empty per-user library (`sharedDataDir`, `servermode.HasLibrary`).
  Listing the top folder is what lets that check succeed. NTFS lets whoever
  may list a folder read its entries' attributes, never their contents.
- **The public certificate.** Beyond loopback the window trusts the server by
  the key in `tls\cert.pem` (`certpin.SPKI`) and loads nothing without it. The
  service grants Users read on that file after loading it, every start, since
  a new certificate is a new file. `key.pem` beside it gets nothing.

Nothing else a person's process does reads the folder. Logs, crash reports,
backups, the update and the face models are reached through the API, as the
server. `service install`, `service tools`, `reset-auth`, `restore` and the
installer already need an elevated shell.

It is applied by `service install`, by a switch to the service before the
library is copied in, and **at every start of the service**. An install from
before it is closed by its first start after the update. That start rewrites
every inherited entry in the tree, and gives back to Administrators anything
inside that the tools guard below refuses: an entry owned by an account other
than SYSTEM or Administrators, or one that grants someone else write. An owner
can always rewrite its own permissions, so a file planted while the folder was
open would otherwise stay its planter's. Links are reported, not removed.
A start that finds the descriptor already in place reads it and moves on.

Not covered: a systemd install. Its data directory is `/var/lib/lancast`,
created with the service account's mode, and ProgramData's add-files grant has
no counterpart there.

## ffmpeg's privileges

ffmpeg and ffprobe read every library file, and those files are untrusted input
(see *Threats explicitly out of scope*). The question this section answers is
how much a malicious file gets if it exploits them. Today the answer is
everything: **the Windows service runs as LocalSystem, and its children run as
SYSTEM**. Under systemd the unit has no `User=` line, so they run as root.

### Which binary runs

As of R09-1, which binary runs is settled. The service runs ffmpeg only from a
folder that Administrators and SYSTEM alone can change. That is checked from
the folder's owner and DACL on every start, and the service's PATH is filtered
the same way. A per-user install is copied into the protected `tools` folder at
install, not recorded where the user can replace it. What is left is what the
binary does with a hostile file.

The face worker is held to the same rule (R10-5, R12-1). As the service,
`lancast-faces.exe` is run only from beside `LANcast.exe`, where the installer
puts it, never from the data directory's `faces` folder or from PATH. Before
every start, the guard checks the worker, its folder, the faces folder and the
`onnxruntime.dll` the worker loads from it. A refusal is shown in Settings ›
Photos with its reason.

### The pinned build

The in-app download is pinned to an exact asset and checksum (ADR 0043):
`FFmpeg-Builds/FFmpeg-Builds` `autobuild-2026-10-31-12-59`, ffmpeg
**n8.1.3-27-g8dca1586f4**, win64 GPL static. That is 27 commits past n8.1.3
on the 8.1 release branch. n8.1.3 was tagged 2026-09-21 and the build was made
2026-10-31, so on 2026-11-14 it is **two weeks old and on the current point
release**.

v0.11.0 shipped pinned to an August build (n8.1.2-44, `autobuild-2026-08-18`).
By 2026-11-14 that asset answered 404, so Download ffmpeg failed. The
repository had moved from `BtbN/FFmpeg-Builds`, and it keeps daily autobuilds
for only about two weeks. Only month-end builds (`autobuild-YYYY-MM-3x`) are
kept long-term. The re-pin to a month-end build fixed it. **Pin month-end
builds only.** A pin is only as durable as the release it points at, and
nothing in CI checks that the pinned URL still answers.

An update touches:

- `windowsAMD64` in `internal/mediatools/install.go`: URL, SHA-256, size and
  version string.
- The two sizes quoted in `docs/api.md`.
- A check as the service, because a new ffmpeg can change which encoders pass
  the startup test encode, which filters exist (tone mapping), and what the
  argument list means. That means the swap-and-play steps in `CLAUDE.md`: a
  forced transcode on each hardware path the machine has, and `lancastd.log`
  read for `ffmpeg reported errors`.

Nothing else pins the version. A user's own ffmpeg, recorded or copied at
install, is whatever they installed.

### Lowering it: what was costed

Every ffmpeg and ffprobe launch already goes through `internal/childproc`
(`Hide`), which a source test enforces. That is about a dozen files in seven
packages: `transcode`, `probe`, `preview`, `subtitle`, `marker`, `coverart` and
`photo`. One wrapper beside `Hide` could change how all of them start. Three
ways were considered.

**1. A restricted token derived from the service's own (Windows).** Start
children with `SysProcAttr.Token` set to a token made by `CreateRestrictedToken`
(`DISABLE_MAX_PRIVILEGE`) and lowered to **Low integrity**. SYSTEM holds
`SeAssignPrimaryTokenPrivilege`, so `CreateProcessAsUser` works without a
password.

- *What it buys:* the child holds no privileges (no `SeDebug`, `SeTcb`,
  `SeImpersonate`, `SeBackup`/`SeRestore`, `SeLoadDriver`), and mandatory
  integrity stops it writing anything not labelled Low: Program Files, System32,
  the registry, the data directory, other processes. A compromised decoder
  cannot install itself, replace LANcast, or take over the machine.
- *What it does not buy:* it is still the SYSTEM identity, and Low integrity
  does not stop reading by default. It can still read `secrets.key`, the
  database, the TLS private key and every user's files, and it still has the
  network. Limiting reads needs restricting SIDs, and those break libraries in
  user profiles, which the files' ACLs grant only to their owner.
- *Work:*
  - The token, via `advapi32!CreateRestrictedToken` and `SetTokenInformation`
    (neither is wrapped in `x/sys/windows`), with tests: about a day.
  - Moving the ffmpeg call sites to the wrapper, keeping the restart helpers on
    the full token: half a day.
  - Finding everywhere ffmpeg *writes* and labelling only those folders Low:
    1–2 days. That covers HLS segments under the transcode root, previews,
    converted subtitles, optimized copies, and any temp file (SYSTEM's `%TEMP%`
    is not Low). Each folder labelled Low becomes writable by every Low process
    on the machine, so the list has to be exact.
- *The risk that decides it:* GPU work at Low integrity in **session 0**. NVENC,
  QSV and AMF encoders and D3D11VA decode all open device objects. Whether that
  works from a Low-integrity child of a service is unknown, and finding out
  needs the installed service on each vendor's hardware. Only NVIDIA is
  available here. That is the same class of failure as v0.8.0's `-hwaccel auto`:
  correct in every test run from a shell, and broken for every user. If it
  fails, the encoder test at startup must run under the same token, or it
  certifies an encoder the real sessions cannot open.
- *Estimate:* **4–6 days, plus a verification sitting as the service, with a
  vendor matrix that cannot be completed on one machine.**

**2. A separate low-privilege account for the children.** For example a
dedicated local user, logged on with S4U from the service and started with
`CreateProcessAsUser`. This limits reads properly, but it means managing an
account and its rights, and granting it read access to every library folder,
including those in user profiles. It is also the S4U/LSA plumbing that LANcast
has none of. Larger than (1), with the same GPU unknown.

**3. Running the whole service as a virtual account (`NT SERVICE\lancastd`).**
This is the clean end state, and it is not an ffmpeg change. It touches library
access, NFO writing beside media, the updater writing to Program Files, the
switch to and from the service (ADR 0092), and certificate and firewall setup.
This is a 1.x design change with its own ADR.

**On Linux** the cheap step is a `User=` (or `DynamicUser=`) line in the
systemd unit, plus `ProtectSystem=strict`, `ReadWritePaths=` the data
directory, and `NoNewPrivileges=`. That would apply to the server and ffmpeg
alike. Its cost is data-directory ownership on upgrade and read access to
libraries, and it needs the Linux install run (run plan S6) to prove it.

### Decision

Option 1 is more than a few days of work, and its deciding risk can only be
retired as the service on hardware this project does not have. **It is a 1.1
item, not a 1.0 change.** Until then the exposure stays where *Threats
explicitly out of scope* puts it: a malicious media file that exploits ffmpeg
runs as SYSTEM, or as root under systemd. Keeping the pinned build current is
the mitigation available now, and it rots quietly: see *The pinned build*.

## Threats explicitly out of scope

- **An administrator, or SYSTEM, reading the data directory.** They can read
  the database and every key directly; the application cannot defend against
  its own host. This used to say "a user with filesystem access", which on a
  default Windows install was every local account. It no longer is: see *The
  data directory*.
- **Malicious media files.** ffmpeg and ffprobe process library files, which are
  untrusted input by any strict reading. This is accepted rather than defended:
  the files are the user's own media, the tools are the same ones every other
  media server uses, and they run as child processes rather than linked
  libraries. It is listed here so the acceptance is deliberate rather than
  overlooked. Those child processes run as SYSTEM under the Windows service
  (root under systemd); what lowering that would cost is in *ffmpeg's
  privileges* above, and it is a 1.1 item.
- **Denial of service.** A large enough scan or enough concurrent streams will
  exhaust resources. Acceptable for a household server.

## Open decisions

Tracked in [roadmap.md](roadmap.md):

1. ~~**Transport security.**~~ **Resolved** — built-in TLS with bring-your-own or
   self-signed certificates ([ADR 0014](adr/0014-transport-security.md)), plus
   an opt-in public certificate for a public name
   ([ADR 0081](adr/0081-a-public-name-can-earn-a-public-certificate.md)).
2. ~~**Multi-user accounts.**~~ **Resolved** — accounts with an admin/member
   role split ([ADR 0015](adr/0015-multi-user-accounts.md)); per-user watch
   state.
3. ~~**Session management UI.**~~ **Resolved** — each account lists and signs
   out its own devices; administrators can do the same for every account.
4. ~~**ffmpeg and untrusted input.**~~ **Decided, not resolved** — see *Threats
   explicitly out of scope*. Parsing library media is now routine and the
   exposure is accepted knowingly. Running ffmpeg with fewer privileges was
   costed for 1.0 and deferred to 1.1 — see *ffmpeg's privileges*.
5. ~~**Per-library visibility.**~~ **Resolved** — an administrator can hide a
   library from a member, alongside the content-rating ceiling
   ([ADR 0084](adr/0084-a-library-can-be-kept-from-an-account.md)).
