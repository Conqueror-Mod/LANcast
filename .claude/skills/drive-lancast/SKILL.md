---
name: drive-lancast
description: Drive the running LANcast desktop app on this machine to see real behaviour — verifying a fix against the live library, hunting UI bugs, checking counts. Use whenever the task needs the app rather than the code. Covers the native window via computer-use, which is the only surface that shows current server state.
---

# Driving LANcast

The app is a WebView2 desktop client (`LANcast Client`) talking to a local
server. Drive **the native window, through computer-use**. Everything below was
learned by doing it and getting it wrong first.

## Use the native app, not a browser

**Never drive this app through the in-app Browser pane.** It cannot reach the
local TLS origin, and where it loads anything at all it is a *separate client
with its own cache* — so it shows a different, usually staler world than the
window the user is looking at. A long-lived tab has reported 170 items in review
while the native app, freshly restarted, correctly said 14. If a session is
"driving LANcast" in a browser, it is reporting on a ghost.

Claude in Chrome is a legitimate second choice **only** when the task needs
devtools — the console, the network log, cache inspection. Say so explicitly
when choosing it, and remember it is still a different client from the user's.

## Getting on screen

1. `request_access(["LANcast Client"])` — the exact Start-menu name.
2. `open_application("LANcast Client")` if it is not already running. It usually
   is; opening again just re-activates.
3. Screenshot. Then read the two traps below before concluding anything is wrong.

### Trap 1 — masked windows occlude the app

Windows that are **not** in the allowlist are replaced in the screenshot by a
solid rectangle at their real position. That rectangle sits over LANcast
regardless of z-order, and clicking the title bar does **not** clear it: the
masking is positional, not stacking. A big black block over the middle of the
app is this, not a rendering bug.

The fix is the user's: ask them to minimise the offending windows (the
screenshot output names them). Do not request access to Chrome, Discord and the
rest just to clear a mask — that hands over their tabs and messages to solve a
layout problem. Ask first and let them choose.

### Trap 2 — the frontmost-app guard

If a non-allowlisted app takes focus — a game launching, Chrome popping up — the
next click fails with an explicit error naming it, and the batch stops. This is
the guard working. Take a fresh screenshot, wait for the user, and retry; do not
fight for focus, and never click blind.

### Multiple monitors

This machine has three: `C27F398` (primary, where LANcast lives), `Display`, and
`Roku 55R4AX`. `switch_display("<name>")` moves the capture; `switch_display("auto")`
restores. If an app "won't come to the front", check the other two before
concluding it is broken — and note that tray-resident apps (SteelSeries GG) may
not raise at all.

## Stable coordinates

Maximise the window first (button near `(1402, 8)`); a maximised window gives a
**1456×819** screenshot frame, which every coordinate below assumes.

Collapsed left rail, by icon:

| target | y |
|---|---|
| Home | 41 |
| Movies | 96 |
| Music | 122 |
| Photos | 148 |
| TV Shows | 174 |
| Add-ons | 200 |
| Live TV | 226 |
| Downloads | 252 |
| People | 492 |
| Settings | 733 |
| Account / profile | 760 |

All at `x = 22`. Library page furniture: the **Collections** button sits at
`(100, 158)` on a movie library, **Playlists** at `(248, 158)` on a music one,
and the grid count reads at roughly `(1050–1300, 78–100)` — `zoom` it rather
than squinting.

**The rail expands on hover and overlays the content**, covering everything left
of about `x = 155`. Before clicking anything near the left edge, move the mouse
to `(900, 400)` and wait a second so it collapses. When reading a page, park the
cursor away from the rail for the same reason.

Use `zoom` freely for counts and subtitles — tile subtitles and header counts are
too small to read reliably in a full-frame screenshot, and a misread number here
turns into a wrong bug report.

## Read-only by default

Default rule: **click nothing that writes.** Navigation, filters, opening detail
pages, expanding panes, scrolling — all fine. Stop and ask before any of:

- **Remove** (a title, a library, a location, a channel source), **Delete playlist**
- **Scan** or **Refresh metadata** — a scan with `write_nfo` on writes sidecars
  next to real media, and the database being a copy does not make the media a copy
- **Sign out**, changing a password (which drops every session), any Settings toggle
- Adding a channel source — the server fetches a URL, and the URL must come from
  the user
- Playing a channel, which starts an ffmpeg session on their machine

Opening a detail page, starting local playback of a file, and scrolling are all
safe and often necessary.

## Making sure you are seeing current state

The client caches. Before reporting that a number is wrong:

- Navigate away and back — most lists refetch on mount past their stale window.
- Prefer a value you can cross-check: a grid header total against a nav count,
  or a Settings → Libraries row against the library page.
- If the user has just rescanned or updated, say which of the two you are
  looking at. A count read mid-scan is not evidence of anything.

## Reporting

Say what was verified and what was only observed. Screenshots prove appearance,
not cause — when something looks wrong, confirm the mechanism in the code before
calling it a bug, and say plainly when you have not. Several of this project's
real bugs were invisible in the source and only showed up here; two of them were
pages that looked completely finished while silently truncating their data.
