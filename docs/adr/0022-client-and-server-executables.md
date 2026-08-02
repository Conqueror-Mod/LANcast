# ADR 0022 — Two executables, no terminal

Date: 2026-08-02 · Status: accepted · **amends [ADR 0016](0016-packaging-and-distribution.md)**

## Context

[ADR 0016](0016-packaging-and-distribution.md) decided packaging as *one* binary:
the server with the web client embedded (`go:embed`), viewed in a browser, plus a
`service` command to run it on boot. That is sound, but it leaves two rough edges
for a non-technical household user:

- **A terminal window.** Double-clicking a console binary pops a black window;
  closing it kills the server. The service avoids this on boot, but a manual start
  still shows a console.
- **"Open a browser and type an address" is not an app.** There is no single
  thing a user launches that feels like *LANcast*.

The goal is now explicit: **two branded executables, and no terminal for either.**
A server that runs in the background, and a client the user launches to get to the
UI. The hard constraint is that this must not cost the project its identity —
**pure Go, no CGO, one static binary per artifact** ([ADR 0001](0001-go-and-pure-go-sqlite.md))
— and must not drag in a heavy unaudited UI runtime ([ADR 0013](0013-transcode-pipeline.md)).

That constraint rules out the obvious "make it feel native" answers: a webview
shell needs CGO and platform SDKs, and Electron/Tauri is a large external
toolchain and dependency set. Both trade the project's core for polish.

## Decision

**Two executables, and the UI stays the browser.**

### `lancastd` — the server daemon

- Unchanged as the server; still embeds the web client and serves it over HTTP.
- **No console window.** On Windows it is built with the GUI subsystem
  (`-ldflags -H=windowsgui`), so a double-click starts it silently.
- **A system tray presence** when run interactively on a desktop: the LANcast
  icon with a small menu (open UI, start/stop, quit). The tray is **Windows-only**
  — Linux/NAS/Pi are headless and use the systemd service from ADR 0016, where a
  tray is meaningless. macOS remains deferred.
- The `service` command ([ADR 0016](0016-packaging-and-distribution.md), built)
  is still the boot-time path; the tray is for the interactive case.

### `lancast` — the client launcher

- A tiny, separate, pure-Go executable — the thing a user double-clicks.
- **Finds or starts the server**: checks `GET /api/health`; if nothing answers,
  launches `lancastd` (windowless) and waits for it to come up.
- **Opens the UI in the default browser** at the server's address.
- A tray icon + menu (open, quit) so it is not a fire-and-forget process.
- No console, no CGO.

### The UI remains browser-based

Deliberately **not** a native window. Reusing the shipped web client with zero
rewrite is what keeps this cheap and keeps [ADR 0001](0001-go-and-pure-go-sqlite.md)
and the audit line ([ADR 0013](0013-transcode-pipeline.md)) intact. The two exes
are thin native shells around the same HTTP UI that already exists.

### Branding

Both executables embed the icon set already produced. The installer uses the
`LANcast_text` wordmark in `assets/`. Icons are compiled into the Windows
binaries (a `.ico` via a resource), so a built exe carries the LANcast icon in
Explorer and the tray with no external file.

## Consequences

**Good — the ethos survives contact with "make it an app."** No CGO, no Electron,
no webview SDK; each exe is still one static file. The UI is the web client we
already ship, unchanged.

**Good — "no terminal" is real for both roles.** The server runs windowless
(service on boot, tray interactively); the launcher is a GUI-subsystem exe that
opens the browser. A household user never sees a console.

**Good — the split matches how the two are actually used.** The server is a
long-lived background daemon; the launcher is a short-lived "take me to LANcast"
action. Two executables model that honestly, rather than overloading one.

**Cost — two artifacts per platform.** The goreleaser matrix from ADR 0016 now
builds `lancastd` **and** `lancast` for each target. Real, and mechanical.

**Cost — a Windows systray dependency (or hand-rolled Win32).** The tray is
platform code. It is **Windows-only** and can be done CGO-free — either a
pure-Go-on-Windows systray library or direct Win32 calls through
`x/sys/windows`. The exact choice is an implementation decision made in the tray
increment and weighed against the audit line; either keeps the no-CGO promise.

**Cost — a little coordination logic.** The launcher must locate the server exe
(alongside it in the install) and know its address, and decide whether to start
it or find it already running (a service, or a prior launch). Small, and
contained in the launcher.

**Cost — Linux gets no launcher UX.** On a headless server there is no browser to
open and no tray; the launcher is a desktop convenience, primarily Windows. Linux
remains the systemd service and a browser pointed at the host. That is the
correct shape for a NAS, not a gap.

## Alternatives considered

- **Native webview shell** (a desktop window embedding the UI). Rejected: the
  Go webview libraries need CGO and platform SDKs, breaking the single-binary,
  no-CGO story that is a stated identity of the project.
- **Electron / Tauri.** Rejected: a large external toolchain and dependency set,
  squarely against the small-dependency ethos and the audit line.
- **One binary with an `--open` flag** (no separate client). Rejected: it does not
  deliver the "separate client exe you launch" the vision asks for, and still
  shows a console unless also made windowless — at which point a tray/launcher is
  wanted anyway.

## Revisit if

- **A native window becomes worth the CGO cost** — e.g. offline/kiosk use where a
  browser dependency is unwanted. The two-exe split makes swapping the launcher
  for a native shell an isolated change.
- **A macOS test path appears** — the tray and launcher extend to macOS
  (`launchd` for the service) as a one-target addition, deferred with the rest of
  macOS in ADR 0016.
- **The tray dependency proves heavy or CGO-bound** on some future target — fall
  back to hand-rolled Win32, keeping the no-CGO promise.
