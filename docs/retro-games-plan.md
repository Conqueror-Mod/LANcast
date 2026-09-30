# Retro games — build plan

**Status: back burner — planned, not started, no work scheduled.** The direction and stage order were approved on
2026-09-30. The decisions are in
[ADR 0073](adr/0073-a-retro-game-is-a-file-the-server-owns.md), which stays
*proposed* until stage 1 begins and its open questions are answered. This
document is the work breakdown only. Where the two disagree, the ADR wins.

Some of this was written from memory and not checked: core licences, how the
buildbot pins versions, and the header rules in the DAT files. Each of those is
marked **verify**. Check them against the source before relying on them.

## Estimates at a glance

| Stage | Delivers | Traditional estimate | At this project's pace |
|---|---|---|---|
| 1 | ROM library: scan, identify, art, browse | 2 weeks | 1–2 releases |
| 2 | Desktop player for GBA/NES/SNES/Genesis, with saves to the server | 4–5 weeks | 2 releases |
| 3 | N64 with a GL hardware context | 3–4 weeks | 1–2 releases |
| *stop-gap* | Launch an installed RetroArch | 2–3 days | 1 session |
| *later* | Browser player | 1–2 weeks | 1 release |

The total through stage 3 is about **two to three months** on the traditional
estimate. The column on the right is a guess from recent release cadence, not
a commitment. **The risk is concentrated in stage 3.** GL context handling
through `syscall` and driver differences can only be judged on real hardware,
so budget for a round of fixes after the first test on a real GPU.

---

## Stage 1 — ROM library on the server

**Goal:** a `retro` library lists your ROMs by console, with correct titles and
box art. Nothing plays yet, and the detail page says so.

### Work

1. **Schema:**
   - The library kind `retro` and the item kind `rom`.
   - A nullable `platform` column on `media_item`, with its migration.
   - Bump `CurrentSchemaVersion` and add a test in the `migrate5x_test.go`
     style that rewinds and replays the migration.
2. **Platform detection** in `internal/media`:
   - A pure function from path to platform: extension first, folder name to
     break ties.
   - One named test per extension, in the style of `probe/decide_test.go`.
   - Ambiguous extensions (`.bin`, `.zip`) resolve only by folder name, and
     otherwise stay unknown rather than being guessed.
3. **Scanner:**
   - A walker that accepts ROM extensions for `retro` libraries and marks
     missing ROMs rather than deleting them.
   - `skipped_kind` counts files in a ROM library that are not ROMs.
4. **Hashing:**
   - `internal/retro/romhash`: CRC32 and SHA-1 over the normalised contents.
   - Normalisation is pure: N64 byte order converted to `.z64` order, and the
     iNES header stripped (**verify** which other systems' DATs exclude
     headers).
   - Fixtures are small synthetic byte slices, never real ROMs.
   - Zipped ROMs are hashed from the inner file.
5. **DAT provider:**
   - `internal/meta/retrodb` parses libretro-database's DAT format (a
     clrmamepro-style text format) with a pure parser tested against
     fixtures.
   - It matches on hash and sets title, region and year, respecting locks.
   - The DAT files are fetched once on request and pinned, so there is no
     network access during a scan.
6. **Artwork provider:**
   - Box art, title screen and snapshot from libretro-thumbnails, looked up by
     the matched DAT name (**verify** the URL scheme and how it replaces
     characters the file system does not allow).
   - Off until enabled, and cached through `internal/artwork`.
7. **API:**
   - A `platform` filter on `GET /api/items`, plus the new kind in the
     responses.
   - Update `docs/api.md` and `docs/openapi.json` in the same commit, and run
     `npm run openapi`.
8. **Client:**
   - A library grid grouped or filtered by console, and a detail page showing
     platform, region and year.
   - A "Plays in the LANcast desktop app — coming later" state instead of a
     Play button.
   - Invalidate the listing after a rescan (the most-repeated bug in the
     project).
9. **Sharing:** `writeSharedItems` excludes `retro`, and a test covers it.

### Done when

- A test library (never the live one) of mixed N64, SNES and GBA files, in all
  three N64 byte orders, scans with every file identified and one row per
  game.
- Renaming a file to nonsense changes nothing, because identity comes from the
  hash.

---

## Stage 2 — desktop player, framebuffer systems

**Goal:** press Play on a GBA ROM in the desktop client and play it with a
controller. In-game saves and save states land on the server. NES, SNES and
Genesis then work through the same host with no new host code.

### Work

1. **Fetching cores:**
   - `internal/retro/cores` holds a pinned table of URL, checksum, licence and
     version for each system.
   - It downloads, verifies the checksum, unpacks into the client's data
     directory and swaps the files atomically, following ADR 0043.
   - Candidate defaults (**verify** each licence): mGBA (GBA, MPL-2.0), Mesen
     (NES, GPLv3), bsnes (SNES, GPLv3), BlastEm (Genesis, GPLv3).
2. **Binding:** `internal/retro/libretro` loads the DLL by full path and
   exposes the `retro_*` functions and callbacks through `syscall`. This layer
   stays thin, and everything above it runs against an interface.
3. **Host:**
   - `internal/retro/host` owns the run loop on a locked OS thread and the
     environment callback. That callback answers the core's questions about
     pixel format, directories, core options and variables.
   - Video accepts XRGB8888, RGB565 and 0RGB1555 frames and scales them with
     the correct aspect ratio and integer scaling.
   - Audio uses the batched sample callback into WASAPI, and the audio buffer
     controls the pace.
   - Tests use a fake core that emits known frames and audio, and cover pacing
     and save handling without any DLL.
4. **Window:** use the popups native video already has. Fullscreen and docked
   behaviour follow mpv's, including the per-monitor DPI handling from
   v0.9.41.
5. **Input:**
   - XInput via `xinput1_4.dll`, mapped to RetroPad, with a keyboard mapping
     as a fallback.
   - While a game runs, the game owns input. Escape, or Guide + Start, opens a
     pause overlay (Resume, Save state, Load state, Quit). ADR 0004's focus
     model resumes when the overlay opens.
6. **Saves:**
   - A `rom_save` table plus files in the data directory.
   - `GET/PUT /api/items/{id}/saves/{slot}`, documented in `api.md` and
     `openapi.json`.
   - In-game save RAM is flushed on quit and on a timer. Save states are
     stored with the core name and version, and a state from another version
     is refused with a message.
   - Newer wins, and the previous copy is kept.
7. **Streaming the ROM:** a ROM is loaded whole, not streamed. The client
   fetches it with a stream ticket (ADR 0068) and keeps it in memory, or in a
   temporary file for cores that need a path (**verify** which of the chosen
   cores support `need_fullpath = false`).

### Done when

- GBA, NES, SNES and Genesis each start, play at full speed with correct pitch,
  and accept controller input.
- A save made on one PC loads on a second PC.
- Quitting in the middle of a game loses nothing that was written to save RAM.

---

## Stage 3 — N64

**Goal:** N64 games play at full speed from the desktop client with an XInput
controller, analog stick included.

### Work

1. **GL hardware context:**
   - Create a WGL context on the video popup through `opengl32.dll` and
     `wglGetProcAddress` via `syscall`.
   - Answer `SET_HW_RENDER` for `RETRO_HW_CONTEXT_OPENGL_CORE`, provide the
     framebuffer object and `get_proc_address`, and present with
     `SwapBuffers`.
   - Handle context loss when the window is resized or moves to another
     monitor.
2. **Core:** Mupen64Plus-Next (GPLv2, **verify**) as the default, using
   GLideN64. Vulkan (ParaLLEl-RDP) is **not** in this stage and needs its own
   decision if it is ever wanted.
3. **Input:**
   - Analog stick range and deadzone.
   - The C buttons mapped to the right stick.
   - Controller Pak / Rumble Pak selectable through a core option.
4. **Core options UI:** a short, curated list for each system, such as N64
   resolution upscaling. Not every option the core exposes.
5. **Verification:** on your own machine and GPU, with the installed client,
   play three titles that stress different parts of the renderer. Read the
   client log for GL errors. Tests cannot prove this stage works.

### Done when

- Three chosen N64 titles run at full speed with correct graphics.
- Moving the window between the 100% and 150% monitors neither breaks the
  context nor misplaces the picture.

---

## Stop-gap (optional) — launch RetroArch

If RetroArch is installed, a ROM's Play button starts
`retroarch.exe -L <core> <rom>`. The launch reuses the mechanics in
`internal/games`, and the ROM path is re-verified against its library root
before launch. This only works where the client machine can see the ROM path,
such as a local drive or a mapped share. It is removed when stage 3 ships.

## Later — browser player

WebAssembly builds of the cores in the web client, so a TV, phone or laptop can
play without the desktop app. It needs its own ADR covering the licence
(EmulatorJS is GPLv3), which clients get it, and whether N64 is offered there
at all.

## Things that must not change

- ROM identity comes from the hash, and a match is locked like any other
  match. A rescan reconciles files and does not re-identify games.
- No core, ROM or BIOS is bundled in any release artifact.
- A save is never overwritten without the previous copy being kept.
