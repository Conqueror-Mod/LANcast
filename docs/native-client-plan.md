# Native desktop client — stage 1 build plan

[ADR 0023](adr/0023-native-desktop-client.md) decided a native client in two
stages. This plans **stage 1 only**: own the window. Stage 2 (own playback,
libmpv) is not planned here and should not be started until stage 1 has been
lived with.

## What changed since the ADR was written

ADR 0023 was written assuming a webview means CGO, and listed two costs on that
basis: *"CGO in the client, and per-platform client builds"*, plus the note that
`lancastd` cross-compiles but the client would stop doing so.

**That assumption is wrong for Windows, and it was tested rather than argued.**
A spike using `github.com/jchv/go-webview2` — a pure-Go binding that drives the
WebView2 COM interfaces through `syscall` — builds with **`CGO_ENABLED=0`** and
opens a real window:

```
$ CGO_ENABLED=0 go build -o wvspike.exe .
$ ./wvspike.exe
Id              : 27364
ProcessName     : wvspike
MainWindowTitle : LANcast
```

It navigated to the running dev server. Total dependency footprint is three
modules: `go-webview2`, `go-winloader`, and `golang.org/x/sys`.

So for stage 1 on Windows:

- **[ADR 0001](adr/0001-go-and-pure-go-sqlite.md)'s no-CGO posture survives** —
  in the client as well as the server.
- **The release matrix does not need per-OS runners.** The existing goreleaser
  setup ([ADR 0016](adr/0016-packaging-and-distribution.md)) keeps working.
- Two of the ADR's stated costs shrink to approximately zero.

ADR 0023 should be amended to record this, because those costs are part of why
the decision reads as expensive, and it is now cheaper than it was written to be.

### The cost that replaced them

A new third-party dependency in a shipped binary, and this project has a stated
position on that. [ADR 0013](adr/0013-transcode-pipeline.md) refused to vendor
~300 KB of hls.js on the grounds that the build "will not ship unaudited
third-party code", and accepted a worse default to hold the line. ADR 0023
already argued that the principle is really about *the browser bundle* — code
executed in the page, on every client, on every device — and not audit surface
generally.

That argument has to actually be made and written down, because `go-webview2`
is:

- **untagged** — no semver release, only a pseudo-version
  (`v0.0.0-20260205173254-...`), so "pin the version" means pinning a commit
- small and readable, but **effectively single-maintainer**

Pinning a commit in `go.mod` with a `go.sum` hash is the mitigation, and it is
the same mitigation the plugin trust model already relies on
([ADR 0021](adr/0021-plugin-distribution-and-trust.md)). It should be a
deliberate, recorded choice — not something that arrives with a `go get`.

And it is more than source. Inspecting what the spike actually pulled in:

| | |
|---|---|
| `go-webview2` | 3,563 lines of Go across 40 files |
| `go-winloader` | 2,086 lines — a **from-memory PE loader** |
| Embedded binary | Microsoft's prebuilt `WebView2Loader.dll`, 137 KB, `//go:embed`-ed per architecture |
| Load order | `WebView2Loader.dll` **from disk first**, falling back to the embedded copy **loaded from memory** |

Two consequences, both specific to this project:

- **A prebuilt DLL compiled into the shipped client is the hard version of the
  ADR 0013 argument**, not the easy one. That ADR refused *readable JavaScript
  source* on audit grounds. An opaque binary blob is a bigger ask, and the
  "it was about the browser bundle" reading has to be asserted knowingly rather
  than assumed.
- **Loading a DLL from memory is a technique AV and EDR products flag.** For a
  signed installer that lands on someone else's desktop, that is an operational
  risk of exactly the shape this project keeps getting caught by — invisible
  here, visible only there.

### Decision: vendor a trimmed copy

**Chosen 2026-08-07.** The COM plumbing is copied in-tree; the embedded DLL and
the from-memory loader are deleted; Microsoft's own signed `WebView2Loader.dll`
ships beside the executable and the installer places it there.

The disk-first load order is what makes this work — with the loader on disk the
memory path never runs, so removing it costs nothing at runtime.

What this buys: the tested COM code, no binary blob inside the LANcast binary,
no memory-loading technique in a distributed installer, and everything in-tree
and reviewable at the commit that adds it. What it costs: ~2–3k lines that were
written elsewhere are now maintained here, and an upstream fix is a manual port
rather than a version bump. That trade matches how this codebase already treats
Windows internals — the singleton mutex DACL and the low-privilege SCM query are
both hand-held rather than delegated.

Rejected: **taking the dependency as-is** (fastest, but ships the blob and
contradicts ADR 0013 at its strongest point) and **binding WebView2 from
scratch** (cleanest, but weeks of COM lifetime management for a capability the
spike already proved reachable).

**This decision does not survive a failed playback test.** If a webview cannot
play what the browser plays, none of the above matters — see below.

## Scope of stage 1

**Replace one call.** `cmd/lancast/main.go` is 131 lines that already do
everything except own the window:

```go
if err := desktop.OpenBrowser(desktop.ResolvedURL(l.addr)); err != nil {
```

Singleton acquisition, finding-or-starting the server, the tray, and the alert
box all stay exactly as they are. The window replaces the browser handoff and
nothing else.

### Steps, in order

0. ~~Play something in the spike.~~ **Done, and it passes** — see below.
1. ~~Amend ADR 0023, and decide the dependency question.~~ **Done** — the
   amendment is in the ADR, and the decision is to vendor a trimmed copy
   (above).
2. ~~A `clientwindow` package behind a build tag.~~ **Done** —
   `internal/clientwindow`, Windows-only with an unsupported stub elsewhere.
   `Open` shows a window and blocks; `Check` says why it cannot, which is a
   separate question from whether it can (see below).
3. ~~Wire it into the launcher behind a flag.~~ **Done** — `lancast -window`,
   with the browser still the default.
4. **Flip the default** once it has survived real use, keeping a `-browser`
   escape hatch. A separate commit, so flipping back is one revert. **Not yet:**
   the window has run for an evening on one machine, which is not "survived
   real use", and the list of unproven environments below has not shortened.
5. **Certificate trust**, which is the point of the exercise: the client talks
   to its own server and can trust that certificate deliberately.
   [ADR 0014](adr/0014-transport-security.md) is unchanged for every other
   device — see the trap below.
6. **Installer and Start-menu entries** get revisited only after the default
   flips.

### What stage 1 does *not* include

- **Playback changes.** `<video>` in a webview is the same `<video>`. The codec
  tax is stage 2 and nothing here reduces it.
- **The desktop lifecycle options** — *Open on Windows start*, *Close to tray*
  ([plan](desktop-lifecycle-plan.md)). Those were gated on this work, and they
  unblock the moment step 4 lands, but they are their own change.
- **The music mini-player**, which is gated on nothing here and can proceed in
  parallel ([plan](music-client-plan.md)).
- **macOS and Linux.** Windows is where the friction is and where the pure-Go
  path exists. A browser fallback elsewhere is the acceptable retreat ADR 0023
  already named.

## What the spike proved

Step 0 ran against the dev server with a real library. **The gate passes.**

- **`<video>` plays.** A film started, and six seconds later the frame had moved
  on — the studio card to a scene, not a stuck first frame. The transport's
  auto-hide fired, which only happens while the element is not paused, so two
  independent signals agree.
- **Keyboard reaches the page.** Space paused it and the chrome came back at
  `0:37 / 1:26:42`. This was the quiet risk: a host that swallows keys would take
  the spatial focus model ([ADR 0004](adr/0004-keyboard-focus-model.md)) with it.
- **The design system renders**, gold focus ring included, and the browse grid,
  detail page and album view are indistinguishable from the browser.
- **Progress writes persist.** The film reappeared under Continue Watching with
  a bar, and the session survived a server restart.

That is the whole of the step-0 question answered: the approach is viable and
the vendoring work is worth doing.

**Audio was not tested.** It goes through the same element, so the expectation
is that it works, but the music player is new and expectation is not evidence.

## What the spike still did not prove

Worth being precise, because the failure modes in this area have all been
environmental:

- It ran **from a terminal, as the developer, with the WebView2 runtime already
  present** on Windows 11. It has not run from an installed artifact, under a
  fresh user profile, or on a machine without the evergreen runtime.
- It talked to a **plain-HTTP loopback server**, so the certificate question —
  one of the reasons for doing this at all — was never exercised.
- No **DPI / multi-monitor** behaviour was observed.
- Nothing was played that **transcodes**. Direct play through a webview is the
  easy case; the fragmented-MP4 path is the one with a history.

The lesson this project keeps relearning is that the unit of verification is the
installed artifact on a real desktop. A window driven from a terminal-launched
spike is evidence the approach is viable, not that it ships.

### It also found a bug the browser had been hiding

The artist page displayed `TEST MUSIC LIBRARY::artist=ABBA` where a filename
belongs, and a bare `DC` for AC/DC. A container's synthetic key was being run
through `filepath.Base` and rendered as a file name — on every artist, album,
and folderless season, in the browser client too, for as long as those rows have
existed. Fixed separately.

Nobody had noticed it in the browser. Something about looking at the same client
in an unfamiliar frame made it obvious, which is worth remembering the next time
a surface is described as "just a hosting change".

## The trap to keep in view

ADR 0023 names it and it is worth repeating where the work happens: **stage 1
makes the certificate warning invisible without solving it.** Every phone,
tablet and TV browser on the LAN still meets the self-signed certificate, and
once the desktop stops showing it, the author stops seeing the problem that
everyone else still has.

The same applies more broadly. The browser client must stay a first-class,
exercised surface — the reason this project is called LANcast is that a phone in
another room can watch something.
