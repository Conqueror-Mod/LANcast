# ADR 0077 — A browser can play a game

Date: 2026-10-08 · Status: **proposed**

Written overnight as a proposal for Chris to decide. Nothing in it is built.
The questions marked **decide** are his; the ones marked **verify** are facts
not yet checked against the source, and must be before anything is built.

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

## What exists to build on

**libretro publishes WebAssembly builds of the same cores.** RetroArch's stable
release has an `emscripten` build beside the Windows one, at
`https://buildbot.libretro.com/stable/1.22.2/emscripten/RetroArch.7z`
(about 731 MB, last modified 2025-11-20, read from the directory listing on
2026-10-08). It is the same release the desktop client's cores are pinned to
(#796). **Verify:**
- which of the seven default cores it contains;
- each core's file names (a `.js` loader and a `.wasm` module, as RetroArch's
  web player expects);
- whether any of them is built with threads, which needs cross-origin
  isolation (below).

**EmulatorJS** is a front end for those cores in a web page. It is GPL-3.0
(**verify** against its own repository), and since 4.0.9 it ships without
cores, which come from its releases or CDN. Using it would save writing a
libretro host in JavaScript. It would also bring a second front end with its
own menus, its own save handling and its own idea of where cores come from,
all of which LANcast already has.

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
   user's machine. The LANcast project never hosts or ships a core. **This is
   the reading proposed here.**
2. **LANcast bundles the cores in `internal/web/dist`.** That is distribution
   by the project, inside the commercial build. **Refused** for the same reason
   bundling was refused in ADR 0073.

Under reading 1, the server operator is serving GPL binaries to their own
household, as they would by putting RetroArch on a network share. Whether that
creates an obligation to offer source is a question about the operator, not
the project. The source is libretro's and is public, and the Settings page
would link it, as NOTICE already does for the desktop. **Decide:** whether that
reading is acceptable to you, or whether you want legal advice first, as
ADR 0053 anticipated for anything near the commercial licence.

**EmulatorJS's own GPL-3.0** is a separate question. Its JavaScript would be in
LANcast's web bundle, which *is* distributed by the project, so it is refused
under ADR 0053 for the same reason as option 2. The proposal is a small
libretro host of LANcast's own in the page (below), which is AGPL/commercial
like everything else, and talks to the cores through RetroArch's own web
loader.

## Proposed decision

1. **The server fetches the emscripten cores on an administrator's request**,
   with the same pinning as #796:
   - the archive by SHA-256;
   - each core's `.js` and `.wasm` by SHA-256;
   - the archive deleted afterwards.

   They live in the server's data directory and are served under `/cores/`
   only to signed-in accounts. Ceilings do not apply to cores; they apply to
   games.
2. **A page-side player, `RetroWeb`,** loads a core, hands it the game's bytes
   (fetched with the ticket, as the desktop does) and draws to a `<canvas>`.
   Sound goes through Web Audio, the controller through the Gamepad API and the
   keyboard as on the desktop. Saves go to the same `rom_save` routes, so a game
   saved in a browser continues in the desktop client and back.
3. **Consoles in the browser:** NES, SNES, Game Boy, Game Boy Color, GBA,
   Master System and Mega Drive, which are framebuffer cores. **N64 and
   PlayStation are not offered in the browser** at first:
   - N64 needs WebGL2 and a fast machine, and is the console most likely to run
     badly on a television's browser.
   - PlayStation needs a BIOS, and a BIOS uploaded to the server so a browser
     can use it is a file LANcast must then guard.

   **Decide** whether that is right; the detail page would say *"Plays in the
   desktop app"* for those two, as it does for everything today.
4. **Which clients:** any browser with WebAssembly, Web Audio and the Gamepad
   API, which is every current desktop browser and most television browsers.
   **Phones are not offered play** until there are on-screen controls, which is
   a design of its own. **Decide** whether phones matter enough to design
   those.
5. **The desktop client keeps its native player.** The browser player is for
   clients that have no other, and the desktop app never falls back to it: the
   native player is faster, has OpenGL for N64, and is what was tested.

## Questions this does not answer

- **Cross-origin isolation.** A threaded core needs `SharedArrayBuffer`, which
  needs the page served with `Cross-Origin-Opener-Policy: same-origin` and
  `Cross-Origin-Embedder-Policy: require-corp`. COEP would also govern every
  image the page loads, including artwork. If no proposed core is threaded
  (**verify**), none of this is needed.
- **A television browser's Gamepad API.** Whether an LG or Samsung TV browser
  exposes a USB or Bluetooth pad is not something a desktop test can show.
  The first test on a real TV decides it.
- **Latency on a TV.** A TV's browser is often far slower than a PC. The NES
  and SNES cores are cheap; whether GBA and Mega Drive hold full speed there
  is a measurement, not a guess.

## Alternatives considered

- **EmulatorJS, as is.** Fastest to a first game. Refused on licence (its
  GPL-3.0 code would be in LANcast's distributed bundle) and because it brings
  a second set of menus, saves and core sources beside the ones LANcast has.
- **Stream the desktop client's picture to the browser.** That would be
  cloud-gaming within one house: no cores in the browser at all, and every
  console including N64. Refused for now because it needs a desktop client
  running and a video encoder per player, which is a far larger system than
  this. It is worth its own ADR if TV latency rules the browser player out.
- **No browser player.** Retro games stay a desktop-app feature. That is
  coherent and costs nothing; the cost is that the TV and the phone see a
  library they cannot use.

## Rough size

On a traditional estimate, two to three weeks for the framebuffer consoles:
the server fetch (a few days, most of it #796 again), the page-side host (a
week), and saves and input (a few days). After that, a round of fixes from the
first real television.
