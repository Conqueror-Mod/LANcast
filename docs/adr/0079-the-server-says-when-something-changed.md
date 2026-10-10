# ADR 0079 — The server says when something changed

Date: 2026-10-10 · Status: **proposed**

## Context

A screen in LANcast learns that what it shows is out of date in exactly two
ways:

1. **A mutation made in that same window** invalidates the queries it knows it
   touched (CLAUDE.md, "a write that changes what a list holds must invalidate
   that list").
2. **A poll**, on about twenty queries, almost all of them only while a job is
   running (`running ? 1000 : false`).

Anything the *server* changes by itself reaches neither. The global default
is `refetchOnWindowFocus: false` (`main.tsx`), so a screen that is open shows
what it fetched when it mounted, for as long as it stays open.

The first evening with a remote peer (2026-10-09) hit this three times in an
hour, and each one looked like a broken feature rather than a stale picture:

- **Pairing.** The server promoted ObsidianTower from `added` to `paired` when
  the roster call succeeded. Settings (`usePeers`, no interval) went on saying
  "Added, not yet mutual" until the page was left and reopened. The pair spent
  most of an evening removing and re-adding a pairing that was working, and
  every re-add really did reset it to `added`.
- **Presence.** Kiousu's People page did not show Chris playing a film until it
  was forced to refetch.
- **Promotion itself** runs only from `GET /api/people/peers` — that is, only
  while somebody has the People page open. A pairing nobody looks at from that
  page never becomes mutual. That is a separate bug (§5 moves it into the server),
  but it is the same shape: the server's state moving was tied to a picture
  being drawn.

This is not the "missing invalidation" bug in CLAUDE.md. That bug is a write
in *this* window that forgot a key; it is fixed by finding the key. These are
changes made by the server, by another window, or by another server, and no
amount of care at mutation sites can reach them, because there is no mutation
site in this window.

Polling every query would fix it the expensive way: around seventy query keys,
the peer ones each costing a round trip to another household, and a
minimised window polling for nothing. It would also leave every new screen to
remember its own interval, which is the same discipline problem again.

## Decision

**One server-sent event stream per signed-in window, carrying the names of
things that changed — never the things themselves.**

### 1. `GET /api/events`

`text/event-stream`, same session cookie as every other call, same `Origin`
check. Each event is a **topic**:

```
event: changed
data: {"topics":["peers"]}

event: changed
data: {"topics":["library:12","items"]}
```

No payloads. The client already has a typed endpoint for every fact, and the
server owns truth: an event says *ask again*, and the answer comes from the
same route it always did. That keeps the contract tiny (one route, one event
shape, a list of topic names in `docs/api.md`), keeps authorization in one
place (a topic name discloses nothing a refetch would not refuse), and means a
missed event can never leave a client holding wrong data — only old data, which
§4 handles.

Server-sent events rather than a WebSocket: one direction is all this needs,
it is plain HTTP through the existing mux, TLS and auth, and `EventSource`
reconnects by itself. The desktop client is Chromium; a browser tab is too.

### 2. A hub in the server, fed where state changes

`internal/events` holds subscribers and fans out topic names. It is in-memory
and per-process, like `together` — nothing is persisted, because a topic is
only meaningful to somebody listening now.

Publishing happens **where the state changes, not where a handler returns**:
`SetPeerState`, `ReplaceRemotePeople`, a scan finishing, an enrichment batch
landing, a presence beat arriving. A handler that publishes would miss every
change made by a worker, which is exactly the class of change this exists for.

Events are coalesced (one `changed` per topic per ~250 ms) so a scan touching
nine thousand tracks is one event, not nine thousand.

### 3. One subscriber in the client

`useServerEvents()` mounts once at the app root and maps topics to query-key
prefixes:

| topic | invalidates |
|---|---|
| `peers` | `["peers"]`, `["peer-presence"]`, `["peer-libraries"]` |
| `presence` | `["peer-presence"]`, `["people"]` |
| `library:{id}`, `items` | `["items"]`, `["libraries"]`, `["home"]` |
| `room:{id}` | `["together", id]` |
| … | (the full table lives beside the hook, and in `docs/api.md`) |

The table is the single place a new screen opts in. **A new query key whose
data the server can change on its own needs a row here**, which makes the
rule a reviewable diff instead of a habit.

### 4. A reconnect invalidates everything active

Events sent while the stream was down are gone. On every reconnect, including
waking from a minimised window, the client invalidates all active queries
once. That turns "the window slept through something" from stale-forever into
one refetch. This also makes `refetchOnWindowFocus` unnecessary, and it stays
off.

### 5. Other servers stay pull, the poll moves to the server

Federation remains pull-only; this ADR adds no federation route. What changes
is *who* polls. Today each open People page asks every peer every 10 s. Instead
the server keeps one presence and roster refresh per paired peer, and the
promotion to `paired` (§Context) runs there as well. When the answer differs
from the last one it publishes `presence` or `peers` locally. One outbound call
per peer per interval, whatever the number of windows. A pairing becomes
mutual whether or not anybody is looking at a page.

### 6. Polls that remain

Job progress (`running ? 1000 : false`) stays a poll. It is a value changing
continuously that the screen wants at a fixed rate, which is what polling is
for. An event at every percent would be a poll with extra steps. The stream
announces that a job *started* and *finished*, and the poll runs in between.

## Consequences

- Stale-until-remount stops being the default. A server-side change reaches
  every open window within a coalescing interval.
- `docs/api.md` and `openapi.json` gain one route and a topic table. Third-party
  clients can use the same stream or ignore it and keep polling. Nothing they
  do today breaks.
- One long-lived connection per window. HTTP/1.1's six-connections-per-origin
  limit applies to a browser tab, so the stream must be the only one held open.
  HLS segment fetches are short. Worth measuring with two tabs and a film.
- Every place that changes shared state gains one `events.Publish` line, and a
  missing one is the same quiet bug as a missing invalidation, moved to the
  server. Mitigation: the store methods that change peer, library and item
  state publish themselves, so callers cannot forget.
- jsdom has `EventSource` only if stubbed, so the client tests get a fake
  stream. The table in §3 is tested as data: each topic invalidates its keys.

## Not decided here

- Whether the together room's 5 s follower poll becomes `room:{id}` events.
  Probably yes, and the drift correction stays periodic, but sync timing
  deserves its own measurement first.
- Pushing between servers. If one remote peer per household ever becomes many,
  a federation stream is the obvious next step, and it would have its own ADR.
