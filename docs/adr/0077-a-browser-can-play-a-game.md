# ADR 0077 — A browser can play a game

Date: 2026-10-08 · Status: **accepted** 2026-10-10

Proposed on 2026-10-08. Chris answered its three questions on 2026-10-10, and
the facts it left to check were checked the same day (§Verified). Nothing in it
is built yet.

## Context

[ADR 0073](0073-a-retro-game-is-a-file-the-server-owns.md) put retro games in
the desktop client. It is the only player, and the ADR named a browser player
as the one stage left: *"WebAssembly builds of the cores in the web client, so
a TV, phone or laptop can play without the desktop app. It needs its own ADR
covering the licence, which clients get it, and whether N64 is offered there
at all."* This is that ADR.

Today a ROM library opened in a browser tab lists every game, shows its box
art and rating, and says *"Plays in the LANcast desktop app."* The server half
of playing already exists and is not desktop-specific:

- A game's files are fetched with a stream ticket the page mints with its own
  session (`romhash.GameFiles`, `ticketRoute`). A browser can mint the same
  ticket.
- Saves and save states are `rom_save` rows on the server, per person and per
  game, reached through the same ticket. A browser can read and write them.
- Ratings, ceilings and the shared-library rules apply to the listing, not to
  the player, so nothing about them changes.

What the browser lacks is the emulator.

## Verified

Checked on 2026-10-10. These were marked **verify** in the proposal.

**What a web core is.** libretro's WebAssembly build is not a set of bare
cores. Each core ships as `{core}_libretro.js` plus `{core}_libretro.wasm`
(mGBA's `.wasm` is 4.1 MB). Each pair is **RetroArch itself compiled with that
one core built in**, rendering through WebGL/WebGL2. libretro's own web player
(web.libretro.com) loads them this way: it imports the `.js`, then sends
RetroArch commands such as `LOAD_CORE`. So the page drives RetroArch's module,
which is GPL-3.0 like the cores, and is covered by the same reading (below).

**Which cores exist.** libretro's web player lists 90-odd cores in its
`core_list.js`. That list is built from the nightly, and the stable 1.22.2
archive (`stable/1.22.2/emscripten/RetroArch.7z`, about 731 MB) is published
only as a whole. So the exact contents of the pinned archive are confirmed
when the server first hashes it. Against the desktop defaults in
`internal/retro/cores/cores.go`:

| Console | Desktop core | Web build | In the browser |
|---|---|---|---|
| Game Boy, Color, Advance | mGBA (MPL-2.0) | yes | **mGBA**, the same core |
| Master System | Gearsystem (GPL-3.0) | yes | **Gearsystem**, the same core |
| NES | Mesen (GPL-3.0) | **no** | **FCEUmm** (GPL-2.0) |
| Mega Drive | BlastEm (GPL-3.0) | **no** | **ClownMDEmu** (AGPL-3.0) |
| SNES | bsnes (GPL-3.0) | **no** | **not offered** (below) |
| N64 | Mupen64Plus-Next | no | not offered (decided) |
| PlayStation | SwanStation | no | not offered (decided) |

**Licences that rule a core out.** The other web cores for SNES and Mega Drive
are not open-source licences in the sense ADR 0053 depends on:

- snes9x, in every variant (`snes9x`, `2002`, `2005`, `2010`): the
  non-commercial grant says that "commercial rights" will never be given.
- Genesis Plus GX and PicoDrive: redistributions "may not be sold, nor may
  they be used in a commercial" product.

LANcast has a commercial licence, so none of the three is used, however it
reaches the household. SNES is therefore desktop-only until libretro publishes
a web build of bsnes or another freely licensed SNES core.

**Threads.** None. The web cores contain no `SharedArrayBuffer`, no shared
`WebAssembly.Memory` and no workers. The one `ENVIRONMENT_IS_PTHREAD` is a
defensive `typeof` check in the camera driver. **Cross-origin isolation is not
needed**, so COOP/COEP headers are not added and artwork is unaffected.

**EmulatorJS** is GPL-3.0, confirmed from the LICENSE in its own repository.

## The licence, which is the decision that matters

**LANcast must not distribute GPL cores**, for the reason ADR 0073 gave: a GPL
work inside a commercially licensed product (ADR 0053). The desktop client
meets that by fetching cores from libretro onto the user's machine, on request,
pinned and checked (ADR 0043, #796).

A browser player changes **who serves the core**. The browser gets every byte
from the LANcast server, so the server would hold the cores and serve them to
the household's browsers. Two readings:

1. **The server fetches, exactly as the desktop client does.** An
   administrator presses a button on the server (Settings → Retro games), and
   the server downloads the pinned emscripten archive from libretro, checks it
   and keeps the cores. They are then served to that server's own signed-in
   accounts. This mirrors ADR 0048, where the server downloads ffmpeg onto the
   user's machine. The LANcast project never hosts or ships a core.
2. **LANcast bundles the cores in `internal/web/dist`.** That is distribution
   by the project, inside the commercial build. **Refused** for the same reason
   bundling was refused in ADR 0073.

Under reading 1, the server operator is serving GPL binaries to their own
household, as they would by putting RetroArch on a network share. Whether that
creates an obligation to offer source is a question about the operator, not
the project. The source is libretro's and is public, and the Settings page
links it, as NOTICE already does for the desktop.

**Decided (Chris, 2026-10-10): reading 1 is acceptable.** It is the same
reading the desktop client already ships under, applied to the server.

That reading covers **open-source licences that ask for source**: GPL-2.0,
GPL-3.0, AGPL-3.0 and MPL-2.0. It does not stretch to a licence that refuses
commercial use outright, which is why snes9x, Genesis Plus GX and PicoDrive
are excluded above. Whether a non-commercial core fetched by a household
breaches that household's licence is a different question, and LANcast does not
put people in the position of having to answer it.

**EmulatorJS's own GPL-3.0** is a separate question. Its JavaScript would be in
LANcast's web bundle, which *is* distributed by the project, so it is refused
under ADR 0053 for the same reason as option 2. The page-side host is LANcast's
own (below), AGPL/commercial like everything else, and drives RetroArch's
module the way libretro's own web player does.

## Decision

1. **The server fetches the emscripten cores on an administrator's request**,
   with the same pinning as #796:
   - the archive by SHA-256;
   - each core's `.js` and `.wasm` by SHA-256;
   - the archive deleted afterwards.

   Only the cores in the table above are extracted, and nothing outside it,
   which also keeps the non-commercial cores off the disk. They live in the
   server's data directory and are served under `/cores/` only to signed-in
   accounts. Ceilings do not apply to cores; they apply to games.
2. **A page-side player, `RetroWeb`,** loads a core's module, hands it the
   game's bytes (fetched with the ticket, as the desktop does) and lets it draw
   to a `<canvas>`. Sound goes through Web Audio, the controller through the
   Gamepad API, and the keyboard works as on the desktop. RetroArch's own menu
   is not shown; LANcast's pad menu sits over the canvas, as it does on the
   desktop.
3. **Saves use the same `rom_save` routes.** Battery saves (`.srm`) carry
   between the browser and the desktop client for every console. **Save states
   carry only where both use the same core**: Game Boy family and Master
   System. NES and Mega Drive states made in the browser load only in the
   browser, and the other way round, because a state is a snapshot of one core's
   memory. The save list labels a state with the core that made it, and loading
   one into the other core is refused with that reason instead of failing.
4. **Consoles in the browser:** NES, Game Boy, Game Boy Color, GBA, Master
   System and Mega Drive.
   **Decided (Chris, 2026-10-10): N64 and PlayStation are desktop-only.**
   - N64 needs WebGL2 and a fast machine, and is the console most likely to run
     badly in a television's browser.
   - PlayStation needs a BIOS, and a BIOS uploaded to the server so a browser
     can use it is a file LANcast must then guard.

   **SNES is desktop-only as well**, for the licence reason above. It is not a
   performance call and is revisited if a freely licensed web build appears.
   The detail page says *"Plays in the desktop app"* for all three.
5. **Which clients:** any browser with WebAssembly, WebGL, Web Audio and the
   Gamepad API, which is every current desktop browser and most television
   browsers.
6. **Phones get on-screen controls.**
   **Decided (Chris, 2026-10-10): on-screen controls are designed,** and phones
   are offered play once they exist. It lands with the mobile layout work
   already owed elsewhere in the client, and wants its own short design pass:
   - which controls each console needs (a d-pad and two buttons for NES; four
     face buttons and shoulders for GBA and Mega Drive);
   - where they sit in portrait and landscape;
   - how they hand over to a real pad when one connects.

   Until then a phone shows the game's page and *"Plays with a controller, or
   in the desktop app."*
7. **The desktop client keeps its native player.** The browser player is for
   clients that have no other, and the desktop app never falls back to it: the
   native player is faster, has OpenGL for N64, uses the more accurate cores,
   and is what was tested.

## Questions this does not answer

- **A television browser's Gamepad API.** Whether an LG or Samsung TV browser
  exposes a USB or Bluetooth pad is not something a desktop test can show.
  The first test on a real TV decides it.
- **Latency on a TV.** A TV's browser is often far slower than a PC. FCEUmm and
  Gearsystem are cheap; whether mGBA and ClownMDEmu hold full speed there is a
  measurement, not a guess. ClownMDEmu is also younger than BlastEm, so its
  compatibility on the household's own Mega Drive set is measured at the same
  time.
- **The pinned archive's exact contents.** The table is from libretro's
  nightly web player. If 1.22.2 lacks one of the five cores, that console is
  desktop-only in the browser until the pin moves.

## Alternatives considered

- **EmulatorJS, as is.** Fastest to a first game. Refused on licence (its
  GPL-3.0 code would be in LANcast's distributed bundle) and because it brings
  a second set of menus, saves and core sources beside the ones LANcast has.
- **snes9x for SNES.** The only SNES core with a web build. Refused, because
  it withholds commercial rights outright (above).
- **Stream the desktop client's picture to the browser.** That would be
  cloud-gaming within one house: no cores in the browser at all, and every
  console including N64 and SNES. Refused for now because it needs a desktop
  client running and a video encoder per player, which is a far larger system
  than this. It is worth its own ADR if TV latency rules the browser player out.
- **No browser player.** Retro games stay a desktop-app feature. That is
  coherent and costs nothing; the cost is that the TV and the phone see a
  library they cannot use.

## Rough size

On a traditional estimate, two to three weeks for the six browser consoles:

- the server fetch: a few days, most of it #796 again;
- the page-side host: a week;
- saves and input: a few days, including labelling which core made each state.

After that, a round of fixes from the first real television. On-screen
controls for phones are sized with the mobile layout work, not here.
