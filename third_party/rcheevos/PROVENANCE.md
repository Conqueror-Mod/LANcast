# rcheevos — RetroAchievements in the desktop player

The desktop player earns achievements through rcheevos, RetroAchievements' own
C library, built into `lancast-cheevos.dll` with a small shim of LANcast's and
loaded through syscall like libmpv and the emulator cores
([ADR 0112](../../docs/adr/0112-achievements-are-earned-through-the-server.md)).

## What ships

| | |
|---|---|
| File | `lancast-cheevos.dll`, beside `LANcast.exe` |
| Licence | **MIT** (`upstream/LICENSE`), plus LANcast's shim |
| rcheevos | **v12.5.0**, commit `1433173220a7eaede6a9ed7a18e94117be1821e0` |
| Source | https://github.com/RetroAchievements/rcheevos |

The binary is not in this repository. [`build.sh`](build.sh) compiles it from
the sources here with a mingw-w64 gcc, and checks two things before it
finishes: the DLL exports exactly the functions in
[`shim/lancast_cheevos.h`](shim/lancast_cheevos.h), and it depends on nothing
but KERNEL32 and the Windows C runtime.

## What was copied, and what was changed

From the tag, unmodified:

- `src/` → `upstream/src/`
- `include/` → `upstream/include/`
- `LICENSE` → `upstream/LICENSE`

**One file moved:** `test/libretro.h` → `upstream/include/libretro.h`.
`rc_libretro.c` includes `libretro.h` for the memory-map structures, and
upstream keeps its copy among the tests, which were not vendored. It is
rcheevos' own trimmed copy of the libretro structures, under rcheevos' licence,
and otherwise byte-for-byte upstream's.

Left out on purpose: the tests, the Visual Studio projects, and the
RAIntegration bridge. `src/rc_client_raintegration.c` is vendored, because it
is part of `src/`, but `build.sh` does not compile it — it loads the
RetroAchievements developer toolkit, which has no place in a player.

Nothing in `upstream/` is patched. If that ever changes, the patch belongs
beside this file and in this note.

## Why it sits here and not in a Go package

Go refuses to build a package directory holding `.c` files unless cgo is on,
and LANcast builds with cgo off. `third_party/` is not a Go package, so the C
can live here without touching the Go build.

## Updating

1. Check out the new tag of rcheevos, and copy `src/`, `include/`, `LICENSE`
   and `test/libretro.h` as above.
2. Update the version and commit in this file and in NOTICE.
3. Run `build.sh`, then the binding tests in `internal/retro/cheevos`. The
   shim's interface version (`LC_INTERFACE_VERSION`) changes only when the
   shim's own functions do, not when rcheevos does.
