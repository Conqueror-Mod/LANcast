# ADR 0112 — Achievements are earned through the server

Date: 2026-11-01 · Status: **accepted** 2026-11-01 · Follows [ADR 0090](0090-retroachievements-are-yours-to-connect.md)

Chris accepted it as written on 2026-11-01. That covers both of his choices:
every RetroAchievements call goes through the server (section 3), and play is
casual first, with hardcore built in but hidden until RetroAchievements
approves LANcast (section 5).

ADR 0090 put a person's RetroAchievements progress on their profile, and
left **earning** achievements for its own record. This is that record: an
achievement unlocks while you play a game in the desktop player, and counts
on retroachievements.org.

## Context

**What earning takes.** RetroAchievements' own library, **rcheevos** (C, MIT),
does the work every supported emulator uses:
- it hashes a ROM the way RetroAchievements identifies games (`rc_hash`);
- it watches the game's memory every frame and decides when an achievement's
  conditions are met;
- it runs leaderboards and Rich Presence ("Level 3, 2 lives").

The host provides a memory reader and an HTTP call. It calls
`rc_client_do_frame` once per emulated frame, and handles the events
(achievement triggered, leaderboard submitted, server error).

**A different credential from the profile's.** ADR 0090 stores a **Web API
key**, which reads. Earning uses RetroAchievements' **Connect** API. You log
in once with your **password**, and get back a **token** that every later
session uses. The password needn't be kept; the token is the credential.

**Hardcore is RetroAchievements' decision, not ours.** Unlocks come in two
modes: *casual* (softcore) and *hardcore*, which counts for more on the site.
RetroAchievements accepts hardcore unlocks only from emulators it has
approved ([hardcore compliance
requirements](https://docs.retroachievements.org/general/hardcore-compliance-requirements.html)):
- **a unique, stable user agent**: `LANcast/<version> (Windows 10.0) <core>/<version>`;
- **six months of public release**, a findable privacy policy, and a public
  list of every shipped core with its licence;
- **in hardcore, these are blocked:** loading a save state, rewind, slow
  motion and frame advance, cheats, and debuggers;
- **a resumed session must drop to casual;**
- **switching casual to hardcore needs a full reset;** hardcore to casual is
  allowed mid-game;
- **leaderboards, Rich Presence and a secure offline unlock queue are
  required.**

**What LANcast's player already does that hardcore forbids:**
- **Continue** loads the `auto` save state;
- **Rewind** (#892), and the **save state slots**.

Fast forward isn't on RetroAchievements' list.

**How the desktop player is built:**
- The release binaries are pure Go: `CGO_ENABLED=0`, with a CI check that no
  cgo leaks in.
- C libraries are loaded at run time through `syscall`: libmpv (ADR 0067) and
  the libretro cores themselves (ADR 0073).
- The faces worker already cross-compiles C with mingw in CI.
- The host exposes a core's system RAM (`Memory(MemorySystemRAM)`) but doesn't
  handle `RETRO_ENVIRONMENT_SET_MEMORY_MAPS`. Some cores
  describe their memory only through maps, and RetroAchievements' addresses
  follow those maps. Which of LANcast's seven desktop cores need maps is to
  be measured, not assumed.

**The browser player** (ADR 0077) is RetroArch's own web build. Whatever
achievement support it has would talk to RetroAchievements from the browser,
outside anything LANcast controls. It is not ours to wire.

## Decision

### 1. rcheevos runs in the desktop player, as a DLL LANcast builds

- **A pinned rcheevos commit**, built in CI with mingw into
  `lancast-cheevos.dll`, as the faces worker is built. The SHA is recorded the
  way the core pins are.
- The DLL is loaded through `syscall`, like libmpv. The release binaries stay
  free of cgo.
- **A thin C shim in the same DLL** owns the per-frame work, so Go is not
  called back per memory read:
  - memory reads go straight to the regions the host registers once per game,
    using rcheevos' libretro memory-map helper;
  - only the rare calls cross into Go: an HTTP request, and an event.
- Each callback is made **once per process** (`sync.OnceValue`). Go allows
  about 2,000 and never frees one; a callback made per session would bring
  back the crash #987 fixed.
- **Rejected: porting rcheevos to Go.** It's the runtime every approved
  emulator shares, it changes as RetroAchievements adds condition types, and
  a port that disagreed with it once would be a broken achievement nobody
  could explain.
- **Rejected: cgo.** It would make every release build a C cross-compile, for
  one optional feature.

### 2. The host learns memory maps

`SET_MEMORY_MAPS` is accepted and recorded per session. Without it,
achievements on map-only cores would read the wrong bytes and never fire.
This is useful on its own and lands first.

### 3. Every RetroAchievements call goes through the LANcast server

The shim's HTTP calls go to the person's own LANcast server, which forwards
them to retroachievements.org. The desktop window never contacts
RetroAchievements itself.

- **The token never leaves the server.**
  - Connecting: the password goes through the server to RetroAchievements
    once, and is never stored.
  - The returned token is sealed beside the Web API key (ADR 0090's sealer,
    `<data>/secrets.key`).
  - The client logs rcheevos in with a placeholder; the server puts the real
    token into each request it forwards.
- **Only the calls rcheevos makes are forwarded.** These are
  `dorequest.php`'s request types, on an allow-list (login, game data, start
  session, award achievement, submit leaderboard entry, ping). This is not a
  general proxy.
- **The user agent is the client's**, checked against the format above, so
  RetroAchievements sees which core played.
- **The offline queue lives on the server.** An unlock or leaderboard entry
  RetroAchievements can't take (no internet, an outage) is kept in the
  database, in order, and sent when RetroAchievements answers again. A client
  that quits doesn't lose it, and it stays where LANcast already keeps the
  truth.

**Why not let the client talk to RetroAchievements directly?** That's how
other emulators do it, and it's simpler. But it would put the token on every
machine the person plays on, and a second place that contacts the internet on
its own. It would also leave the offline queue to whichever client happened
to be running. One door to RetroAchievements, the server's, is the
no-phone-home principle applied rather than restated.

### 4. Opt-in, per account, as ADR 0090

- Nothing is earned, and nothing contacts RetroAchievements, until a person
  turns on **Earn achievements** for their own account and logs in.
- A game played by an account without it is untouched.
- Turning it off deletes the token and stops all contact. So does deleting
  the account.
- No achievement is ever submitted for anybody but the account that is
  playing.

### 5. Casual first. Hardcore is built in, and offered once approved

**Decided:**
- **The rules are built now.** With hardcore on:
  - Continue starts from the in-game save (SRAM) instead of the auto state, or
    offers to drop to casual;
  - the state slots can save but not load;
  - rewind and slow motion are disabled;
  - the game shows a visible hardcore marker.
- **The switch isn't offered until RetroAchievements approves LANcast.**
  Until then every session is casual, and the toggle says why. Applying needs
  the privacy policy and the core licence list published, and six months of
  public release. LANcast's first public release was v0.3.2 on 2026-08-02, so
  the earliest application date is **2027-02-02**. If RetroAchievements
  counts from the retro player (v0.9.69, 2026-10-08), it's 2027-04-08. Which
  one applies is RetroAchievements' call, not ours to assume.

### 6. What it looks like

The page draws the unlock pop-up, leaderboard trackers and challenge
indicators over the game, from the shim's events. That's the same page that
draws the game menu. Badges come through ADR 0090's media proxy, so the
viewer's machine never fetches from RetroAchievements. Rich Presence goes to
RetroAchievements; LANcast doesn't show it to the household. Rich Presence
can't be turned off, and the switch says so.

### 7. Progress in save states

`rc_client_serialize_progress` is stored alongside every save state (the
`auto` state and the slots), and restored when it loads. Without it, an
achievement half-met when you saved resets when you load.

## Schema

- **`retro_account` gains `connect_token_sealed`** (BLOB, nullable). Null
  means earning is off.
- **A new table, `retro_unlock_queue`:** account, request type, the form
  body as rcheevos built it, when it was made, attempts, and the last error.
  It is ordered by insertion and deleted on success.

## Order of work

1. **Memory maps in the host**: a test against a real mGBA dump. Useful
   without achievements.
2. **The DLL:**
   - the CI build and the pin;
   - the shim, with Go bindings;
   - `rc_hash` checked against RetroAchievements' published hashes for dumps
     already in the test library.
3. **The server door:** connect, the token, the allow-listed forward, the queue,
   and `docs/api.md` + `openapi.json`.
4. **Casual play:** the per-frame calls, events into the page, the pop-up, and
   progress in save states.
5. **Hardcore rules**, built and tested with the switch hidden.
6. **The application to RetroAchievements**: the privacy policy, the core
   licence list (most of it is in NOTICE already), and the user agent.

## Consequences

- **A new C dependency in the client**, built by LANcast, in the same
  position as libmpv. Its licence goes into NOTICE.
- **The server becomes the client's way to RetroAchievements.** A server that
  is down stops new sessions from loading achievements. An unlock made while
  it's unreachable is lost unless rcheevos' own retry outlasts the outage.
  The queue is on the server, so it covers RetroAchievements being down, not
  LANcast being down.
- **Hardcore changes what Continue means**, and that has to be said in the
  game menu, not discovered.
- **The browser player earns nothing.** Its achievement support, if any, is
  RetroArch's, and stays off.

## Rejected

- **Porting rcheevos to Go**, and **cgo**: section 1.
- **The client talking to RetroAchievements directly**: section 3.
- **Offering hardcore before approval.** RetroAchievements would not count
  the unlocks, and a switch that does nothing is worse than none.
- **Reusing the Web API key for earning.** It can't. Connect needs a token,
  which only a password login gives.

## Not verified yet

- **Whether rcheevos' libretro memory-map helper handles every desktop core
  LANcast ships:** Mesen, bsnes, BlastEm, Gearsystem, mGBA, Mupen64Plus-Next
  and SwanStation. Each needs one achievement seen firing on a real dump
  before it is listed as supported.
- **The exact Connect parameters** the server forwards and rewrites (the
  token field, and an offset for delayed unlocks), taken from the pinned
  rcheevos source when it is built rather than from memory.
- **Whether RetroArch's web build has achievement support at all.** It
  doesn't matter to this decision: it stays off either way.
