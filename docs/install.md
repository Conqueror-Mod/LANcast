# Installing LANcast

LANcast ships as two small executables — `LANcast-Server` (the daemon) and
`LANcast-Client` (the launcher you open) — with no runtime to install. ffmpeg is
an optional extra
for transcoding.

## The one rule that matters most

> **A service uses an explicit, machine-wide data directory.** A Windows service
> runs as a service account whose per-user config dir is *not* yours, so a service
> left on the default would build a **second, empty library** somewhere you never
> look. `LANcast-Server service install` pins the data dir explicitly —
> `%ProgramData%\LANcast` on Windows, `/var/lib/lancast` on Linux — and any
> interactive `LANcast-Server` you point at that same path sees the same library
> ([ADR 0016](adr/0016-packaging-and-distribution.md)).

If your library ever looks empty after installing as a service, this is why:
you (or an interactive run) are pointed at a different data dir. Pass the same
`-data` path.

## Windows

### Installer (recommended)

Run **`LANcast-Setup-<version>.exe`** from the release. It installs both exes to
`Program Files\LANcast`, **registers and starts the server as a service**, and
adds a Start-menu and desktop shortcut to **LANcast** (the launcher). Double-click
that shortcut to open the app in your browser — no terminal, ever. Uninstalling
removes the service and shortcuts but **leaves your library** in
`%ProgramData%\LANcast`.

### From the archive (manual)

Unzip `lancast_<version>_windows_amd64.zip` anywhere, then either:

- **Just run it**: double-click `LANcast-Client.exe`. It starts the server (windowless)
  and opens your browser, and sits in the system tray.
- **Run it on boot as a service** (an elevated terminal):

  ```
  LANcast-Server.exe service install
  LANcast-Server.exe service start
  ```

## Linux (server / NAS / Raspberry Pi)

Extract the archive for your architecture (`linux_amd64` or `linux_arm64`), then
install the service:

```
sudo ./LANcast-Server service install
sudo ./LANcast-Server service start
```

This writes a systemd unit pinned to `/var/lib/lancast` and enables it on boot.
Then open `http://<host>:8080` in a browser. `LANcast-Client` (the launcher) is a desktop
convenience and is not needed on a headless server.

Manage it with `service status | stop | uninstall`, or `systemctl … lancastd`.

## First run

The first account you create becomes the admin. **Until an account exists the
server binds `127.0.0.1` only** — reachable from the machine itself and nowhere
else; restart after creating the account to reach it from other devices. See
[security.md](security.md).

### Locked out

There is no password reset over the network, by design. Recover locally: stop
the server, then run `reset-auth` from the directory the binary is in.

```
Stop-Service lancastd
LANcast-Server.exe reset-auth          # shows what it would remove
LANcast-Server.exe reset-auth -yes     # does it
Start-Service lancastd
```

On Windows this needs an **Administrator** shell — the data directory belongs
to the service account. On Linux, `sudo`.

Accounts and sessions go; watch history, libraries, and settings stay, and the
new admin inherits the old one's resume points. Then create an account as
above. See [security.md](security.md#losing-the-password).

## ffmpeg (optional)

LANcast plays most files directly with no dependency. **Transcoding and
codec-based playback decisions need ffmpeg** — install it and put it on `PATH`
(the service records its directory so a service account finds it):

```
winget install Gyan.FFmpeg      # Windows
sudo apt install ffmpeg          # Debian/Ubuntu
```

Without ffmpeg, LANcast still runs and serves direct-play files; it says so
plainly at startup rather than failing.

## When the server will not stay up

A LANcast running as a Windows service writes to **`lancastd.log` in its data
directory** — `%ProgramData%\LANcast\lancastd.log` for the default install. One
previous generation is kept as `lancastd.log.1`; nothing older, so it cannot
fill a disk.

That file is where the reason lives. A service has no console, so anything it
would have printed is otherwise discarded, and Windows' own event log only says
*that* it stopped, never why.

Two entries in the event log are worth telling apart if you look there:

- **7023** "terminated with the following error" — the server reported a
  failure. It decided to stop, and the log file says why.
- **7034** "terminated unexpectedly" — the process disappeared without telling
  Windows anything, which is what an external kill looks like.

The service restarts itself after an unexpected exit — three attempts with
increasing delays, then it stays down rather than looping. A server that cannot
start at all (a database written by a newer build is the usual cause) fails
those three and stops, leaving a clean record instead of a restart storm.

To watch it live, stop the service and run the server in a terminal:

```
Stop-Service lancastd
& "C:\Program Files\LANcast\LANcast-Server.exe" -addr :8080 -data "C:\ProgramData\LANcast"
```
