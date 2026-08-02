# Packaging & distribution — build plan

Implements [ADR 0016](adr/0016-packaging-and-distribution.md) as amended by
[ADR 0022](adr/0022-client-and-server-executables.md): ffmpeg documented not
bundled, a goreleaser cross-compile matrix on a version tag, service management
built into the binary, an **explicit machine-wide data dir** for services — and,
per ADR 0022, **two branded executables with no terminal** (`lancastd` the
server daemon, `lancast` the client launcher), the UI still the browser. This
plan turns those into reviewable increments.

**Updated after ADR 0022.** Increments 1–2 (build-time version, service command)
shipped under the original single-binary framing and are unaffected. Increments
3–6 below are the revised sequence for the two-executable, no-terminal shape.

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

## Increment 3 — server: windowless + Windows tray + icon (ADR 0022)

Make `lancastd` an app, not a console process, on the desktop.

- **Windowless**: the Windows build links with `-ldflags -H=windowsgui`, so a
  double-click starts the server with no console. (Linux is unaffected — it runs
  as the systemd service.)
- **Windows tray** (build-tagged, Windows-only, **CGO-free**): the LANcast icon
  in the system tray with a menu — open UI, start/stop, quit. On a headless
  Linux/NAS/Pi there is no tray; that path stays the service. The tray hosts the
  same `run(ctx, …)` server and drives its shutdown, exactly like the service
  handler.
- **Embedded icon**: compile the `.ico` (from the icon set) into the Windows exe
  via a resource, so Explorer and the tray show the LANcast icon with no external
  file.
- **The dependency call** is made here: a pure-Go-on-Windows systray library vs
  hand-rolled Win32 through `x/sys/windows`, weighed against the audit line —
  documented in the PR.

**Done when:** the Windows build runs windowless from a double-click, sits in the
tray with the icon and a working menu, and stopping from the tray shuts the
server down cleanly. Pure/testable helpers (menu actions → server lifecycle) are
unit-tested; the tray render itself is hand-verified on Windows.

## Increment 4 — client launcher exe `lancast` (ADR 0022)

A tiny, separate, pure-Go executable — the thing a user launches.

- **Find or start the server**: probe `GET /api/health`; if nothing answers, spawn
  the sibling `lancastd` (windowless) and wait for it to come up.
- **Open the UI** in the default browser at the server's address.
- **Tray** icon + menu (open, quit). No console, no CGO.
- Server-location + health-probe logic is pure and unit-tested; browser-open and
  process-spawn are thin platform shims.

**Done when:** launching `lancast` with no server running starts `lancastd` and
opens the browser; launching it with the service already running just opens the
browser; both without a console window.

## Increment 5 — goreleaser + release CI (two exes)

- **`.goreleaser.yaml`**: builds **both** `lancastd` and `lancast` for the matrix
  — `windows/amd64`, `linux/amd64`, `linux/arm64` — Windows binaries linked
  `-H=windowsgui` with the embedded icon, version injected via ldflags, each
  target archived with README + LICENSE, checksums generated. No CGO, so it all
  builds on one `ubuntu-latest` runner. (Linux archives ship `lancastd` and the
  systemd path; `lancast` is a desktop convenience, primarily Windows.)
- **`.github/workflows/release.yml`**: on a pushed `vX.Y.Z` tag, run goreleaser
  and publish a GitHub Release, built from the **committed `internal/web/dist`**
  (CI already fails on dist drift), so no `npm` at release time.
- **Smoke test**: start the freshly built Linux `lancastd`, hit `/api/health`,
  stop it — a cross-compiled artifact that does not boot is a failed release.

**Done when:** a test tag produces a draft release with archives + checksums for
each target (both exes), labelled with the tag version, and the Linux smoke test
passes.

## Increment 6 — installer + install/operations docs

- A **Windows installer** that lays down both exes, registers the service
  (`lancastd service install`), places a Start-menu/desktop shortcut to `lancast`,
  and carries the **`LANcast_text` wordmark** (`assets/`) for its branding.
- A `docs/install.md` that **leads with the data-dir rule**, then covers download
  + run, the optional ffmpeg install, and `service install`/`uninstall` per
  platform; README gains a short "Install a release" pointer.
- Confirm the **ship-release skill** still owns cutting the tag + notes; goreleaser
  owns everything after the tag.

## Sequencing & PRs

1. ~~build-time version~~ — **merged** (#70).
2. ~~`service` command~~ — **merged** (#71).
3. **Server windowless + Windows tray + icon**.
4. **Client launcher `lancast`**.
5. **goreleaser (both exes) + release workflow + smoke test**.
6. **Installer + install/operations docs**.

## Deliberately out of scope

- **macOS** — deferred (no test path); tray + launcher + `launchd` extend to it as
  a one-target addition later ([ADR 0016](adr/0016-packaging-and-distribution.md),
  [ADR 0022](adr/0022-client-and-server-executables.md)).
- **A native webview / Electron UI** — rejected in [ADR 0022](adr/0022-client-and-server-executables.md);
  the UI stays the browser.
- **Signed installers / package managers** (winget, Homebrew, `.deb`, Docker) —
  additive polish on top of the archives, later.
- **Bundling ffmpeg** — a size/licensing trade to revisit only if a zero-config
  transcoding build is demanded.

## What the maintainer does (not code)

Cutting a real release is a **tag push** (the ship-release skill), which triggers
the workflow. The Windows-service and tray/launcher UX are **hand-verified on a
Windows box** — I write the exact steps; the machine is yours.
