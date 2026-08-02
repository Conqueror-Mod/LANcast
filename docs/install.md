# Installing LANcast

LANcast ships as two small executables — `lancastd` (the server) and `lancast`
(the launcher you open) — with no runtime to install. ffmpeg is an optional extra
for transcoding.

## The one rule that matters most

> **A service uses an explicit, machine-wide data directory.** A Windows service
> runs as a service account whose per-user config dir is *not* yours, so a service
> left on the default would build a **second, empty library** somewhere you never
> look. `lancastd service install` pins the data dir explicitly —
> `%ProgramData%\LANcast` on Windows, `/var/lib/lancast` on Linux — and any
> interactive `lancastd` you point at that same path sees the same library
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

- **Just run it**: double-click `lancast.exe`. It starts the server (windowless)
  and opens your browser, and sits in the system tray.
- **Run it on boot as a service** (an elevated terminal):

  ```
  lancastd.exe service install
  lancastd.exe service start
  ```

## Linux (server / NAS / Raspberry Pi)

Extract the archive for your architecture (`linux_amd64` or `linux_arm64`), then
install the service:

```
sudo ./lancastd service install
sudo ./lancastd service start
```

This writes a systemd unit pinned to `/var/lib/lancast` and enables it on boot.
Then open `http://<host>:8080` in a browser. `lancast` (the launcher) is a desktop
convenience and is not needed on a headless server.

Manage it with `service status | stop | uninstall`, or `systemctl … lancastd`.

## First run

The first account you create becomes the admin. **Until an account exists the
server binds `127.0.0.1` only** — reachable from the machine itself and nowhere
else; restart after creating the account to reach it from other devices. See
[security.md](security.md).

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
