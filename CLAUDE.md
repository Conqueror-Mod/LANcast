# LANcast — working conventions

Read this before making changes. `docs/` holds the reasoning; this file holds
the rules.

## What this project is

A self-hosted media server: Plex's streamlined functionality with Kodi's
customizability. Four principles govern everything — server owns truth, clients
are thin, everything interesting is a provider, no phone-home. They are spelled
out in [README.md](README.md) and are not negotiable without an ADR.

## Layout

```
cmd/lancastd/      entrypoint: flags, wiring, shutdown
internal/config/   data dir, listen address, settings
internal/store/    SQLite access; owns schema.sql
internal/media/    filename → metadata heuristics (guessing lives ONLY here)
internal/scan/     filesystem walk and library reconciliation
internal/meta/     provider contract, matching, merge  (+ nfo/, tmdb/)
internal/enrich/   metadata enrichment worker
internal/artwork/  content-addressed image cache
internal/auth/     password hashing, server-side sessions
internal/probe/    ffprobe wrapper + direct-play/remux/transcode decision
internal/transcode/ ffmpeg pipeline: progressive fMP4 and HLS
internal/subtitle/ discovery, WebVTT conversion
internal/playlist/ .m3u parsing and import (pure parser + importer)
internal/api/      HTTP handlers
internal/web/      embedded client assets
docs/adr/          decision records (13 and counting — read before re-deciding)
```

## Rules

**Never leak `*sql.DB`.** `store` exposes typed methods. Handlers and the
scanner do not write SQL.

**All filename guessing lives in `internal/media`.** M2 metadata providers will
overwrite those fields; nothing else may develop its own opinion about what a
filename means.

**Every handler that turns a DB row into a filesystem path must re-verify
containment** within the owning library root after `filepath.Abs`. The database
is trusted, but this is the boundary where a bad row becomes arbitrary file
access.

**Scanning marks missing, never deletes.** An unmounted drive must not destroy
library data.

**Schema changes need a migration and an ADR** if they change the shape of the
data model rather than adding a nullable column.

**API changes are contract changes.** Update `docs/api.md` in the same commit.
That doc drifting from the handlers is the most damaging documentation failure
in this project, because it is what third-party clients build against.

**Locked fields are never overwritten.** From M2, editing a field locks it. No
provider refresh, rescan, or merge may touch a locked field, and a `locked`
match state is never re-scored. A rescan reconciles *files*; it does not
re-litigate *identity*. This has a permanent integration test — if it fails,
LANcast has become the thing it was built to replace.

**A playlist's membership is locked once a human edits it.** An `.m3u` on disk
seeds a playlist and is not the playlist (ADR 0030): the importer skips any
playlist carrying a `members` lock, so a rescan can never undo an edit. This is
the locked-fields rule applied to membership, and it has the same standing —
a rescan reconciles *files*, it does not re-litigate *decisions*.

**A playlist may hold the same track twice.** It is the only listing in the
system that can return a duplicate id, which is why `playlist_entry` is keyed on
position and why the queue cursor is a position rather than an id. Anything that
keys a playlist on item id silently shortens it.

**One normalizer.** Title normalization for matching reuses `clean` and
`SortTitle` in `internal/media/parse.go`. A second normalizer that disagrees
with the first is a bug factory.

**An unsecured server binds loopback only.** No password set means
`127.0.0.1` and nothing else — the API exposes filesystem browsing and library
creation at arbitrary paths, so an open port is arbitrary read access. Binding
wider is gated on a password being set, and that gate is not a configuration
convenience to be relaxed. Changing the password deletes *every* session
including the caller's own; that is the point of server-side sessions, not a
bug to smooth over. CSRF is defended twice — `SameSite=Strict` **and** an
`Origin`/`Referer` check on every state-changing method. Keep both.

**Process execution stays split from parsing.** `probe.ParseJSON` is pure, so
the direct-play/remux/transcode rules are tested against fixtures with no
ffmpeg installed and no media on disk. Any change that makes the decision
require a live process makes dozens of codec cases untestable in milliseconds.
The same split is why probing runs in its own worker rather than inside
enrichment.

**Progressive fMP4 is the default output, and hls.js is deliberately not
vendored.** The server produces HLS too, but browsers cannot play it without a
~300KB third-party library that this build will not ship unaudited — that is a
stated trade (ADR 0013), not an oversight. Do not make HLS the default client
path by pulling in a dependency.

## Design work

Read [docs/design.md](docs/design.md) before touching any UI. The one rule that
is easy to violate accidentally:

**Gold is earned, not ambient.** `--gold` at ~20% opacity at rest, full strength
on hover/focus/selection, and it means *where you are* and nothing else. Never
use gold to indicate favorite, new, 4K, or any other state — the moment gold
means two things, the focus signal is dead.

## Running it against real media

Test libraries live outside the repo, one folder holding `TEST MOVIE LIBRARY`,
`TEST SHOWS LIBRARY`, `TEST MUSIC LIBRARY`, `TEST PICTURE LIBRARY`. **Never point
a test instance at the live library** — a scan with `write_nfo` on writes
sidecars next to the media, and the database being a copy does not make the
media a copy.

`devseed` creates those libraries in a data directory and turns NFO writing off:

```bash
go build -tags devseed -o LANcast-Server.exe ./cmd/lancastd
./LANcast-Server.exe devseed -data <dev data dir> -root "<test libraries>" -scan
```

It is idempotent, and it is **behind a build tag so it is absent from release
binaries** rather than merely undocumented in them.

It deliberately does not create an account. A credential compiled into the
program is a credential that ships, and the loopback rule above does not survive
one. Create the account by hand, once, in a data directory kept between
sessions — everything else is repeatable.

## Before claiming done

```bash
go test ./...          # ~757 test funcs
go build ./...
npm --prefix web test  # ~38 client tests, vitest + jsdom
```

All three must pass. The client suite is newer than the rest of this file: it
exists because the picture-in-picture work needed to know whether a media
element survives being moved between documents, and it has since caught things
no Go test could — a settings shell whose panes were not wired to its buttons,
and a queue whose shuffle could not be told apart from a broken one by reading
it. `npm run build` in `web/` type-checks as a side effect. If a test fails, say so with the output — do not describe
partial work as finished.

Narrowing while iterating:

```bash
go test ./internal/probe/                        # one package
go test ./internal/probe/ -run TestRemuxWhenOnlyContainerIsWrong -v
```

The decision rules in `internal/probe/decide_test.go` are the model to copy:
one named test per codec/container combination, asserting the decision *and*
its reason. A test that starts needing a real encode is a design smell.

## Anything touching ffmpeg is verified **as the service**, not from a shell

LANcast ships as a Windows service (`lancastd`), and a service runs in
**session 0** — no desktop, no window station, and no Direct3D. A shell you
launch by hand does not. Anything that asks the GPU for something can therefore
pass every test you can think to run and still fail for every user.

It has happened once, and it cost a release. v0.8.0 added `-hwaccel auto`;
ffmpeg chose DXVA2, DXVA2 needs a D3D device, and as a service there is none:

```
[DXVA2] Failed to create Direct3D device
Device creation failed: -1313558101.
```

ffmpeg exited before writing a byte, so **every HEVC title span on a spinner for
ever** in the shipped build.

What makes it worth a section is that nothing was skipped. The unit tests
asserted the flag was present and it was. A benchmark measured a real 8.6-to-3.1
core saving. An HDR comparison confirmed identical colour at SSIM 0.9957. The
change was even driven in the running app. Every one of those launched ffmpeg
from an interactive session, where DXVA2 initialises fine — including the
in-app check, because the test server was started from a shell rather than
installed. The only environment never exercised was the one that ships.

So, for a change to `internal/transcode`, `internal/probe`, or anything else
that shells out to ffmpeg or ffprobe:

1. Build, then swap the binary into the installed service and restart it —
   backing up `C:\Program Files\LANcast\LANcast-Server.exe` first, so a
   rollback is one copy. This is the *only* check that means anything for GPU
   work; running `LANcast-Server.exe` from a terminal reproduces the wrong
   session every time.
2. Play a file that actually takes the path you changed, and read
   `lancastd.log` for `ffmpeg reported errors` afterwards. A stuck spinner is
   what this failure looks like from the front; the log is where it says why.

Two habits fall out of it, both cheap:

**Never let ffmpeg choose.** `auto` picked the one method that cannot work
here. Name the method, and derive it from something already proven in this
process — the encoder is chosen by a real encode at startup, which makes it
evidence about *this session* rather than a capability list. `Encoder.decodeAccel`
is that pattern.

**"Falls back to software" is a claim about a codec, not about a device.**
ffmpeg does fall back when a card cannot decode a *format*; it exits when the
device cannot be *created*. That distinction was written into a comment as
though it were behaviour, and it was wrong. If a comment asserts what ffmpeg
does in a failure case, it is worth having watched it do that.
