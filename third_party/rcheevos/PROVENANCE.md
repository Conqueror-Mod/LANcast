# rcheevos — RetroAchievements' own library, built by LANcast

The desktop player earns achievements by running
[rcheevos](https://github.com/RetroAchievements/rcheevos), the library every
RetroAchievements-approved emulator uses, against the game's memory
([ADR 0112](../../docs/adr/0112-achievements-are-earned-in-the-desktop-player.md)).

## What ships

| | |
|---|---|
| File | `rcheevos.dll`, beside `LANcast.exe` |
| Licence | **MIT** (rcheevos; and libretro.h, from libretro-common) |
| rcheevos | v12.5.0, commit `1433173220a7eaede6a9ed7a18e94117be1821e0` |
| SHA-256 | `45db6d569ce1bb84814643ce6a01fd65d205ef4f4a1f9ffc63286500367f5e52` |

The DLL is committed at [`x64/rcheevos.dll`](x64/rcheevos.dll), as the
WebView2 loader is: it is 337 KB, and a file that small is easier to audit in
the repository than as a separate download. **It is reproducible.** Built by
[`build.sh`](build.sh) from the commits pinned in
[`versions.env`](versions.env), the same toolchain gives the same bytes, so
anyone can rebuild it and compare the hash above. The build strips at link
time and suppresses the PE timestamp, which is what makes that true.

## What is in it

- rcheevos itself, every `.c` file except the RAIntegration bridge (a
  developer tool that loads another DLL) and the external-client shim it
  uses, built with `RC_CLIENT_SUPPORTS_HASH` so it identifies games itself.
- [`lancast_rc.c`](lancast_rc.c), LANcast's shim. rcheevos' own API takes
  structures and callbacks shaped for C; the shim turns them into flat calls
  the Go side can make through `syscall` with no cgo. It exports only `lrc_*`
  functions, and `build.sh` refuses a build that exports anything else.
- Softcore only: the shim turns hardcore off on every path (ADR 0112 §4).

## Changes from upstream

**rcheevos is not patched.** One behaviour of v12.5.0 is worked around in the
shim instead: unloading a game straight after its last achievement unlocks
crashed in `rc_client_raise_mastery_event`, which read a game rcheevos had
already cleared. The shim runs one frame before unloading, which delivers
the pending event first (and means the person hears that they completed the
set). `TestRealRcheevosUnlocksFromMemory` covers that moment.

## What it may not do

rcheevos builds every request; it sends none itself. Each goes through the
player's `Sender` to the LANcast server, which checks it (`ra.Check`), adds
the person's token and passes it on. The DLL has no network code of its own
to call.

## Builds

Each row is a DLL that shipped. Check a file with `sha256sum rcheevos.dll` (or
`Get-FileHash`).

| date | rcheevos | size | SHA-256 |
|---|---|---|---|
| 2026-11-01 | v12.5.0 | 337,408 | `45db6d569ce1bb84814643ce6a01fd65d205ef4f4a1f9ffc63286500367f5e52` |
