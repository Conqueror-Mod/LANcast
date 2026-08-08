# Desktop lifecycle — start, close, and what stays running

**Status: ready to build. The gate is met.** This document was written while
LANcast had no window of its own and said so: *close to tray* has no referent
while the UI is a browser tab. [ADR 0023](adr/0023-native-desktop-client.md)
stage 1 is now built — `LANcast-Client.exe -window` opens a window LANcast owns
— so the reason to wait is gone.

Revised 2026-08-08 with three changes: the gate is recorded as met, the
half-stop that made "closed" dishonest is now fixed, and the one question this
plan had not answered — *where a client-local setting is ticked, when the
Settings page is served by the server* — is answered below.

The deliverable is two tickable options:

- **Open on Windows start**
- **Close to tray**

They are small. The reason they need a plan is that "close" currently has three
different meanings in LANcast and none of them is wrong.

## The problem: closing what, exactly?

There are three things a person might mean by "I closed LANcast", and today they
do three different things:

| What was closed | What happens to the server | What the user sees |
|---|---|---|
| The **browser tab** | Nothing. It was never the app, only a view of it. | The app "closed" and the server is still there |
| **Tray → Quit** | The server stops, if this process owns it. | Correct, but only if they found the tray |
| The **service** (`lancastd`) | Nothing — neither of the above can touch it. It is delayed-auto-start, so it also comes back after a reboot on its own. | A server that "will not stay closed" |

Every row is behaving as designed. The confusion is that one word covers all
three, and nothing on screen distinguishes them.

This was observed directly: a server believed to be "hanging in the background
at close" turned out to be the installed service, started by the Service Control
Manager 2m17s after a boot, with no client involved at all. Diagnosing that took
a `Get-CimInstance` and an `sc.exe qc`. The single-instance guard now names the
holder — the service, its pid, and its data directory
(`internal/service/running.go`) — and that fix shipped first because it was
cheap and it is what makes the rest of this legible. But naming the state is not
the same as letting someone choose it.

## What gets built

### Two options, in the desktop client's own settings

**Open on Windows start** — the client launches at login.

**Close to tray** — the window's ✕ hides to the tray instead of quitting. Off by
default: a ✕ that does not close is a surprise, and the tray is where apps go to
be forgotten about. On, it is the Plex behaviour.

### They belong to the desktop client, not the server

This is the decision that is easy to get wrong, and it goes against the grain of
where every other setting in LANcast lives.

`GET/PUT /api/settings` is **machine-wide and shared** — TMDB keys, ffmpeg
directory, NFO writing. It is admin-only and it applies to the server everyone
on the LAN talks to. "Close to tray" is not that. It is a property of *one
person's desktop*, on one machine, and putting it in server settings would mean
one household member's window preference silently changing another's, and a
phone in the kitchen carrying a setting about a tray it does not have.

So: local client configuration, per user, per machine. Not the API, not
`config.Settings`, no schema change, no migration. The server does not learn
about this at all.

### The lifecycle rule underneath them

Both options are surface. The substance is one rule the desktop client must
follow, and it is the rule that makes the table at the top stop being confusing:

> **Closing a window never stops a server the window does not own.**

Concretely, on close the client asks one question — did *I* start this server?

- **The service owns it.** The window closes (or hides). The server keeps
  running, because a media server other people are streaming from does not stop
  because one person shut a window. Nothing is asked and nothing is stopped.
- **This client started it** (no service installed, the common single-machine
  case). The server stops with the window, unless *Close to tray* is on, in
  which case both stay and the tray is the visible remainder.

Today's tray Quit conflates these: it cancels the shutdown context regardless.
That is defensible while the tray *is* the app; it stops being defensible the
moment there is a window, because then quitting is something a person does
casually and often.

### Interaction with the service's own auto-start

Both auto-starts can be on at once, and this is fine — but only after stage 1,
which is part of why the gate exists.

The service starts at boot, machine-wide, as LocalSystem. The client starts at
login, per user. With both on, the client comes up, finds the service already
holding the single-instance name, and **attaches to it as a client** rather than
starting a second server. That is already the shape `trayRun` implements today
(`openExisting`), so the behaviour is not new — what is new is that it stops
looking like a failure, because there is a window to show for it.

Worth surfacing in the UI when it happens: "connected to the LANcast service"
beats a window that looks identical whether it owns a server or not. That is
another instance of the project's recurring rule — the state exists, it just has
no voice.

## The ruling this must satisfy

Stated during v0.6.0 testing, and it is the acceptance test for the whole
change:

> If the server is closed, it needs to fully close. The process must not hang.
> There should be no invisible hanging services after this implementation.

Two halves, and they are not the same work.

**A stop must complete.** This half is now **built** — `Stop-Service lancastd`
did not keep LANcast stopped: the server waited open-endedly on in-flight
connections, the service handler told the Service Control Manager the stop would
be instantaneous, and Windows judged the resulting delay a hang, logged event
7031, and let the recovery policy restart it. Shutdown is now bounded and forced
at the end, and the SCM is told how long to wait. A stop that is asked for now
happens.

**A close must mean something the user chose.** This half is what remains, and
it is the rest of this document. Note what the ruling does *not* say: it does
not say the server should always stop. It says there must be nothing running
that the user did not knowingly leave running. Those are different, and the
difference is the whole design — a service the user installed on purpose is not
an invisible hanging process, it is a media server doing its job. An invisible
hanging process is one nobody chose and nothing names.

So the rule at the top of this section stands, unchanged and now enforceable:

> **Closing a window never stops a server the window does not own** — and
> whatever keeps running says so, by name, where the user is looking.

## Where the tickboxes live

This plan asserted "client-local config, no API change" and left a hole: the
Settings page is HTML served by the *server*, and a client-local setting cannot
be read or written through `/api/settings` without becoming a server setting —
which is exactly what must not happen.

Resolved: **the window binds a host object, and the page feature-detects it.**

`internal/webview2` already exposes `Bind(name string, f interface{})`, and the
client already owns a per-user directory at `%APPDATA%\LANcast\client` where
the window's profile and session cookie live. Those two facts are the whole
mechanism:

- The client binds a small surface — read the lifecycle preferences, write them
  — backed by a JSON file in its own directory. Per user, per machine, never on
  the network, never in the server's database.
- The Settings page checks whether that binding exists. **In the LANcast window
  it renders a Desktop section; in a browser tab it renders nothing at all.**

Feature detection rather than a capability flag from the server, because the
server genuinely does not know: the same server serves the window, a browser on
the same machine, and a phone in the kitchen, and only one of those has a tray.
A setting that appears in a browser tab and silently governs a different process
is worse than a setting that is absent.

This keeps the original promise exactly — no API change, no schema change, the
server never learns about it — while giving the options somewhere real to be
ticked.

**The consequence to accept:** these options are invisible unless you are using
the native window. That is correct rather than a limitation. A browser tab has
no tray to reduce to and no ✕ that LANcast owns, so there is nothing for the
setting to govern, and offering it there would be a promise the client cannot
keep.

## What "no invisible hanging services" means concretely

Three checks, all of which must hold when this is done:

1. **Nothing survives a close that the user did not choose.** If this client
   started the server and *Close to tray* is off, closing the window stops the
   server, and the process is gone — verified by process list, not by the window
   disappearing.
2. **Anything that does survive is named.** If the service owns the server, the
   window says so before it closes rather than leaving a mystery: "the LANcast
   service keeps running" is the difference between a background service and a
   hanging one. The single-instance guard already names the holder
   (`internal/service/running.go`); this is the same sentence, moved to where
   the decision is made.
3. **Quitting from the tray stops what the tray owns, and only that.** Today's
   tray Quit cancels the shutdown context regardless of who started the server.
   That was defensible while the tray *was* the app; with a window it is not,
   because quitting becomes something people do casually.

## Why it waited (kept for the record)

The gate has been met; this section is why it existed, and it is worth keeping
because the reasoning was right.

Because *close to tray* had no referent while the UI was a browser tab.

Closing a browser tab is not closing LANcast, and no setting inside the page can
change what the browser's ✕ does. Shipping the tickbox today would mean either a
setting that governs the tray-icon process the user never interacts with
directly, or one that appears to promise something about a window LANcast does
not own. [ADR 0022](adr/0022-client-and-server-executables.md) chose the browser
deliberately and [ADR 0023](adr/0023-native-desktop-client.md) is the decision to
stop; the accumulated cost of *not owning the window* is exactly what ADR 0023
catalogues, and this is one more item on that list rather than a separate
problem.

So this waited for stage 1, and now it is small — a window that exists can be
hidden, and an app that owns its window can honestly describe what its close
button does.

**What does not wait:** naming what is already running, which shipped. If the
lifecycle stays confusing in the meantime, the next cheap step is the same kind
— making the tray tooltip and the Settings page say whether this server is the
service or this process, rather than leaving it to be inferred.

## Close to tray — the mechanism, investigated 2026-08-08

Steps 1-5 shipped without this one. The preference is stored and readable and
nothing consumes it, because "the web view and the tray both want the main
thread's message loop" had been taken as a blocker. Investigated properly, it is
not one, and the three facts that decide it are worth writing down before anyone
tries again.

**Windows message queues are per thread, not per process.** Two message loops
can coexist. `fyne.io/systray` calls `runtime.LockOSThread()` in its `init()`,
which pins the *main* goroutine — but a second goroutine that calls
`runtime.LockOSThread()` itself gets its own thread and its own queue, and
`systray.Run` creates its window inside `nativeLoop` on whatever thread it runs
on. So the tray can live on a locked secondary goroutine while the web view
keeps the main thread. The conflict is real only if both are run from the same
goroutine, which is what the current code does.

**The close button is interceptable, and currently is not intercepted.** The
vendored binding's window procedure already handles `WM_CLOSE`
(`internal/webview2/webview.go`), and destroys the window:

```go
case w32.WMClose:
    _, _, _ = w32.User32DestroyWindow.Call(hwnd)
```

Close-to-tray is exactly this branch calling `ShowWindow(SW_HIDE)` instead when
the preference is on. That needs a hook on the binding — an option the caller
sets, defaulting to today's behaviour — and a note in
`internal/webview2/PROVENANCE.md`, because that package is a trimmed vendored
copy and every local change to it has to stay visible.

**Restoring is `ShowWindow` plus `SetForegroundWindow`**, from the tray's Open
item, on the window's own thread.

### What this means for the ruling

A tray icon is what makes close-to-tray legitimate rather than an invisible
process: something stays on screen, and Quit stops what the tray owns. Without
it, the option would be "keep a server running with nothing to show for it",
which is the case the ruling forbids — which is why the toggle currently ships
disabled with that exact reason rather than hidden.

### Why it was not built in this pass

It is a Windows threading change plus a modification to a vendored binding, and
neither can be honestly verified by reading. The check is behavioural: a tray
icon appears beside a window, closing the window leaves both the icon and the
server, reopening from the tray restores the same window, and Quit stops the
server this client started and nothing else. That wants someone watching the
screen, and preferably an installed artifact rather than a terminal-launched
build — the same unit of verification the native-client plan already insists on.

Scoped for whoever picks it up:

1. `OnClose func() bool` on the webview options, defaulting to destroy. Record
   it in PROVENANCE.md.
2. Tray on a goroutine that locks its own OS thread; window stays on main.
3. `runWindow` consults the preference on close: hide when on, otherwise today's
   behaviour unchanged.
4. Tray Quit keeps the ownership rule — it stops the server this client started
   and never one it merely attached to.
5. Verify by watching it, then remove the "not yet available" reason from the
   Settings option in the same change. A toggle that works and still says it does
   not is worse than either.

## Build order

1. **Client-local preferences file** in `%APPDATA%\LANcast\client`, with the
   two options defaulting to off. Read on launch, written on change.
2. **Host binding** exposing read and write to the page, plus one honest fact
   the page cannot otherwise know: whether this client started the server or
   attached to something already running.
3. **Close behaviour** — the ownership rule, and the named survivor when the
   window closes over a server it does not own.
4. **Settings section**, rendered only when the binding is present.
5. **Open on Windows start** — the run key, and the uninstaller clearing it.

Steps 1–3 are the substance; 4 is surface and 5 is independent of the rest. If
step 3 turns out to need anything from the server, stop: the design has drifted.

## When it is built

- Both options are client-local config; **no API change and no schema change**.
  If either turns out to need the server, that is a signal the design drifted.
- **Off by default, both.** Auto-start and a ✕ that does not close are both
  things a user should opt into, and this project's stated posture is that
  surprising background behaviour is a bug even when it is convenient.
- **The uninstaller has to clear the run key.** An auto-start entry pointing at
  a deleted executable is a login-time error dialog forever, and it is the
  classic thing to forget.
- **Verify by installing the artifact and rebooting** — twice, once with the
  service installed and once without. Every bug in this area found so far
  (v0.4.1, v0.4.3, and the boot-time confusion above) was invisible from inside
  the repository and appeared only on a real desktop.
