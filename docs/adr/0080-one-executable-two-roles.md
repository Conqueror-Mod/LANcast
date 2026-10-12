# ADR 0080 — One executable, two roles

Date: 2026-10-11 · Status: **accepted** 2026-10-11 · **supersedes the split in
[ADR 0022](0022-client-and-server-executables.md)**, amends
[ADR 0023](0023-native-desktop-client.md)

The plan is [executable-consolidation-plan.md](../executable-consolidation-plan.md).
Chris chose the process model (§1) and how the tray acts on the server (§3) on
2026-10-11. The rest is the plan's recommendation, recorded so the steps that
build it have one thing to follow.

## Context

LANcast ships `LANcast-Server.exe` and `LANcast-Client.exe`. On a desktop that
means three processes once the service is installed: the service, the server
binary running again as a tray icon in the user's session, and the client
window. Most of the desktop code in both binaries exists to coordinate the last
two:

- whether a tray is alive to restore a hidden window (`raise.HoldTray`,
  `TrayPresent`);
- the tray asking another process to quit its window (`raise.Quit`, "Quit the
  LANcast app");
- a Close to tray setting whose only purpose is to ask that question;
- two run-key values, and two Start-menu entries.

That arrangement produced the "two LANcast icons" report, an Exit that left
processes running in Program Files and fouled an update, and a window that
could hide with nothing able to bring it back.

The goal, stated by Chris: one `LANcast` executable, and a tray that is always
there with Plex's tray functionality. No Close to tray option.

**The threading question was already answered.** Commit `991875f8` (2026-08-08)
ran a systray icon on its own locked OS thread beside the WebView2 window on
the main thread, in the client, and shipped. It was removed by #442 because of
the two-icon complaint, not because it failed. Windows message queues belong to
threads; the conflict was only ever two loops on one goroutine.

## Decision

### 1. One file, two roles

| Process | Runs as | Started by | Owns |
|---|---|---|---|
| `LANcast.exe service run` | LocalSystem, session 0 | the SCM at boot | the server: port, database, workers, ffmpeg |
| `LANcast.exe` | the signed-in user | run key, shortcut, double-click | the tray **and** the window, in one process |

The service stays. Aither is publicly reachable and friends depend on it, so
the server must run after a reboot with nobody signed in. The Plex model (no
service, the tray *is* the server) was rejected for that reason, although it
would have made the session-0 GPU rule (CLAUDE.md) disappear.

On a machine with no service installed, `LANcast.exe` hosts the server
in-process behind the same tray, as `LANcast-Server.exe tray` does today, and
Exit stops it. Closing never stops a server you do not own, and always stops
one you do.

### 2. The tray is always there, and ✕ hides

Because the tray and the window share a process, the tray exists exactly as
long as the window can be hidden. ✕ always hides; **Exit** in the tray closes
the window and the icon and leaves the service running. The menu:

```
Open LANcast
──────────────
✓ Start LANcast at login
──────────────
Update libraries
Cancel library update        (greyed when nothing is scanning)
──────────────
Check for updates…
About LANcast…
──────────────
Exit
```

Deleted with it: `desktopprefs.CloseToTray` and its toggle, `raise.HoldTray`
/ `TrayPresent` / `Quit`, "Quit the LANcast app", the client's old tray file,
and the second run-key value. `raise.Show` stays, for a second double-click
raising the running window.

### 3. Tray actions go through the signed-in page

The tray holds no credentials, and a scan is an admin action. The tray asks the
page, by a fixed action name through the window controller. The page, which is
signed in, calls the existing API, or shows the window at sign-in if it cannot.
The page reports scan state back through a binding, so Cancel is greyed except
during a scan. **No new authority and no local back door.** A named pipe to
the service was the alternative and was rejected as a new trust boundary.

The web view is created on first need, not at login (lazy). A scan from the
tray before the window has ever opened opens the window. Keeping WebView2
resident for every signed-in user costs more than that.

### 4. The server program becomes a package

`cmd/lancastd`'s package main moves unchanged to `internal/serverapp`
(`serverapp.Main`), so both entrypoints can run it while the two binaries still
ship. The single `LANcast.exe` dispatches the server's subcommands (`service`,
`serve`, `reset-auth`, `restore`, `devseed`) to it. Its default, with no
subcommand, is the desktop: tray and window.

On Linux the binary is headless. `serve` is the systemd entrypoint, the unit
keeps the name `lancastd.service`, and desktop mode is a clear error.

### 5. The transition release is installer-only

The in-app updater stages files **by name** (`installFiles`), and the service is
registered against `LANcast-Server.exe`. An installed updater cannot cross to a
single `LANcast.exe` on its own. The release that makes the switch is marked
installer-only (the updater refuses to stage it, and the release notes say
so). The installer already stops the service, kills old processes by their old
names, re-registers the service and rewrites shortcuts and run keys.

A bridge release, in which the service rewrites its own registration, was
rejected: a self-modifying service registration is exactly the kind of change
that fails only on a real machine.

**Checked before that release:** Aither's inbound firewall rules. Program-scoped
rules exist for older executable names (`lancastd.exe` … `lancast-server.exe`);
the 8080 and 443 rules must be port-scoped, or renaming the file cuts friends
off without LANcast reporting anything.

## Build order

1. ~~The threading test~~: answered by `991875f8`.
2. **`internal/serverapp`** (this ADR's first change): a pure move with no
   behaviour change. `cmd/lancastd` becomes a wrapper.
3. **The client runs the server's subcommands** through `serverapp`. Both
   executables still ship.
4. **The tray moves into the window process.** The coordination code goes in
   the same change, and tray actions go through the page.
5. **Packaging:** one `LANcast.exe`, the installer migration, run keys and
   shortcuts, goreleaser, and the installer-only transition.
6. **Verify the installed artifact** with a reboot, both with the service and
   without, checking processes rather than windows, and the firewall from a
   phone on cellular.

## Consequences

**Good:** one icon, one file, one run key, and a tray that cannot be missing
when the window needs it. A large amount of cross-process code is deleted
rather than added.

**Good:** the server keeps its service, so nothing about uptime, session 0 or
ffmpeg changes.

**Cost:** one release must go out installer-only.

**Cost:** the desktop binary carries the server code and the server binary
carries the window code. Both are already pure Go with no cgo, so this is
size, not a new build requirement. `lancast-faces` stays separate as the one
cgo build (ADR 0052).

## Revisit if

- **The tray's scan and cancel need to work with no window ever opened** for
  the session. That would need either eager creation (memory) or a privileged
  channel (a new trust boundary).
