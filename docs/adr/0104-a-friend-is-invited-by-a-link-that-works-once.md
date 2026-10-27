# ADR 0104 — A friend is invited by a link that works once

Date: 2026-10-27 · Status: **accepted** · Option C of [docs/lighter-join-plan.md](../lighter-join-plan.md)

## Context

There are two ways to watch with a friend. Pairing two servers (ADR 0044,
made lighter by ADR 0103) suits a friend who keeps their own library. A
friend who only wants to watch yours does better with an account on your
server: that has worked since ADR 0015, and Watch Together on one server since
#780.

The friction is the handover. An administrator makes the account, chooses a
password for somebody else, and sends both over a channel that now holds a
working password indefinitely. The friend then has to find where to change it,
or never does.

## Decision

**An administrator makes an invite, not an account.** `POST /api/invites`
returns a secret once. The link is `https://<public name>/join#<secret>`: the
public name when the server has one (ADR 0081), otherwise the address the
administrator is using, with a warning when that is this machine only.

**The secret rides in the fragment.** A browser never sends a fragment to the
server, so the secret is never in a request line, the request log, a proxy's
log or a Referer. The page reads it and sends it in a POST body.

**Whoever opens it chooses their own name and password**, the same rules as
any account (ADR 0015, `auth.HashPassword`). They are signed in on the spot,
and can add a passkey or two-factor from Settings afterwards like anybody else.

**It works once, for 7 days**, and an administrator can withdraw it before
then. Redeeming deletes it in the same transaction that creates the account,
so two people racing the same link get one account between them.

**It is always a member.** No route makes an invite for an administrator.
Granting the right to add libraries, which is arbitrary filesystem read
(ADR 0015), is a decision about a person already known, made on their account.

**Limits are set before anybody sees anything.** An invite carries the two
limits an account can have, a content ceiling (ADR 0071) and libraries kept
from it (ADR 0084), and both apply in the transaction that creates the account.
Otherwise the friend's first minute would be spent in everything.

**Stored as a hash.** The `invite` table holds the SHA-256 of the secret, the
administrator's note ("for Georgia"), the limits, who made it and when, and
when it expires. A copy of the database is not a set of working links.

**Checking and redeeming are public and throttled.** `POST /api/invites/check`
and `POST /api/invites/redeem` need no session, because the caller has no
account yet. Both are keyed per address on the sign-in throttle, so a link
cannot be guessed by trying (the secret is also 256 bits). A spent, withdrawn,
expired or unknown secret gets the same answer, so the answer does not say
which.

## Consequences

- Schema revision 84 adds `invite`. One-way, like 80–83.
- "Invite a friend" sits beside "Add account" in Settings › Users & access.
  The direct form stays, for an account made for somebody in the room.
- What the friend needs is a link and a server they can reach. For the host,
  that means the public name. A friend outside the house cannot use a LAN
  address, and the page says so when it makes one.
- No passwordless sign-in is added. A passkey is still a second step, made
  after signing in once, because a passkey needs the public name (ADR 0082),
  and not every server has one.
