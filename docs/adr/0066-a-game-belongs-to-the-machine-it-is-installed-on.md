# ADR 0066 — A game belongs to the machine it is installed on

Date: 2026-09-11 · Status: **proposed**

Extends [ADR 0022](0022-client-and-server-executables.md) (the client and server
are separate executables). Keeps [ADR 0002](0002-one-wide-media-item-table.md)
and [ADR 0020](0020-plugin-isolation-boundary.md) standing by staying out of
both. Supersedes nothing.

## Context

The roadmap captured "installed games as a library — Steam and Epic" as the
first backlog item that **breaks the model rather than extending it**, and said
it needed an ADR before a design. This is that ADR.

The request as first put: sign in to Steam or Epic through the browser, grant
LANcast access, and see your installed games in a Games tab, grouped by
provider. Launcher only — LANcast starts the game and gets out of the way.

Three facts decide almost everything, and none of them is a preference.

### Signing in cannot tell you what is installed

Steam's browser sign-in is OpenID 2.0, and what it returns is a SteamID. That
is all. The Web API reached with that id and a developer key lists what an
account **owns**, and only when the profile's game details are public. Nothing
on Valve's side knows which of those games are on *this* disk — that fact lives
in the Steam client's own files and nowhere else.

Epic is starker. There is no public sign-in that grants a third party an
account's library. The tools that show one borrow the launcher's own client
credentials, which is a terms-of-service question and a thing that breaks the
day Epic rotates them.

So a sign-in would add a network dependency, a credential to store, and a
phone-home — and still not answer the question the feature asks.

### The installed list is a local read

Steam writes `steamapps/libraryfolders.vdf`, naming every library folder, and
one `appmanifest_<appid>.acf` per game inside each. Both are Valve KeyValues
text. A manifest carries the name, install directory, size on disk, last-played
time, and `StateFlags`, whose bit 4 means *fully installed* — a half-downloaded
game has a manifest too. Steam also caches library artwork on disk under
`appcache/librarycache`.

No key, no account, no network. *No phone-home* holds without an argument.

### The server cannot launch anything anyone would see

LANcast's server is a Windows service in session 0: no desktop, no window
station. A game started from it would run where nobody can see it. And a game
is installed on one PC — the phone, the TV and every browser tab in the house
cannot run it. Every media kind so far is a file the server can read and stream
to anything on the LAN; a game is neither.

## Decision

### Read Steam's files; do not sign in

The Games tab lists what Steam's local files say is fully installed. There is
no sign-in, no Web API call and no stored credential. Owned-but-not-installed
games are out of scope, because they are the one thing that would need an
account.

### Steam only, for now

Epic's installed games are also a local read — a folder of JSON `.item`
manifests under `ProgramData` — so it is not ruled out on principle. It is
deferred because it keeps no artwork on disk, and a grid of lettered
placeholders is not the feature. It is the obvious second reader, and the
package is shaped so it slots in beside Steam's.

### A game belongs to the desktop client, and the server holds nothing

The desktop client (`cmd/lancast`) reads its **own** machine's disk and
launches through window bindings, the same mechanism `lancastDesktopState`
already uses. The server has no games table, no games endpoint and no
`media_item` rows.

This is what keeps ADR 0002 intact. The alternative — rows in `media_item` with
a machine id — makes every existing listing lie: games in search, in Recently
Added, on a phone that can never run them, unless every listing grows an
exclusion. That is the six-nullable-columns argument ADR 0002 used to keep
channels out, and here there is not even a reason to have the row.

It also means there is no API change, so `docs/api.md` and `openapi.json` are
untouched.

The tab exists only where the bindings do. A browser tab, a phone or the TV
never shows it; a browser pointed at `/games` is told that games live in the
desktop app.

### Off by default

A desktop preference, `games`, beside close-to-tray, and false until somebody
turns it on. A media server should not start listing your games because it
happened to find Steam. The check lives in the **process** — every binding
refuses while it is off — and not only in the page.

### Core client code, not a plugin

The WASM sandbox (ADR 0020) exists to refuse exactly the capability a launcher
needs. A "run this URI" capability narrowed to `steam://` would still be the
first crack in *deny by default*, and every plugin after it would arrive with
the precedent. Launching belongs in the client executable, which is already
trusted code on the user's own machine.

### The page names a game; it never names a URI or a path

This is the security rule, and it is the part worth arguing with.

The window's bindings are callable by whatever page is loaded in it. That page
is the pinned server's, but "the page is trusted" is the assumption a boundary
exists to not rely on. So:

- The page passes an **appid** and nothing else. It never supplies a URI, a
  command, or a filesystem path.
- Launch and open-folder **rescan** and act only on an id found fully installed
  on disk at that moment.
- The client builds `steam://rungameid/<appid>` itself, rejecting any id that is
  not all digits, and opens it with `desktop.OpenBrowser`, which hands the URI
  to `rundll32 url.dll,FileProtocolHandler` as a single argument with no shell.
- Open-folder opens the install path derived from the manifest, and that path
  must stay **contained** within its library folder after cleaning — the same
  rule the server applies when a database row becomes a file path. An
  `installdir` of `..\..\Windows` is refused, not opened.
- Artwork reaches the page as `data:` URIs, so the page never learns a path.

A page that is compromised can, at worst, start or reveal a game that is
already installed.

### What v1 holds

A grid of installed games with sort (name, last played, size) and a name
filter; a detail page with Play and Open folder; and hide and favourite. Hide
and favourite are per client, in a `games.json` beside the client's own
preferences — the server has no opinion about them because it has no games.

Favourite is **not gold**. Gold means *where you are* (see `docs/design.md`),
and the moment it also means *favourite* the focus signal is dead.

## Consequences

- A person with two gaming PCs sees each PC's games on that PC and nowhere
  else. That is true to where the games are, and it is the cost of the server
  holding nothing.
- The desktop client grows its first feature that is not about the window, so
  it ships through the **installer** rather than the in-app server updater.
- Steam's file formats are not a published contract. A change to them breaks
  the reader quietly, so the parsers are pure, fixture-tested, and report
  *Steam not found* distinctly from *no games* rather than showing an empty
  grid either way.

## Considered and rejected

- **Steam OpenID and the Web API.** Answers *owned*, not *installed*; adds a
  key, a credential and a phone-home.
- **Epic via the launcher's client credentials.** No legitimate path, and
  fragile.
- **A server-side games table, one row per machine.** Other devices could see
  what is installed on the gaming PC without being able to do anything about
  it, at the price of breaking ADR 0002's model.
- **A plugin with a launch capability.** Reopens the one door ADR 0020 was
  built to keep shut.

## Later, not now

Epic, then GOG, Xbox and EA — each another local manifest reader. Artwork for
readers with none on disk. Owned-but-not-installed games. Playtime.

---

## Amendment, 2026-09-12 — which display a game opens on

Status: **proposed**. Extends this ADR rather than superseding it; everything
above still stands.

### Why this needs an amendment at all

The decision above drew a line at *launcher only*: LANcast hands Steam an app
id and gets out of the way. Putting a game on a chosen monitor crosses that
line, because there is no way to ask for it — it can only be done by reaching
out and moving another program's window after it appears. That is a different
kind of act from starting one, and it deserves to be written down rather than
absorbed quietly.

### The constraint that shapes the whole feature

**No launcher can tell a game which display to use.** `steam://rungameid/<id>`
takes no such argument, and there is no Steam setting for it either. A game
chooses its screen from its own configuration, from whichever display Windows
calls primary, or from where its window was the last time it ran.

So a picker cannot pass a preference along. Whatever LANcast offers here, it
has to *cause*.

### Decision: move the game's window once it appears

After launching, the client watches for a new top-level window that looks like
a game — visible, titled, and big enough not to be a splash or a tooltip — and
moves it onto the chosen display's work area. It never resizes it: a game sized
its own window for the render target it chose, and changing that is not ours to
do.

The watch is bounded. It polls for a minute and a half and then gives up
silently, because a game that has not opened a window by then is either still
decompressing shaders or never starting, and neither is improved by a client
that keeps looking for ever.

### What this honestly cannot do, and says so

- **Exclusive fullscreen ignores it.** A game that takes a display exclusively
  puts itself where its own settings say, and a `SetWindowPos` against that is
  either undone or meaningless. Borderless and windowed — which is what most
  modern games default to — are what this actually serves.
- **A game running as administrator ignores it too.** Windows blocks a
  lower-integrity process from moving a higher-integrity window, and several
  anti-cheat systems run elevated.

Both are stated in the picker, in the interface, before somebody chooses. A
feature that quietly does nothing for half its cases is worse than one that
names them: the failure here is invisible from the inside — the game opens,
just not where it was asked to — so nothing would ever look broken.

### Rejected: temporarily making the target display primary

This is the only mechanism that moves an exclusive-fullscreen game, and it was
still refused.

Making a display primary rearranges the whole desktop — icons reflow, other
programs' windows move, the taskbar jumps — and putting it back requires
knowing when the game exited. LANcast never learns that: Steam launches the
game detached, so the client sees no process to wait on. The realistic outcome
is somebody's desktop left rearranged after playing, with LANcast having no
idea it owes them a restore.

Trading a permanent, visible change to somebody's whole desktop for a better
success rate on one class of game is not a trade this project should make
silently, and making it loudly would mean a second scary option nobody wants to
read. The narrower mechanism that never touches anything but the game's own
window is the one that fits *server owns truth, clients are thin* — the client
acts on its own machine, on the thing it just started, and nothing else.

### Asked once, remembered per game

An unanswered game shows the picker on Play; an answered one launches straight
away. The answer lives in the per-client `games.json` beside hidden and
favourite, because it is a fact about this desk and this person, exactly like
the rest of that file.

**Stored as the device name** — `\.\DISPLAY2` — and never as coordinates. Two
monitors swapped in Windows' display settings swap their rectangles with them,
so a stored position follows the geometry rather than the screen, and somebody
who rearranged their desk would find their games opening on the wrong monitor
with nothing having changed in LANcast. This is the same rule the window's own
placement already follows.

"Wherever it would have opened" is a stored answer too, not an absent one. The
difference between *chosen to leave it alone* and *never asked* is what decides
whether the picker appears again.

### The picker is an in-DOM modal, never a native dialog

A native dialog in a frameless WebView2 window is a focus trap: dismissing it
does not reliably hand keyboard focus back to the web contents, so the app
keeps painting and clicking while nothing can be typed into. It reads as a
random freeze that fixes itself when the user alt-tabs away and back.
