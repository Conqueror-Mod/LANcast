# S6: Linux amd64 under systemd (2026-11-16)

Run-plan item S6 ([v1-run-plan.md](../v1-run-plan.md), v1-plan §3D.4). The
question was whether the Linux amd64 archive installs and serves under systemd
when a Linux user follows `docs/install.md`.

**Result: it fails as documented.** `sudo ./lancast service install` writes a
unit that never serves. The unit is enabled and "active", but nothing ever
listens on the port. Every 90 seconds it exits 1, and systemd restarts it for
ever. With a one-word change to the unit (`serve`), everything else worked:
first account, library, scan, probing, forced ffmpeg transcodes over HLS,
`reset-auth`, restart on boot and `service uninstall`. The journal had no
`ffmpeg reported errors` and no `level=ERROR` for the whole run.

Linux amd64 cannot be called "supported" until the unit is fixed. The fix is
small (see [the blocker](#blocker-the-unit-runs-the-desktop-launcher-not-the-server)).

## Environment

| | |
| --- | --- |
| Host | Windows 11 Pro 10.0.26300 (Aither) |
| Distro | WSL2 Ubuntu 26.04 LTS, x86_64. `/etc/wsl.conf` has `[boot] systemd=true`, and PID 1 is `systemd` |
| Build | `main` at `b6eaa2cb`, cross-built the way goreleaser's `lancast-unix` build is (`CGO_ENABLED=0`, `-s -w`, `main.Version` and `api.Version` = `0.11.0`). It does not have the release's provider keys or webhook, and neither matters here. Packed as `LANcast_0.11.0_linux_amd64.tar.gz` with README.md, LICENSE, NOTICE and COMMERCIAL.md, like the `unix` archive |
| Library | `/mnt/d/My Projects/Test Libraries/TEST MOVIE LIBRARY` (12 films). `write_nfo` is off by default, so nothing was written next to the media |
| ffmpeg | `apt install ffmpeg`: Ubuntu's 8.0.1-3ubuntu2 |

The v0.11.0 release asset was not downloaded. It has the same fault:
`git show v0.11.0:internal/service/service.go` writes the same serve-less
`ExecStart`, and `cmd/lancast/main.go` at v0.11.0 hands over to the server only
on a subcommand.

## What was run

On the Windows side (PowerShell), the build:

```powershell
$env:CGO_ENABLED="0"; $env:GOOS="linux"; $env:GOARCH="amd64"
go build -trimpath -o <scratch>\pkg\lancast -ldflags "-s -w -X main.Version=0.11.0 -X lancast/internal/api.Version=0.11.0" ./cmd/lancast
```

Inside the distro, as root (`wsl -d Ubuntu -u root`, which is what `sudo` is
in the doc):

```bash
tar xzf LANcast_0.11.0_linux_amd64.tar.gz -C ~chris/lancast-s6
cd ~chris/lancast-s6
./lancast service install      # install.md, Linux section
./lancast service start
```

The result:

```
installed lancastd, data dir /var/lib/lancast
warning: ffmpeg/ffprobe were not found — LANcast will direct-play only until they are installed
```

And the unit it wrote:

```ini
[Service]
Type=simple
ExecStart=/home/chris/lancast-s6/lancast -data /var/lib/lancast -addr :8080
Restart=on-failure
RestartSec=5
```

## Blocker: the unit runs the desktop launcher, not the server

`internal/service/service.go` `SystemdUnit` writes `ExecStart=<exe> -data … -addr …`
with no subcommand. It dates from when the Linux binary was `lancastd`, a server
only. Since ADR 0080 (v0.9.79, 2026-10-12) the Linux binary is `cmd/lancast`,
and its `main` hands over to the server only when `os.Args[1]` is a server
command. A launch that starts with flags is the **desktop launcher**.

This is what happens under systemd, measured:

1. The launcher calls `ensureServer`. Nothing answers on `:8080`.
2. `installedServiceState()` asks systemd about `lancastd`. The answer is
   `active`, because the launcher *is* `lancastd`, so it calls
   `waitForInstalledService` instead of spawning a server.
3. It waits 90 s for a server that will never start, prints
   `The LANcast service is active and has not answered at http://localhost:8080 after 1m30s.`
   and exits 1.
4. `Restart=on-failure` starts it again 5 s later. This repeats for ever.

```
13:54:31 systemd[1]: Started lancastd.service - LANcast self-hosted media server.
13:56:01 lancast[139]: LANcast: The LANcast service is active and has not answered at http://localhost:8080 after 1m30s.
13:56:01 lancast[139]: It is most likely still starting. Opening LANcast again in a moment usually finds it.
13:56:01 systemd[1]: lancastd.service: Main process exited, code=exited, status=1/FAILURE
13:56:06 systemd[1]: lancastd.service: Scheduled restart job, restart counter is at 1.
13:57:37 lancast[694]: LANcast: The LANcast service is active and has not answered at http://localhost:8080 after 1m30s.
13:57:42 systemd[1]: lancastd.service: Scheduled restart job, restart counter is at 2.
```

Through all of this, `ss -ltnp` showed nothing on 8080, and `/var/lib/lancast`
was never created. Between restarts, `systemctl status` and
`lancast service status` both report `active`, so a user who checks the
service is told it is fine. S5's notes guessed the launcher "may limp along by
accident". It does not.

`internal/service/service_test.go:69` asserts the serve-less line
(`ExecStart=/usr/bin/lancastd -data /var/lib/lancast -addr :8080`), so the test
locks in the fault rather than catching it.

**Fix:** have `SystemdUnit` write `ExecStart=<exe> serve -data … -addr …` and
update the test. Windows is not affected, because the Windows service uses
`service run`. A Linux machine already installed keeps its old unit until
`service install` runs again, so the release notes need to say that. Wave G's
quoting fix (R10-11) changed the same line and kept the missing subcommand.

### The workaround this run used to keep going

```bash
mkdir -p /etc/systemd/system/lancastd.service.d
cat > /etc/systemd/system/lancastd.service.d/s6-serve.conf <<'EOF'
[Service]
ExecStart=
ExecStart=/home/chris/lancast-s6/lancast serve -data /var/lib/lancast -addr :8080
EOF
systemctl daemon-reload && systemctl restart lancastd
curl -s http://127.0.0.1:8080/api/health
# {"api_version":1,"api_versions":[1],"status":"ok","version":"0.11.0"}
```

Everything below was run with this drop-in in place.

## What worked

| Step | Result |
| --- | --- |
| Before any account exists | Binds `127.0.0.1:8080` over http only. The WARN line says so, as documented |
| `apt install ffmpeg` while the server runs | Picked up **without a restart**: `/api/auth/status` → `can_convert: true`, and `/api/media-tools` → `directory: /usr/bin`, probe and transcode available. Probing of the new library ran at once |
| `POST /api/auth/setup` (`s6admin`, a random `s6-fake-…` password, `install_media_tools:false`) | 200. `restart_required: true` |
| `POST /api/libraries` (`kind: movie`, the test library) | 201, and the scan started by itself |
| Scan | `seen=12 changed=12 missing=0 elapsed_ms=909` |
| Probing | 12/12 `probed: true` within seconds. The decisions read correctly: Aladdin and Small Soldiers direct, dts/ac3 audio-only transcodes, HEVC full transcodes |
| `max_height=480` decisions | Aladdin: `video is 720p, above the 480p limit`, encode video, copy audio, target 480 |
| HLS, Aladdin (h264 720p → ceiling) | Playlist 200 in 3.2 s, `X-LANcast-Playlist: complete`, VOD, 867 segments, `#EXT-X-ENDLIST`. `init.mp4` 1,410 B and `seg00000.m4s` 269,532 B both 200. ffprobe of init+segment: **h264 854×480 + aac** |
| HLS, Ernest Saves Christmas (HEVC 1916×1040) | Playlist 200 in 3.4 s, complete, 914 segments. ffprobe: **h264 884×480 + aac** |
| `DELETE /api/stream/{id}/hls` | 204 for both, logged as `superseding transcode` |
| Journal, whole run | `journalctl -u lancastd \| grep -E 'ffmpeg reported errors\|level=ERROR'` matched nothing |
| After the account, restarted | `TLS enabled certificate=generated` and `listening addr=:8080 scheme=https`. Plain http answers `307` to https, as documented |
| Enabled on boot | WSL powered the VM off twice between commands (see the notes at the end). Both times the unit came back on boot by itself, with the library intact |
| `reset-auth -data /var/lib/lancast` (service stopped) | Dry run: `would remove 1 account and 2 sessions`. With `-yes`, they were removed. After a restart it was back to loopback only and unconfigured, and the library (12 items) survived |
| `lancast service status` / `stop` / `uninstall` | `active`, then exit 0, then `uninstalled lancastd`. The unit file and the `multi-user.target.wants` link were removed |

The encoder chosen was `h264_nvenc`, because WSL2 passes the NVIDIA GPU through
(`/usr/lib/wsl/lib/libcuda.so`). A typical headless Linux box without one gets
the software encoder, and that path was not exercised here. The startup check
for GPU HDR tonemapping failed on OpenCL (`-1001`) and correctly fell back to
the CPU tone map. It is logged at INFO, not as an error.

## Where install.md (as of b6eaa2cb) was wrong or missing a step

A separate session (S5) is rewriting install.md's Linux section in parallel,
and its notes say some of these are already addressed there. Check each one
against that version before acting on it.

1. **The service steps produce a server that never serves.** This is the
   blocker above, so it is a code fault rather than a wording one. The doc's
   "Then open `http://<host>:8080`" is wrong for two reasons as well: until an
   account exists the server is loopback-only, so `<host>` only works as
   `localhost` on the machine itself; and once an account exists it is https.
2. **Where to extract is never said.** The unit pins `ExecStart` to wherever the
   archive was unpacked (here `/home/chris/…`). If somebody unpacks it in
   `~/Downloads` and later tidies up, the service breaks. The doc should name a
   place (`/usr/local/bin` or `/opt/lancast`) before `service install`.
3. **The service runs as root.** The unit has no `User=` and there is no
   service account. The doc does not say so. Either the doc says it runs as
   root, or the unit gains a `lancast` user. That is a design decision for an
   ADR, not something to slip in.
4. **ffmpeg comes after the service steps, and the first-run download it
   promises does not exist on Linux.** The ffmpeg section says "You do not have
   to install it yourself… The screen where you create the first account offers
   the media tools". That fetch is Windows amd64 only
   (`internal/mediatools/install.go:109`). On Linux, `apt install ffmpeg` is the
   only route, and it is better done *before* `service install` so install
   records it. (Doing it afterwards also worked here, because `/usr/bin` is on
   systemd's default PATH.)
5. **"Locked out" and "Restoring a backup" use Windows commands only**
   (`Stop-Service`, `LANcast.exe`). On Linux, `sudo ./lancast reset-auth`
   targets **`/root/.config/LANcast`**, not the service's `/var/lib/lancast`. It
   says `no accounts exist; nothing to reset`, then names the right database
   and the `-data` flag. That hint saved the run, but the doc should say
   `sudo systemctl stop lancastd`, `sudo ./lancast reset-auth -data /var/lib/lancast`,
   and the same for `restore`.
6. **"When the server will not stay up" is Windows only.** On Linux the server
   logs to the journal (`journalctl -u lancastd`), and nothing goes to a
   `lancastd.log` in the data dir. It is also where the blocker above is
   visible, so it is the first thing a Linux user needs.
7. **Uninstall leaves the data dir**, which is right and matches Windows, but
   the Linux section does not say so, or that `/var/lib/lancast` is the thing
   to delete for a clean removal.

## Other findings (code, not doc)

- **The database is world-readable on Linux.** `/var/lib/lancast` is `0755`,
  and `lancast.db`, `-wal`, `-shm` and `history.json` are `0644`. The database
  holds password hashes, session and API-key material, and everyone's watch
  history. `secrets.key`, `config.json`, `identity/` and `tls/` are correctly
  `0600`/`0700`. `dataperm.Protect` is a no-op off Windows, and its comment
  says the directory is "created by the package with the service account's
  mode". There is no package, and no service account. This is the Linux side
  of R10-3, and it needs a `0700` data dir (or `0600` files) in its own fix.
- **A bare `reset-auth` creates a database.** Run without `-data`, it left a
  new, empty 712 KB `lancast.db` in `/root/.config/LANcast`. A recovery command
  that finds nothing to recover should not create a database as it goes.
- `/api/auth/status` reported `restart_required: true` before any account
  existed, apparently because ffmpeg had just appeared. It did no harm,
  because probing and transcoding worked without a restart, but it says a
  restart is needed when it is not.

## Notes for whoever runs this again

- **WSL powers the VM off a few seconds after the last `wsl.exe` exits.** That
  restarts the service and wipes `/tmp`. Here it lost the cookie jar and the
  generated password, which is why the run went through `reset-auth`. Hold the
  VM up with a background `wsl -d Ubuntu -u root -- sleep 3600`, and keep state
  under `/root`.
- Pass every bash step as a script file (`wsl -d Ubuntu -u root -- bash /mnt/c/…/script.sh`).
  Quoting `$(…)` or `\n` inside `wsl … bash -c '…'` from PowerShell breaks.

## Teardown

```bash
./lancast service stop && ./lancast service uninstall
rm -rf /etc/systemd/system/lancastd.service.d && systemctl daemon-reload   # the workaround drop-in; uninstall leaves it
rm -rf /var/lib/lancast /root/.config/LANcast ~chris/lancast-s6 /root/s6-work
apt-get purge -y <the 108 packages from the ffmpeg transaction in /var/log/apt/history.log>
```

After teardown, the unit could not be found, nothing listened on 8080, ffmpeg
and ffprobe were gone, and the data directories were removed. WSL's own
configuration was not changed.
