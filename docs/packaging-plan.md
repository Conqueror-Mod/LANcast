# Packaging & distribution — build plan

Implements [ADR 0016](adr/0016-packaging-and-distribution.md), whose decisions are
already settled: one self-contained binary per platform, ffmpeg documented not
bundled, a goreleaser cross-compile matrix on a version tag, service management
built into the binary, and an **explicit machine-wide data dir** for services.
This plan turns those into reviewable increments.

**The thing to get right** (ADR 0016 calls it out): a service runs as an account
whose `os.UserConfigDir()` is *not* the installing user's, so a service left on
the per-user default silently builds a second, empty library. Every service path
pins `--data` explicitly (`%ProgramData%\LANcast`, `/var/lib/lancast`) and the
docs lead with it.

**Testing reality:** OS service registration (Windows SCM, `systemctl`) cannot run
on the Linux CI runner. So the plan mirrors the project's `probe`/`transcode`
philosophy — **split the pure, testable core from the OS call**. Data-dir
resolution and systemd-unit generation are pure functions with unit tests; the
actual register/start/stop is a thin platform shim, hand-verified.

## Increment 1 — build-time version

- Replace `const api.Version = "0.2.0"` with `var Version = "dev"`, injected at
  build via `-ldflags "-X lancast/internal/api.Version=vX.Y.Z"`, so the binary,
  `GET /api/health`, and the release tag can never disagree.
- A `lancastd -version` flag prints it and exits.
- A small build helper (Makefile target or `build.sh`) that stamps the version
  from `git describe`, so a local build is labelled too.

**Done when:** a stamped build reports its version via `-version` and `/api/health`;
an unstamped `go build` still runs, reporting `dev`.

## Increment 2 — the `service` command

`lancastd service install | uninstall | start | stop | status`. Structured as a
pure core plus per-OS shims.

- **Pure core** (`internal/service`, no OS calls): resolve the machine-wide data
  dir per GOOS; assemble the service config (data dir, listen addr, and the
  **ffmpeg path** — recorded explicitly, since a service's `PATH` may not include
  a user-installed ffmpeg); generate the systemd unit text. All unit-tested.
- **Linux shim**: write the unit to `/etc/systemd/system/lancastd.service`,
  `systemctl daemon-reload` + `enable --now`; uninstall/stop/status the same way.
- **Windows shim** (`//go:build windows`, `golang.org/x/sys/windows/svc` — already
  a dependency): register a real Windows service, not a Task Scheduler entry;
  the service's run loop hosts the existing server. CI compiles it under
  `GOOS=windows go build`; the register/start path is hand-verified (documented).
- **The data-dir pin** is enforced here: `install` refuses to proceed without a
  resolved explicit `--data`, so the trap above cannot happen.

**Done when:** the pure core is fully tested; `service install` on Linux produces
a working, boot-persistent unit pinned to `/var/lib/lancast`; the Windows path
compiles and is documented for hand-verification.

## Increment 3 — goreleaser + release CI

- **`.goreleaser.yaml`**: the matrix from ADR 0016 — `windows/amd64`,
  `linux/amd64`, `linux/arm64` — each archived with the README + LICENSE,
  checksums generated, version injected via ldflags. No CGO, so it all builds on
  one `ubuntu-latest` runner with no cross toolchain.
- **`.github/workflows/release.yml`**: on a pushed `vX.Y.Z` tag, run goreleaser
  and publish a GitHub Release. It builds from the **committed `internal/web/dist`**
  (CI already fails on dist drift), so no `npm` at release time.
- **Smoke test**: the job starts the freshly built Linux binary, hits
  `/api/health`, and stops it — a cross-compiled artifact that does not boot is a
  failed release, not a shipped one.

**Done when:** a test tag produces a draft release with three archives + checksums,
each labelled with the tag version, and the Linux smoke test passes.

## Increment 4 — install & operations docs

- A `docs/install.md` (or `deploy.md`) that **leads with the data-dir rule**, then
  covers: download + run, the optional ffmpeg install, and
  `service install`/`uninstall` per platform.
- README "Build and run" gains a short "Install a release" section pointing at it.
- Confirm the **ship-release skill** still owns cutting the tag + notes (goreleaser
  owns everything after the tag); note the handoff so the two don't overlap.

## Sequencing & PRs

1. **PR 1 — build-time version** (small; unblocks ldflags for release).
2. **PR 2 — `service` command** (pure core + Linux + Windows shim).
3. **PR 3 — goreleaser + release workflow + smoke test**.
4. **PR 4 — install/operations docs**.

## Deliberately out of scope (ADR 0016, "revisit if")

- **macOS** — dropped for now (no test path); a one-line matrix + `launchd`
  addition when a test path appears.
- **Signed installers / package managers** (MSI, winget, Homebrew, `.deb`, Docker)
  — additive polish on top of the archives, later.
- **Bundling ffmpeg** — a size/licensing trade to revisit only if a
  zero-config transcoding build is demanded.

## What the maintainer does (not code)

Cutting a real release is a **tag push** (the ship-release skill), which triggers
the workflow. The first Windows-service verification is by hand on a Windows box —
I can write the exact steps, but the machine is yours.
