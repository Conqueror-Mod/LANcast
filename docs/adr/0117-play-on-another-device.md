# ADR 0117 — Play on another of your own devices

Date: 2026-11-07 · Status: **accepted** 2026-11-07 · Related: [ADR 0079](0079-the-server-says-when-something-changed.md), [ADR 0086](0086-a-tv-browses-as-somebody.md), Watch Together (`internal/together`)

## Context

The last item on the owner's build list is "screen share / casting". Asked
what that meant (2026-11-07), the owner chose **play on my other device**:
from the phone, send what you are browsing to the LANcast window on the
TV-side PC, and drive it like a remote. Plex calls this "play on"; it is
also what most people mean by casting.

The roadmap's Chromecast and AirPlay entries are a different feature. They
send to *someone else's* receiver, and each needs its own protocol work and
its own ADR. DLNA (ADR 0086) already lets a TV pull from LANcast. What was
missing is one LANcast client telling another LANcast client what to do.

The pieces are already in the tree:

- **The event stream** (ADR 0079) already reaches every signed-in window, and
  `PublishTo` already limits a topic to one account's windows.
- **Watch Together** already shows that the server can hold, in memory, which
  windows exist and what they are playing, and that those windows can follow
  what the server says.
- **The player** (`PlaybackProvider`) already exposes `play`, `seekTo`,
  `togglePlay`, `stop`, `playNext`, `playPrev` and `changeVolume`. A remote
  command only has to call one of them.

## Decision

**Accepted by the owner on 2026-11-07**, as built (PR #1018).

### 1. A *player* is a signed-in window that offers itself

A window that is willing to be played on registers with
`PUT /api/remote/players/{id}`, giving its name and what it is playing. It
reports again every 5 s and whenever its item or play state changes. A player
not heard from for 20 s is gone. The id is random, made by the window and kept
in its local storage, so the same device keeps the same id across reloads.

**In memory, like `together`.** A player that is not open is not a player, so
there is nothing to keep across a restart, and no schema change.

**Opt in, per device.** The desktop window offers itself by default. A browser
tab or a phone does not, because a browser will not start sound without a tap
on the page, so a film sent to an idle tab would arrive silent and paused. Any
device can turn it on in Settings › Playback ("Let my other devices play
here"), and can give itself a name there.

### 2. Only your own account sees and controls your players

Players are keyed by account *and* id. `GET /api/remote/players` lists the
caller's players and nobody else's, and a command to an id the caller does not
own is `404`, exactly as if it did not exist. There is no admin view and no
"play on someone else's screen": a household member taking over another
member's TV is not a feature, it is a prank waiting to happen.

The commanded window plays through its own session, so every check that
already applies (library access, rating ceiling) applies unchanged. The server
does not need to judge the item at all.

A federated guest cannot reach these routes. They are not on the guest
allow-list (`guestgate.go`), which is what keeps them off.

### 3. Commands go through the server, announced by a topic

`POST /api/remote/players/{id}/commands` queues one command for that player:
`play` (an item, an optional queue, an optional start position), `pause`,
`resume`, `seek`, `stop`, `next`, `previous` or `volume`. The server publishes
the topic `remote` to that account's windows. The player fetches its waiting
commands with `GET /api/remote/players/{id}/commands`, which hands them over
and clears them.

This keeps ADR 0079's rule: **topics, never payloads.** An event says "ask
again", and the commands come from an ordinary authenticated route. A window
that missed the event still finds its commands at its next report, because the
report's answer says how many are waiting. A lost event delays a command by at
most one report and never loses it.

A player holds at most 16 waiting commands, and a command older than 30 s is
dropped unrun. A film that starts a minute after you pressed Play, when you
have already given up and walked away, is worse than one that never starts.

### 4. The controller converges on reports, as Watch Together's followers do

The remote screen shows each player's last report and extrapolates the
position from it while it is playing, as `expectedPosition` does for a room.
The topic `players` tells the account's windows that a player appeared or left,
or that its item or play state changed. Position alone does not publish:
ADR 0079 §6 says a value that moves continuously is extrapolated or polled, and
announcing every report would be a refetch every 5 s in every window.

### 5. "Move to" hands over the place you are at

From the player, *Play on…* sends the current item to another device at the
current position, with the same queue, then stops here. From an item page,
*Play on…* sends the item to start where Play would start it. Both use the same
`play` command; the difference is only what the sending window fills in.

## As built (2026-11-07)

- **Where the remote lives.** The player bar has a cast button (*Move here*),
  an item page has *Play on…* beside Play, and a **Devices** page holds the
  remote for after you have walked away from either. *Play on…* and the rail's
  Devices entry appear only once another of your devices is offering itself, so
  a one-screen household is not shown a list that can only be empty. The
  phone's More sheet follows the same rule, as it mirrors the rail.
- **A moved film skips the pre-roll.** "Move here" sends a position, and the
  receiving window opens the player as a continuation rather than "from the
  start". From the start is what asks the server for a pre-roll (ADR 0099), so
  claiming it would open a film moved at minute forty on a trailer. The seek
  to the sent moment waits until the new item is playing and has settled for
  600 ms, so the new source's own start cannot land after it and undo it.
- **Volume is sent on release**, not on every step of a drag: each step would
  be a command for the device to collect.
- **Books are not sent.** They play in their own player (ADR 0116), which this
  does not drive yet.

## Not decided here

- **Chromecast, AirPlay and pushing to a DLNA renderer.** Each sends to a
  receiver that is not LANcast and is its own ADR, as the roadmap says.
- **Sharing a screen** (a desktop or a game window) into a Watch Together room.
  Offered on 2026-11-07 and not chosen.
- **Players on a paired server.** A command crossing to another household
  would be a federation route and is out of scope; this is one server's
  own windows.
