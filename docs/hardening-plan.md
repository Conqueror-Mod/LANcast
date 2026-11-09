# Code hardening before 1.0 — for discussion

Written 2026-11-09 for the owner's note "discuss code hardening" (v1-plan §9),
starting from v1-plan §3E. This is a proposal, not a change: every item says
what was found in the code, what fixing it costs, and when it should land.
Nothing here has been built.

The question each item answers is the one 1.0 raises: **strangers will run
this on machines that face the internet.** A household server owned by its
developer has been the threat model until now.

## What to decide

| # | Item | Found | Cost | Proposed |
|---|---|---|---|---|
| 1 | Request bodies have no size limit, before sign-in too | 64 handlers | about a day | **1.0** |
| 2 | No dependency vulnerability check in CI | none | an hour | **1.0** |
| 3 | Fuzz the parsers that read files people bring | 2 of ~8 | half a day each | **1.0**, the four named |
| 4 | ffmpeg runs as LocalSystem | every child | 3–5 days + service checks | **1.1**, said plainly in 1.0 |
| 5 | The ffmpeg pin moves only with a release | 2026-08-18 build | an hour to check | **1.0**: check, and name it in the notes |
| 6 | security.md read against the code | — | a day | **1.0** (already §3E) |
| 7 | No general API rate limit | known | — | after 1.0 |
| 8 | CI actions pinned by tag, not commit | 4 actions | an hour | after 1.0 |

## 1. Request bodies have no size limit

**Found.** 64 handlers decode JSON with `json.NewDecoder(r.Body)` and no
`http.MaxBytesReader`; only the newer ones (invites, guest sessions, API keys,
library sharing) cap their bodies. There is no limit around the whole handler
either, and the server sets `ReadHeaderTimeout` but no read timeout for the
body.

Most of those 64 need an account, which bounds the harm to people already let
in. Four do not:

- `POST /api/auth/login` (`auth.go`, `authLogin`)
- `POST /api/auth/login/two-factor` (`twofactor.go`, `authLoginTwoFactor`)
- `POST /api/auth/passkey/login` (`passkeys.go`, `passkeyLogin`)
- `POST /api/auth/setup` (`authSetup`; open only before the first account)

Anyone who can reach the port can send one of these a body of any size, slowly
or quickly, and the decoder reads until it has a JSON value. One long string is
enough to make the server hold it all in memory. The sign-in rate limit does not
help, because it counts attempts *after* the body is read.

**Proposed fix.** One wrapper in `api.Handler` that caps every request body at
1 MiB, and a short list of routes that legitimately take more, each with its
own cap: collection posters, the custom certificate, plugin bundles, and
anything else that uploads a file. A test walks the router and fails if a route
takes a body without being under the cap or on the list, so a new handler
cannot quietly skip it. That is the same shape as the API contract tests, and
it is what keeps this from being a one-time sweep.

A read deadline for non-streaming routes belongs with it. A film is a long
*response*, never a long *request*, so a body that has not arrived in 30
seconds is not a slow phone.

## 2. No dependency vulnerability check

**Found.** CI builds, vets and tests, but nothing checks `go.mod` or
`web/package-lock.json` against known advisories.

**Proposed fix.** A `govulncheck ./...` step in `ci.yml`. It reports only
vulnerabilities in code that is actually called, so it is quiet unless
something matters. For the client, `npm audit --omit=dev --audit-level=high`,
since only what ships to the browser is exposure. Both fail the build.

## 3. Fuzz the parsers of files people bring

**Found.** Two packages have fuzz targets, `comic/pdf` and `picture`, and both
earned them. Other parsers read files LANcast did not write and are not fuzzed:

- **Subtitles** (`internal/subtitle`): SRT and ASS to WebVTT, from any file
  next to a film, and from a friend's server.
- **Playlists** (`internal/playlist`): `.m3u` from library folders.
- **NFO** (`internal/meta/nfo`): XML sidecars, often written by other tools.
- **MilkDrop presets** (`internal/milkdrop`, its EEL parser): preset files
  people download in packs.

These are the four proposed for 1.0. Each is pure Go, so a bad file cannot
run code, but it can still do damage. A panic inside a request is recovered
by the HTTP server. A panic in a scan or worker goroutine is not, and it takes
the whole server down. A parser that hangs or allocates without bound stalls
a scan.
Comic archives (`.cbz`, `.cb7`) deserve a look for decompression bombs while
in there.

Each target runs for a short fixed time in CI, with its corpus checked in.

## 4. ffmpeg runs as LocalSystem

**Found.** The Windows service is created with no account named
(`internal/service/manager_windows.go`), so it runs as LocalSystem, and so does
every ffmpeg and ffprobe it starts. security.md already lists malicious media
as accepted and out of scope, and that was a fair call for one household. It is
weaker for 1.0: friends' servers send media to each other, and a parser bug in
ffmpeg would then run as SYSTEM on the receiving machine.

**Options, cheapest first.**

- **Restricted token for the children.** Start ffmpeg with
  `CreateRestrictedToken`, dropping the Administrators group and most
  privileges. The service keeps its account and the files it needs, and ffmpeg
  loses the power to do much with a compromise. The work is in the details:
  the transcode output folder must stay writable, and hardware encoding must
  still work in session 0. CLAUDE.md says why that last one is checked **as
  the service**, never from a shell. About 3–5 days, most of it verification.
- **A virtual service account** (`NT SERVICE\lancastd`) for the whole server.
  Cleaner, but it touches everything: library folders on other drives and
  network shares need read access granted, the data folder needs moving or
  re-permissioning, and the updater replaces files under Program Files. Weeks,
  with real risk of breaking existing installs on upgrade.

**Proposed.** Decision 5 says cost it, then decide, and the cost is above
"a few days". So, as recorded: the restricted token in 1.1, and for 1.0
security.md says plainly that ffmpeg runs with the service's full rights, and
why that is accepted for now. A job object around each child, with a memory
ceiling and kill-on-close, is cheap and could land in 1.0 if wanted. It limits
a runaway, not a compromise.

## 5. The ffmpeg pin

**Found.** `internal/mediatools/install.go` pins one BtbN static GPL build:
ffmpeg 8.1.2, built 2026-08-18, by URL, SHA-256 and size. The pin is good
practice: nothing unverified is ever run. It also means ffmpeg is updated only
when a LANcast release moves the pin. Linux uses the system's package manager
and is patched by it.

**Still to check before 1.0.** Does moving the pin replace an ffmpeg that
LANcast already installed, or only reach new installs? If only new ones, a
security fix in ffmpeg never reaches an existing server. The 1.0 notes should
name the version shipped either way.

## 6. security.md against the code

Already v1-plan §3E. The parts written before these features existed, which
need a paragraph each: guest tickets, invite links, play on another device,
webhooks (an admin-set URL the server calls: is it allowed to reach the LAN?),
the metrics key, and the built-in provider keys. "Still unprotected" now has
the body limit (item 1) to add or, better, to remove once it is fixed.

## 7 and 8. After 1.0

- **API rate limiting** beyond sign-in. Listed in security.md as still
  unprotected. With the body cap in place, what remains is an account holder
  being expensive on purpose, which is a household problem.
- **CI actions pinned by commit**, not by tag (`actions/checkout@v4` and
  others). A moved tag runs someone else's code with the repository's release
  secrets. Worth doing, after 1.0.

## Not proposed

- **Plugins.** The wazero sandbox is deny-by-default with per-host rate limits.
  Nothing found that needs to change for 1.0.
- **Downloaded native code.** libretro cores, the face model and ffmpeg are
  all pinned by SHA-256, and the cores run in the desktop app as the user, not
  in the service.
