# Desktop lifecycle — start, close, and what stays running

**Status: planned, deliberately not built yet.** Gated on
[ADR 0023](adr/0023-native-desktop-client.md) stage 1 — a native window. The
reasoning for the gate is in "Why not now" at the end, and it is the most
important part of this document.

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

## Why not now

Because *close to tray* has no referent while the UI is a browser tab.

Closing a browser tab is not closing LANcast, and no setting inside the page can
change what the browser's ✕ does. Shipping the tickbox today would mean either a
setting that governs the tray-icon process the user never interacts with
directly, or one that appears to promise something about a window LANcast does
not own. [ADR 0022](adr/0022-client-and-server-executables.md) chose the browser
deliberately and [ADR 0023](adr/0023-native-desktop-client.md) is the decision to
stop; the accumulated cost of *not owning the window* is exactly what ADR 0023
catalogues, and this is one more item on that list rather than a separate
problem.

So this waits for stage 1, and then it is small — a window that exists can be
hidden, and an app that owns its window can honestly describe what its close
button does.

**What does not wait:** naming what is already running, which shipped. If the
lifecycle stays confusing in the meantime, the next cheap step is the same kind
— making the tray tooltip and the Settings page say whether this server is the
service or this process, rather than leaving it to be inferred.

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
