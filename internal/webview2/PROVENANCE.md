# Vendored: go-webview2

This directory is a **trimmed copy** of a third-party package, not code written
here. It is vendored rather than imported, deliberately — see below.

## Source

| | |
|---|---|
| Upstream | `github.com/jchv/go-webview2` |
| Commit | `v0.0.0-20260205173254-56598839c808` |
| Licence | MIT — `LICENSE`, unmodified, © 2020 John Chadwick, portions © 2017 Serge Zaitsev |
| Vendored | 2026-08-08 |

Upstream has **no tagged release**. "Pin the version" therefore means pinning a
commit, which is one of the reasons this is a copy: a copy is pinned by being a
copy, and it is reviewable in the diff that adds it.

## Why vendored rather than imported

Decided in [docs/native-client-plan.md](../../docs/native-client-plan.md). The
package as published carries two things this project will not ship:

1. **Three prebuilt `WebView2Loader.dll` binaries**, `go:embed`-ed into the
   consuming executable — 137 KB of opaque binary compiled into
   `LANcast-Client.exe`. [ADR 0013](../../docs/adr/0013-transcode-pipeline.md)
   refused 300 KB of *readable JavaScript* on the grounds that this build will
   not ship unaudited third-party code. A binary blob is the harder version of
   that same call, not an easier one.
2. **`github.com/jchv/go-winloader`**, a from-memory PE loader used to map that
   embedded DLL when it is not found on disk. Loading a DLL from memory is a
   technique AV and EDR products flag. For a signed installer landing on
   someone else's desktop, that is a support problem waiting to happen — and
   exactly the class of failure this project only ever discovers on a machine
   that is not this one.

Both are gone, along with the dependency on `go-winloader`.

## What was changed

- `webviewloader/` → **`loader/`**, rewritten to load `WebView2Loader.dll`
  **from disk only**. Upstream already tried disk first and fell back to
  memory, so the removed path is the fallback, not the normal one. Nothing is
  lost at runtime.
- Added `ErrLoaderMissing`, so a missing DLL says what is missing and where it
  should be instead of producing a nil window and no explanation.
- `internal/w32` → `w32/`, `pkg/edge` → `edge/`, import paths rewritten to
  `lancast/internal/webview2/...`.
- `cmd/demo` and the embedded `sdk/` DLLs dropped.

No functional change to the COM plumbing in `edge/` — that is the part worth
having, and it is left alone so a future re-sync against upstream is a readable
diff.

## The DLL that replaces the embedded ones

Microsoft's own redistributable ships beside the executable, placed there by
the installer:

| | |
|---|---|
| File | `third_party/webview2/x64/WebView2Loader.dll` |
| Version | 1.0.992.28 |
| SHA-256 | `C4674ACF95F0800793A4A6D61132ADF5DFA694C218E482D86093494C4E84100A` |
| Signature | Valid — `CN=Microsoft Corporation, O=Microsoft Corporation, L=Redmond, S=Washington, C=US` |

Verified with `Get-AuthenticodeSignature` at vendoring time. It is a file on
disk next to the binary rather than bytes inside it: visible in the install,
checkable by hash, and signed by the party that wrote it.

## Re-syncing later

An upstream fix is a manual port, which is the cost of this choice. Diff
upstream at the new commit against the vendored commit above, apply what
matters to `edge/` and `w32/`, and leave `loader/` alone unless the upstream
loader itself changed — it is the file this copy deliberately disagrees with.

## Local addition — `WebViewOptions.OnClose` (2026-08-08)

A hook consulted in the window procedure's `WM_CLOSE` branch. Returning false
keeps the window alive; nil or true destroys it, which is the upstream
behaviour unchanged.

It exists because close-to-tray has to hide the window instead of ending the
process, and `WM_CLOSE` is handled inside this package — there is no hook from
outside. Recorded here because every local change to a vendored copy has to stay
visible; the alternative was forking the window procedure into the caller, which
is more code in a worse place.

## Local addition — `WindowOptions.At`, `X`, `Y` (2026-10-17)

Creates the window at a given position. Upstream shows the window inside
`CreateWithOptions`, centred on the primary display, before returning;
LANcast then moved it to the monitor it was on last time, and the window was
visible on the wrong screen for as long as embedding the browser took.
Reported as a small white window flashing at startup. Unset, the behaviour is
upstream's.

Creating the window *hidden* and showing it once placed was tried first and
left the page blank: the browser embedded into a hidden window never drew.
Creating it in the right place keeps upstream's order of show-then-embed.

## Local addition — the video overlay (2026-09-16)

`overlay.go`, the `overlay`, `shield` and `handingOff` fields, a branch in
`wndproc` for the click-catching shield over the docked picture, one early branch in
`wndproc` for the main window and one for the overlay, a `WM_DPICHANGED` case
that takes the rectangle Windows suggests (the client is per-monitor DPI aware), two
interface methods on `WebView` (`EnterVideoOverlay`, `LeaveVideoOverlay`), and
`SetBackground` / `Reparent` on `edge.Chromium`.

During native video playback (ADR 0067) the page moves into a transparent,
owned `WS_EX_NOREDIRECTIONBITMAP` popup above the main window, which libmpv
draws into. The ADR 0067 Phase 0 spike established that a transparent WebView2
in the *same* window never shows a GPU swapchain beneath it, so a second window
is the only shape that works. It keeps position, size, visibility and focus in
step with the main window from inside this package's window procedure, which is
the only place those messages arrive. Outside playback none of it runs.

## Local addition — certificate errors are refused (2026-11-15)

`edge/ICoreWebView2_14.go` (the `ICoreWebView2_14` vtable, the
`ServerCertificateErrorDetected` handler and its arguments), the
`serverCertificateError` field and `ServerCertificateErrorCallback` on
`edge.Chromium`, its registration in `CreateCoreWebView2ControllerCompleted`,
and `WebViewOptions.OnCertificateError`.

Upstream registers no handler, so a certificate error is Chromium's own
interstitial, and that offers a way through. In this app that meant a pin
mismatch could be clicked past, sending the session cookie to whichever key
answered (review finding R11-3). The handler cancels every certificate error
and then tells the host, which shows the server picker. The pinned key itself
never reaches it: `--ignore-certificate-errors-spki-list` exempts that key
before the event is raised.

`ICoreWebView2_14` extends `_2` to `_13`, and only their slot counts are
written out (as an anonymous array); `edge/vtable14_test.go` pins the offset.
A runtime older than 1.0.1245 has no `_14`; that is logged and the old
behaviour stays.

## Local addition — a window keeps to its own origins (2026-11-15)

`edge/ICoreWebView2NavigationStarting.go` (the handler and its arguments), the
`navigationStarting` field and `NavigationStartingCallback` on
`edge.Chromium` with its registration, `MessageCallback` now taking the
message's source document as well as the message, and
`WebViewOptions.AllowMessage` / `AllowNavigation` with the check at the top of
`msgcb`.

Upstream injects every binding into every document and never asks which one
is calling, and it lets a page navigate the main frame anywhere. Once the
window could show a remote server's page (ADR 0070), any script there, or any
site it navigated to, could call every native function (review finding
R11-2). The policy itself lives in `internal/clientwindow/origins.go`; this
package only provides the two hooks. `put_Cancel` takes a BOOL by value. A
generated binding elsewhere passes a pointer, which is wrong.

## Local change — a panicking binding rejects its promise (2026-11-15)

`callbinding` recovers a panic in a bound Go function, logs it with its stack,
and returns an error, so the page's promise is rejected. Upstream lets the
panic through. Bindings run on the window's UI thread, so a panic in any one of
them ended the whole desktop app. A malformed game icon did that every time the
Games screen drew the game (review R12-3). The results of `v.Call` were renamed
from `res` to `out`, because the function now has named results.
