# Installing LANcast

LANcast ships as one small executable — `LANcast.exe` on Windows, `lancast`
on Linux — with no runtime to install. Opened, it is the app: a window and its
tray icon. Installed as a service, the same file is the server
([ADR 0080](adr/0080-one-executable-two-roles.md)). ffmpeg is an optional extra
for transcoding, and on Windows LANcast can fetch it for you.

| Platform | Status |
|---|---|
| Windows 10 and 11, x64 | Supported: the app, its window, and the service |
| Linux amd64 | Supported: the server as a systemd service, used from a browser |
| Linux arm64 (Raspberry Pi and similar) | *Built, not run*: cross-compiled for every release, never run on an arm64 machine. Reports welcome |
| macOS | Not built |

## The one rule that matters most

> **A service uses an explicit, machine-wide data directory.** A Windows service
> runs as a service account whose per-user config dir is *not* yours, so a service
> left on the default would build a **second, empty library** somewhere you never
> look. `LANcast service install` pins the data dir explicitly —
> `%ProgramData%\LANcast` on Windows, `/var/lib/lancast` on Linux — and any
> interactive `LANcast serve` you point at that same path sees the same library
> ([ADR 0016](adr/0016-packaging-and-distribution.md)).

If your library ever looks empty after installing as a service, this is why:
you (or an interactive run) are pointed at a different data dir. Pass the same
`-data` path.

On Windows that folder belongs to the service: **only SYSTEM and Administrators
can read it**, and the service puts that back at every start. It holds the
database, the server's private keys and the provider keys, so an ordinary
account on the same PC is kept out of them. To read anything in it — the log,
for instance — use an Administrator shell, or read the log from
Settings › Troubleshooting › Server log instead. Running the server by hand
against it (`LANcast.exe serve -data "C:\ProgramData\LANcast"`) needs an
Administrator shell too.

## Windows

### Installer (recommended)

Run **`LANcast-Setup-<version>.exe`** from the release. It installs
`LANcast.exe` to `Program Files\LANcast` and adds a Start-menu and desktop
shortcut to **LANcast**. One page asks **how LANcast runs**:

- **As a Windows service** (ticked by default). The server starts with Windows
  and serves your other devices whether or not anybody is signed in. Its
  library lives in `%ProgramData%\LANcast`.
- **Without one.** The server runs only while the LANcast app is open, and its
  library lives in your own `%APPDATA%\LANcast`. Choose this if you only ever
  watch on this PC.

Once a library exists in either place, Setup keeps it the way it is and shows
the box greyed with the reason: the two keep their libraries in different
folders, and switching would open an empty one. Moving a library between them
later is Settings › This device › This app › **Run as a Windows service**
([ADR 0092](adr/0092-the-library-moves-with-the-server.md)).

Double-click the shortcut to open the app in its own window, with LANcast's
icon in the notification area — no terminal, ever. Closing the window keeps
LANcast in the tray; **Exit** from the icon closes it, and a service keeps
serving. The finish page offers the browser instead if you would rather have a
tab. Uninstalling removes the service and shortcuts but **leaves your library**
where it was.

### From the archive (manual)

Unzip `LANcast_<version>_windows_amd64.zip` anywhere, then either:

- **Just run it**: double-click `LANcast.exe`. With no service installed it
  runs the server itself, behind its window and tray, and **Exit** stops it.
  There is one LANcast icon.
- **Run it on boot as a service.** The service runs `LANcast.exe` as
  LocalSystem, so first move the unzipped folder somewhere only administrators
  can change, such as `C:\Program Files\LANcast` — not `Downloads`. Then, in an
  Administrator terminal in that folder:

  ```
  .\LANcast.exe service install
  .\LANcast.exe service start
  ```

  `service install` protects `%ProgramData%\LANcast`, records where ffmpeg is
  (see [ffmpeg](#ffmpeg)), and registers the service. `service status`,
  `service stop`, `service restart` and `service uninstall` do what they say.

## Linux (server / NAS / Raspberry Pi)

amd64 is supported; arm64 is *built, not run* (see the table above). Install
ffmpeg from your distribution first — LANcast's own ffmpeg download is
Windows-only — so the service finds it:

```
sudo apt install ffmpeg
```

Extract `LANcast_<version>_linux_amd64.tar.gz` (or `_linux_arm64`) and put
`lancast` where it will stay: the unit runs it from the path you install it
from, as root.

```
tar -xzf LANcast_<version>_linux_amd64.tar.gz
sudo install -m 755 lancast /usr/local/bin/lancast
sudo lancast service install
```

This writes `/etc/systemd/system/lancastd.service`, pinned to
`/var/lib/lancast`, and enables and starts it; `sudo lancast service start` is
only needed after a `stop`. Manage it with `sudo lancast service
status | stop | restart | uninstall`, or `systemctl … lancastd`, and read its
log with `journalctl -u lancastd`. The unit has no `User=` line, so the server
and its ffmpeg run as root; [security.md](security.md) says what that costs and
how to narrow it.

**Upgrading from 0.11.0 or earlier:** the unit those versions wrote started the
desktop launcher instead of the server, so it never answered on its port. Run
`sudo lancast service install` once more after installing the new binary; it
rewrites the unit with `serve` and restarts the service. From this version the
server also makes `/var/lib/lancast` readable by root alone (mode 0700) every
time it starts.

To run the server in the foreground instead, `lancast serve`. `lancast` with no
arguments starts a server and tries to open a browser, which a headless machine
does not have.

Face grouping and photo search need a separate helper, `lancast-faces`, which
is published for Linux amd64 only.

## First run

The first account you create becomes the admin. **Until an account exists the
server binds `127.0.0.1` only** — reachable from the machine itself and nowhere
else. See [security.md](security.md).

- **On Windows**, open the LANcast shortcut on the server PC; the window is on
  the same machine, so this just works.
- **On a headless Linux machine**, reach it through an SSH tunnel from the
  computer you are sitting at, then open <http://localhost:8080> there:

  ```
  ssh -L 8080:127.0.0.1:8080 you@your-server
  ```

The setup form creates the account and, ticked by default on Windows, fetches
ffmpeg. Then **restart the server** so it listens on your network:

```
Restart-Service lancastd             # Windows, in an Administrator PowerShell
sudo lancast service restart         # Linux
```

Without a service, Exit the app from its tray icon and open it again.

After the restart the server answers on **HTTPS**, with a certificate it made
for itself: open `https://<host>:8080` (a plain `http://` address is redirected).
A browser warns about that certificate until you trust it; the LANcast window
pins it and does not. Settings › Server › Network takes a certificate of your
own instead.

**Add a library:** Settings › Server › Libraries, say what kind of media it
holds, point it at the folder, and scan. A service reads that folder as its
own account, not as you: on Windows a drive letter you mapped (`Z:`) belongs to
your sign-in and the service does not see it, and on Linux root reads
everything. A library on a local disk is the simple case.

### Locked out

There is no password reset over the network, by design. Recover locally: stop
the server, then run `reset-auth` from the directory the binary is in.

**Pass `-data` with the service's folder.** Without it, `reset-auth` (and
`restore`, below) work on *your own* per-user folder, which a service never
uses; they then say "no accounts exist" and name the folder that does hold
one. On a server without a service, leave `-data` off.

```
Stop-Service lancastd
.\LANcast.exe reset-auth -data "C:\ProgramData\LANcast"          # shows what it would remove
.\LANcast.exe reset-auth -data "C:\ProgramData\LANcast" -yes     # does it
Start-Service lancastd
```

On Windows this needs an **Administrator** shell — the data directory belongs
to the service account. On Linux:

```
sudo systemctl stop lancastd
sudo lancast reset-auth -data /var/lib/lancast -yes
sudo systemctl start lancastd
```

Accounts go, and with them everything that can sign one in: sessions,
passkeys, API keys, two-factor and pending invites. Watch history, libraries,
and settings stay, and the new admin inherits the old one's resume points. Then create an account as
above. See [security.md](security.md#losing-the-password).

### Restoring a backup

A backup is the database and nothing else ([ADR 0058](adr/0058-a-backup-is-the-database.md)).

**From Settings:** Backup & storage › **Restore** on the backup you want. LANcast
backs up the database as it is now, restarts, and swaps the file in before
anything opens it. The backup it took first is in the same list, so restoring
the wrong one is undone the same way. If the restore fails, the server starts
with the database it had and the Backup page says why.

**By hand**, when the server will not start, or on a server that cannot restart
itself (Restore is then not offered). Swapping the database out from under a
running server is how a restore becomes the incident, so stop it first:

```
Stop-Service lancastd
.\LANcast.exe restore -data "C:\ProgramData\LANcast" -from "D:\Backups\lancast-backup.db"        # shows what it would do
.\LANcast.exe restore -data "C:\ProgramData\LANcast" -from "D:\Backups\lancast-backup.db" -yes   # does it
Start-Service lancastd
```

Same Administrator requirement as `reset-auth`, and for the same reason.

The database being replaced is **kept** beside it as
`lancast.db.replaced-<timestamp>`, so restoring the wrong backup can be undone;
delete it once you are satisfied. Everyone is signed out, because the sessions
belonged to the database that was replaced.

A backup itself holds **no logins**: they are cleared when the backup is
written, so a backup file copied to a stick cannot sign anyone in
([ADR 0058](adr/0058-a-backup-is-the-database.md), as amended). Backups written
before that are cleared on restore instead.

A backup taken by a **newer** LANcast is refused by name, before anything is
moved, because migrations are one-way. Update first, then restore. A backup
from an older build is migrated forward and is fine.

**Artwork is not in a backup.** The cache is roughly forty-six times the
database's size and is re-fetchable, so posters arrive again over the following
hours. If the media now lives at a different path, edit the library's location
in Settings and scan — a scan reconciles *files*, and every correction, rating
and watch position in the backup is kept.

## ffmpeg

LANcast plays most files directly with no dependency. **Conversion and
codec-based playback decisions need ffmpeg**, and without it a server appears to
work and then does not play things — which is the failure
[ADR 0043](adr/0043-media-tools-are-fetched-not-bundled.md) exists to end.

**You do not have to install it yourself.** The screen where you create the
first account offers the media tools as a **ticked option**, stating what will
be downloaded, from where, how large it is, under which licence, and that it is
optional — before anything is fetched. Setup does not block on the download: the
server is usable immediately and the fetch runs alongside it with progress in
Settings, so a file that will not play *while it is still running* is
explainable rather than mysterious. Settings › Server › Transcoding has the same
**Download ffmpeg** button for later, and for a server whose admin unticked it
([ADR 0048](adr/0048-media-tools-install-themselves-on-first-run.md)). The
download is offered on Windows only; on Linux, install your distribution's
`ffmpeg` package.

The build is pinned and checksum-verified before it is unpacked, installed into
the `tools` folder of the data directory by an atomic move, and a partial
install reads as absent. It is a GPL build and is named as one.

### An ffmpeg you installed yourself

LANcast finds ffmpeg in the folder you set in Settings › Server › Transcoding,
beside `LANcast.exe`, in the data directory's `tools` folder, or on `PATH`, in
that order.

**As a Windows service, it runs ffmpeg only from a folder that only
Administrators and SYSTEM can change.** The service runs as LocalSystem and
ffmpeg reads every file in your libraries, so an ffmpeg that any ordinary
account could replace would be a way to run code as SYSTEM. A per-user install
— `winget install Gyan.FFmpeg` puts it under your `AppData` — is therefore
**not used**, and Settings › Server › Transcoding shows it as *Not used*, with
the reason.

Either press **Download ffmpeg**, or have LANcast copy your own into its
protected `tools` folder, from an Administrator shell:

```
.\LANcast.exe service tools                                # finds it on your PATH
.\LANcast.exe service tools -ffmpeg-dir "D:\Tools\ffmpeg\bin"  # or names the folder
.\LANcast.exe service restart
```

`service install` does the same copy when it finds ffmpeg, so installing ffmpeg
*before* the service saves a step. An ffmpeg in a folder that is already
protected, such as one under `C:\Program Files`, is used where it is.

On Linux the same rule applies to ownership and modes: a folder or binary
someone other than root (or the server's own user) can write to is refused.
The distribution's `/usr/bin/ffmpeg` passes.

With no ffmpeg at all, LANcast still runs and serves direct-play files; it says
so plainly rather than failing.

## When the server will not stay up

A LANcast running as a Windows service writes to **`lancastd.log` in its data
directory** — `%ProgramData%\LANcast\lancastd.log` for the default install. One
previous generation is kept as `lancastd.log.1`; nothing older, so it cannot
fill a disk. Open it from an Administrator shell (the folder is closed to
ordinary accounts), or, while the server is up, read it in
Settings › Troubleshooting › Server log. On Linux, `journalctl -u lancastd`.

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

To watch it live, stop the service and run the server in an Administrator
terminal:

```
Stop-Service lancastd
& "C:\Program Files\LANcast\LANcast.exe" serve -addr :8080 -data "C:\ProgramData\LANcast"
```
