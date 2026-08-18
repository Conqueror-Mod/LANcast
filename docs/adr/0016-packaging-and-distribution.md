# ADR 0016 — Packaging and distribution

Date: 2026-07-26 · Status: **superseded by [ADR 0022](0022-client-and-server-executables.md)** (two executables, no terminal) · the packaging decided here was never built

> **The ffmpeg half is amended by
> [ADR 0043](0043-media-tools-are-fetched-not-bundled.md)**: still not bundled,
> but no longer only documented — the app fetches the tools when asked. This
> ADR invited that revisit "once there is demand"; a second install that could
> not play most of its library was the demand.

> **Deferred, decisions recorded.** The build is not being done yet — branding
> came first. The open questions were settled so the work can start cold later:
> service management is **built into the binary**; **macOS is dropped** for now
> (no way to test it currently — revisit if a test path appears); ffmpeg is
> **documented, not bundled**. The rest of this ADR is the plan to pick up from.

## Context

LANcast works end to end — scan, browse, play, transcode, subtitles, accounts,
TLS — but the only way to run it is `go run ./cmd/lancastd` from a checked-out
source tree with a Go toolchain installed. That is fine for development and
impossible for anyone else. This ADR decides how LANcast becomes something a
person downloads and runs, and keeps running.

Three facts shape every choice below:

- **It is already a single binary.** The client is embedded (`go:embed`), and
  SQLite is pure Go ([ADR 0001](0001-go-and-pure-go-sqlite.md)) so there is no
  CGO. One file contains the server, the database driver, and the entire UI.
- **ffmpeg is the only external dependency, and it is optional.** LANcast
  degrades gracefully without it (no transcoding, no probe-based decisions), and
  says so plainly at startup.
- **Windows is the primary target**, with Linux, a Raspberry Pi, and a NAS named
  as goals in the roadmap. No CGO means cross-compiling to all of them is a
  matrix of `GOOS`/`GOARCH`, not a per-platform build farm.

## Decision

**Ship one self-contained binary per platform; do not bundle ffmpeg.** The
binary needs no runtime beyond the OS. ffmpeg stays a documented, optional
install — bundling it would multiply the download size several times over, drag
in its licensing, and defeat the "one file" story for the majority who direct-play.
Startup already reports whether ffmpeg was found, so its absence is legible.

**Cross-compiled release archives, built by goreleaser on a version tag.**
A pushed tag (`vX.Y.Z`) triggers a GitHub Actions job that cross-builds the
matrix, packages each target as an archive with a README and license, generates
checksums, and publishes a GitHub Release. Targets:

| GOOS | GOARCH | Covers |
|---|---|---|
| windows | amd64 | the primary desktop target |
| linux | amd64 | servers, most NAS |
| linux | arm64 | Raspberry Pi 4/5, ARM NAS |

macOS (`darwin`) is deliberately out of the matrix for now: there is no way to
test it currently. It is a one-line addition to the matrix (and a `launchd`
plist in `service install`) whenever a test path exists.

No CGO means every one of these builds on a plain `ubuntu-latest` runner with no
cross toolchain. The committed `internal/web/dist` is what the binary embeds, and
CI already fails a build whose `dist` drifts from source, so a release builds
from the committed bundle without running `npm` at release time.

**Service management is built into the binary, with no new dependency.** A
`lancastd service install | uninstall | start | stop | status` command registers
LANcast to start on boot and stay running:

- **Windows** via `golang.org/x/sys/windows/svc`, already an indirect dependency
  — a real Windows service, not a Task Scheduler entry.
- **Linux** by writing and enabling a `systemd` unit.

(macOS `launchd` support lands with the macOS target, deferred above.)

This is chosen over a cross-platform service library (kardianos/service and the
like) to keep the dependency set as small as it is today: the platform pieces
are small and the standard library plus `x/sys` already cover Windows.

**A service uses an explicit, machine-wide data directory — never the per-user
default.** This is the trap. A Windows service runs as a service account whose
`os.UserConfigDir()` is *not* the installing user's, so a service left to the
default would build a second, empty library somewhere the owner never looks.
`service install` therefore records an explicit `--data` path — `%ProgramData%\LANcast`
on Windows, `/var/lib/lancast` on Linux — and the running service and an
interactive `lancastd` pointed at the same path see the same data.

**Version comes from the tag, not a constant.** The hardcoded
`api.Version = "0.2.0"` is replaced by a value injected at build time with
`-ldflags -X`, so the binary, the `GET /api/health` response, and the release tag
can never disagree. The existing ship-release skill owns cutting the tag and the
notes; goreleaser owns everything after it.

## Consequences

**Good — installing is downloading one file.** No runtime, no toolchain, no
container required. `ffmpeg` is a second optional download for anyone who wants
transcoding, and the app tells them when they need it.

**Good — every target builds from one machine.** The no-CGO decision from M0 pays
off here: the release matrix is pure cross-compilation, fast and reproducible,
with no per-platform runners or C cross toolchains.

**Good — "start on boot and stay up" is a first-class command.** On the primary
Windows target especially, `lancastd service install` turns a foreground process
into a managed service, which is the difference between a demo and something a
household actually relies on.

**Cost — service accounts and file paths need care.** The explicit data-dir rule
above exists because the default would silently misbehave under a service. The
same applies to ffmpeg: a service's `PATH` may not include a user-installed
ffmpeg, so the service config records ffmpeg's location the same way it records
the data dir, rather than trusting `PATH`.

**Cost — five artifacts to test, not one.** Cross-compiling is easy; *verifying*
each artifact runs is not automatic. At minimum the release job smoke-tests the
Linux binary (start, hit `/api/health`, stop); the Windows service path is
verified by hand until it is worth automating.

**Cost — no installer yet.** This ships an archive and a `service install`
command, not an MSI or a winget/Homebrew package. That is deliberate sequencing:
the binary and the service command are the substance; a signed installer and
package-manager presence are a later polish that depend on this existing first.

## The thing that is easy to get wrong

The per-user data directory. Everything about LANcast's storage has quietly
assumed an interactive user whose `UserConfigDir` is stable — and a service
breaks that assumption without any error, just an empty-looking library and a
second copy of the data on disk. The service install path must pin `--data`
explicitly and the docs must lead with it, or the first thing every service user
hits is "where did my library go?"

## Revisit if

A signed Windows installer (MSI) or package-manager distribution (winget,
Homebrew, a `.deb`, a Docker image) becomes worth the maintenance — each is
additive on top of the archives decided here. Or if bundling ffmpeg for a
"batteries-included" build is worth the size and licensing once there is demand
for a zero-configuration transcoding install.

## Amendment — signed releases, 2026-08-09

Automatic updates are being built, and they change what a release has to prove.
Auto-install means a process running as LocalSystem downloading a binary and
executing it: if the distribution channel is ever untrustworthy, that is remote
code execution as SYSTEM. The `checksums.txt` this pipeline already publishes
does not help, because it is served from the same place as the download — it
proves the bytes arrived intact, not that the project produced them.

So a release now carries **`checksums.txt.sig`**, an Ed25519 signature over the
checksum list. One signature covers every artifact: match a download against its
digest in a signed list and the download is as good as signed. Verification is
offline, against a public key compiled into the binary, and depends on nothing
that was downloaded alongside it.

**A separate key from the plugin project key** ([ADR 0021](0021-plugin-distribution-and-trust.md)).
Plugin provenance and release provenance are different trust domains; one key
for both means a compromise of either is a compromise of everything. They share
an algorithm and nothing else.

**An unsigned release is a distinct answer, not a failure.** A build cut without
the key publishes no signature, and the updater treats that as installable by
hand but never automatically. Releases made before this existed keep working on
those terms. A signature that is *present and wrong* is refused outright.

**A build with no key compiled in refuses everything.** A verifier that waves
downloads through when it cannot check is worse than no verifier, and that is
the case the tests pin hardest.

### Setting it up

```
go run ./cmd/lcsign keygen -out release.key
```

The public half goes in `internal/release/publickey.go`; the private half goes
in the `RELEASE_SIGNING_KEY` repository secret and nowhere else. `*.key` is
already gitignored. Until both are set, releases publish unsigned and automatic
installation stays unavailable — which is the intended failure mode rather than
an outage.
