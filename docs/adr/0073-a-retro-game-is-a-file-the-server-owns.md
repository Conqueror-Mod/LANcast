# ADR 0073 — A retro game is a file the server owns

Date: 2026-09-30 · Status: **proposed** — the direction and the stages were
approved on 2026-09-30. The decisions below are proposals until someone starts
stage 1 and confirms them. The build plan is
[retro-games-plan.md](../retro-games-plan.md).

Extends [ADR 0002](0002-one-wide-media-item-table.md), because a ROM is a new
`kind` on `media_item`. Stands beside
[ADR 0066](0066-a-game-belongs-to-the-machine-it-is-installed-on.md) without
amending it: 0066 is about games installed on a PC, and this ADR explains why a
ROM is not one of those. Reuses the libmpv loading pattern from
[ADR 0067](0067-the-desktop-client-plays-through-libmpv.md) and
[ADR 0069](0069-lancast-builds-its-own-lgpl-libmpv.md), and the
fetched-not-bundled mechanics from
[ADR 0043](0043-media-tools-are-fetched-not-bundled.md).

## Context

The request: play old console games, N64 first, from inside LANcast, without
installing and updating a separate emulator front end. Jellyfin has reportedly
added something similar. How it is built was **not checked** and did not shape
anything here.

ADR 0066 kept installed PC games out of the server entirely. They live on one
PC, a service in session 0 cannot launch anything anyone would see, and no
other device in the house can run them. **None of that is true of a ROM.** A
ROM is a file the server can read, identify, hash and serve, which is exactly
what every other media kind is. The difference that matters is not where the
file lives but **what can play it**: a film plays in any browser, and a ROM
needs an emulator core on the client.

Three facts shape the decision.

### The emulator is the product, and libretro already exists

libretro is a C API that separates an emulator (a *core*, one DLL per system)
from the program hosting it (a *frontend*). RetroArch is the best-known
frontend, but the API is designed for any program to host a core. A core
exports a small set of functions (`retro_init`, `retro_load_game`, `retro_run`,
`retro_serialize`, and a few more) and calls back into the host for video
frames, audio samples, input state and environment questions.

This has the same shape as libmpv, which the desktop client already loads by
full path through `syscall` with no CGO (ADR 0067). A libretro host is the same
technique pointed at a different DLL.

### N64 is the hard system, and it is the one asked for

Most cores (NES, SNES, Genesis, GBA) render in software and hand the host a
finished framebuffer each frame. The host copies it to a texture and draws it.
That is cheap to build.

N64 cores are different. Mupen64Plus-Next renders through **OpenGL**, and
ParaLLEl N64's accurate renderer needs **Vulkan**. The core asks the host for a
hardware rendering context (`RETRO_ENVIRONMENT_SET_HW_RENDER`) and draws into
the host's framebuffer object. The software-only N64 renderer (Angrylion) is
accurate but too slow to be the default. Owning a GL context from pure Go
through `syscall` into `opengl32.dll` is feasible, but it is where most of the
N64 effort goes. It is also where GPU drivers differ, so it has to be verified
on real hardware rather than in tests.

The client runs in the signed-in user's session, **not session 0**, so the
v0.8.0 DXVA2 failure described in CLAUDE.md cannot happen in the same form. The
lesson still applies: a GPU path is only proven on the machine it ships to.

### Core licences rule out bundling

LANcast ships under AGPL-3.0 with a commercial licence available
([ADR 0053](0053-the-licence-lancast-ships-under.md)). The cores have their
own licences:

- The N64 cores (Mupen64Plus-Next, ParaLLEl N64) are **GPLv2**.
- Several popular cores (Snes9x, Genesis Plus GX, PicoDrive) are
  **non-commercial** licences. A commercial build may not ship them, and there
  is no LGPL build to make, unlike libmpv.

*These licences were recalled while writing this ADR, not re-checked. Verify
each core's `LICENSE` before stage 2 uses it.*

So LANcast does not distribute cores. It fetches them on the user's request,
the way ADR 0043 fetches ffmpeg. That also makes "LANcast keeps the player up
to date" true without any maintenance burden.

## Decision

### A ROM is a `media_item` row, in a library of its own kind

- A new library kind, `retro`, whose items are `kind = 'rom'`. There is **no
  new item table**. ADR 0002's claim that a new media type is a new `kind`
  holds here, because a ROM is a server-side file like every other kind.
- The console is stored in a new nullable `platform` column (`n64`, `snes`,
  `gba`, …). Adding a nullable column is not a change to the data model's
  shape, but it gets a migration like any other.
- The platform comes from the file's extension, with the folder name breaking
  ties (`.bin` is ambiguous). That rule is **filename guessing, so it lives in
  `internal/media`** and nowhere else.
- A scan marks missing and never deletes, as it does for every other library.

The name `rom` is deliberate. ADR 0066 uses "game" for PC games that are never
server rows, and giving two opposite concepts the same word would be a trap.

### Identity comes from the file's hash, checked offline

- Each ROM is hashed (CRC32 and SHA-1) and looked up in **libretro-database's
  DAT files**, which are derived from No-Intro and Redump. The DATs are data
  files, so identification makes no network call and *no phone-home* holds.
- This is a metadata provider in `internal/meta`, like NFO and TMDB. A match
  sets the canonical title, region and year. Locked fields stay locked, as
  everywhere else.
- **Hash normalisation is part of the lookup.** N64 dumps come in three byte
  orders (`.z64` big-endian, `.v64` byte-swapped, `.n64` little-endian), and
  the DATs hash the big-endian form. NES and some SNES dumps carry a header
  that No-Intro hashes exclude. Normalisation is a **pure function tested
  against fixtures**, split from file reading the way `probe.ParseJSON` is
  split from running ffprobe.
- Box art comes from the libretro-thumbnails image sets, fetched by the
  matched name. It is a **provider that is off until enabled**, because it is a
  network fetch.

### The first player is built into the desktop client, on libretro

- A new `internal/retro` package loads a core DLL by full path through
  `syscall`, like `internal/mpv`. There is no CGO and no link step, so
  replacing the DLL replaces the core.
- The picture goes into the same owned popup windows native video already
  uses: the overlay, the video window and the shield.
- Sound goes through WASAPI, and the core runs at its own frame rate with the
  audio device controlling the pace. Input comes from XInput, mapped onto
  libretro's standard controller layout (the RetroPad), with a keyboard
  fallback.
- `retro_run`, the GL context and every callback stay on **one locked OS
  thread** (`runtime.LockOSThread`). libretro and OpenGL both assume that.
- The host logic (run loop, pacing, input mapping, save handling) is written
  against an interface, and the `syscall` binding is a thin layer under it. The
  logic can then be tested with a fake core, since pure Go cannot build a real
  core DLL for tests.

### Cores are fetched, pinned and checksummed

- Each supported system has **one default core**, chosen for accuracy and for a
  licence LANcast's commercial build could at least point to (GPL or more
  permissive). Non-commercial cores are never defaults.
- A core is downloaded from a **pinned URL with a recorded checksum** and
  checked before it is unpacked into the client's data directory, following
  ADR 0043 in every respect. A partial install counts as absent.
- The first play on a system installs its core after one confirmation. That
  follows ADR 0048's first-run pattern, not ADR 0043's "only when asked".
- An advanced user can point a system at their own core DLL.

*Where pinned builds come from is still to be checked.* libretro's buildbot
publishes stable builds per version, which may be pinnable. If not, the
fallback is a pinned mirror whose redistribution terms are checked core by
core.

### Saves belong to the server and follow the person

- In-game saves (the core's save RAM) and save states (`retro_serialize`) are
  uploaded to the server **per user, per item**. That needs a new
  `rom_save(user_id, item_id, slot, …)` table, which is a schema change. The
  bytes are stored as files in the data directory, not as blobs in SQLite.
- In-game saves work across core versions. **Save states do not**, so each
  state records the core and version that wrote it, and a state from a
  different version is refused rather than loaded into a crash.
- If two machines save the same slot, the newer one wins and the previous copy
  is kept. That is deliberately simple, because saving on two PCs at the same
  moment is rare in one household.

### What LANcast will not do

- **Supply ROMs or BIOS files.** N64, SNES, NES, Genesis and GBA need no BIOS.
  Systems that do (PS1 and later) are out of scope until someone asks, and then
  the BIOS is something the user provides.
- **Share ROM libraries with paired servers by default.** Sending a ROM to
  another household is a different act from streaming a film to it.
  `writeSharedItems` excludes the `retro` kind until a separate decision says
  otherwise.
- **Stream a server-side emulator to a thin client.** The server is a session-0
  service with no GPU context (see CLAUDE.md), so it cannot render games. The
  emulator runs on the client.

## Stages

The order was approved on 2026-09-30. Details and estimates are in the plan.

1. **ROM library on the server.** Scan, identify, art, browse. No playing yet.
2. **Desktop player for framebuffer systems.** Start with GBA (mGBA, MPL-2.0),
   then NES, SNES and Genesis on the same host. This stage proves core
   fetching, picture, sound, controller input and saves to the server.
3. **N64.** OpenGL hardware context, the analog stick, Mupen64Plus-Next as the
   default core, and a real-GPU verification pass.

*Optional stop-gap:* before stage 3, a ROM could launch an installed RetroArch
with the right core, reusing the launch mechanics in `internal/games`. It is
removed once stage 3 ships, and it is never the answer on its own, because it
keeps the maintenance burden this feature exists to remove.

*Later:* a browser player (WebAssembly cores) so TVs, phones and laptop
browsers can play. That needs its own decision about which clients get it.

## Alternatives considered

- **Launch RetroArch and nothing else.** Days of work, because
  `internal/games` already launches executables. Rejected as the design,
  because the user still installs and updates RetroArch, which is the problem.
  Kept only as the stop-gap above.
- **Browser player first.** It reaches every client, but N64 in WebAssembly is
  the weakest combination of system and runtime. It is also a second copy of
  the emulation stack, with its own licence (EmulatorJS is GPLv3). Deferred,
  not rejected.
- **Put ROMs outside `media_item`, as channels are.** Channels got their own
  table because they are not files. ROMs are files, so ADR 0002's reasoning
  applies to them directly.
- **A plugin.** ADR 0020's sandbox cannot load a native DLL or open a GPU
  context, and granting that would weaken the sandbox for every plugin that
  followed. This is the same tension ADR 0066 described.

## Open questions

These were asked on 2026-09-30 and not answered yet. Answer them before stage 1
starts:

1. **Where will people play?** Only at the desktop PC, or also on a TV, phone
   or laptop browser? The answer decides how soon the browser player comes.
2. **Which systems besides N64?** The answer decides whether stage 2 starts
   with GBA or goes straight to the systems that matter.
3. **Which controller?** XInput pads are the planned default. N64-style USB
   pads usually need DirectInput or HID and a mapping screen.
4. **Does save sync across machines matter,** or is it one PC? The server-side
   saves are designed either way, but priority follows the answer.
5. **Is the RetroArch stop-gap wanted at all?**

## Consequences

- One new library kind, one nullable column, and one new table (`rom_save`).
  Each one needs an entry in `docs/api.md` and `docs/openapi.json` when it
  gets an endpoint.
- The desktop client gains a second native media engine next to libmpv, and
  with it a second place where GPU behaviour varies by machine.
- A browser or phone client sees ROM libraries and cannot play them until the
  later browser stage. The detail page tells the person where the item plays,
  rather than the library being hidden from them.
