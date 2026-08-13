# Updating and starting LANcast without a terminal — plan

Status: **both built** (v0.6.14) · 2026-08-12 · follows [ADR 0022](adr/0022-desktop-modes.md),
[ADR 0016](adr/0016-release-and-update.md)

Two complaints, one shape: **the server does work the user cannot see, and then
waits for them to guess.**

## What already exists

Worth stating first, because the gap is smaller than it looks and the fixes are
not rewrites.

- **Double-clicking `LANcast-Server.exe` already opens the tray**, not a
  terminal (`bareLaunchUsesTray`, ADR 0022). No elevated shell is needed for a
  normal launch — the elevation this session hit was for `Start-Service`, which
  is a different thing.
- **Clicking the client already starts a server** if none is answering
  (`launcher.ensureServer`), and stops the one it started when it closes.
- **`Restart now` exists** in Settings → Updates, and spawns a detached
  `service restart` helper.

So three of the pieces are built. What is missing is what happens **when the
answer is not the happy path**, which is the case the user actually met.

## Problem 1 — the update finishes in the dark

Today, `Download` stages the update and the panel says *"LANcast 0.6.13 is
ready · restart"*. Then:

- **On a service install**, `Restart now` works.
- **Anywhere else** — a server started from a shortcut, a tray launch, a
  terminal — `Restart now` answers **412** with *"close LANcast and open it
  again to finish the update"*, which is the moment the user is on their own:
  nothing says when the swap happened, so the only way to find out is to start
  the server again and look at the version.

That is the deception: the application knows exactly when the update completes,
and does not say.

### Decision

**A staged update finishes itself, whatever started the server.**

1. **A relaunch helper for the non-service case.** The same trick the service
   path already uses: spawn a detached copy of the current binary in a `finish
   update` mode, which waits for this process to exit (so the files are
   unlocked), applies the staged files, and **starts the server again with the
   arguments it had** — tray if it was tray, foreground if it was foreground.
   Windows permits renaming a running executable, which is what makes this safe.

2. **The panel says what is happening, in the panel.** Four states, not one:
   `Downloading…` → `Ready to install` (with what will happen spelled out:
   *"LANcast will restart itself; this takes a few seconds"*) → `Installing…`
   → **`Updated to 0.6.13`**, confirmed by reading `/api/health` back after the
   server answers again. The last one is the message that does not exist today
   and is the whole complaint.

3. **The client waits and reconnects** rather than showing a dead page: it
   already polls health for the splash; the same poll covers a restart, with a
   *"LANcast is restarting"* state instead of an error.

4. **The notice is not only in Activity.** A staged update is a one-line banner
   in the shell, dismissible, because "an update is ready" is not an activity
   log entry — it is a thing the user is expected to act on.

## Problem 2 — one click should mean one click

The remaining hole is real and this session walked into it: **the server binary
does not know about the service.**

With LANcast installed as a service and stopped, double-clicking
`LANcast-Server.exe`:

- takes the tray path, finds no server answering,
- starts *its own* server as the logged-in user,
- against the machine-wide data directory the **service account owns**,
- which fails with `attempt to write a readonly database (8)` — an error whose
  text explains nothing to anyone who has not read the SQLite source.

### Decision

**Launching the server when a service is installed starts the service.**

- On a bare launch, look for the installed service first. If it exists and is
  stopped, start it — **one UAC prompt**, via `ShellExecute` with `runas`, which
  is the ordinary Windows way to ask and is one click, not a terminal.
- If the service is running, do what the tray already does for a running
  server: open the UI and exit rather than adding a duplicate process.
- If no service is installed, behave exactly as today: run in-process with a
  tray icon.
- **Never silently die.** Any fatal error on the windowless path gets a message
  box that says what to do — the readonly-database case becomes *"LANcast is
  installed as a service and its data belongs to the service account. Start the
  LANcast service to continue."*

The tray menu gains **Open LANcast · Status · Check for updates · Stop server**,
which is the Plex-shaped surface being asked for, and the Stop item is what a
user currently has no way to do without Task Manager.

## What this does not do

- **No auto-elevation for anything but starting the service.** A media server
  that silently acquires administrator rights is a different product.
- **No change to how the update is verified.** Signature checking and staging
  stay exactly as ADR 0016 left them; this is about what happens *after* a
  verified update is staged.
- **No Linux tray.** There is no tray there and the service manager is systemd,
  which already restarts a unit on its own.

## Built

Both, in that order. Problem 1 landed as the relaunch helper plus the four-state
panel and the shell banner; problem 2 as the service-first launch, the elevation
prompt, and the failure messages below.

One thing changed on contact with the code: the tray gained **Check for
updates** as a deep link into Settings rather than a status dialog. The answer
already lives in the application, and a message box repeating it would give the
same fact two places to be wrong.

## Order (as planned)

Problem 1 first. It is the one the user hit twice, it is contained to the update
panel plus one helper mode, and its worst failure is cosmetic. Problem 2 touches
process launch and elevation, where the worst failure is a server that will not
start at all — so it wants the ground clear when it lands.
