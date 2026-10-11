# One executable: design plan

*Written 2026-10-11. A plan for an ADR (proposed 0080, superseding the split in
[ADR 0022](adr/0022-client-and-server-executables.md) and amending
[ADR 0023](adr/0023-native-desktop-client.md)). The process model is decided;
nothing is built yet.*

## The goal

`LANcast-Server.exe` and `LANcast-Client.exe` become one `LANcast.exe`. The
tray stops being optional: it is always there while LANcast is open on the
desktop, it carries the menu Plex's tray carries, and the **Close to tray**
setting is deleted rather than defaulted on.

## Decided: one executable, two roles

One file is not one process. The service runs in session 0, which has no
desktop, so it can never draw a tray icon or a window. The choice was about
processes, and it went this way:

| Process | Runs as | Started by | Owns |
|---|---|---|---|
| `LANcast.exe service run` | LocalSystem, session 0 | the SCM at boot | the server: port, database, workers, ffmpeg |
| `LANcast.exe` | the signed-in user | the run key, a shortcut, a double-click | the tray **and** the window, in one process |

**Rejected: the Plex model** (one process in the user's session, no service).
It is the only option that makes ffmpeg's GPU work without care (see
CLAUDE.md on session 0), and it collapses everything into one process. It was
rejected because the household server is publicly reachable and friends depend on it: under
that model the server is down after every reboot until somebody signs in.
The data directory would also have to leave `ProgramData`.

**What stays true:** the session-0 rule for ffmpeg is unchanged, because the
server is still the service.

## Why this is mostly deletion

Most of the current desktop machinery exists to coordinate **two user-session
processes** (the server's tray and the client window). Put the tray and the
window in one process and the coordination has nothing left to do.

| Goes | Why it existed |
|---|---|
| `desktopprefs.CloseToTray` and its Settings toggle | the ✕ could only hide if some *other* process could bring the window back |
| `raise.HoldTray` / `raise.TrayPresent` | the client asking whether that other process was alive |
| `raise.Quit` and "Quit the LANcast app" | the tray asking a different process to close its window |
| `cmd/lancast/tray_windows.go` (`runLauncherTray`) | the client's own icon, already retired in practice |
| the `LANcast Tray` run-key value | two processes, two run keys |
| `openClientApp` (launch `LANcast-Client.exe` beside the server) | the tray and the window were different binaries |

`raise.Show` survives, but only for **second launches**: double-clicking
`LANcast.exe` while it is already running still has to raise the existing
window, and that is the one place cross-process signalling remains.

## The tray menu

Modelled on Plex's, with LANcast's wording rules intact (Exit never stops the
service, so it never raises a UAC prompt):

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

- **Open LANcast** shows the window (it is hidden, never destroyed, while the
  process lives).
- **Start LANcast at login** is one run key for this process. The service
  already starts at boot.
- **Check for updates… / About…** open Settings panes in the window, as today.
- **Exit** closes the window and removes the icon. The service keeps running,
  and the tooltip says so.

### Update libraries: through the window's session (decided)

The tray is not the server and holds no credentials. A scan is an admin action
on an authenticated API. So the tray **asks the page**, and the page, which is
signed in, calls the API. This adds no new authority, no local back door and no
new trust boundary.

Because the tray and the window are now the same process, this is a direct call
rather than IPC:

1. The tray calls the window controller's `Eval` with a fixed action name
   (`lancastTray("scan")` / `lancastTray("cancel-scan")`). Only those names go
   in; the page owns what they mean.
2. The page checks it is signed in as an admin. If so, it calls the existing
   scan endpoints. If not, it shows the window at the sign-in screen, so a
   tray click never fails silently.
3. **Greying Cancel:** the page already receives scan progress from the event
   stream ([ADR 0079](adr/0079-the-server-says-when-something-changed.md)). It
   reports `{scanning: bool}` back through a bound function, and the tray
   enables or disables the item from that. The server learns nothing new.

**The trade-off to accept:** the tray can only act while a page is loaded and
signed in. That is also what makes it safe.

**Open question: does the web view exist while the window is hidden?**
- *Eager:* create it hidden at login. The tray always has a live session; it
  costs WebView2's memory (~100 MB+) for every signed-in user, all the time.
- *Lazy (recommended):* create it on first need. A scan from the tray before
  the window has ever opened opens the window, and the scan then runs from
  there.

## Threading

Already worked out in
[desktop-lifecycle-plan.md](desktop-lifecycle-plan.md#close-to-tray--the-mechanism-investigated-2026-08-08):
Windows message queues are per thread. The window keeps the main thread; the
tray runs on a goroutine that calls `runtime.LockOSThread()` and owns its own
queue. Two catches:

- `fyne.io/systray` locks the main goroutine in its `init()`. Check that
  this does not fight the web view for the main thread before building
  anything else; if it does, the fallback is a hand-rolled `Shell_NotifyIcon`
  (ADR 0022 already allows that).
- Tray click handling keeps one goroutine per item. The reason is in
  `cmd/lancastd/tray_windows.go`: systray drops clicks that arrive while a
  handler is busy.

## No service installed

On a machine without the service (a dev box, or a portable install),
`LANcast.exe` hosts the server **in-process** behind the same tray, as
`lancastd tray` does today. Exit then stops the server, because this process
owns it. That keeps the ownership rule from the lifecycle plan: closing never
stops a server you do not own, and always stops one you do.

## Command line

| Today | After |
|---|---|
| `LANcast-Client.exe` | `LANcast.exe` |
| `LANcast-Client.exe -browser` | `LANcast.exe -browser` |
| `LANcast-Server.exe tray` | `LANcast.exe` (desktop mode is the default) |
| `LANcast-Server.exe service …` | `LANcast.exe service …` |
| `LANcast-Server.exe` (foreground server) | `LANcast.exe serve` |
| `resetauth`, `restore`, `devseed` | unchanged, under `LANcast.exe` |

On Linux the binary is headless. `serve` is the systemd entrypoint, and desktop
mode is a clear error instead of a crash.

The Windows binary stays `-H=windowsgui`. A subcommand that prints (`resetauth`)
already copes with that via `console_windows.go`.

`lancast-faces` stays a separate binary: it is the one cgo build (ADR 0052).

## Migration: the part that can hurt

### The updater cannot cross this release on its own

`internal/update/download.go` stages files **by name** (`installFiles`):
`LANcast-Server.exe`, `LANcast-Client.exe`, the DLLs. An installed v0.9.x
updater looking at an archive that holds only `LANcast.exe` stages nothing, or
stages DLLs beside binaries it never replaces. Even if it staged the new file,
the service's registered command still points at `LANcast-Server.exe`.

Two ways across:

- **A. Installer-only release (recommended).** The consolidating release is
  marked "run the installer" in its notes, and the updater refuses to stage it
  (a `requires_installer` field in the release manifest, or simply no Windows
  update archive for that tag). This is honest, happens once, and the
  installer already knows how to stop the service, kill processes and
  re-register.
- **B. A bridge release.** v(N-1) teaches the updater the new name and has the
  service rewrite its own binPath (`ChangeServiceConfig`, which LocalSystem
  can do) during the swap. It costs more, and it is a self-modifying service
  registration, which is exactly the kind of thing that fails only on a real
  machine.

### Everything that names an executable

- **Service registration:** `sc.exe` binPath → `LANcast.exe service run …`.
- **Run keys:** `LANcast` (client) and `LANcast Tray` (server tray) become one
  `LANcast` value pointing at `LANcast.exe`. The installer deletes the old
  `LANcast Tray` value; `autostart.Target`'s `own`/`foreign` names change.
- **Shortcuts:** Start menu "LANcast Client" and "LANcast Server" become one
  "LANcast"; the desktop shortcut and `DisplayIcon` are retargeted.
- **Installer `taskkill` lines** keep the *old* names too, so an upgrade kills
  stray processes from the previous layout.
- **Firewall: check this before shipping.** The server's inbound rule for 8080
  must be port-scoped, not program-scoped. A rule bound to
  `LANcast-Server.exe` would silently cut friends off the moment the file is
  renamed, and nothing in LANcast would report it.
- **Client data:** `%APPDATA%\LANcast\client` (WebView2 profile, prefs,
  window placement) stays where it is. `close_to_tray` is ignored when read
  and dropped on the next save.
- **goreleaser:** one Windows build (`LANcast`) instead of two. On Linux, keep
  the `lancastd.service` unit name and change its `ExecStart` to
  `lancast serve`.
- **Docs:** README, CLAUDE.md layout and swap instructions, the memory notes
  on swapping the installed server, and the release notes for the transition.

## What does not change

- No API change and no schema change. If any step turns out to need one,
  stop: the design has drifted.
- The server's no-cgo build. Both current binaries already build with
  `CGO_ENABLED=0`, and the client's web view, mpv and libretro bindings go
  through `syscall`.
- The service, its account, its data directory, and its delayed auto-start.

## Build order

1. **Spike the threading** (systray and WebView2 in one process, on two
   threads). If it fails, the plan changes shape, so it comes first.
2. **Fold `cmd/lancastd` into one `cmd/lancast`** with subcommands. The window
   path is the default.
3. **Tray in the window process**, then delete the coordination code listed
   above in the same change, so nothing half-dead survives.
4. **Tray actions through the page:** the `Eval` action names, the state
   binding, and greying Cancel.
5. **Installer and goreleaser:** one binary, run-key and shortcut migration,
   old names in `taskkill`.
6. **Updater:** refuse to stage the transition release (option A).
7. **Verify the installed artifact**, twice: once with the service installed
   and once without, each with a reboot. Check processes in Task Manager, not
   by eye. Check the 8080 firewall rule from a phone on cellular.

## Acceptance

- Exactly one LANcast icon in the notification area, whenever LANcast is
  running on the desktop, with no setting that can remove it.
- ✕ hides the window; Open brings back the same window, at the same place.
- Exit leaves the service running, and no `LANcast.exe` survives in the user
  session.
- Update libraries from the tray starts a scan visible in the app. Cancel is
  greyed except during a scan.
- After a reboot with nobody signed in, a friend's server can still reach
  this one.
