# ADR 0097 — A webhook goes where the owner points it

Date: 2026-10-26 · Status: **accepted**

## Context

The roadmap: a URL the owner sets, which receives a POST when something
happens — a film starts or stops, a scan finishes. Plex's are what Home
Assistant automations and Discord bots are built on: lights that dim when a
film starts, a message when the nightly scan fails.

Three things make it careful, and the roadmap entry named all three.

- **It is outbound traffic** under a no-phone-home principle.
- **A URL the server fetches is a way to make the server reach into the
  LAN.** That is also the point — Home Assistant lives on the LAN — so it is a
  question of who may set one, not of where it may point.
- **Playback events name an account and a title**, which ADR 0035 treats as
  that person's business.

## Decision

### Outbound only to where the owner pointed it, and nowhere by default

Webhooks are consistent with no phone home because **the owner types the
destination, and nothing is configured by default.** LANcast contacts no
service of its own and suggests none. A server with no webhooks sends nothing,
exactly as before.

### Administrator-only, at most ten

Making, changing, testing and removing a webhook are administrator powers,
gated as library creation is: an administrator can already point the server at
any path on the disk, and pointing it at a URL is the same class of reach. Any
http or https URL is allowed, LAN addresses included, because the LAN is where
the useful receivers are. **Redirects are not followed**: the server sends to
the URL it was given and nowhere it is sent on to. Ten is far more than a
household uses and bounds the work one event can cause.

### Playback events only for people who said yes

`playback.started` and `playback.stopped` carry an account's name and the work
being watched. **They are sent only for accounts that turned it on
themselves**, in Your account › Privacy, and there is **no administrator route
to that switch**, for the reason ADR 0035 gives for activity sharing: a switch
somebody else can flip on your behalf is not consent. It is off for everyone,
including the administrator who made the webhook. Stored as a nullable column
on `user` (`share_playback_webhooks`, migration 80); null is off.

What is sent is what People already shows: the account and the **work's**
title — a show, never an episode (ADR 0045 §3). No position, no item id, no
library. They come from the presence tracker's transitions, so a pause longer
than presence's twenty-second window reads as a stop, and resuming as a start.
That is stated in the API notes rather than hidden.

### Signed, bounded, and honest about failure

Each webhook has a secret, made by the server and shown once when the webhook
is created. Every delivery carries `X-LANcast-Signature: sha256=<hex>`, the
HMAC-SHA256 of the body with that secret, so a receiver can tell LANcast's
POSTs from anybody else's. Also `X-LANcast-Event` and a `X-LANcast-Delivery`
id.

A delivery has ten seconds, and is tried three times (after two and ten
seconds) before it is given up. Only a 2xx counts. Deliveries are queued and
sent off the request path; when the queue is full an event is dropped and
counted rather than slowing the server down. The last outcome of each webhook —
when, the status, the error — is kept in memory and shown beside it in
Settings, with a **Test** button that sends `ping` and waits for the answer.

### The events

- `playback.started`, `playback.stopped` — as above.
- `scan.finished` — a library, how the scan ended (completed, failed or
  cancelled), what it saw and changed, and how long it took.
- `backup.taken` — the file and whether a person or the schedule took it.
- `ping` — only from Test.

A webhook subscribes to a list of these. The live-updates stream (ADR 0079)
was considered as the source and is not used: its events are deliberately
contentless topic names, made for a screen that will refetch, and a webhook's
receiver has nothing to refetch with. The events here are emitted where the
server already knows what happened.

## Consequences

- Home Assistant and Discord work with nothing in between.
- Every new event is a decision about what it discloses, and belongs in this
  ADR's list before it ships.
- Webhook settings live in `settings.json`, like the provider keys, so they are
  not in a database backup and a restored backup does not resurrect a webhook
  somebody deleted.
