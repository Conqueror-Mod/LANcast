# ADR 0095 — A proxy is believed by name

Date: 2026-10-26 · Status: **accepted**

## Context

Everything in LANcast that asks who a request is from reads the connection's
address: the per-address login throttle, the per-account wait and its
home-network exemption, the sign-in page's account picker, the request log, and
a session's device address.

`docs/security.md` recommends a reverse proxy as one way to reach the server
from outside, and said what that costs: behind a proxy on the same machine,
every caller is `127.0.0.1`. The roadmap listed it as hardening. It is worse
than the throttle seeing one client:

- the per-account wait **never applies**, because every caller is at home;
- the sign-in page **lists every account name to the internet**, because the
  picker is shown to the home network and every caller is at home;
- every failed password from anybody counts against one shared address.

Reading `X-Forwarded-For` from anybody is not the fix. The header is whatever
the caller sends, and "I am 127.0.0.1" is the first thing an attacker types.

## Decision

**An owner-named list of trusted proxies**, `trusted_proxies`, in Server ›
Network. Addresses or CIDR networks. **Empty by default**, which is exactly the
behaviour before this.

**Read only from those addresses, and right to left.** Each proxy appends the
address it received the request from, so the rightmost entries were written by
proxies the owner runs and anything further left was written by the caller.
The first entry from the right that is not itself a trusted proxy is the
client. A hop that does not parse ends the walk at the last good one. A trusted
proxy that sends no usable header leaves the request as it was.

**Applied once, at the edge, by rewriting `RemoteAddr`** (`auth.TrustProxies`,
outermost in `Server.Handler`). Every reader of the caller's address then sees
the client without being taught about proxies. Teaching each of them would be
five places to get it right and one to forget, and the one that forgets is a
bypass.

**Refused:** an entry that does not parse, a network wider than an IPv4 `/8` or
an IPv6 `/16`, and more than 32 entries. Trusting `0.0.0.0/0` is not a proxy
configuration; it is turning the check off while appearing to have configured
it. The list is stored normalised, so what Settings shows is what is enforced.

**Only `X-Forwarded-For`.** `Forwarded` (RFC 7239) and `X-Real-IP` are not
read. Every common proxy sets `X-Forwarded-For`, and reading three headers
means three parsers and the question of which wins when they disagree.

## Consequences

- An owner with a reverse proxy adds its address once and the throttle, the
  per-account wait and the account picker behave as they do without a proxy.
- An owner who does not add it is exactly where they were before, and the
  security notes now say so plainly.
- A change applies to the next request, with no restart, and is audited.
- DLNA and federation are unaffected in practice: neither is reached through a
  web proxy, and if one were, seeing the real peer is the more correct answer.
