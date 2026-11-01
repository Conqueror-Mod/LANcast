# ADR 0111 — Achievements are earned in the desktop player

Date: 2026-11-01 · Status: **accepted** 2026-11-01 · Follows [ADR 0090](0090-retroachievements-are-yours-to-connect.md)

ADR 0090 let a person *read* their RetroAchievements progress on their profile
and left *earning* it for a record of its own. This is that record.

Chris decided on 2026-11-01:

- **Softcore only, for now.** Unlocks count as softcore. Save states, rewind
  and fast-forward stay usable. Hardcore needs RetroAchievements to approve
  LANcast as an emulator, which it cannot do before LANcast meets their terms
  (§8).
- **Through the server.** The player never contacts RetroAchievements. Every
  request goes to the person's own LANcast server, which holds their
  credentials and talks to retroachievements.org, as it already does for the
  profile.
- **Desktop app only.** The native player earns achievements. The browser
  player, which is RetroArch's own web build (ADR 0077), does not.

## Context

RetroAchievements has two halves, and ADR 0090 used only one of them.

- **The Web API** (`API_Get…`, keyed by a person's Web API key) reads
  progress. It cannot award anything.
- **The connect API** (`dorequest.php`) is what an emulator uses: log in, start
  a session for a game, award an achievement, send rich presence. It is keyed
  by a **login token**, which `r=login2` returns for a username and password.
  The Web API key cannot be used in its place.

What runs during play is **rcheevos**, RetroAchievements' own C library (MIT),
used by RetroArch and every approved emulator. Its `rc_client` does the whole
emulator side:

- identifies a game by hashing it the way RetroAchievements does
  (`rc_client_begin_identify_and_load_game`);
- evaluates every achievement's conditions against the console's memory once
  a frame (`rc_client_do_frame`);
- sends the session, unlocks and rich presence, retrying an unlock it could
  not send and saying so (`RC_CLIENT_EVENT_DISCONNECTED`, then `…RECONNECTED`).

It makes no network calls itself. The host gives it two callbacks: one to read
memory and one to perform an HTTP request (`rc_client_create(read_memory,
server_call)`). The second callback is what makes "through the server"
possible without changing rcheevos at all.

Rewriting rcheevos in Go was considered and rejected: the condition runtime,
the per-console hashing and the memory maps are the bulk of it, they change as
RetroAchievements adds features, and a reimplementation that disagrees with
theirs awards or misses achievements wrongly. LANcast uses the real library.

## Decision

**1. rcheevos is a DLL, built the way libmpv is.**

- `third_party/rcheevos/build.sh` cross-compiles a pinned rcheevos release
  tag, with `RC_CLIENT_SUPPORTS_HASH`, into `rcheevos.dll` for Windows x86_64
  with mingw-w64, from Linux. It is the ADR 0069 recipe: pinned source,
  nothing patched, published as a release asset, and fetched by
  `fetch.sh` against a recorded SHA-256. `PROVENANCE.md` says what was built
  from what.
- A small C file beside it (`lancast_rc.c`, in the DLL) holds what is awkward
  across a syscall boundary: the memory regions, read directly from pointers
  rather than through a Go callback per byte, and the libretro memory map
  (rcheevos' own `rc_libretro` helpers).
- The client loads it with `syscall`, as it loads cores and libmpv. No cgo.
- Like libmpv, it is optional at runtime: a client without it plays exactly
  as today and says achievements are unavailable.

**2. The person connects once, with their password, and LANcast keeps a
token.**

- On the profile, under the existing RetroAchievements connection, a person
  turns on **Earn achievements while playing** by typing their
  RetroAchievements password.
- The server calls `r=login2` with it, keeps the **token** RetroAchievements
  returns, sealed with the ADR 0090 key, and **discards the password**. It is
  never stored or logged.
- The token is a credential that can award achievements, so it is held as the
  Web API key is: sealed, never returned by any route, deleted on disconnect
  and with the account.
- The account must be the one already connected for reading (§1 of ADR 0090);
  a token for a different RetroAchievements user is refused.

**3. The player's requests go to the server, which adds the token.**

- `server_call` in the client posts rcheevos' request (its form body) to
  `POST /api/items/{id}/retroachievements` on the person's own server, with
  the stream ticket the page minted for the game, the credential the player
  already uses for the game's files and saves (ADR 0073).
- The player logs in with `rc_client_begin_login_with_token` using a
  placeholder token. The server replaces the `t=` field with the real token
  for the calling account, sends the request to
  `https://retroachievements.org/dorequest.php`, and returns the answer with
  any `Token` field removed. The token never reaches the client.
- The route forwards to `dorequest.php` and nothing else, only the request
  types `rc_client` sends for a softcore player at v12.5 (`login2`, `gameid`,
  `achievementsets`, `startsession`, `ping`, `awardachievement`), only for the
  RetroAchievements account the caller connected, never with a password in
  it, and only for a caller who has turned earning on. It is not a general
  proxy.
- The `User-Agent` is `LANcast/<version>` followed by the clause rcheevos
  writes for itself (`rc_client_get_user_agent_clause`), which the client
  sends with each request. RetroAchievements requires this format.

**4. Softcore.**

- `rc_client_set_hardcore_enabled(client, 0)` before every game. rcheevos'
  default is hardcore, so this is set, not assumed.
- The server also refuses an `awardachievement` that claims hardcore, so a
  client cannot earn hardcore unlocks LANcast has not been approved to give.
- Leaderboards submit only in hardcore, so there are none for now.

**5. Unlocks are not lost when the internet is.**

- While playing, rcheevos retries an unlock it could not send, and the
  player shows "Achievements offline, will send later".
- **New for LANcast:** if the server cannot reach RetroAchievements, it keeps
  the `awardachievement` request in a queue in the database, without the
  token, and answers the player as RetroAchievements would on success. It
  retries the queue with backoff, adding the token at send time, and drops an
  entry only when RetroAchievements answers it. The award is sent with the
  time it waited added to its "seconds since unlock" (`o`) and signed again
  (rcheevos' signature is an MD5 of public fields, with no secret), so
  RetroAchievements dates it when it was earned. The person's profile shows
  "N unlocks waiting to send".
- This means an unlock can be shown in the player before RetroAchievements
  has it. That is what an offline unlock is, and the profile says so.

**6. The player.**

- When a game starts and earning is on, the player identifies it with
  `rc_client_begin_identify_and_load_game`, from the bytes it already loads
  (or the file, for a core that reads from disk).
- `rc_client_do_frame` runs on the core thread after each `retro_run`, and
  `rc_client_idle` while paused or in the menu.
- The host records `RETRO_ENVIRONMENT_SET_MEMORY_MAPS`, which some cores use
  instead of `retro_get_memory_data`, and hands both to rcheevos.
- An unlock shows as a small toast in the corner with its badge, fetched
  through the server's existing media route (ADR 0090 §5).
- The game menu gains an **Achievements** page: this game's set, what is
  unlocked, and progress where the set defines it.
- Rich presence ("Level 3, 2 lives") goes to RetroAchievements as rcheevos
  sends it. LANcast does not show it to anybody else.
- A game RetroAchievements does not recognise plays as before, and the menu
  says it has no achievements.
- Save states keep working. rcheevos' own progress is saved inside each
  LANcast state (`rc_client_serialize_progress_sized`), so loading a state
  does not re-award or forget what happened after it.

**7. Consoles.** Every console the desktop player has a core for
(ADR 0073): NES, SNES, N64, Game Boy family, Master System, Mega Drive,
PlayStation. LANcast's console names map to RetroAchievements' console ids in
one table, which is tested.

**8. Hardcore, later, and what it needs.** RetroAchievements approves an
emulator for hardcore once it:

- sends a unique user agent (§3 does);
- has been publicly released for six months;
- has a privacy policy;
- lists its cores' licences;
- disables state loading, rewind, slow motion and cheats while hardcore is on.

When LANcast applies, a hardcore switch and those restrictions are a follow-up
to this record, not a new one.

## Schema

Additive, one revision:

| Change | Holds |
|---|---|
| `retro_account.token_sealed` (nullable BLOB) | the sealed login token; null when earning is off |
| table `retro_unlock_queue` | `id`, `user_id`, `body` (the request without its token), `queued_at`, `tries`, `last_error` |

`DeleteUser` removes the queue rows with the `retro_account` row, in the same
transaction.

## Not in this decision

- **The browser player.** RetroArch's web build has rcheevos inside it, but
  it would send from the viewer's browser to RetroAchievements, which breaks
  no phone-home. Routing it through the server would mean patching RetroArch.
  Not done.
- **Hardcore** (§8) and **leaderboards**.
- **Showing anybody else's achievements**, which ADR 0090 already rules out.

## Consequences

- Achievements earned in LANcast appear on retroachievements.org, and on the
  person's LANcast profile through ADR 0090, as softcore.
- The client gains one DLL, about a few hundred kilobytes, built from source
  LANcast pins.
- The server holds a second credential per person, which can award
  achievements. It is sealed as the first is, and a database backup alone
  still carries nothing usable.
- A person who turns earning on has typed their RetroAchievements password
  into LANcast once. The page says it is used once to get a token and is not
  kept.
