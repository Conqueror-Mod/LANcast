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
