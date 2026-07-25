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

## Before claiming done

```bash
go test ./...          # ~296 test funcs
go build ./...
```

Both must pass. If a test fails, say so with the output — do not describe
partial work as finished.

Narrowing while iterating:

```bash
go test ./internal/probe/                        # one package
go test ./internal/probe/ -run TestRemuxWhenOnlyContainerIsWrong -v
```

The decision rules in `internal/probe/decide_test.go` are the model to copy:
one named test per codec/container combination, asserting the decision *and*
its reason. A test that starts needing a real encode is a design smell.
