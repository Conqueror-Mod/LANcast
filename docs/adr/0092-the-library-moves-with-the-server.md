# ADR 0092 — The library moves with the server

Date: 2026-10-19 · Status: **proposed**

Chris asked for "a tickable option in the app to install as a service",
because not every user needs LANcast as one. The installer now asks
(`installer/lancast.nsi`, ServicePage, branch `feat/optional-service`). It
cannot offer the choice once a library exists, and greys the box out with
the reason. This ADR is the other half: a Settings control that switches an
existing install between the two modes, moving the library as it goes.

## Context

The two ways of running keep their library in different folders, and the
folder is chosen by which one is running, not by a setting:

| | Service | Only while the app is open |
|---|---|---|
| Runs as | LocalSystem, session 0 | the signed-in person |
| Data folder | `%ProgramData%\LANcast` (`service.DefaultDataDir`, pinned at install) | `%APPDATA%\LANcast` (`config.DefaultDataDir`) |
| Started by | Windows, at boot | the app (`ensureServer`), stopped when it exits |

Three things already decide "which mode is this machine in" by looking for
`lancast.db` in the machine-wide folder:
- The app's `sharedDataDir` pins a server it starts to `%ProgramData%` when a
  database is there.
- `ensureServer` waits for the service, and never starts a rival, when a
  service is installed and the app would use that folder.
- The installer locks its checkbox on which folder holds a database.

So the mode is not the service registration alone. It is the registration
plus where `lancast.db` is. Change one without the other and:
- **To a service without moving:** the service opens an empty machine-wide
  folder. This looks like everything has been deleted.
- **Away from a service without moving:** the app's server opens the
  machine-wide folder, which belongs to SYSTEM. `BUILTIN\Users` has read
  access only, so the server fails with `attempt to write a readonly
  database`.

A library is not just the database. On Aither the machine-wide folder holds
about 10 GB: artwork 6.9 GB, optimized copies 2.3 GB, faces 631 MB, retro
saves 304 MB. It also holds the server's identity (`identity/`, ADR 0044,
never regenerated), `secrets.key` (ADR 0090), the TLS certificate the window
pins (`tls/`), and `config.json`. All of it has to travel. A server that
arrives without its identity has to be paired again by every friend. One
that arrives without `secrets.key` cannot open the secrets in its own
database.

## Decision

**1. The switch is made from the desktop app on the server's own machine.**
It is not an HTTP route. Three reasons:
- Only that process can name the destination. "Only while the app is open"
  means *this person's* `%APPDATA%`, and a request from a phone has no such
  folder. The service runs as SYSTEM, so its own `%APPDATA%` is the wrong
  one.
- It needs a UAC prompt. A UAC prompt can only be shown to someone sitting
  at the machine, and never from session 0.
- The server cannot move the database it has open. That is the same argument
  `restore` makes (ADR 0058).

So it is a window binding, like Start at login. It appears in Settings ›
This app, under "How the server runs", and only:
- to an administrator;
- in the desktop window;
- when the window is showing this machine's server.

Windows is the real boundary, though. The work is done by an elevated
helper, and nobody gets past the consent prompt without a Windows
administrator's rights. Someone with those rights can already copy the
database by hand, so the LANcast role check only decides who sees the
control. It is not what protects the data.

**2. One elevated helper does the whole switch.** The helper is
`LANcast.exe server-mode to-service|to-app`, started with ShellExecuteEx
`runas` and waited on. That means one prompt for the whole switch. The
unelevated app passes in what only it knows:
- the per-user folder;
- the person's account (SID);
- the listen address;
- a file where the helper reports its progress and result.

An elevated process run with someone else's administrator credentials
resolves `%APPDATA%` to *that* account. That is why the folder is passed in,
not looked up.

**3. The library is copied, not moved.** Moving looks attractive: the two
folders are usually on the same volume, so a rename would be instant. It is
rejected for two reasons:
- **Permissions don't follow a move.** A moved file keeps the permissions it
  inherited from its old folder. Files moved out of `%ProgramData%` would
  stay read-only to the person who now owns the library.
- **An interrupted move splits the library.** Some folders would end up in
  each place, and neither copy would be whole.

A copy has neither problem. New files inherit their new folder's
permissions, and the source is untouched until the end. Before starting,
the helper checks the destination volume has room for the copy, and refuses
if it doesn't.

Not copied:
- `cache/`, the transcode cache. It is disposable.
- `client/`, the window's own WebView2 profile, which is open while the app
  runs.
- Earlier set-aside databases (`lancast.db.replaced-…`, `lancast.db.moved-…`).

Everything else goes.

**4. `lancast.db` appears in the destination only when the copy is proven.**
- The database and its `-wal`/`-shm` are copied under the name
  `lancast.db.incoming`. SQLite derives the sidecar names from the database
  name, so the copied WAL stays attached to its database.
- The copy is opened (`store.VerifyCopy`). The helper runs `quick_check`,
  checks the schema version isn't newer than this build, and checkpoints the
  WAL into the file.
- Only then is the file renamed to `lancast.db`. A rename on one volume is
  atomic.

Before the rename, an interrupted switch leaves only `.incoming` files in the
destination. Nothing treats those as a library, and the next attempt clears
them.

**5. The source is never deleted.** Once the destination is proven, the
source's `lancast.db` is renamed `lancast.db.moved-<stamp>`. Its other files
stay where they are. Renaming the database is what flips every check in the
Context section, because they all look for that exact name. Keeping the
rest means a switch can be undone by hand. The confirmation tells the person
the old folder can be removed once they are happy, and how much space that
gives back.

A database that is already in the destination is set aside the same way
(`lancast.db.superseded-<stamp>`), never overwritten or deleted. It belongs
to the mode that is not running, so nothing is using it. Aither has one: a
`%APPDATA%\LANcast\lancast.db` left from before the service was installed.

**6. The order of steps makes every interruption recoverable.**

To the app (from a service):
1. Stop the service and wait for it to stop. The service holds the database
   open until then.
2. Give the person their folder (§7), then copy and prove the library into it
   (§3, §4).
3. Set the service to start manually, so a reboot can't bring it back.
4. Place the copy: set aside any database already in the per-user folder,
   and rename the copy to `lancast.db`.
5. Set the source database aside (§5).
6. Uninstall the service.
7. The app starts its own server on the per-user folder.

If any of steps 1–5 fails, the helper undoes what it did: it removes the
`.incoming` files, sets aside a copy it had already placed, sets the service
back to automatic and starts it again. The window then waits for the
service, as it does at login. If only step 6 fails, the library is already
across. The switch succeeds with a warning, and switching again removes the
service.

To a service (from the app):
1. The app stops the server it started.
2. Copy and prove the library into `%ProgramData%`.
3. Install the service to start **manually**.
4. Place the copy.
5. Start the service and wait up to a minute for it to answer. A server
   running as SYSTEM that answers is proof the copy opens as the account that
   will own it.
6. Set the service to start automatically.
7. Set the per-user database aside.

The service is registered before the copy takes its name, and starts at
boot only once it has answered. Each half closes a gap:
- A boot-start registration over a folder with no library would create an
  empty one at boot.
- A library in `%ProgramData%` with no registration is a folder the app can't
  write.

If step 3 fails, the `.incoming` files are removed and nothing else has
changed. If step 4 fails, the service is uninstalled as well. If step 5
fails, the helper:
- uninstalls the service;
- renames the copy `lancast.db.failed-<stamp>`, so the installer and the app
  don't treat it as the library;
- leaves the per-user library exactly as it was.

The app then starts its own server again.

If the helper is killed, here's what each moment leaves:
- **During a copy (the long part):** an `.incoming` file and an intact
  source in the mode that was running. To the app, the service is stopped
  but still starts at boot; the window says it isn't running until it is
  started or the machine restarts.
- **To the app, between setting the service to manual and setting the source
  aside:** a stopped service set to manual, with the library still in
  `%ProgramData%`. The window says the service isn't running. Starting it, or
  switching again, finishes the job.
- **To the app, between setting the source aside and uninstalling:** the
  library is in the per-user folder and an idle service is still registered.
  It won't start at boot. The app starts its own server, and Settings offers
  to finish removing the service.
- **To a service, between installing and placing:** a manual service with no
  library, which the app ignores. It starts its own server, and switching
  again replaces the registration.
- **To a service, between placing and the service answering:** a manual
  service over the library. The window says the service isn't running;
  starting it finishes the job.

Nothing in any of these states deletes anything.

**7. Permissions are set, not assumed.**
- **To the app:** before copying, the helper grants the person's SID full
  control of the per-user folder, inherited by everything inside. The
  copied files are then the person's own, even though an administrator's
  process wrote them.
- **To a service:** `%ProgramData%` already gives SYSTEM full control,
  inherited. The helper doesn't widen it.

**8. The listen address goes with the mode.**
- **To a service:** the service is installed with the address the app was
  serving on.
- **To the app:** the app's server uses the address the window was opened
  with. The port is not recorded anywhere outside the service's command line
  (ADR 0089 §1), so a service port changed in Settings applies until the app
  is next started. After that the app's default (`:8080`) applies. The
  confirmation says so when the port isn't the default.

## Consequences

- **The switch takes a while.** On Aither it is a 10 GB copy. The helper
  reports progress to its report file, and Settings shows it. The window
  stays open throughout, but the server is down for the whole copy. Anyone
  watching is interrupted, as with an update.
- **Twice the disk space, for a time.** The old library stays until the
  person removes it. LANcast won't delete it for them. That would be the
  first place in the project where LANcast deletes a library, and doing it
  on a schedule is a decision for another ADR.
- **The installer's lock stays.** Setup still doesn't move libraries, so the
  box stays greyed out once one exists. Its explanation can now point to
  Settings › This app.
- **Windows only.** On Linux the service is a systemd unit the operator
  wrote, in a folder they chose (ADR 0016). LANcast has no business moving
  it. The binding isn't offered there.
- **Friends are unaffected.** The identity, certificate and port travel with
  the library, and the machine's address doesn't change.
