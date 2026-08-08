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

**The alternative worth pricing before committing:** binding WebView2 directly.
The spike proves the COM interfaces are reachable from pure Go with no CGO; the
dependency is convenience, not capability. That is more code owned here and less
code trusted from elsewhere, and this project has historically chosen the former.
It is a real option, not a straw man.

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

1. **Amend ADR 0023** with the CGO finding, and decide the dependency question
   (vendor-a-binding vs bind-it-here) explicitly. Nothing else starts until this
   is settled, because it determines what the window is built on.
2. **A `clientwindow` package** behind a build tag, Windows-only, with a
   no-op/error implementation elsewhere — the same shape `tray_windows.go` and
   `tray_other.go` already use. One function: open a window at a URL, block
   until closed.
3. **Wire it into the launcher behind a flag**, defaulting to the browser.
   `lancast -window` opts in. This is what makes the whole thing revertible and
   lets both paths be compared on the same machine on the same day.
4. **Flip the default** once it has survived real use, keeping `-browser` as the
   escape hatch. A separate commit, so flipping back is one revert.
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

## What tonight's spike did not prove

Worth being precise, because the failure modes in this area have all been
environmental:

- It ran **from a terminal, as the developer, with the WebView2 runtime already
  present** on Windows 11. It has not run from an installed artifact, under a
  fresh user profile, or on a machine without the evergreen runtime.
- It has not been asked to **play anything**. A webview that renders a grid and
  chokes on a video element would invalidate the whole approach, and that is the
  single highest-value next experiment.
- Nothing was checked about **focus behaviour** — the spatial focus model
  ([ADR 0004](adr/0004-keyboard-focus-model.md)) assumes browser keyboard
  semantics, and a webview host can intercept keys differently.
- No **DPI / multi-monitor** behaviour was observed.

The lesson this project keeps relearning is that the unit of verification is the
installed artifact on a real desktop. A window opening in a terminal-launched
spike is evidence the approach is viable, not that it works.

## The trap to keep in view

ADR 0023 names it and it is worth repeating where the work happens: **stage 1
makes the certificate warning invisible without solving it.** Every phone,
tablet and TV browser on the LAN still meets the self-signed certificate, and
once the desktop stops showing it, the author stops seeing the problem that
everyone else still has.

The same applies more broadly. The browser client must stay a first-class,
exercised surface — the reason this project is called LANcast is that a phone in
another room can watch something.
