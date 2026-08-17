# ADR 0023 — A native desktop client

Date: 2026-08-02 · Status: accepted · shipped by v0.3.2 · **amends [ADR 0022](0022-client-and-server-executables.md)**

## Context

[ADR 0022](0022-client-and-server-executables.md) chose a browser UI deliberately
and said why: reusing the shipped web client with zero rewrite is what kept the
desktop story cheap, and kept [ADR 0001](0001-go-and-pure-go-sqlite.md)'s pure-Go,
no-CGO posture intact. It then named the condition for revisiting:

> **A native window becomes worth the CGO cost** — e.g. offline/kiosk use where a
> browser dependency is unwanted. The two-exe split makes swapping the launcher
> for a native shell an isolated change.

That condition has arrived, though not for the reason predicted. It is not kiosk
use. It is the accumulated operational cost of not owning the window, which the
v0.4.x releases made concrete:

- **A cached permanent redirect made the app unreachable.** The server answered
  plaintext with a 301 to HTTPS; browsers cache that forever; a later
  loopback-only server then spoke plaintext to a browser that would only speak
  TLS, and showed `ERR_SSL_PROTOCOL_ERROR`. The server could not fix it — the
  state lived in a browser profile. The user's route back was a private window
  (fixed in v0.4.1, but only for redirects not yet cached).
- **Certificate warnings are structural.** A LAN-bound server serves a
  self-signed certificate ([ADR 0014](0014-transport-security.md)) and every
  first visit warns. That is honest, and it is also a permanent papercut on a
  machine that trusts its own server.
- **The launcher/browser handoff has no good failure mode.** A first launch that
  started the server but opened nothing looked like the app was broken
  (fixed in v0.4.1); a launch while a service was already running looked
  identical to a launch that did nothing.
- **Two processes racing over one port and one name.** The launcher-plus-service
  arrangement produced a cross-session single-instance bug that silently allowed
  two servers, and before v0.4.1 two *databases*
  (fixed in v0.4.3). Each fix was correct; the shape that produced them is
  inherent to a shell that hands off to a program it does not control.

And one that is not a bug at all, but a permanent tax:

- **The browser owns codec support, not LANcast.** The direct-play/remux/transcode
  engine, the named client profiles, the MP4 muxability rules and the 10-bit
  detection all exist because `<video>` decodes a narrow, browser-specific set. A
  desktop player that decodes what ffmpeg decodes would direct-play nearly
  everything, and the decision engine would become a path for *other* clients
  rather than the primary one.

Two facts about this project make the trade different from the general case.
**LANcast is operated by its author** for close friends and family; there is no
user base to migrate and no third-party client to break. And the **HTTP contract
is versioned and public** ([ADR 0018](0018-api-contract-and-versioning.md)), so
any client — native, browser, or something later — remains possible against the
same server.

## Decision

**A native desktop client, delivered in two stages.** The browser UI is not
removed; it stops being the desktop path.

### The server does not change

`lancastd` stays pure Go, no CGO, one static binary per platform. [ADR 0001](0001-go-and-pure-go-sqlite.md)
is untouched, and `CGO_ENABLED=0` still builds it. This is the load-bearing part
of the decision: ADR 0022 already split the client into its own executable, so
CGO enters *that* binary and nothing else. The thing ADR 0001 protects — a server
that drops onto a NAS or a Pi as one file with no runtime — is unaffected.

### Stage 1 — own the window

A native client window hosting the existing web UI through the platform web
view (WebView2 on Windows). The React client, the design system, the keyboard
focus model ([ADR 0004](0004-keyboard-focus-model.md)) are reused unchanged.

This alone removes the whole class of problems above: no default-browser
handoff, no cached redirect, no certificate prompt (the client talks to its own
server and can trust its certificate deliberately), no ambiguity about whether
clicking the icon did anything.

### Stage 2 — own playback

Replace `<video>` in the desktop client with a real media player (libmpv), while
browse, detail and settings stay the web UI.

This is where the codec tax goes away. A desktop client that decodes what ffmpeg
decodes direct-plays HEVC, 10-bit, AC-3, DTS, TrueHD, MKV — the cases that today
force a transcode or a remux. Transcoding remains for clients that need it, which
is what it was always for.

> **Amendment, 2026-08-08 — the codec tax is smaller than this paragraph says.**
> HEVC is the largest item on that list, and it turned out not to need a native
> player at all. Chromium on Windows decodes it in hardware; the server was
> re-encoding it only because the conservative `browser` profile could not know
> that and no client had ever said otherwise. Clients now report what they can
> decode (`?can=`, [plan](../client-capabilities-plan.md)), and an HEVC file
> that used to be re-encoded is direct-played.
>
> What remains for stage 2 is real but narrower: **AC-3 and E-AC-3** (measured
> `no` in the same browser), **DTS**, **TrueHD**, and **MKV** as a container.
> Those are worth having and they are not the sentence above.
>
> **Stage 1 is unaffected.** Owning the window was never mostly about codecs —
> the cached redirect, the certificate wall, and the handoff with no good
> failure mode all stand, and the certificate wall turned out to be worse than
> this ADR assumed rather than better.

Staged rather than at once because stage 1 delivers the daily relief and
validates the direction, and stage 2 is where the weight is. Stage 1 is worth
shipping even if stage 2 never happens.

### The browser stays

A phone, a tablet, a TV browser and another laptop on the LAN keep working
exactly as they do now, against the same server and the same API. That reach is
the product; the native client is an additional, better desktop surface, not a
replacement for it.

## Consequences

**Good — the operational papercuts stop.** Cached redirects, certificate
warnings, and browser-handoff ambiguity are all consequences of rendering in a
program LANcast does not control. Owning the window ends them rather than
patching each one as it appears.

**Good — the design system survives.** Stage 1 is a hosting change, not a
rewrite. Everything in [design.md](../design.md), the gold rule, and the focus
model keep working because it is the same client.

**Good — playback gets better at stage 2, and the server gets simpler.** Fewer
transcodes means less CPU, fewer decision paths exercised in anger, and one
fewer place for a wrong answer to produce silence or a black frame.

**Cost — CGO in the client, and per-platform client builds.** The client can no
longer be cross-compiled from one machine the way `lancastd` is. The release
pipeline currently builds both executables for every target from one runner
([ADR 0016](0016-packaging-and-distribution.md)); that stops being true for the
client and the pipeline has to grow per-OS runners or drop non-Windows client
builds. **The server keeps cross-compiling normally.**

> **Amendment, 2026-08-07 — this cost does not apply to stage 1 on Windows.**
> It was asserted from reasoning and turned out to be wrong when tested. A
> pure-Go WebView2 binding (`github.com/jchv/go-webview2`, driving the COM
> interfaces through `syscall`) builds with `CGO_ENABLED=0` and opens a real
> window against a running server. So stage 1 keeps
> [ADR 0001](0001-go-and-pure-go-sqlite.md)'s no-CGO posture *in the client*,
> and the existing single-runner release matrix keeps working.
>
> The cost that replaces it is a **third-party dependency in a shipped binary**,
> which is the same argument this ADR already has with
> [ADR 0013](0013-transcode-pipeline.md) below — and the binding is untagged, so
> pinning it means pinning a commit. Binding WebView2 directly here is a real
> alternative, since the spike proves the capability is reachable without the
> dependency. That choice is open and is step 1 of the
> [stage 1 plan](../native-client-plan.md).
>
> **Stage 2 is unaffected.** libmpv is still CGO, and the per-platform build cost
> above still lands the moment stage 2 starts.

**Cost — a runtime dependency on Windows.** WebView2 is present on Windows 11
and on updated Windows 10, and needs the evergreen bootstrapper otherwise. That
is a real install-time consideration for a project whose current answer is "one
executable, no runtime".

**Cost — this contradicts the letter of [ADR 0013](0013-transcode-pipeline.md).**
That ADR refused to vendor ~300 KB of hls.js because the build "will not ship
unaudited third-party code", and accepted a worse default (progressive fMP4) to
hold the line. Shipping libmpv means shipping orders of magnitude more
third-party code than the thing that was refused. Either the principle is really
about the *browser bundle* specifically — code executed in the page, in every
client, on every device — or it is about audit surface generally, in which case
stage 2 cannot be justified while the hls.js refusal stands. **This ADR asserts
the former, and ADR 0013 should be amended to say so explicitly rather than left
to read as a contradiction.**

**Cost — licensing needs settling before stage 2.** mpv and its ffmpeg
dependency are LGPL or GPL depending on how they are built and which components
are enabled. LANcast's own licence and distribution have to be checked against
whichever configuration ships. This is not a blocker, it is a decision that must
be made deliberately and not discovered after the fact.

**Cost — two playback paths.** The native client and the browser client will
behave differently, and a bug reproduced in one may not appear in the other.
Given the whole of v0.4.x was spent finding bugs that only appeared on a real
installed desktop, this deserves respect: it doubles the surface where "works on
mine" can hide something.

## Alternatives considered

**Keep the browser UI and keep patching.** Every individual problem so far had a
correct fix, and they landed. But they arrive steadily, the state that causes
them lives somewhere LANcast cannot reach, and the codec tax never goes away.
Rejected because the fixes treat symptoms of not owning the window.

**Stage 1 only — a webview shell, forever.** A legitimate stopping point, and if
stage 2 is never funded this is where it lands. Not chosen as the *decision*
because it leaves the codec tax in place, which is the larger of the two costs.

**A fully native UI toolkit (Fyne, Gio) instead of a webview.** Would remove the
web engine dependency and could stay closer to pure Go. Rejected: it discards
the React client, the design system implementation, and the focus model, and
rebuilds them in a toolkit with weaker layout and text rendering. The cost is
enormous and the gain is a dependency argument.

**Electron or Tauri.** Electron ships a whole browser — the dependency objection
at its maximum. Tauri is a good fit technically but introduces Rust to a Go
project for the shell alone.

## The thing that is easy to get wrong

**Letting the native client become the only client.** The reason this project is
called LANcast is that a phone in another room can watch something. A desktop
client that quietly becomes the only tested path — where the browser UI rots
because the author stopped using it — gives up the actual product for developer
comfort. The browser client must stay a first-class, exercised surface, and the
API contract is what keeps that honest.

The second trap is subtler: **stage 1 makes the certificate problem invisible
without solving it.** A native client can trust its own server's certificate
deliberately, which is correct — but every other device on the LAN still meets
the self-signed warning, and the author will stop seeing it. Do not let a fixed
desktop experience hide an unfixed one everywhere else.

## Revisit if

- **Stage 1 does not actually remove the friction.** If a webview brings its own
  state problems, stop before stage 2 and reconsider — the whole case rests on
  owning the window being materially better than borrowing one.
- **The client build cost proves worse than estimated.** Per-platform builds for
  the client are the largest structural cost here; if they turn the release into
  a chore, a browser-only fallback for non-Windows is an acceptable retreat.
- **Licensing for libmpv lands somewhere incompatible**, in which case stage 2
  becomes "native shell, browser playback" and the codec tax stays.
- **A second person ever runs LANcast on a desktop.** Nothing here is wrong in
  that case, but the "sole operator" argument that makes the trade easy stops
  applying, and the browser client's status as a fallback matters more.
