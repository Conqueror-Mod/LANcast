# Security posture

> **Status: multi-user accounts, an audit log, paired servers, and an opt-in
> public name.** Each person has an account with a role (admin or member), and
> an administrator can narrow what a member sees; an unsecured server binds to
> loopback only; authorised mutations are recorded; a server can pair with
> another and admit its people as guests; and a server with a public name can
> earn a publicly trusted certificate and offer passkeys. This document
> describes what is protected, what is not, and what remains open.

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
an administrator route, whoever owns it, and cannot mint, list or revoke keys.

## What requires a session

Everything under `/api/` except these:

| Public | Why |
|---|---|
| `GET /api/health` | A monitor should not need credentials |
| `GET /api/auth/status` | The client must know whether to show setup or login |
| `POST /api/auth/setup` | Only works while unconfigured, which is loopback-only |
| `POST /api/auth/login` | Obviously |
| `POST /api/auth/passkey/login/options`, `POST /api/auth/passkey/login` | Passkey sign-in: whoever is signing in has no session yet. Throttled with the password login |
| `GET /api/auth/accounts` | The sign-in page's account picker. Names only, and only to the home network; anybody else gets an empty list. An administrator can turn it off |
| The web assets | The login form lives in them — gating the page behind a session nobody can obtain yet is a locked door with the key inside |

The `/api/federation/` routes skip the session gate too, but are not public:
they are for paired servers, authenticated by the peer's pinned certificate
instead, and each one refuses a caller that did not present one. They are a
fixed list rather than a prefix, so a new route under that path is gated until
somebody adds it deliberately.

**Streaming is gated.** `GET /api/stream/{id}` requires a session. A public
stream URL would make the password decorative.

## CSRF

Two independent defences, because either alone leaves a gap:

- **`SameSite=Strict` on the session cookie.** A cross-site request does not
  carry it.
- **An origin check on every state-changing method.** `POST`, `PUT`, `PATCH`,
  and `DELETE` must present an `Origin` or `Referer` matching the request host.

A request with neither header is allowed: non-browser clients (curl, a TV app,
a script) legitimately send neither, and they are not the CSRF threat. Reads
are not origin-checked — the session gate covers them, and blocking
cross-origin `GET` would break embedding a stream URL.

This mattered specifically because adding a session cookie to an API that
previously had nothing worth stealing is what *creates* CSRF exposure. Before
auth, a malicious page could already reach `localhost:8080`; it just had
nothing to gain.

## Brute force

Login attempts are throttled per client IP — 10 per 5 minutes, decaying on
their own. A password is one guessable secret, so unlimited attempts against it
is the entire attack. Passkey sign-in and changing a password share the same
counter.

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
LANcast.exe reset-auth -yes    # removes every account and session
```

(`lancast reset-auth` on Linux. Since [ADR 0080](adr/0080-one-executable-two-roles.md)
the server is the same executable as the app; see [install.md](install.md).)

The authority this requires is "can run a program on the server", which is the
same authority that could read the database file directly — so it grants an
attacker nothing they did not already have, and grants the owner a way back in.

Watch history survives: those rows are the library's data, not the account's,
and the replacement admin takes the same user id, so resume points reconnect.
Libraries, artwork, and settings are untouched. Afterwards the instance is in
the state a fresh install is in — unconfigured, and loopback-only until an
account exists.

On Windows the data directory is owned by the service account, so this needs an
elevated shell; the command says so rather than reporting a bare SQLite
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

**Presence is never written down.** Who is watching what, right now, is shared
only under a per-account, per-peer grant, and there is no history and
deliberately no *last seen watching*
([ADR 0045](adr/0045-live-presence-between-paired-servers.md)). The harm ADR
0035 names is a record that accumulates; this one does not exist to accumulate.

**A remote guest is a principal, not an account.** Admitted by a ticket their
own server signs, default-deny in middleware — because `requireAuth` grants by
default and withdraws by exception, which is the wrong direction for somebody
who is not a user here. A guest may stream **only the item the room is
playing**: allow-listing the route would not be enough, since that handler
streams whatever id it is handed. A guest writes no state at all
([ADR 0046](adr/0046-remote-guests.md)), and what they may be sent is capped by
the host rather than by their own capability
([ADR 0047](adr/0047-remote-streaming-is-capped-by-the-host.md)).

## Still unprotected

- **No API rate limiting** beyond sign-in and changing a password.

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

**Stream containment.** `GET /api/stream/{id}` re-resolves the item path with
`filepath.Abs` and verifies it still falls inside the owning library root
before opening anything. The database is trusted, but a hand-edited or
corrupted row must not become arbitrary file read access. Covered by tests
asserting both an outside path and a traversal path are refused *and* that the
file contents are not served.

**Artwork hashes are validated.** A hash arrives from a URL path and becomes a
filesystem path, so it is checked for length and hex-only characters first.
Traversal attempts are a cache miss, never a file read.

**Filesystem paths are never serialized.** `Item.Path` is `json:"-"`. Clients
have no use for server paths, and withholding them keeps the layout private
even when the API is reachable.

**The TMDB key is write-only.** Stored `0600` in `config.json`, never in the
database, and `GET /api/settings` reports only `configured: true`. A secret
readable back out of an API leaks through screenshots, logs, and shared
sessions.

**Browse lists directories only.** Never files, never contents, and hidden or
system directories are omitted.

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
LANcast loopback-only behind it.

Built-in TLS with a self-signed certificate
([ADR 0014](adr/0014-transport-security.md)) closes the plaintext hole on the
LAN; on its own it is not a substitute for any of the above when reaching
LANcast from outside the network.

Treat the LAN itself as semi-trusted. A compromised phone, TV, or IoT device on
your network can reach the login endpoint and is inside the boundary LANcast
relies on.

## Threats explicitly out of scope

- **A user with filesystem access to the data directory.** They can read the
  database and the TMDB key directly; the application cannot defend against
  its own host.
- **Malicious media files.** ffmpeg and ffprobe process library files, which are
  untrusted input by any strict reading. This is accepted rather than defended:
  the files are the user's own media, the tools are the same ones every other
  media server uses, and they run as child processes rather than linked
  libraries. It is listed here so the acceptance is deliberate rather than
  overlooked.
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
   exposure is accepted knowingly.
5. ~~**Per-library visibility.**~~ **Resolved** — an administrator can hide a
   library from a member, alongside the content-rating ceiling
   ([ADR 0084](adr/0084-a-library-can-be-kept-from-an-account.md)).
