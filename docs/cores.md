# Emulator cores and their licences

Every emulator core LANcast plays games with, the licence each is under, and
where its source is. RetroAchievements asks an emulator it approves for
hardcore to publish this list ([ADR 0112](adr/0112-achievements-are-earned-through-the-server.md)),
and it says where each one's source is for anybody who wants it.

**LANcast does not ship any of these.** No core is in the installer, the
portable zip or the updater. A core is fetched only when somebody asks for it,
from libretro's own stable release, and checked before it is used
([ADR 0073](adr/0073-a-retro-game-is-a-file-the-server-owns.md),
following [ADR 0043](adr/0043-media-tools-are-fetched-not-bundled.md)):

- **The release:** RetroArch 1.22.2's stable `windows/x86_64` and
  `emscripten` archives, from `buildbot.libretro.com`.
- **The pin:** each archive is checked against its recorded SHA-256, then
  each core against its own as it comes out. The archive is deleted
  afterwards.
- **Nothing is fetched until a person asks.** Until then, LANcast has no
  emulator at all.

`internal/retro/cores` and `internal/retro/webcores` are where these are
defined. `TestTheCoreListMatchesTheCode` fails if this page and that code
disagree.

## The desktop player

One core per console. Each was chosen for accuracy and for a licence that is
GPL or more permissive; non-commercial cores are never defaults.

| Console | Core | Licence | Source | SHA-256 of the DLL |
|---|---|---|---|---|
| Game Boy, Game Boy Color, Game Boy Advance | mGBA | MPL-2.0 | https://github.com/libretro/mgba | `0209adf404a8e0525f66a5dbc9cbc62be2733ce48d0a113a25b94df91118e8eb` |
| NES | Mesen | GPL-3.0 | https://github.com/libretro/Mesen | `3c95a20cbdd8882e2fe12ca26ee88dde809c12f8cfd11f45e1436b17000ce972` |
| SNES | bsnes | GPL-3.0 | https://github.com/libretro/bsnes-libretro | `af085c35d344df3024a81588f4491bcbe342554282f2e509fd07222470a7e080` |
| Mega Drive | BlastEm | GPL-3.0 | https://github.com/libretro/blastem | `ad8f1e7cb540d621f202462e5b30b7e74d19c3999b9e715a643218240abb15ce` |
| Master System | Gearsystem | GPL-3.0 | https://github.com/drhelius/Gearsystem | `5446712afbbfb2acab63995c6ab6c471a9ff45e3b3af077a6a92708cae46019e` |
| Nintendo 64 | Mupen64Plus-Next | GPL-2.0 | https://github.com/libretro/mupen64plus-libretro-nx | `bd9146900abe380878e7c054a60eaf426779438c230bf5856f99b88847d7dd30` |
| PlayStation | SwanStation | GPL-3.0 | https://github.com/libretro/swanstation | `25b16255af154058b1d28aee279788de3a601362648bb469147130bc3b553353` |

The PlayStation core needs the console's BIOS. LANcast never supplies one: a
person adds their own.

**Achievements.** RetroAchievements reads a game's memory, so a core has to
expose it. bsnes exposes none, so SNES games earn nothing on the desktop
player today. The others were measured in ADR 0112, except Gearsystem and
SwanStation, for which there was no test dump or BIOS.

## The browser player

RetroArch's own web build, one core per console, served only to the server's
signed-in accounts ([ADR 0077](adr/0077-a-browser-can-play-a-game.md)). The
browser player earns no achievements.

| Console | Core | Licence | Source |
|---|---|---|---|
| NES | FCEUmm | GPL-2.0 | https://github.com/libretro/libretro-fceumm |
| Game Boy, Game Boy Color, Game Boy Advance | mGBA | MPL-2.0 | https://github.com/libretro/mgba |
| Master System | Gearsystem | GPL-3.0 | https://github.com/drhelius/Gearsystem |
| Mega Drive | ClownMDEmu | AGPL-3.0 | https://github.com/Clownacy/clownmdemu-libretro |

## What LANcast does ship

The libraries LANcast's own releases carry, such as rcheevos, libmpv and the
WebView2 loader, are listed with their licences in [NOTICE](../NOTICE).
