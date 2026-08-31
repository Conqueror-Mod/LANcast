# Release archive

Every LANcast release, newest first, with the notes it shipped with.

GitHub's releases page holds only the current series and one entry per earlier
minor; this file is where the rest live. The **git tags are all still present** —
`git checkout v0.6.20` builds that release from source no matter what the
releases page lists.

Licence note: every release up to and including **v0.8.44** was published under
the MIT licence and remains available under it. LANcast moved to AGPL-3.0 at
**v0.8.45** (ADR 0053). Removing an old release's binaries from GitHub does not
retract that grant — the tag, and therefore the source, is still there.

---

## v0.8.45 — 2026-08-31

### LANcast is now licensed under the AGPL-3.0

**This release is the boundary.** Everything up to and including **v0.8.44 was published under the MIT licence and remains available under it** — nothing has been retracted. From v0.8.45 onward LANcast is licensed under the **GNU Affero General Public License, version 3 or later**, and a **commercial licence is available** for anyone who wants to build on it without the AGPL's obligations. See [COMMERCIAL.md](https://github.com/Conqueror-Mod/LANcast/blob/main/COMMERCIAL.md).

If you self-host LANcast for yourself, your household or your friends, nothing changes and there is nothing to buy. That is the case LANcast was built for.

The Affero clause is the one that matters for a media server: plain GPL is triggered by *distributing* software, and a server can be run for other people over a network without ever being distributed. If you modify LANcast and other people use your version, they are entitled to your source. Settings → Server now carries a link to the source for exactly that reason.

### Photographs, by when they were taken

A picture library has a new **Timeline** view beside its folder grid, grouped by EXIF capture time. A holiday spread across three folders is one week, and until now three folders is how it looked.

Photographs with no capture time are their own group at the end rather than being quietly dropped. Each month loads only when you open it, so a library of several thousand pictures does not arrive at once.

### Sensitive folders, corrected by using them

Two things were wrong in the sensitive mark that only real use could show.

**Accepting a folder used to uncover it everywhere**, including the home page, for the rest of the session. Now a cover can only be lifted in two places — the picture library's own grid, and inside the folder itself. On the home page, the shelves, the hero, in search and in collections, marked content stays covered with no way through, and leaving the pictures forgets what you accepted.

**Only folders can be marked now.** A single marked photograph had nowhere it could be viewed, so it was covered everywhere and reachable nowhere. If you have a picture you want covered, put it in a folder and mark the folder. Marks made before this change still work and can still be removed.

### Under the hood

Face grouping has its foundations: the native worker builds and cross-builds and is checked on every change, well before anything depends on it. There is nothing to see yet, and a `lancast-faces` download is on the release page for the curious — it reports honestly that it has no model and refuses to pretend otherwise.

Two quieter repairs: navigating away from a page is no longer recorded as a server error (it accounted for **421 of 474** error lines in the log, burying the real ones), and a cast list that showed bare numbers under some actors' faces now leaves the character blank when the provider gives us a number instead of a name.

### Upgrading

The in-app updater covers this. No database changes in this release.


---

## v0.8.44 — 2026-08-31

A look pass, judged against LANcast's own intro.

### The field has stars now

The intro is mostly stars and the app had none, which was most of why the two didn't feel like the same product. The nebula field gained a star layer, a teal wisp and a low warm ember, and a vignette that gives the whole field somewhere to fall away into.

**The stars twinkle.** Four layers of them, each holding a different set and fading on its own clock — 3.7, 5.3, 8.9 and 13.7 seconds. Those never resync, so which stars are bright doesn't repeat in any sitting. It's drawn with gradients and animated on the compositor: no image to load, no script, and nothing running in the background while you watch a film.

**Detail pages keep their backdrop, without stars.** The full-bleed fanart is the best-looking thing in LANcast and it doesn't need a texture drifting over it. The nebula stays there, because the artwork is tinted into it — that's what stops a detail page becoming a photograph with an app around it.

### Text you can actually read

Metadata lines — the year, the runtime, the codec, the certificate — were a blue-grey on a blue background, and sank into it. They're now pulled towards neutral and lifted.

Worth noting for anyone who tests this sort of thing: the old colour **passed WCAG AA**. It was still hard to read, because the eye separates hue before it separates luminance. A contrast ratio is a claim about two colours; legibility here was a question about a colour and a *field*.

### Depth you can see

Panels, menus and tiles now have a lit top edge, so a raised surface reads as raised. Previously all three elevation levels looked identical — a shadow is the absence of light, the background is already almost black, and there was nothing left to subtract.

Gold is untouched. It still means *where you are* and nothing else.

### Upgrading

The in-app updater covers this — the client is served by the server, so a server update brings the new look with it. No installer needed unless you're also changing the desktop client, tray or window.

No database changes in this release.


---

## v0.8.43 — 2026-08-31

### Sensitive content in picture libraries

A picture library can hold a folder that is private in a way the rest of the library is not. Until now LANcast had one answer for that: don't put it in the library.

Turn on **Settings → Libraries → Allow folders and photos to be marked sensitive**, then right-click any folder or photograph and choose **Mark sensitive**. Marked content is covered wherever it appears — the library grid, the home page, search — showing its name and *Sensitive — click to show*. The first click reveals it; the second opens it. It covers again next time the app opens.

Marking a folder covers everything inside it, including folders nested within it and photographs added later. Acknowledging a folder reveals its contents, so opening one doesn't mean answering the same question two hundred times.

Some deliberate properties:

- **The name stays readable.** A grid of identical unnamed rectangles makes you hunt for the folder you marked yourself.
- **A covered tile never requests the image.** No element, no network request — not a blur over a picture that has already arrived.
- **It obscures; it does not restrict.** Anyone who can see the library can still open the folder. They just have to mean it.
- **Acknowledgement never reaches the server.** It isn't stored, isn't audited, and is forgotten when you sign out. The log records that a folder *was marked* — a decision worth keeping — and never who looked at one.
- **Turning the setting off keeps your marks.** It stops new ones and stops the covering.

### Randomize all ends where it started

Turn on Randomize all, watch a film, then press Play on a different film from its own page — it was still shuffling, and still holding the entire library as its queue.

Both rules that decide what happens when the player opens keyed off *"the caller supplied no queue"*, treating that as returning from the mini-player. A single film supplies no queue either, because it hasn't got one, so the two were indistinguishable. Starting something new is now a new activity: it inherits neither the shuffle nor the queue. Randomize all still turns shuffle on, and returning from the mini-player still doesn't clear it.

### Review has a page of its own

Settings → Activity now holds the Review screen permanently. The nav entry that appears when something needs a look is right as a prompt, but it was the only way in — so a library with nothing outstanding couldn't open the screen at all, and dismissed collision reports couldn't be revisited.

### Upgrading

The in-app updater replaces the server. Client, tray and window changes need the installer.

This release adds database schema revision 34. It applies automatically on first start and there is no downgrade — restore a backup if you need to go back.


---

## v0.8.42 — 2026-08-31

### Recently Added stops being emptied by another shelf's import

Reported as showing only the three films just added — and having disappeared
entirely for a while.

Measured on the reporting library. The newest **40** rows by `added_at`:

| kind | count |
|---|---|
| artist | **35** |
| movie | **3** |
| collection | 2 |

A shelf that asks for forty rows of *anything* and keeps the films
afterwards gets three. Immediately after the music import — 8,882 tracks —
it got **none**, and the shelf vanished.

### The window was the bug, not the sort

Ordering by `added_at` across every kind means whichever shelf has just
received a thousand rows **silently evicts the others**. The more recently
you organise your library, the emptier the home page looks — the opposite of
what it is for.

The hero tile starved the same way: it picks from that same list and also
skips music, so it chose between three candidates rather than forty.

### Each shelf asks for its own kinds

- **video** — excludes `artist, album, track, gallery, photo`
- **music** — asks for `kind=artist`
- **photographs** — already had their own query, for a version of this very
  reason

The pattern existed; video and music had never been given it.

Artists rather than albums because the list is top-level, and for a music
library the top level *is* the artist (ADR 0024). Saying so beats relying on
the shape of a general query, which only appeared to work.

### Against the natural theory

**Nothing was being cleared or aged out.** There is no time cutoff anywhere.
The films were pushed out by *count* — still present, simply below the fold
of a window filled with artists — which is why they come back without a
rescan.

### Tests

Three, stubbing the server state a bulk import leaves behind. Two fail
against the shared window, the first with:

```
expected 'Good evening, chris Recently Added An A…' to contain 'An Older Film'
```

The reported symptom, reproduced in a test.


---

## v0.8.41 — 2026-08-31

### A scan is not finished until the trash is

Asked why CI had been failing for a few days. Measured rather than guessed:
across the last hundred runs there were **four** genuine failures with
**two** causes — the ffmpeg stderr ordering fixed earlier, and
`TestAScanEmptiesTheTrashWhenAskedTo`, seen three times.

That second one was **not a test problem**.

`State` leaving `StateRunning` is how everything learns a scan is over. The
client polls it; so does every test that waits for one.

```go
p.State = StateIdle      // "finished"
s.mu.Unlock()
...
if wants != nil && wants() { s.st.EmptyTrash(...) }   // ← still deleting
```

The scan **announced itself finished and then kept deleting rows**. Anything
refreshing in that gap saw rows that were already condemned.

That is this project's most-repeated bug in a new place, with the usual
consolation removed: normally the server is right and only the picture is
stale — here the server was briefly *wrong*, because it said finished before
it was.

It failed only on CI, never on a desk. The diagnosis turned on one of the
failures landing on a **documentation-only change**, which ruled out recent
code entirely and pointed at ordering.

### What deliberately did not move

The shape check still runs **before** the trash: it reads counts the removed
rows belong to, and a verdict computed against a half-deleted library
describes neither the before nor the after.

Only the announcement moved. The outcome is decided where it always was, and
published once the deleting is done.

### Testing an ordering

Waiting for a scan and hoping is how this hid for days. The new test
observes from **inside the window** — `EmptyTrashWhen`'s predicate is
consulted at the moment of decision, and the scan must still call itself
running then. Put the flip back first and it says so outright.

Second time in one day that chasing a "flaky test" found a real product
race, after a stopped listener eating the next one's first signal. Neither
was test noise, and both were dismissible as it.


---

## v0.8.40 — 2026-08-31

### The tray was dropping clicks

The real cause, found in systray's own source after three releases spent
fixing everything *above* the place clicks were being lost.

```go
select {
case item.ClickedCh <- struct{}{}:
default:          // in case no one waiting for the channel
}
```

A **non-blocking send**, on `make(chan struct{})` — **unbuffered**. A click
arrives only if something is blocked on that exact channel at that instant.
No queue, no error: an undelivered click simply never happened.

The tray multiplexed all **seven** menu items in one `select`, which made
that a shared fate. While the single goroutine sat inside *any* handler —
opening a browser, waiting on a modal — every other item's clicks were
discarded.

That is the entire story of *Start LANcast at login*:

- the tick moved because **Windows draws it**, not us
- the handler **never ran**
- nothing was logged because **nothing had run to log it**

Every layer above was working correctly, on evidence that never arrived.

Each item now has its own goroutine, so every channel always has a waiter
and a slow handler can only ever delay its own item.

### The two releases before this

Both were real fixes for the wrong fault, and both are worth keeping:

| | shipped |
|---|---|
| v0.8.38 | a log, so the tray's silence could be read |
| v0.8.39 | the toggle no longer derives intent from a widget Windows toggles itself |

Neither was the cause.

What makes it worth writing down is that **the tell was there at the first
click and was explained away twice**: no log line at all, from code that
logs on every failing branch, means the code *did not run*. It was read
instead as "it ran and agreed with itself" — the more comfortable reading.

---

**After installing:** toggle *Start LANcast at login* once. `[LANcast Tray]`
should appear under
`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`.


---

## v0.8.39 — 2026-08-31

### The login toggle now writes the run key

The instrument shipped in v0.8.38 paid for itself on the first click.
*Start LANcast at login* moved its tick, changed no run-key value, and
logged **nothing** — and nothing was the tell. Every failing branch in
`toggleLogin` logs something, so silence meant the code had run and
**agreed with itself**.

The cause was one line:

```go
want := !item.Checked()
```

The obvious way to write it, and wrong here. **Windows toggles a checkbox
menu item's visible tick itself when it is clicked**, while systray's
`Checked()` returns its own idea, changed only by `Check` and `Uncheck`.
The two drift the instant the native toggle fires — so the intent computed
from the widget is the *opposite* of what was meant.

Every click was computing *"turn it off"* against a setting that was
already off, succeeding, and agreeing with itself. Silently, and wrongly.

The registry is the setting, so it is asked:

```go
was, err := autostart.Enabled(autostart.Tray)
want := !was
```

That takes the widget out of the decision entirely — the same reasoning
that already sets the tick from a read-back rather than from what was
attempted.

### Ruled out while diagnosing

**"Open LANcast" worked** — so the handler loop was alive and delivering
the whole time. Not a dead goroutine, not a blocked modal, not a missed
click. Arithmetic on a stale bit.

### A note on the tests

The source-reading tests grew a **comment stripper**, because they had
started matching the prose that explains them: a note naming
`item.Checked()` as the thing *not* to do reads identically to doing it,
and a substring search cannot tell an explanation from an instruction.

Found by watching the tests fail against their own documentation.

---

**After installing:** toggle *Start LANcast at login* once. It should now
write the run key — and if it does not, `lancast-tray.log` will say why,
which is what v0.8.38 was for.


---

## v0.8.38 — 2026-08-31

### A tick that reported an intention rather than a state

Watched directly: clicking *Start LANcast at login* moved the tick on every
click and **never wrote a run-key value**. Every loaded user hive, `HKLM`
and both Startup folders were searched afterwards — nothing, anywhere —
while the same package, called from a test binary, wrote it correctly.

The tick was set from what was *attempted*: checked once `Enable` returned
no error, unchecked once `Disable` did. That reads as reasonable, and it
makes a menu **capable of lying** — which is exactly what it did.

It now flips the setting, **reads the state back from the registry**, and
shows that. It cannot claim something the machine does not agree with, and
a failure appears as a tick that does not move — a symptom you can report
rather than a silence. A wanted-versus-is disagreement is logged outright.

### And the reason none of it was visible

`newLogger` writes to **stderr**, and the tray is a GUI process with **no
console**. Every warning it has ever produced went into nothing — including
the one that would have explained this on the first click.

It now writes to `lancast-tray.log` in the data directory, teed with stderr
for the reason the service already does it: `MultiWriter` stops at the
first error, and with no console stderr can fail, which would have silenced
the file too.

Deliberately **its own file** rather than `lancastd.log`. Both rotate by
renaming at a size threshold, and two processes doing that to one file is a
race whose loser is the server's log — fixing silence by risking the
server's log would be the wrong trade.

### What this does not claim

**It is not a fix for the toggle itself.** Why `Enable` reports success
from inside the tray while writing nothing — when the identical call from
another process writes correctly — is not yet known, and was not guessed
at.

This makes the failure legible instead: the tick stops moving, and
`lancast-tray.log` says what happened. The instrument first.

---

**If you use start-at-login:** toggle it once after updating. If the tick
does not move, that is the bug showing itself honestly — send me
`lancast-tray.log` and it should be a short diagnosis rather than a long
one.


---

## v0.8.37 — 2026-08-30

### Two start-at-login switches, one wire

Found by asking whether the client and the server could be stepping on each
other in the right wrong conditions. **They could.**

The client window and the server's tray are separate programs, each with a
*start at login* toggle — and both wrote the **same run-key value**, with
the path taken from whichever process was calling:

| toggled from | wrote |
|---|---|
| client settings | `LANcast` → `LANcast-Client.exe` |
| tray menu | `LANcast` → `LANcast-Server.exe tray` |

Turning it on in the tray replaced the client's entry. Turning it off
anywhere cleared whatever the other had meant. And because both checkboxes
read that one value, **both could show "on" while describing different
programs**.

Seen on the reporting install as `open_at_login: true` in the preferences
file beside **no run-key entry at all**.

### The fix

Each target owns a value name and neither can reach the other's. The client
keeps the original — an existing entry pointing at it should go on working
— and the tray takes its own.

`Enabled` asks *"is this the **other** program's entry?"* rather than *"is
it exactly mine?"*. The path legitimately varies — a moved install, a build
run from a terminal — so demanding an exact executable would report a
perfectly good entry as absent. What must never read as *this* target is an
entry that plainly starts the other one.

A legacy shared entry naming the tray is cleared when the tray enables or
disables; otherwise the same program launches twice at login, or a switch
turned off keeps starting it. One naming the client is left alone — it
belongs to the other target.

Caught while writing it: the registry handles for that sweep were opened
**write-only**, so the read would have failed and the cleanup silently never
fired. The same shape as every other fault this week — code that looks
right and does nothing.

### The preferences file stops keeping its own copy

`OpenAtLogin` was written and never read. The run key starts things at
login and the settings page reads the run key, so this field only recorded
what somebody last asked through one of the two programs — which is exactly
how it drifted.

A second copy of a fact, kept by one of its owners, can only go out of
date. There is now a test asserting it stays gone.

The uninstaller deletes **both** names from one list; forgetting one leaves
a login entry pointing at a removed executable — an error dialog every
morning with nothing obvious to blame.

---

**Upgrading:** either route. If you had *start at login* set from the tray,
check it once after updating — the legacy shared entry is migrated on the
next toggle, and the two switches are now independent.


---

## v0.8.36 — 2026-08-30

### Closing from the tray left processes running

The menu had **two partial endings and no whole one**:

| item | did | left behind |
|---|---|---|
| Quit the LANcast app | closed the window | **this tray process** |
| Exit | removed the icon | **the window** |

Neither is what anybody means by *closing LANcast* — so whichever you
chose, something stayed. And what stayed runs out of the **install
directory**, which is precisely what an update has to move aside. That is
why the next update was tedious.

Exit now asks the app to quit **and then** removes the icon. The order is
deliberate: once `systray.Quit` has run this process is on its way out, and
a Quit sent from a dying process is a race nobody needs to debug.

The service is untouched, and the wording still says so. Stopping it is an
administrator action, and a menu item labelled Exit must not be the thing
that raises a UAC prompt.

### The unsafe-localhost warning was the tell

An *unsafe localhost* warning inside the native app window, and a blank page
after accepting it, is what **no certificate pin** looks like. Beyond
loopback the server is self-signed, and the web view refuses it outright
unless its key is pinned.

`serverCertPin` returns empty on any failure — deliberately — and its
comment claimed the cost of being wrong is *"the window failing to load,
which is loud on its own"*.

**It is not loud.** It is a browser security wall inside a native
application, with nothing in any log connecting the two. A silent fallback
whose failure mode is a security wall is the worst of both.

Measured while diagnosing:

```
SPKI(C:\ProgramData\LANcast)             -> 5l71rnOJCaJsVJuAqJWi5W8c7YBW/…
SPKI(C:\Users\…\AppData\Roaming\LANcast) -> ERROR: no server certificate on disk
```

The second is the fallback path, and it is the whole difference between a
working window and a wall.

The pin is now retried for five seconds — the honest reason for a missing
one is a server still writing its certificate — and if it is still absent
against an `https` server, that is said in a sentence and the browser is
opened instead, where the warning can be accepted properly rather than
clicked through into nothing.

---

**Upgrading:** either route works. If you use the tray, note that **Exit
now closes the app too** — which is the point.


---

## v0.8.35 — 2026-08-30

### Closing LANcast left a process with no way back in

Close-to-tray **hides** the window rather than closing it — a feature only
while an icon exists to restore from.

The client gave up its own icon deliberately; a second LANcast icon beside
the server's had been reported as exactly that, and the server's tray took
over saying Open and Quit to it. But **nothing starts that tray.** The
service runs without one, so a machine that has only booted has no icon
anywhere: the X hid a window nothing could restore, and the app became a
process with no way in and no way out.

The replacement for the deleted icon was real. The guarantee that it exists
was not.

The tray now publishes its presence and the client asks before hiding. No
tray means the X closes, which is what a window with nothing behind it has
to mean. Presence is a **named handle** rather than a file or registry key,
so it goes away with the process that holds it — a crashed tray cannot
leave the client hiding into nothing for ever.

### A lost signal underneath it

A test in the same package had been failing two or three runs in ten,
always on *"signal 1"*, never when run alone. That was worth chasing rather
than tolerating, because in the field it is **a tray's Open doing nothing,
once, for no reason anybody can reproduce** — the very path being fixed
above.

`stop()` closed the event handles **without waiting for the goroutines
blocked on them**. Closing a handle another thread is waiting on is
undefined, and it goes wrong in a way that is not the obvious one: *the
wait does not necessarily fail*. Windows reuses handle **values**, so a
goroutine still parked on a closed one can end up waiting on whatever was
opened next — and these are auto-reset events, where exactly one waiter
wakes per signal. The stale goroutine takes the wake-up, sees its own
stopped flag and returns, and the signal is gone.

Measured rather than argued:

| | failures |
|---|---|
| before | 3/10, then 2/10 |
| after | **0/20**, then **0/15** |

A regression test that provokes the sequence directly — stop a listener,
start another on the same names, signal it — fails **5 times out of 5**
without the wait.

### A banner that would not go away

A staged v0.8.33 was still offered on a machine already running v0.8.34.
The updater applies a staging on a **clean** shutdown while an installer
**force-kills** the service, so the staging outlived the install that had
already delivered a newer build — and applying it would have been a
downgrade. A staged update the running build has overtaken is now discarded
at startup.

---

**Upgrading:** the in-app updater works properly again as of v0.8.34, so
either route is fine. Use the installer if you want the desktop shell
refreshed too — the tray and window changes above live in it.


---

## v0.8.34 — 2026-08-30

Two faults reported together as *"an install hang and improper install"*.
Neither was the installer.

### An update that could never install again

The log had it:

```
could not apply the staged update; the install is unchanged  version=v0.8.33
error="moving LANcast-Server.exe aside:
  rename ...LANcast-Server.exe ...LANcast-Server.exe.old: Access is denied."
```

v0.8.31 and v0.8.32 applied cleanly and v0.8.33 did not, which reads as
impossible until the tray is accounted for.

The server tray is **resident and runs out of the install directory**:

1. One update renames the running image to `.old` — the tray keeps that
   file mapped for as long as it lives.
2. The next update must *replace* that mapped `.old` to reuse the name.
   Windows refuses.

**The first update after the tray starts works, and every one after it
fails — permanently, while looking like a one-off.**

What hid it: `Process.Modules` reports each image by its **load-time**
path, so the tray still appeared to be running the current binary while
actually holding the renamed one. Found by scanning every process for
mapped images.

Backups are now **stamped and never reused**, so nothing is replaced and
nothing can be held. The sweep takes `.old` and `.old.<stamp>` alike —
without both spellings, the very file that caused this would have been
stranded by the version that fixes it.

The package doc claimed the renamed file is deleted on the next startup
*"once it is no longer running"* — true only while the sole thing running
it was the process being replaced. That stopped being true when the tray
became resident, and it is now written down.

### And the same install was wedged a second way

Staging held **one** file while its manifest named **three**, because an
earlier apply consumed two and then failed. Nothing can satisfy such a
manifest: every restart read it, refused, and changed nothing — while a
comment called exactly that state *harmless*.

It is an install that stays on its old version for ever. An unsatisfiable
staging is now discarded and fetched again.

### A window that opened onto nothing

Ruled out in order rather than guessed at:

- the assets were intact — `index.html` and a 479 KB bundle, both served
- the bundle was fine — proxying the live server into a browser that could
  be inspected rendered the sign-in screen with **no console errors**
- killing the client and reopening it fixed it — same binary, same server

So: a race. `ResolvedURL` probes **once**, with a 1.5 second timeout, falls
back to a guess, and **nothing retries**. A window handed a URL nothing
answers sits on its background colour for ever, which cannot be told apart
from an application that does not work.

The wait already existed — on the path where the client starts the server
itself. It was missing from the path an **install** takes: a service
already installed and still coming up while the installer launches the
client straight at it. That is the one moment the server is guaranteed to
be busy, and it was the one path that did not wait.

---

**Upgrading:** use the installer for this one. The in-app updater is what
this release repairs, so it cannot be trusted to deliver its own fix.


---

## v0.8.33 — 2026-08-30

Three playback faults. Every one of them was a comment asserting what
software does, rather than a measurement of what it did.

### A film that lagged every few minutes

The log had said why for weeks and nobody had read it: **twelve transcode
sessions in eighteen minutes, every one at `start_at=0`** — no ffmpeg
error, nothing reaped.

The progressive handler is right that a live transcode cannot be
range-served. Then it adds that saying so *"stops browsers issuing range
requests that could never be satisfied"*. Chromium caps buffered media; at
5.4 Mbps that is a few minutes, and when it evicts and cannot ask for a
range it **restarts the film from byte zero**. The further in you are, the
more must be re-streamed — which is why the first quarter of an hour was
always fine.

A conversion is now delivered as an HLS playlist, so evicting a segment
costs a segment.

Measured before building, not after: a bare `<video>` played a real VOD
playlist from `src` with **no library at all** — `readyState` 4, both
tracks decoding, a genuine seekable range of 0–30.05, forward seek 52ms,
backward 35ms.

**hls.js is still not vendored**, and none of this needed it.

The capability is *discovered, not asked*. `canPlayType` answers `"maybe"`
to a playlist whether or not playback works, so HLS is tried; an engine
that refuses the source falls back and the refusal is remembered for that
device.

### A film that dropped a fifth of its frames

That one never transcoded — it direct-plays. Same machine, minutes apart:

| file | codec | dropped |
|---|---|---|
| I Still Know (1998) | HEVC Main 10 | **19.8%** |
| I Know (1997) | HEVC Main 10 | **19.9%** |
| I'll Always Know (2006) | H.264 | **0%** |

Nothing could have predicted it. `canPlayType` says `"probably"`, and
`mediaCapabilities.decodingInfo()` — whose entire purpose is answering
*will this be smooth* — returned `supported`, `smooth` **and**
`powerEfficient` for the exact shape of the file dropping a fifth of its
frames.

So the client watches instead of asking. The withdrawal machinery already
existed and could only be reached by an **error** — but a file playing
badly has not errored. It is the failure that looks like success.

Hard to trip on purpose: ten seconds of steady playing, 120 frames, a
tenth of them dropped, sampled only while genuinely playing. Only the
claims *that file needed* are withdrawn, and it resumes where you are
rather than restarting.

### Resuming an AVI put the sound two seconds ahead

```
video  start_time =  0.000000
audio  start_time = -1.997143
```

Reported as a 1.8 second lag with the picture behind, which is exactly
what a negative audio start looks like from the sofa.

`-avoid_negative_ts make_zero` was tried first and only **moved** the gap.
It is data, not timestamps: AVI has no per-frame timestamps, so a seek
lands on a video keyframe while the copied audio genuinely begins earlier.
Re-encoding re-times it — 0.000 against 0.000, verified with the command
line this build produces, on the real file at the real resume point.

Scoped to seeks only, never live, and only to `avi` because that is what
was watched failing.

### Two quieter repairs

**Review became unreachable exactly when it still had something to show.**
The screen had already reasoned about this — *"Nothing to review would be
a lie with a collision report below it"* — while the nav link counted only
the match queue. The screen was right; the link won by not being there.

**The edition marker is finally read.** Inert on every row since revision
29, it now reaches rows whose files will never change again — including
the `matched` and `locked` ones re-parse refuses, on the grounds that an
edition marker is not a title: no provider knows which cut a file holds,
only the filename records it.

---

**Upgrading:** the in-app updater swaps the server, which carries the
client assets too — so the playback work arrives that way. Use the
installer if you also want the desktop shell refreshed.


---

## v0.8.32 — 2026-08-30

### One tray icon

v0.8.31 made the server's tray icon a controller for the service, and the
client kept its own. An ordinary desktop therefore showed **two LANcast
icons doing different halves of one job** — neither wrong on its own
terms, but together they asked you to know which was which before you
could do anything.

The server's icon now owns both. It gains **Quit the LANcast app**, so
the client can be closed from the same menu that starts and stops the
service, and the client no longer claims a tray slot it does not need.

### A red build that no commit had broken

`main` went red on the tray change — a commit that touches no Go at all:

```
--- FAIL: TestWhatAStoppedFFmpegSaidIsStillAvailableAtDebug
    ffmpeg's parting words were dropped entirely.
    got: ... msg="live transcode started" ...
```

Not a regression, and not the tray. The *fake ffmpeg* used by the test
printed to stdout, then wrote its broken-pipe line to stderr, then slept.
The test reads four bytes and closes, which cancels the context — so on a
loaded runner **the child was killed before it ever reached the stderr
write**, and there was genuinely nothing for the logger to find.

It passed twenty times out of twenty locally, which is what a race that
needs a busy machine looks like from a desk.

The fake now writes stderr **first**. That makes the stdout bytes the
test waits on *proof* that the stderr line was already emitted, and
`cmd.Wait` drains the pipe before it is read — deterministic rather than
merely likely. Real ffmpeg does write that line on the way out, so the
old order was the faithful one; but what these tests assert is the
**level** the text is logged at, and the ordering was never the thing
under test. A comment on each now says so, because putting it back is the
obvious tidy-up.

The sibling test shared the same fake and asserts an *absence*, so the
race could only ever have made it pass for the wrong reason. It now
actually exercises a killed ffmpeg that did write the line.

---

**Upgrading:** the in-app updater swaps the server only. The tray change
lives in the client and the server executable both, so this one wants the
installer.


---

## v0.8.31 — 2026-08-30

**The tray icon added in v0.8.30 never appeared. This is why.**

Clicking LANcast Server still opened a browser and left nothing behind.

The launcher decides what to do by asking Windows whether the LANcast service is installed. That question was being asked in a way that **only an administrator is allowed to ask** — so on an ordinary launch it came back as an error, the launcher read that as "there is no service", and took the path it has always taken: open a browser, exit.

The new tray was never reached.

### It was not only the tray

Every part of the launcher that depends on knowing about the service has been unreachable on an ordinary launch — including the older behaviour of starting the service for you if it was stopped.

It stayed hidden because the fallback works. Opening LANcast in a browser is a perfectly reasonable thing for a launcher to do, so nothing looked broken.

### The fix

Installing, starting and stopping a service genuinely need an administrator, and asking for one is fair. **Reading whether it is running does not** — and it was only failing because it was going through the same door.

It now asks with the permission an ordinary account already has.

So clicking LANcast Server gives you the icon: open LANcast, open the app, start at login, the settings pages, and Exit — which removes the icon and leaves your server running.

### Please install this one rather than updating in place

The in-app update replaces the server program, and the change here only takes effect when you launch it yourself from the Start menu or a shortcut. Running the installer is the reliable way to get it.


---

## v0.8.30 — 2026-08-30

**The server icon now lives in the notification area, and pressing the client shortcut twice stops opening a browser.**

### LANcast Server sits in the tray

Clicking it used to start the background service, flash a browser open, and quit. No icon appeared, so there was nothing to click again — it looked like the program did nothing.

It stays now, as a set of controls for the server rather than a second copy of it:

- **Open LANcast** — in your browser
- **Open the LANcast app** — the desktop window
- **Start LANcast at login** — a tick; the server already starts on its own, this is about the icon
- **Update libraries…** and **Check for updates…** — these open the settings page where each lives
- **Exit** — removes the icon. **The server keeps running.**

That last one is deliberate. The server runs as a Windows service, and stopping a service needs an administrator prompt — a menu item called "Exit" that asked for one would be a nasty surprise. If you want the server itself stopped, that is still Windows Services.

The two items ending in "…" open a page rather than doing the thing, and are named that way on purpose. Starting a scan is an administrator action, and the tray icon has no account of its own to do it with. Taking you to the button is honest; quietly building a side door into the server would not be.

### Pressing the client shortcut twice

If LANcast was already open, launching it again opened a **browser** beside the window — reported by someone with a mouse button bound to the shortcut, where the Start menu entry behaved correctly only because nothing was running yet.

The shortcuts were never the problem. The app did it: a second launch was written to "reopen the interface" and opened a different one — the browser, which has none of the window's advantages and shows a certificate warning the window exists to avoid.

It now brings the window you already have to the front.

The browser is still what you get if you ask for it, and still the fallback on a machine that cannot run the window. Neither of those is somebody pressing their shortcut twice.


---

## v0.8.29 — 2026-08-30

**The report of works claimed by more than one file can finally be answered.**

### A collision you have looked at stops asking

LANcast reports two files claiming one work and never resolves it — no merging, no ranking, no deleting. That is deliberate: a shared identity can mean a redundant copy, a second edition, one film in two parts, or a file that is simply wrong about what it is, and only you can tell which.

What was missing is that there was no way to say *"I have looked at this one, it is fine"*. So a film in two parts, or a second edition kept on purpose, was listed again every time the page opened. A report that cannot be answered is one people stop reading — and that cost falls on the entries which really do want attention.

There is now a button that records you looked. **Nothing is merged, ranked or deleted; both files stay exactly where they are.**

**It remembers the exact pair.** If a third copy of that film turns up later, the entry comes back — because what is being reported is genuinely new. Marking one as seen is not a promise to stay quiet about that film for ever.

And it does not vanish. The heading counts what is still waiting; the ones you have looked at sit behind a toggle saying how many, and can be brought back. An action that hides something with no trace is one you can neither check nor undo.

### A decision written down: alternate cuts

*Spider-Man: Into the Spider-Verse* and its alternate cut, or *Advent Children COMPLETE* beside a standard release, currently read as two separate films. Making them read as one is a real feature, and three things were measured before designing it — two of which change the problem.

**The filename reading already works.** Both Spider-Verse files resolve to the same title and year, and the alternate cut is recognised as one.

**But that marker is empty on every title already in your library**, and no rescan or re-read will fill it in: it is only written when a file's size or timestamp changes, and those files will never change again. Anything built on it would appear to work on newly added films and do nothing to the ones you have. That is now the first job rather than a detail.

**And one of the two examples is not an alternate cut at all.** "COMPLETE" is not a phrase the reader knows, so it stays part of the title — which means a standard release bought later would be a different film entirely, with nothing to group.

Four approaches are written down with what each really costs. The cheapest is the one shipped above, and the honest recommendation is to live with it before paying for the rest.


---

## v0.8.28 — 2026-08-30

**Four reported faults fixed, and three of them turned out to be a careful comment describing something that was no longer true.**

### An artist could show two "Play all" buttons, and the useful one did nothing

A track sitting in an artist's folder with no album folder around it belongs to the artist and nothing else, so it hangs directly off them. The code assumed that could not happen — *"an artist's children are albums"* — which is true of a tidy folder tree and not of a real library.

The result was two buttons. One found the loose track and played it. The other, the one meaning **play the whole discography**, was handed that track where it expected an album, found nothing inside it, built an empty queue and quietly did nothing.

An artist now has exactly one *Play all*, and it plays albums and loose singles alike, in the order the page lists them.

### A work whose every copy has gone can be forgotten

The report of works claimed by more than one file gained a way to clear a leftover last release, offered only where **another copy was still there** — on the reasoning that a drive going away takes every copy at once, so the offer would disappear exactly when it was dangerous.

Sound reasoning, and a guess. The first thing it met was a film in two halves, both deleted on purpose, replaced by a single file. It refused, and the person was right.

So it checks the actual question now: **can that title's location be read, right now?** A drive that is merely asleep answers plainly and nothing is removed. A location that reads fine and does not hold the file is evidence the file has gone. That is a stronger promise than the old rule, and it lives on the server where nothing can go around it.

### Missing entries can be cleared in bulk after a scan

Off by default. A file that has gone leaves its entry behind, marked missing, so a drive that failed to mount does not cost you a library. Turn this on and a scan removes those entries.

It removes **entries, not files** — along with their watch history, positions and ratings, which is why it asks rather than doing it quietly.

A scan that could not read a location, or that saw no files at all, leaves them alone whatever the setting says. A share remounted at the wrong path reads perfectly and holds nothing, and that is a statement about the walk rather than about the library.

### The audio device list stops offering a choice it does not have

Reported as *"only shows Output 1"*. Browsers will not reveal your audio hardware to a page without microphone permission — the list of attached devices is a fingerprint. What it returns instead is a single anonymous placeholder standing for "system default", which the old numbering turned into "Output 1". Choosing it did nothing, because it already meant what it did.

The row says so now rather than presenting a picker with nothing in it. Where real devices are available, the picker works as before.

It does not offer to ask for the permission. Requesting microphone access in order to name a pair of speakers is a trade this player will not make on your behalf.

### Also

**One title can be refreshed on its own, and a show carries its episodes.** Correcting a single stale entry no longer means re-asking the provider about the entire library.

**A stopped conversion says why it stopped.** Sessions have always recorded when they started and said nothing when they ended, so a stream that quietly went away left no explanation anywhere.

### Not fixed

A collection played with *Play all* that stopped after the first film could not be reproduced. Membership, ordering, the queue, manual and automatic advance, and resuming after a thirteen-minute pause were all tested and all behaved. The logging added here should explain it if it happens again.


---

## v0.8.27 — 2026-08-30

**One title can be asked about on its own, and a scan finally says how long it took.**

### Refreshing a single title

Correcting one stale title meant either **Fix match** — a manual search through candidates — or refreshing the whole library, which is about 1,480 provider lookups to fix one row. The endpoint for doing it to a single title had existed since metadata did, and nothing in the app ever called it.

Those are two different jobs and both are worth having. Fix match is for a title matched to the *wrong work*. This is for one whose details are merely *stale* — a poster changed, a description corrected upstream — where asking again would get it right on its own.

**A show carries its episodes.** Refreshing a series used to update the series and leave every episode exactly as it was, which is not what those words mean.

It answers with how many titles it re-asked about, which is the only thing an action that runs in the background can honestly report. On a series that number is the interesting part: being told **1** means its episodes are locked or cannot be matched by any provider, which was invisible before.

Titles whose match you locked are left alone, as everywhere else.

### A scan says what it cost

The log recorded what a scan **found** — files seen, items changed, items missing — and nothing about how long it took, which is the one number that decides whether a scan is worth making faster.

It now reports the duration in milliseconds, on failures as well: a scan that failed after forty minutes and one that died on its first directory are different problems, and the log said the same thing about both.

The measurement that prompted it is worth passing on. **An unchanged 9,276-track music library now scans in about two seconds**, against roughly 89 before v0.8.21 stopped re-reading every track's tags. So the idea of skipping unchanged folders is closed rather than postponed — there is nothing meaningful left to save, and it had been proposed against the old number.


---

## v0.8.26 — 2026-08-29

**One unplayable file stopped costing a browser every codec it can decode, refreshing metadata stopped being all-or-nothing, and a renamed file's leftover row can finally be tidied away.**

### 130 conversions that never needed to happen

When a file failed to play directly, the client withdrew **every** codec claim it had — not the one that failed. A media element's error does not say which codec let it down, so the safe answer was to disbelieve all of them.

The result on a real server: one TrueHD or DTS file, formats no browser decodes and no claim covers, took HEVC, 10-bit HEVC, 10-bit H.264, AC-3, E-AC-3 and both MP4-carried lossless formats down with it. Its own log priced that at **130 conversions stating "video codec hevc is not supported", on a machine that decodes HEVC in hardware**, plus 81 more for AC-3.

**This had been diagnosed once already, and the diagnosis was wrong.** A comment in the code describes the identical state — *"every claim the client is capable of making"*, *"all four denials were false"* — and blames how long the denials lasted, which was fixed by expiring them after a fortnight. That shortened the damage without touching what caused it, and the same machine reported it again months later with all seven claims down.

A failed file now costs only the claims its own streams actually used. A file whose picture is ordinary H.264 cannot cost you HEVC. A file nobody has probed costs nothing at all, rather than everything — which is the one door the old fault would otherwise come back through.

### The panel that would have shown it said two things at once

Two separate things turn a codec off: one happens automatically when a file fails and expires by itself, the other is you ticking a box and is permanent. The summary described the first using the word that belongs to the second, printed directly above those same codecs shown as **on**.

Both statements were true about different things, and nothing said which was which — so a panel reporting a real fault read as a panel contradicting itself. They are named apart now, and a browser claiming nothing at all says so outright instead of leaving it to be worked out.

### Refreshing metadata is scoped, and says what it costs

It cleared the stamp for a whole library: about 1,480 provider lookups for a real film library, five minutes of work to fix the handful of rows somebody meant. That is how a useful action becomes one people avoid.

Two scopes now — everything, or only what never matched — each **priced before you choose it**, the way clearing watch history already is. The count and the action share one query, so a preview cannot describe a different set from the work.

Both scopes leave alone the kinds no provider can ever answer for, and anything whose match you locked. Counting the first would quote twelve thousand rows on a music library and move none of them; requeuing the second would undo a decision you made.

And it now answers with how many items it queued. That was the one control on the pane whose success was indistinguishable from doing nothing.

### A leftover row from a rename can be forgotten

The report of works claimed by more than one file could be read and never acted on. On a real library **34 of 43 were one file missing and one present** — the signature of renaming files into a proper convention.

The rule that nothing is merged, ranked or deleted stands, and nothing here chooses between two files. One of those two rows describes a file that **no longer exists**, so forgetting it removes a leftover rather than making a judgement.

The offer appears only where another copy of the work is still present. When a drive goes away every row goes missing together — so it vanishes exactly when it would be dangerous, and appears only when the work itself is safe.

### The profile counts viewings

A film watched twenty times was counted once. The tally of repeat viewings existed and the statistics went on summing the flag beside it, so the number it exists to produce was discarded at the moment it was read.

Finished titles and total viewings are separate figures now, and time watched counts a runtime per viewing. A title whose runtime is unknown is still counted once whatever its tally says — there is no measurement of how long one viewing of it was, and inventing time upward is worse than missing it.

Clearing your history keeps the tally, for the same reason it already kept the rest.


---

## v0.8.25 — 2026-08-29

**A live channel could not start itself, "are you still watching" could never fire, and the duplicated transcode sessions finally have an instrument pointed at them.**

### A channel that was ready and never asked

Selecting a live channel reached `readyState 4` with ten seconds buffered and sat at `0:00`. Not starved, not stalled, not refused — **ready, and never asked to play**.

`preroll` is what presses play, and it was stopped from running on anything but the old progressive transport. That was right about the half of its job it is named for: its head start is a guess about a transport that cannot report how much it holds, and a segmented one replaces that guess. It missed that preroll was doing **two** jobs. Nothing took over pressing play.

Playing is now wired for every transport, triggered by the element having media rather than by any library milestone, with no head start — the reading that found this had ten seconds in hand.

### Why it took four attempts

The decision record said the native-HLS path "has unit tests and has never run", because there is no Safari on the machine this was built on. **It is the only path that machine takes.** Chromium answers `maybe` for the HLS media type, the transport chooser reads any non-empty answer as Safari, and the desktop client is handed the playlist every time.

Three fixes were written into the other branch on the strength of that note, each a more careful theory than the last, and **not one of them executed**. They could not be told apart because every wrong answer draws the same picture: a refused `play()`, a `play()` that never happened, and metadata that never arrived are all a channel at `0:00`.

What ended it is a line under the player naming the transport, whether metadata arrived, whether play was asked for, and what refused it:

```
native-hls · metadata no · play not asked · ready 4 · buffered 10.0s
```

One reading answered what four rounds of reasoning had not. It stays, as a feature rather than scaffolding — a channel that will not start is a thing viewers meet, and *the picture never arrived* and *the browser refused to start it* send them to completely different places.

### "Are you still watching" was unreachable

It needed three consecutive automatic advances **and** two hours of them. Three episodes of an ordinary drama clear the count long before the clock, so the prompt never came — it was tested by hand and never fired once.

The clock is gone. Three things nobody chose are exactly as unattended at six minutes each as at two hours each. What keeps the prompt away from someone who is plainly there was never the duration: it is the reset, and choosing, seeking or pausing all clear the run.

### The duplicated transcode sessions get a reading

Two progressive sessions on one item, milliseconds apart, both starting at zero. It is in the log repeatedly and has never been explained — because the log recorded every session's birth at `INFO` and a superseded session's death at `DEBUG`, which is off on a normal server. A doubled start was recorded as two births and nothing else.

The supersede line is now `INFO` and carries how long the session lived and **how many bytes it served**. Zero bytes in a few milliseconds is a media stack opening the source twice; real bytes is a player that genuinely asked again. Different faults, different fixes, and neither choosable without the number.

The client half was measured rather than argued: a new test counts source requests through the real player and finds one press of play is one request, a track change adds exactly one, and moving between films after picking an audio track adds exactly one. A negative result, kept deliberately.

### Also

The playback statistics panel stopped painting itself across the film's title. Built the day before and correct in every assertion the test suite makes about it, because that suite performs no layout — the panel and the title were both exactly where they had been told to be, on top of each other.

Recorded and not fixed: pausing a converted film for several minutes costs a whole new conversion on resume. The response is one un-resumable stream, so a long pause loses the connection and the only recovery is to ask again at the current position. There is no fix on the pause path; segmented output is the fix.


---

## v0.8.24 — 2026-08-28

**The server stops letting you reach wrong conclusions about it, and skipping ahead is about twice as fast.**

### Setting up a new server offers the media tools

Most video files need converting before a browser can play them, and LANcast uses ffmpeg to do it. Without it, most of a library will not play — and there was nothing on screen to say so, which is how people concluded the software itself was broken.

The setup screen now offers to download it, ticked, stating what it is, how large (about 160 MB), where it comes from and under what licence. Untick it and nothing is fetched; you can install it later from Settings.

Nothing is downloaded without you pressing a button that said what it would do.

### An existing server says when it cannot convert

If ffmpeg is missing on a server that is already running, the player now says so instead of showing a black screen — and says something useful depending on who you are. An administrator is told where the button is. Anyone else is told what to ask for, rather than being sent to a settings page they cannot open.

Live TV says it once, above the channel list, because every channel needs converting.

### Titles whose files have gone

If a file has been moved, renamed or deleted, LANcast keeps the title and marks it missing rather than removing it — so an unplugged drive never destroys your library. But the page still offered to play it, and pressing Play failed with a message about the server being busy.

It now says the file is not there and that it comes back on its own if the file returns. Search no longer offers these either, which is how one turned up in the first place.

### "MKV is not supported" is gone

It was never true in the way it read. MKV cannot be played by a browser directly, so LANcast repackages it — the container is rewritten while the video and audio are copied untouched. It is the fastest thing the server does, which is why those files start quickly and look perfect.

The message now says it is repackaging and that nothing is re-encoded, instead of reporting the reason in the server's own words.

### Skipping ahead returns twice as fast

Seeking in a converted file restarts the conversion at the new position, and it was waiting about ten seconds' worth of video before showing anything. Measured: roughly 2.3 seconds to first picture before, about 1.1 seconds now.

### Notes

No database changes. Nothing to do after updating.


---

## v0.8.23 — 2026-08-28

**4K files can be converted again.**

If a file needed converting *and* was larger than about 1080p, the conversion failed before it started — a spinner, then nothing, on a file that plays perfectly well in other players. Nearly half the titles in a large library are big enough to have been affected, though only when they actually needed converting rather than playing straight through.

The cause was a single setting: every hardware conversion declared itself as H.264 "level 4.1", which is a promise about how large a frame can be. Anything bigger than 1080p breaks that promise, and the graphics card refuses to start rather than quietly coping. That level is now worked out from the actual size of the picture being converted.

Nothing changes for files that were already converting fine — 1080p and below get exactly the setting they had before.

### Notes

No database changes. Nothing to do after updating.

This was found using the playback statistics panel and the clearer failure messages added in v0.8.22 — the player said the conversion had not started, and the server log named the reason.


---

## v0.8.22 — 2026-08-28

**A film that plays badly can be told to stop, the picture can say what it is doing, and a conversion that was refused stops pretending to work.**

All three came out of chasing one stuttering film.

### If a film plays but stutters, you can now fix it

Some files play *badly* rather than failing — the picture judders while everything else looks fine. That happens when the browser says it can handle a codec and then cannot really keep up with it. Nothing fails, so nothing was ever recorded, and the file kept being sent the same way.

**Settings → This app → Playback codecs** now lists what your browser claims and lets you switch any of them off. Turn one off and the server converts those files instead of sending them straight through. It stays off until you turn it back on — the automatic retry after a fortnight applies to failures, not to your decision.

Found on a real film: HEVC 10-bit played with a persistent stutter in two different browsers, while the same file converted to H.264 played perfectly.

### The player can tell you what is happening

**Press `i` during playback** for resolution, frame rate, dropped frames with a percentage, and how much is buffered — plus a plain line when the picture is genuinely losing frames.

This is the difference between "the picture is stuttering because frames are being thrown away" and "the picture is stuttering for some other reason", which look identical from the sofa and need completely different fixes.

If your browser will not report frame statistics, it says so rather than showing zeroes.

### A conversion that cannot start now says so

If the server refused to convert a file — because it is already converting as much as it can, or because ffmpeg is missing — the player used to sit on a spinner for ever, which looks exactly like converting slowly. It now stops and tells you, and the server writes it to the log with how many conversions are running.

### Notes

No database changes. Nothing to do after updating.

The statistics panel and the refusal message have not yet been through much real use — they are diagnostics rather than playback changes, so nothing that currently works is affected either way.


---

## v0.8.21 — 2026-08-28

**Live TV gets the transport it should always have had, a music scan stops re-reading your whole library, and a film can be watched twice.**

### Live TV

**Channels can now be served as a segmented stream (HLS) rather than one endless response.** The server always had the machinery and it had never been pointed at live — and when it was, it turned out not to work: for a channel, ffmpeg was told the stream was a complete, finished recording, so it wrote perfectly good video and **never wrote the playlist a player needs to find it**. Measured on a real channel: nine segments produced in 60 seconds, no playlist, no errors, nothing failed. One flag.

**Improved live TV playback** is a new setting under Settings → This app, **off by default**. With it on, channels play through a segmented pipeline that can see how much video it is holding, instead of guessing — which is what every stutter workaround in the player exists to compensate for. Expect less buffering and less drift behind live.

Turn it off and channels play exactly as they always have. It falls back on its own if your browser can't do it, and Safari (which speaks this format natively) gets the simpler path.

This is new and has not been through weeks of living with it — that is why it is off by default and why the old path is one toggle away.

**hls.js is now vendored** to make that possible. It is checked in as a bundle built here from a pinned commit and verified byte-for-byte identical to what the project published — provenance confirmed rather than trusted. Telemetry (CMCD) and DRM are switched off explicitly rather than left at their defaults.

### Scanning

**A music library that has not changed is now scanned in a fraction of the time.** Two things were being redone from scratch every scan:

- Every track was checked for subtitle files sitting beside it — reading its folder twice per track, to look for subtitle formats that cannot apply to audio at all.
- **Every track in the library had its tags re-read on every scan**, even when nothing had changed. On a 9,000-track library that is around 94 seconds, and the scans doing it reported "0 changed" every time.

Tags live inside the file, so if nothing on disk moved, nothing needs re-reading. A scan interrupted partway still does the full pass, so a half-finished scan is never trusted.

### Watch history

**A title can now be watched more than once.** Watched was a yes/no, so a film you have seen twenty times and one you saw once looked identical to the server.

Finishing something again counts again — nothing to press, no mode to be in. Start a film you have already seen, watch it to the end, and it counts. A detail page says "Watched 4 times" from the second viewing onward; nothing appears on posters or shelves.

Marking a title unwatched **keeps** the count. Putting something back on your list is not a claim you never saw it.

Counts start at 1 for anything already marked watched when you upgrade. History that was never recorded cannot be recovered, and 1 is the honest minimum rather than a guess.

### Notes

Includes a database upgrade (schema revision 31). It runs on first start and needs nothing from you.

The live TV setting reports clearly if your server is older than this release rather than blaming the channel — relevant if you update the app before the server.


---

## v0.8.20 — 2026-08-27

### No more scrollbars on the home page

Shelves page with **floating arrows** instead. They appear when you point at a row, and only on the side there is actually more to see — so a row with nothing further to the right does not offer to take you there.

Scrolling still works exactly as before: trackpad, shift-and-wheel, and touch drag are all untouched. Keyboard navigation never used the scrollbar and is unchanged.

### Clearing your watch history no longer erases your statistics

Reported against v0.8.19, and worth explaining because it is a real distinction.

Clearing your history used to zero everything on your profile — things started, things finished, time watched — because those totals were counted from the same records the reset deletes. Somebody with hundreds of hours watched was shown nothing at all.

Those are two different things, and only one of them was ever asked for. **"Forget what I watched"** is about the *list* — which you may want gone because a shared account watched something for you, or because the server is changing hands. **"I have never watched anything"** is a claim about you, and nobody asked to make it.

So a reset now keeps your totals and forgets the list. The history really is gone; the numbers stay true.

Clearing the statistics *as well* will be a separate, opt-in choice. It is not built yet, and until it is your totals are safe.

### Cast faces are bigger again

**120 pixels**, up from 96 — and the images behind them are fetched at higher resolution, because enlarging the circle without that would have made them softer rather than better.

This is as large as they go without loading noticeably heavier images on every page.

### "Are you still watching" is harder to annoy

Pausing or skipping now counts as being there, so the prompt will not appear shortly after you were plainly at the remote. Previously only pressing Next reset it.

### Upgrading

Nothing to do. Faces already downloaded keep the images they have; new ones arrive at the better resolution as your library is enriched.


---

## v0.8.19 — 2026-08-27

**Three of the five features in v0.8.18 could not be reached.** This fixes them, and adds the thing the cast work was missing.

If you installed v0.8.18, this is the one to take.

### Clear watch history now exists

v0.8.18's notes described this feature. It shipped **with no user interface at all** — the server could do it, nothing in the app could ask. That was wrong of the release notes and it is fixed here.

**Settings → Account → Clear watch history.** Three choices:

- **Everything**
- **Only what I finished**
- **Only what I did not finish**

It tells you how many records it will forget **before** you confirm, it only ever touches your own account, and it is recorded in the audit log. There is no undo.

### "Are you still watching" was invisible

The prompt was being drawn into the player's controls, which fade out while nothing is being touched. Since the prompt appears precisely *because* nothing has been touched for hours, it was never visible: playback would simply stop, with nothing on screen explaining why.

It brings the controls back now, so the question is actually asked.

### Cast faces are filters

**Click a face on a film or show page** and the library filters to that person. Alongside the existing cast search in the library, that gives two ways to the same place.

It filters by the *acting* credit specifically, so clicking an actor who also directs gives you the films they are **in**, not everything they touched.

Each face is a normal link — it opens in a new tab, copies as a URL, and comes back with Back.

### Cast faces are also bigger

**56 pixels to 96.** The first size was too small to recognise a face in, which for a picture of a face rather defeats the point.

### Searching cast across your whole library

"Everything this person is in" no longer stops at the boundary between your films and your television. The search could already do it; the API could not express it.

### Why this needed a release of its own

None of the three was caught by the test suite — 42 Go packages and 418 tests, all passing — because **each layer was correct about the half it owned**. A database function nothing calls compiles and tests clean. So does an endpoint no screen uses. There was no test that asked "is this connected to anything", which is exactly the question that was missing.

It was found by opening Settings and looking for the feature.

The cast row has been pulled into its own component so that question can be asked, and five new tests ask it. **426 tests, up from 418.**

### Upgrading

Nothing to do. Actor images appear as your library is enriched — Settings → Metadata → Refresh metadata brings them sooner.


---

## v0.8.18 — 2026-08-27

Five features from the backlog, and two faults found while building them.

### Actor images sit beside the names

A cast list is faces now, not just text. Where a headshot exists it is shown; where it does not — which is most of a cast list below the billed few — you get initials in the same circle, so the row reads as a cast list rather than a row with holes in it.

Fetched once per person and never again, so a large library does not re-download unchanged faces on every metadata pass.

### Cast search actually works

Searching people by name **while filtered to a role returned nothing at all**. Since the cast picker always filters by role, that was every search anyone had ever typed into it — and an empty list looks exactly like "no matches", which is why this was reported as a thin feature rather than a broken one.

Beyond the fix, searching is better than it was:

- **Substring matching.** "niro" now finds Robert De Niro. It previously matched only the start of a first name or a surname, so it found nobody.
- **Every library at once**, where the question calls for it — "everything this person is in" does not stop at the boundary between your films and your television.

### Watched history can be reset

Three options, because "clear my history" means three different things:

- **Everything** — handing the server on, or starting a rewatch.
- **Only what I finished** — a shared account watched something for somebody else.
- **Only what I did not finish** — the film that autoplayed while nobody was in the room.

Those are separate on purpose: forgetting a show you finished should not lose your place in the one you are half way through.

It tells you **how many things it will forget before it does it**, it only ever touches your own account, and it is recorded in the audit log. There is no undo.

### "Are you still watching"

Playback now stops and asks after several things have played automatically over a long stretch.

It counts **automatic advances**, not whether you have touched anything. Sitting still through a two-hour film you chose is not inattention, and a prompt that interrupts you for it is the version of this feature everyone hates. It also needs *both* a count and a time — three short episodes back to back is an evening, not an empty room.

It asks **before** starting the next thing, so nothing is transcoded and nothing is marked watched on your behalf while you are not there.

### Developer tools

The desktop window can open the web inspector, from **Settings → This app**. Off by default, and it starts with the window the next time you open LANcast.

For diagnosing the client itself — until now that was done by reading the server log and inferring what the page must have done.

### Also

- The transcode log records where each stream began, which makes a run of sessions on one film readable instead of a wall of identical lines.

### Not verified

**None of the five features was run in the app before release.** The test suite covers them — 418 client tests and 42 Go packages, all passing — but this project's own history is that the interesting faults are found by using the app, and nothing here was.

Specifically: nothing has watched a headshot being fetched end to end, and the test environment renders no layout, so the cast row and the "still watching" prompt have been reasoned about rather than looked at. Neither is believed broken; both are named because a release note that hides this is worth less than one that does not.

### Upgrading

Nothing to do. Actor images appear as your library is next enriched — Settings → Metadata → Refresh metadata brings them sooner.


---

## v0.8.17 — 2026-08-27

Two faults that had been running for an unknown length of time, neither of which could be reported — in both cases the software behaved correctly at every step and the outcome was wrong.

### Live TV: audio drift, and one channel that froze

`frag_every_frame` — chosen deliberately so a long-GOP channel would not sit on a blank screen waiting for its first keyframe — was emitting **non-monotonic, duplicated timestamps on every live channel it produced**.

Measured against a *fixed* 40-second capture, so it was the flag and not the stream:

| movflags | ffmpeg warnings | video DTS |
|---|---|---|
| `frag_every_frame` | **2,192** | duplicated |
| `frag_duration 200ms` | **0** | clean |

A browser demuxer requires DTS to increase strictly. The visible symptom is **audio drifting out of sync**; on one channel it was a picture frozen at `0:01` for eighty seconds while ffmpeg stayed healthy and went on producing bytes for three more minutes.

The fragment interval moves off the frame and onto the clock (`-frag_duration 200000`), which keeps the reason the old flag was chosen and drops the damage, for 0.9% more bytes. Verified as the **installed service** rather than from a shell.

### Codec support you already had, switched off invisibly

A capability claim that failed once was withheld **for ever**. One machine's store held every claim the client can make:

```
["hevc", "hevc10", "ac3", "eac3"]
```

It had been serving a full 4K HEVC re-encode **and** an audio re-encode for every such film — a core per viewer. Clearing it made the same file **direct-play with no ffmpeg at all**, so all four were false.

Falling back to a transcode is *correct behaviour*, which is exactly why this was invisible: a permanently downgraded machine is indistinguishable from a working one, and the only symptom is a server that seems to work hard.

Now:

- Denials carry a timestamp and **lapse after a fortnight**.
- The old format is read as **expired**, so every install gets one retry on upgrade. If your machine had written codecs off, this release gives them back.
- **Settings → Display** can clear them by hand — for anyone who has just installed a codec extension and would rather not wait two weeks.

A denial is a fact about your machine *today*. A driver update, a WebView2 update, a codec extension being installed — none of them can announce themselves to the client, and a denial that cannot expire will never notice any of them.

### 10-bit H.264 stops being re-encoded

High 10 was excluded from every profile on a comment reading *"not supported by browsers"* — taken for years as a fact about browsers, and actually a fact about nobody having asked. Chromium answers `probably` for `avc1.6e0033`, and High 10 is most of an anime library.

A client that can decode it now claims `high10` and direct-plays it instead of receiving a full video re-encode.

Unlike `hevc10`, it trusts no *native* profile listing: H.264 is listed by every profile, so the same rule would grant High 10 to a set-top box on the strength of it decoding 8-bit.

### Lossless survives a container rewrite

Rewrapping an MKV used to turn FLAC into AAC to change a box — the one conversion here that cannot be undone.

`flacmp4` and `opusmp4` let a client that can read FLAC or Opus *inside* MP4 have them copied instead. Gated on a claim rather than widened for everyone, because "can decode FLAC" and "can decode FLAC in MP4" are different questions and only the first is true of every browser. ALAC needs no claim, MP4 being its native home.

### Also

- The collection poster button no longer carries a redundant label.
- A test that had been failing CI on unrelated pull requests while passing on every developer machine — twice waved through as flaky before anyone read it properly — is fixed.

### Not verified

Three things in this release were tested but never looked at, and they are recorded here rather than quietly shipped:

- The **denial reset row** on Settings → Display has tests for its wiring and none for its appearance.
- A **High 10 file** has not been played with the claim active.
- An **MKV + Opus** file has not been watched remuxing with `audio=copy`.

None is believed broken. All three are the kind of thing a test suite cannot see, which is why they are named.

### Upgrading

Nothing to do. If your machine had quietly written off HEVC or AC-3, this release retries them once.


---

## v0.8.16 — 2026-08-26

### The "Change poster" option is actually reachable now

v0.8.15 added a way to choose which of its films a collection wears, and
on the collections that most needed it there was no button to press.

A collection's own page was not applying the borrowed-poster rule — only
the grid was. So a collection with no artwork of its own showed a poster
in the Collections grid and **nothing at all** on its own page. The
"Change poster" control lives on the poster, so with no poster there was
no control.

Both halves are fixed:

- A collection's page now shows its borrowed poster, the same one the
  grid shows. Galleries had the same problem and are fixed with it.
- The control appears even when there is no poster at all, so a
  collection can always be given one.

Hover a collection's poster to change it.


---

## v0.8.15 — 2026-08-26

### Pick a collection's poster

A collection without artwork of its own borrows its earliest film's
poster. That is a sensible default and it is not always the right face
for a franchise — the Marvel Cinematic Universe wearing Iron Man is the
obvious example.

**Click a collection's poster to change it.** You get a grid of every
film in the collection, and picking one makes it the collection's face.

- Posters rather than a list of titles, because that is how you actually
  decide.
- **Your choice sticks.** It is recorded the same way a corrected match
  is, so a rescan or a metadata refresh cannot quietly put it back.
- **You can undo it.** "Use the default again" returns the collection to
  the automatic choice — and the automatic choice keeps improving, so a
  franchise whose first film you add later will start wearing that.

The button appears on hover, and only for an admin: there is one poster
and everybody on the server sees it.


---

## v0.8.14 — 2026-08-26

### Collections no longer show films you have moved or renamed

A collection could list the same film two or three times. They were not
duplicates — they were files you had renamed or reorganised.

When a file moves, LANcast marks the old entry as missing rather than
deleting it, so that an unmounted drive can never wipe out your library.
Those entries kept their place in the collection, so a franchise you had
tidied up showed every film twice: seventeen tiles for nine films, in
one real case.

Collections now list only films that are actually there. The header
count and the grid agree again.

**Nothing has been deleted.** If a drive comes back, or you move files
back, they reappear exactly as before.

### Collections without their own artwork get a poster

Some collections have no image to fetch — the Marvel Cinematic Universe
is built from the provider's tagging rather than being a record with a
poster behind it, so its tile was blank.

A collection with no image of its own now wears its **first film's**
poster. The MCU shows Iron Man (2008).

It uses the earliest film rather than the best or newest one on purpose:
a tile that changes its face every time you add a film reads as a fault,
and the first film in a franchise is usually the one that names it. A
film that is missing is never used for this.

### Both take effect immediately

Neither needs a rescan or a library refresh — install and they are done.


---

## v0.8.13 — 2026-08-26

### Collections play in release order

A franchise is a sequence, and it was being listed alphabetically —
which put *The* Final Destination (2009) after Final Destination 5
(2011), because it sorts under T.

Collections are now ordered by release year. A film whose year is not
known sorts to the end rather than the front.

### Marvel Cinematic Universe

Your metadata provider only ever says which *narrow* franchise a film
belongs to — Iron Man Collection, Blade Collection, The Avengers
Collection. There is no field anywhere that says "Marvel Cinematic
Universe", so LANcast never had one.

There is now, and it keeps itself up to date. It is built from the
provider's own tagging rather than from a list, so:

- It runs from **Iron Man (2008)** through everything currently tagged.
- **New films join on their own.** Films are usually tagged before they
  are released, so a new one becomes part of the collection as soon as
  you add the file — nothing to edit here.
- A film keeps its narrower collection too. Iron Man appears in **both**
  Iron Man Collection and Marvel Cinematic Universe.

Note that Marvel's "phases" are not part of this. They are a marketing
structure rather than something any provider records, so there is
nothing to build them from.

**This appears after a library refresh.** Films already in your library
were fetched before this existed, so their tags are not known yet — open
the film library and press Refresh once.

### Also

Collection filter chips no longer offer franchises the Collections page
does not show, and they count only films that are actually present — so
a franchise whose second film is on a disconnected drive stops being
offered while it is unreachable.


---

## v0.8.12 — 2026-08-25

### When two files claim to be the same film, LANcast now tells you

Nothing about it is fixed automatically, and that is deliberate.

A new section on the **Review** screen lists works that more than one
file claims to be. For each one you get both paths, both sizes, and what
each filename claimed to be — and a **Compare bytes** button that reads a
sample of each file and says whether they look identical.

**Nothing is merged, ranked, hidden or deleted.** Two files sharing an
identity can mean four completely different things: a redundant copy, a
genuine second edition, one film split across two discs, or a file that
is simply wrong about what it is. Only you can tell which, and a server
that quietly picked one would hide the problem instead of showing it —
including, in the library this was built against, a 1989 film that had
been wearing a 2022 film's identity for years.

The byte comparison samples rather than reading whole files, so it is
fast even on a 14 GB remux. It says "identical, so far as sampled"
because that is exactly what it checked. A file it cannot open is
reported as unreadable rather than as different.

The report is **admin-only**, since it shows file paths.

### Edition markers are kept

A file named `Blade Runner (Director's Cut).mkv` has always had the
marker stripped so it matches the right film. The marker is now kept as
well, and shown — so two editions of one work can be told apart instead
of appearing as two identical rows.

It is shown, not believed. The file that prompted all of this called
itself an alternate cut and turned out to be a byte-for-byte copy of the
theatrical version.


---

## v0.8.11 — 2026-08-25

### Right-click menus work near the edges of the window again

Right-clicking a poster in the last row of a grid, or hard against the
right-hand side, appeared to do nothing.

The menu was opening the whole time. It was being drawn *underneath* the
docked player in the bottom-right corner — and not by chance: a menu that
would run off the edge of the window gets moved back on screen, which
puts it exactly where the docked player sits. So the tiles most likely to
need moving were the ones certain to end up hidden.

Menus now sit above the docked player. The full-screen player and the
photo viewer still cover them, which is correct — those own the screen.

### The database gets much smaller

The cleanup added a few versions ago was dropping cached metadata after
30 days. Measured on a real library, that cache turned out to be **80 MB
of a 102 MB database** — and nothing in it was old enough for a 30-day
rule to remove. The database would have kept growing for another
fortnight and then lost most of its size all at once.

Cached provider responses are now kept for **7 days**. That still covers
what the cache is for — a scan or an import enriching a lot of titles at
once — and stops it holding tens of megabytes for months. Every entry is
re-fetched on demand, so nothing is lost but a request.

On the library this was measured against, the first cleanup frees around
**70 MB**.

It also takes effect **promptly** now. Changing a retention setting used
to do nothing visible for up to a day, because the server remembered
having tidied up recently but not which rules it used. It records both,
so a changed setting is applied at the next check rather than tomorrow.


---

## v0.8.10 — 2026-08-25

### The database cleanup added in v0.8.8 now actually runs

It never did. Not once.

It was scheduled to run 24 hours after the server started — and the
timer went back to zero on every restart. A server that gets updated, or
a PC that gets rebooted, never reached 24 hours of continuous uptime, so
the cleanup that was supposed to keep the database from growing for ever
simply never happened.

It now remembers when it last ran, and checks the **date** rather than
how long the server has been up. A server that has been off for a month
tidies up shortly after it comes back; one that restarts every hour still
tidies up once a day. The first check waits five minutes after startup,
so nothing heavy competes with the app you just opened.

If you have been running LANcast for a while, expect the first run to
report a number and the database file to get smaller.


---

## v0.8.9 — 2026-08-25

### Shows play in order again

Reported as "Futurama is not playing in order". Nothing about Futurama
was wrong — the episodes are stored correctly and every screen displayed
them correctly. **Shuffle was on**, left over from somewhere else.

Shuffle is deliberately a session setting: coming back to the player from
the mini-player should not silently switch it off. But that rule was
applied to *every* way of entering the player, including the ones that
hand over a specific sequence — **Continue** on a show, pressing an
episode row, an album's track list, a collection's play. None of those
said anything about shuffle, so all of them inherited whatever was left
on, and played a correctly ordered queue in a random order.

The screens kept showing the right order the whole time, because the
shuffled order only ever existed inside the player. That is what made it
look like the queue was broken rather than a switch being stuck.

Handing the player a sequence now means "play this sequence". Turning
shuffle on explicitly still works, and returning from the mini-player
still leaves your shuffle alone.

To see it: press **Randomize all** on a film library, then **Continue**
on any show. Before this it played out of order; now it does not.

### Play all on a show got three fixes

The right-click **Play all** and **Mark all** on a show now use the
server's own episode list rather than walking seasons in the client. That
matters for shows whose episodes sit directly under the show, and for
shows where an episode has been given its own sort title.

Two more, for everything else with children:

- **Long containers are no longer cut off at 100 items.** A gallery with
  more than a hundred photos queued the first hundred and silently
  stopped.
- **Missing files are no longer queued.** A disconnected drive put
  unplayable entries in the queue, which stalled it rather than skipping
  past them.


---

## v0.8.8 — 2026-08-24

Four fixes. Three of them were found by looking at the running app.

### Randomize all no longer starts with the same film every time

It was as deterministic as it felt. Every "play all" navigated to the
first item and left the randomising to shuffle — but shuffle deliberately
**pins** whatever is currently playing to the front of its order, so that
turning it on mid-queue doesn't strand everything ahead of you.

Given a fixed first item, it pinned the same film every time. The shuffle
was real; it was a shuffle of everything *except* position one, which is
the only position anybody looks at.

Fixed for a library's **Randomize all**, a show's **Shuffle**, and the
container right-click menu.

### Films you skipped past no longer start at 0:05

Progress is saved every five seconds, and the only rule was "don't save
zero". So skipping through a queue to find something to watch left a
five-second bookmark on every film you passed — and each one then resumed
into it *and* turned up on Continue Watching, a shelf claiming you were
part way through things you glanced at.

A resume point now has to mean something: the smaller of one minute or 5%
of the runtime. Under that, playback starts at the beginning. The
proportional half matters for music, where a minute into a three-minute
song is a third of the way in.

**This repairs itself.** Films already carrying a stray five-second
position start at 0:00 on the next play — nothing to clean up by hand —
and they stop appearing on Continue Watching as they are played.

### A poster's tooltip no longer covers its own menu

Right-clicking a poster opened the menu, and then the browser drew the
film's title on top of it, hiding whichever item was under the cursor.
The tooltip now steps aside while the menu is open.

### The database stops growing for ever

Two things were kept indefinitely with no upper bound: the audit log, and
cached responses from metadata providers. Neither was ever looked at
again once it was old.

A daily pass now drops both, and **reclaims the space** — deleting rows
from SQLite doesn't shrink the file on its own, which is why this is a
real saving rather than bookkeeping.

Audit events are kept for **90 days** by default. You can change that in
Settings, and **0 keeps them for ever** if the audit trail is the point.
Cached provider responses go after 30 days regardless; every one of them
refetches on demand.

Crash reports are untouched — they are small, and an old one is often the
only record of a rare fault.


---

## v0.8.7 — 2026-08-24

### A channel that runs dry stops pausing every few seconds

The hold and the resume shared one threshold. `shouldHold` released *below*
`PREROLL_SECONDS`, `shouldStartPlayback` resumed *at* it — one boundary, tested
from both sides, with no gap between them.

A channel sitting near that boundary holds at 2.9 seconds, resumes at 3.0, has
that 3.0 eaten by the play head within three seconds, and holds again. **A pause
every few seconds, each one correct by the rule and wrong by the outcome.**

Resuming now waits for 5 seconds where starting still waits for 3. The
asymmetry is right on its own terms, not just as a fix: at start nothing is
known about the channel, and after a drought exactly one thing is — *it just
ran out*. Resuming on the evidence that was enough to begin with is how a
channel that stalls once stalls for ever. ExoPlayer draws the same line, 1
second to begin against 5 to resume.

**The head start does not move.** ExoPlayer's low start value assumes a player
that skips gaps and sits 30–60 seconds behind the edge; a bare media element
does neither, and one second in hand is the exact condition that caused the
original bug. Only the resume threshold changed.

### A proposed amendment to ADR 0013

The live TV path now has a written argument for adopting MSE — and only for
live TV — with hls.js vendored as pinned, reviewable source rather than pulled
from a CDN.

It is recorded as **proposed**, not accepted. The decision is not taken, and
the standing rule that this build ships no unaudited third-party player is
unchanged. The fix above is step one of that amendment and the only step that
does not depend on its outcome.


---

## v0.8.6 — 2026-08-24

### Every poster answers a right-click

Right-clicking a **show** used to give you the browser's menu. So did a season,
an album, an artist, a collection and a gallery — a container offered no
actions, so the tile drew no menu at all.

Containers now have their own list:

- **Play all** and **Shuffle**, gathered from the whole hierarchy — season one
  before season two, and a record's tracks in the order the record plays
- **Mark all as watched** / **unwatched** (played, for music)
- **Go to details**

A collection and a playlist deliberately keep only *Go to details*: their
membership is not a parent-child relationship, so there are no children to
gather, and a **Play all** that silently queues nothing is worse than none.

### Three things that existed and could only be reached from one place

- **Add to playlist** was on a track *row* and never on a track's poster
- **Remove from library** was on the detail page and a track row only — which
  left it unreachable for an **episode** and for a **photograph** entirely,
  because nothing in this client navigates to an episode's page and a photo has
  none. It stays admin-only, and it is always the last item in the menu.
- **Play from start** did not exist anywhere. "Play" resumed, so the one thing a
  progress bar invites you to want was the one thing the menu could not do.
  It appears only where there is a position to ignore, and it works by
  forgetting that position — so it clears the resume point on every device,
  which is what picking it means.

### Live TV says when it is catching up

A channel that has drifted is brought back by playing 10% faster. That is
subtle enough to be heard as the *stream* being wrong rather than as a
correction being applied — which is how it was reported after v0.8.5.

A small **Catching up** label now sits beside the channel name whenever
playback is above 1.0x, and disappears when it settles. If the audio sounds
fast and the label is there, that is the catch-up. If it sounds fast and the
label is absent, the catch-up is exonerated and the fault is elsewhere — which
makes the report answerable from the native window, where there are no
devtools.

The stream itself was measured over 293 seconds rather than the ~25 that cannot
see a half-percent error: **30.0 fps exactly**, audio at 46.87 packets/second —
exactly what AAC-LC at 48kHz requires — and **40 milliseconds of drift over five
minutes**, a steady offset rather than a growing one. Whatever is being heard,
the stream is not running fast.


---

## v0.8.5 — 2026-08-24

Live TV stops stopping. This release is one long investigation, and most of what it found was that earlier explanations were wrong.

### The player was pausing itself

The fault carried for months as *"channels play at the wrong speed"* was not speed, not timestamps, and not the server.

`hold()` — the code that waits for a cushion when a live stream runs dry — was wired to the browser's `waiting` event. On a progressive stream that event means *"I cannot render the next frame right now"*; it fires at fragment boundaries and on any brief hiccup, and it says nothing about how much media is in hand. Measured on a real channel:

```
waiting fired 113 times in 135 seconds     ~0.84/sec
buffered while paused: 117-142s           median 131s
time spent paused:  28.1%
effective playback: 0.76x real time
```

The player was being paused for **more than a quarter of every minute while holding over two minutes of media**. The buffering you saw every few seconds was never the symptom of a drought — it *was* the mechanism, and no amount of catch-up could outrun it.

The buffer decides now, not the event. After: **0.6% paused**, and the one hold left had 7 seconds in hand, which is exactly when it should hold.

### The server stops relaying a stranger's rhythm

A channel was passed straight through, so the provider's publishing pattern reached the browser verbatim:

| | before | after |
|---|---|---|
| gap p99 | **5,326 ms** | **152 ms** |
| gap max | **9,850 ms** | **153 ms** |
| silences over 1s | **7, totalling 41.7s of a 42s window** | **0** |

Ninety-eight percent of that window was silence between bursts. The server now reads ahead and hands the stream out at the rate the stream actually runs at, so the silence is absorbed on the side that can see it.

The price is **12.4 seconds to first byte, paid once**. That is the trade a jitter buffer makes, and for live television it is worth it: nobody can tell whether they are eight or fourteen seconds behind a broadcast, and everybody can tell when it stops. A channel takes a little longer to start and then it runs.

Because the server holds that buffer now, the client's own cushion came **down** from 12 seconds to 3 — the two add up, and paying twice for the same protection would double the startup for nothing.

### What was ruled out, by measurement rather than argument

Worth listing, because each of these was a plausible suspect and several were confidently blamed at some point:

- **The server's timing** — 29.9 frames per wall-second against a declared 30.
- **The audio** — 46.9 packets per second against exactly what AAC-LC at 48 kHz requires, with audio and video agreeing to within 70 ms across twenty channels.
- **The stream itself** — mean packet step 0.033 s, zero holes. The backwards video steps are B-frame reordering, not discontinuities.
- **`-fflags +genpts`** — the standing suspect since v0.6.46, exonerated twice, the second time against a real channel rather than a synthetic one.

**"Far too fast" was never reproduced**, and this release does not claim to fix it. If it is real it is a separate fault.

### Also

**A remux is no longer reported as a transcode.** The activity panel called every ffmpeg session "Transcoding for playback" whatever it was doing, so a live channel being *copied* — the common case — announced itself as a transcode. The two differ by an order of magnitude in cost. It also sent this investigation after an encoder that was never running, on the evidence of a badge, while the server log two lines away said `video=copy`.

**Seeking a live channel is gone.** v0.8.4 tried to correct drift by moving the play head. The live endpoint cannot be range-requested, so a seek that misses leaves the player waiting for bytes nobody will fetch — reported as a channel holding 22 seconds of media at `0:00` that would not restart however many times you pressed play. Drift is now closed by playing 10% faster until it settles, which cannot strand anything.

---

Server-only update: everything here ships inside `LANcast-Server.exe`, including the web client, so the in-app updater delivers all of it. No installer needed.


---

## v0.8.4 — 2026-08-23

### Live TV stays live

A channel drifted further behind reality the longer it ran. The player's own clock, read during the fault:

```
0:48 / 1:14      play head 26s behind the incoming data
1:23 / 2:17      54s behind, thirty seconds later
```

The play head advanced at 1.0x throughout — nothing was slow and nothing was fast. The **gap** grew, and nothing in the client had an opinion about it.

The cause was the buffering itself. When a live source runs dry the player pauses and waits for a head start, which is right; but it resumed **wherever the drought stopped it**. On a bursty provider — five-second silences are normal and measured — that happens several times a minute, and each pass moved the play head permanently further from live. The buffering you saw every few seconds was never the fault. It was the recovery, running again and again, each pass adding lag that nothing took back. The buffer grew without bound for the same reason.

A resume now lands near the live edge instead of in place, and a standing correction handles drift that arrives without a stall. Two thresholds rather than one: jumping to the very edge lands with nothing in hand and stalls at the next silence, which turns one visible jump into a permanent cycle of them. Ordinary buffering never provokes a seek, and a **paused** player is left alone — pausing is your decision, and the correction happens on the first tick after you press play.

Worth saying what this release does *not* fix: the roadmap's long-standing "channels play too fast" is still unreproduced. Measured against a wall clock the play head ran at 1.0x, and frame counts ruled out the timestamp fault that was suspected. If it is real it is a separate problem, and this does not claim otherwise.

### A remux is no longer reported as a transcode

The activity panel called every ffmpeg session "Transcoding for playback" whatever it was doing, so a live channel being **copied** — the common case — announced itself as a transcode. The two differ by an order of magnitude: a remux is a few percent of one core, an encode is most of one.

It now says "Remuxing for playback" when the streams are being copied. This mattered more than it looks: it sent the Live TV investigation above down the wrong path for an hour, on the evidence of a badge, while the server log two lines away said the streams were being copied.

### See who is watching what, on a paired server

The first half of Watch Together across two machines. Once two servers are paired, the People page lists the accounts on the other one, and a person can grant a **named** person there the ability to see that they are watching something.

What that discloses is bounded and does not grow: that you are online, that you are watching or idle, and the **work** by title — never the episode ("Cowboy Bebop", not "Cowboy Bebop S01E02"), never how far in, never music or photographs. It is **off until you turn it on**, it names one person rather than a server, and it is never written down: there is no presence history, no "last seen watching", and no route that could answer either. Revoking takes effect on the next poll, mid-film, and unpairing a server revokes everything on it at once.

Under it, the piece that was missing: peers can now actually talk to each other. Connections are mutually authenticated TLS with the other server's key pinned — the certificate presented must carry the key that arrived in the invite, or the connection does not happen. Fetching a peer's roster is what finally establishes that a pairing is mutual, so a peer stops being stuck at "added".

**Joining is not built yet.** Seeing that a friend is watching something is the whole feature for now; there is deliberately no button promising more.

### Faster HEVC playback

Chased from intermittent lag on a 1080p HEVC file that was not a bitrate or disk problem. Hardware decode returns 10-bit frames on the card, and a stale assumption in the pipeline copied every one of them back to system memory for a CPU conversion that no longer needed to happen. On a 10-bit source that had quietly become the most expensive thing in the chain.

Frames now stay on the card where nothing in the chain needs them off it.

---

Server-only update: everything here ships inside `LANcast-Server.exe`, including the web client, so the in-app updater delivers all of it. No installer needed.


---

## v0.8.3 — 2026-08-23

### Menus without a mouse

Press **`c`** — or the dedicated menu key — on whatever is focused, and you get the same menu right-clicking gives you. Arrows move through it, Enter chooses, Escape closes and puts you back where you were.

This matters most in bigscreen, which is driven by arrow keys for a remote that has no right button. Until now anything living only behind right-click did not exist there, and that had grown to include marking a track played and queueing anything at all.

The key is rebindable in Settings → Keyboard, because a remote's menu button sends whatever its maker chose. `c` is the default beside the dedicated key because plenty of laptops have neither that key nor a mouse worth using — and because it is what Kodi has meant by "context menu" for twenty years.

### The same menu everywhere

Right-click worked on a poster in a library grid and did nothing on the same poster in search, in a collection, or under a detail page. All four draw the same tile and only one of them offered anything.

They now behave identically. Shows, albums, seasons and galleries still offer nothing rather than a menu of things that do not apply to them, and photographs are left alone.

**Episode rows have one too** — play, mark watched, queue it, or open the episode's own page, which nothing else in LANcast could reach.

### A queue you can see

Queueing something used to put it somewhere invisible. Items queued by hand play before the queue resumes, but the queue panel only listed the queue — so the only evidence "play next" had worked was the track eventually playing.

The panel now has an **Up next** section listing what you queued, and anything there can be taken back off.

Removing works on the entry you pressed rather than the first one with that title, so queueing the same song twice and dropping one leaves the other where it was.


---

## v0.8.2 — 2026-08-23

### Play next and add to queue

Right-click a film, an episode or a track and you can now put it on the queue instead of replacing what is playing. It is the first thing the context menus offer that was not already reachable somewhere else — nothing in LANcast could queue anything before this, and every play action replaced the queue outright.

**Play next** puts it immediately after whatever is playing. **Add to queue** puts it behind anything else queued the same way. Both leave the queue itself alone, so a shuffled library stays in the order it was already playing rather than reshuffling because you added one song, and turning shuffle off still gives back the album order it always would have.

Queueing something with nothing playing simply plays it.

### Fixes

**A show resumed with Continue stopped after one episode.** Continue handed the player a single episode with no queue behind it, so there was nothing to advance to when it finished — the show simply stopped, and re-entering the player replayed the episode you had just watched. It now queues the episodes from that point onward. Play from the start was never affected.

**The "close and reopen" message could not be dismissed by closing and reopening.** When the window is older than the server, LANcast says so. It was telling everyone to restart even when there was nothing waiting to be applied — in which case reopening runs the same program, the versions still differ, and the message comes back for ever. It now says which of the two situations you are in, and only asks for a restart when restarting will actually finish something.

**A music library reported its number of artists as its number of items.** The libraries screen showed "1,158 items" for a library holding 9,276 songs, immediately after a scan that had just found all of them. It counts songs now, matching the sidebar, which was right all along. Picture libraries had the same fault and count photographs. Films and shows are unchanged, and deliberately: a film is one item, and a television library is counted in shows rather than episodes.

### Note

If LANcast tells you this window is older than the server, that message is now accurate — and if it asks you to reinstall rather than restart, reinstalling really is what is needed. The in-app updater replaces the server *and* the client, so this only arises when the two have been installed separately.


---

## v0.8.1 — 2026-08-23

### Fixes a broken v0.8.0

**HEVC titles would not play.** If you installed v0.8.0 and a film sat on a loading spinner for ever, this is why, and this release fixes it.

v0.8.0 added hardware decoding and asked ffmpeg to pick the method itself. On Windows it picked DXVA2, which needs a Direct3D device — and LANcast runs as a service, where there is no desktop and no Direct3D. The converter failed to start rather than falling back to software, so every title needing a video conversion produced nothing at all.

The decoder is now chosen to match the encoder already in use rather than guessed, so it uses the same graphics driver the server has already proven it can reach. AMD cards decode in software for now: that path has the same Direct3D dependency underneath and there was no AMD machine to verify it on, and an unverified guess is what caused this in the first place.

**If you are on v0.8.0, update.** Nothing else is affected — direct play, remuxing and audio-only conversion never touched this code — but anything that needed a video re-encode was broken outright.

### Right-click menus

The first context menus in the client.

**Continue Watching and Continue Listening.** Right-click a title for **Mark as watched** and **Remove from Continue Watching** — two separate actions, because they mean different things. Both clear the tile; only one records that you saw it. Marking a film you abandoned as watched would have every unwatched filter repeat that afterwards, and forgetting the position on one you finished elsewhere throws away the fact you watched it. The wording says which is which, and follows the item: a track offers "Mark as played" and "Remove from Continue Listening".

**Library grids.** Play, a watched toggle, and Go to details — on films, episodes and tracks. Shows, albums and photographs open no menu rather than a menu of things that do not apply to them.

**Track rows.** Play, mark played, add to a playlist, remove from this playlist, and remove from the library. Nothing was taken off the row to make space: the reordering arrows stay buttons because a remote cannot use a menu, and everything a row could already do, it still can.

Marking a track played is new — until now nothing in the client could set that, since a track has no poster and no page of its own.

Menus are mouse-only for now. There is no keyboard or remote route into one, so these actions are not available in bigscreen mode.

### Also

- A menu opened near the right or bottom edge of the window stays on screen instead of running off it.


---

## v0.8.0 — 2026-08-22

### Playback

**A paused film no longer skips to the next one.** Pause a converting film for more than ninety seconds and its converter was reaped underneath it; pressing play then jumped to the next title. A progressive stream carries no duration, so the browser reports a cut stream and a finished film identically, and the player believed it. Ending short of the known runtime is now treated as an interruption and playback resumes from where it stopped. A paused conversion is also kept alive for ten minutes rather than ninety seconds, so the ordinary pause costs nothing at all.

**Converting is substantially lighter on the machine.** Video was being decoded in software even when the encoder was running on the graphics card, which on 1080p HEVC is the expensive half of the work. Hardware decoding is used when hardware encoding is, falling back on its own for any file the card cannot handle. Measured over sixty seconds of a 1080p HEVC film: **8.6 CPU cores busy before, 3.1 after** — same throughput. Files needing HDR-to-SDR conversion gain less, because the tone map rather than the decode is the expensive half there.

**Randomize all gives a different order each time.** It shuffled once and then handed back the same running order on every press, for the life of the session.

**A seek no longer leaves its old conversion running**, and one viewer's playback can no longer be ended by another starting something. A conversion slot that is only being *held* — by a paused film — is now given up to somebody who wants to use it, rather than making them wait out the full idle timeout.

### Library

**Deleted titles leave the library view.** A scan correctly marked missing files as gone and the browse grid never heard about it, so a deleted film sat on screen with its poster until the client was restarted. Finishing a scan now refreshes the views a scan changes.

**The libraries pane no longer looks half-built.** Every library carried five buttons of equal weight — twenty-five controls on a five-library server, with Remove as prominent as Scan. Scan keeps its button; Edit, Re-read filenames, Refresh metadata and Remove moved into a per-library menu.

**Refresh metadata says something.** It was the one action that gave no feedback at all. All three per-library actions now answer in one place in the row, and a scan that was already running says so instead of failing silently.

**Adding a library scans it.** Settings has always said it did. It did not, so a new library sat at "0 items · never scanned" until the Scan button was found — which is indistinguishable from a library pointed at the wrong folder.

**Scan every library in one press.** The rescan timer had always done this internally; there was no way to ask for it by hand.

### Groundwork for Watch Together between servers

Phase 1 and 2 of the federation plan ([ADR 0044](docs/adr), [ADR 0046](docs/adr)). Servers now have a durable identity — an Ed25519 keypair and a fingerprint — and can be paired by pasting an invite, with pairings listed and removable in Settings.

**This grants nothing yet.** A pairing records that two servers know who each other are; it does not share a library, expose a person, or allow anyone to join anything. Watching together across two servers is still to come. Pairing is admin-only, because opening a network relationship is the same class of power as adding a library.

### For anyone building against the API

- **Schema revision 27.** The database migrates on first start; take a copy first if you keep one.
- `POST /api/libraries/scan` starts a scan of every library, reporting which started and which were already busy.
- `POST /api/libraries` now starts a scan of the library it created.
- New peer routes for listing, adding, and removing pairings.
- A documentation correction worth knowing: a second scan on a library already scanning answers `409` with the **running scan's progress**, not the `{ "error": … }` envelope every other failure uses. That has always been the behaviour; the documentation described a different one. Branch on the status, not the body.


---

## v0.7.4 — 2026-08-20

**The sharing toggle shows the state the server holds.**

Reported as the option that lets others see what you have watched not saving
when ticked.

**It saved.** The setting had been stored correctly all along. What it could
never do was read the value *back*, so the checkbox rendered unticked on every
mount however long ago you had opted in — and turning it on again simply wrote
the value it already had.

`GET /api/people` excludes the caller by design: a row for yourself in a list of
other people is noise you read past every time. The settings toggle looked for
itself in that list anyway, never found itself, and fell through to *off*. Local
state hid it until you left the pane and came back. No route reported the
caller's own setting at all.

For a privacy control that is the worst direction to be wrong in. It tells you
that you are private while you are sharing.

**If you had turned this on and found it off again, it was on.** Worth checking
Settings → Account now that the control tells the truth, in case it has been
sharing since you first tried.

### Fixed

- `GET /api/auth/status` now carries `user.sharing`, the caller's own
  activity-sharing choice. It reports only your own value, and a read failure
  omits the field rather than failing the route the whole app polls.

### API

Adding a response field is additive under ADR 0018 and needs no version bump.
`docs/api.md` records it.


---

## v0.7.3 — 2026-08-19

**The home screen fits the screen it is on, and the grid does what you tell it.**

### The hero was sized against height alone

`--hero-height` was `clamp(280px, 31vh, 380px)`. On a wide window that makes the
hero roughly a 5:1 letterbox, and a 16:9 backdrop dropped into it with
`background-size: cover` keeps about 40% of its own height — which is why every
hero read as a close-up of the middle of something. It is now
`clamp(300px, max(31vh, 23vw), 520px)`: `max()` takes whichever constraint is
larger, so a tall window keeps exactly the height it had and a wide one gets a
box the artwork can survive.

Everything downstream of that was sized for the old box:

- The crop is framed off the top third rather than the middle, where a frame
  puts its subject.
- The type column grew from `52ch` to `min(72ch, 58%)`. At 52ch every word sat
  inside the left third of a 1920 window and the remaining 60% was empty scrim.
- The synopsis clamp went from two lines to three. Two cut essentially every
  synopsis mid-sentence.
- The poster scales with the hero instead of staying at 150px — left fixed
  inside a taller hero it would have looked *smaller* than before the fix.

### Tile size is a control now

A stepped slider in the library header, six steps from 96px to 306px.

Discrete rather than continuous because the grid is
`repeat(auto-fill, minmax(N, 1fr))`: most pixel values between two useful column
counts render identically, so a continuous slider would spend two thirds of its
travel doing nothing visible, which reads as a broken control.

The default step is 160px — exactly what the grid has always been — so no
existing library resizes on upgrade. The setting is per device, on the same
reasoning as bigscreen: how big a poster wants to be is a fact about the screen
you are looking at, not about you. The chosen size is written as a custom
property on the grid element rather than the document root, so a size picked in
a movie library does not silently follow you into playlists, search and detail.

### Fixed

- **A container whose children have all gone no longer goes missing with them.**

### Design work: Watch Together across two servers

No behaviour change in this release. The design for two LANcast instances seeing
each other is now written down — a plan and four decision records:

- **ADR 0044** — server identity and peering. An Ed25519 keypair per data
  directory, out-of-band mutual introduction, peer connections pinned to the
  peer's key. Closes the half of ADR 0014 that ADR 0014 named and left open: it
  encrypts the wire, it does not authenticate the server.
- **ADR 0045** — live presence between paired servers, amending ADR 0035. Two
  changes, and only one was invited: sharing with a named person rather than
  everybody is the granularity ADR 0035 parked explicitly, while disclosing what
  somebody is watching *right now* is not, and the ADR says so rather than
  pretending otherwise. Presence is never persisted, there is no history, and
  there is deliberately no "last seen watching".
- **ADR 0046** — remote guests. A remote person is a principal, not an account:
  admitted by a ticket their own server signs, default-deny in middleware, and
  permitted to stream only the item the room is playing. A guest writes no
  state at all — no resume position, no history, no trending contribution.
- **ADR 0047** — remote streaming is capped by the host. Capability is the
  guest's; the ceiling is the host's.


---

## v0.7.2 — 2026-08-19

The season page is finished, and its episodes have pictures.

### Episode stills appear

They were there all along. `item_artwork` held **993 rows of kind thumb** — every Futurama episode among them — and the whole path from TMDB to the API had always worked. The tiles were blank because the poster grid asked for `artwork.poster`, which an episode does not have.

Which meant v0.7.1 shipped a bug: the new row put the content-addressed **hash** straight into `src`, where every other screen passes it through `artworkURL`. A hash is truthy, so the row took the image branch and rendered a **broken image on all 993 episodes that had a still** — strictly worse than the episode number it was meant to fall back to. If your season rows showed broken images rather than big numbers, that was this.

### Watched state is yours to correct

The tick on a row is a control now. A season page is where somebody fixes this — an episode watched on the television, or one the player marked finished while walking a queue — and until now there was **no way back from a wrong verdict anywhere in the app**.

Marking unwatched sends a **position of zero** along with the flag. Leaving the position behind would put the episode straight back on the Continue shelf, and the server's own rule (`watched := asked || past the threshold`) would not clear it either.

The mark control stays visible when off, rather than appearing on hover: a control that only exists under the mouse is one the focus model cannot find, and this is meant to work from a sofa.

### Continue on a season

The season header gains **Continue** and demotes Play all to secondary, matching the show page so the two do not disagree about what a season offers. No server change was needed — the next-episode query matches episodes by their parent, so a season id answers with that season's next episode.

### Spoilers are a setting, not an accident

**Settings → Display**, three choices, hiding the synopsis by default. An overview is written as a summary rather than a tease, so the next row down a season list gives away what you were about to watch.

The still stays at the default — a frame rarely gives a plot away, and it is what makes a row identifiable at a glance. The strongest setting withholds it too, reusing the typographic state that already existed for missing artwork.

**Protection applies only to episodes with no progress at all.** Two minutes in you have already met whatever the first scene gives away, and hiding it then is how a spoiler setting ends up switched off.

A withheld synopsis **says so** rather than leaving the line blank: silence reads as missing metadata, which is the exact failure this screen was built to stop looking like.

### Verified

- Go: full suite across 38 packages.
- Client: 251 tests across 37 files; lint clean.


---

## v0.7.1 — 2026-08-19

A season reads like a season, and a 10-bit film stops stuttering.

### Seasons are lists now

The season page drew episodes with the movie grid: 2:3 tiles, a title underneath, nothing else. An episode is not a poster — it is a 16:9 still with a synopsis, a runtime and a place in an order — and the reason the page looked unfinished was never the missing artwork.

Episodes are now **wide rows**: still on the left, number and title, runtime and air date and rating, synopsis clamped to two lines, and a progress bar **only when there is progress**. Pressing a row plays that episode and queues the rest of the season behind it.

Two absences are the design rather than omissions:

- **No bars on an untouched season, and none on a finished one.** Watched is a tick and a receding title, because a bar pinned at 100% is a fact nobody needs stated eleven times.
- **No grey rectangle where a still is missing** — the episode number, large, in the space the still will take. That is why this shipped before any artwork work: every row takes that path today and none of them look broken.

### A film that stuttered while its audio played perfectly

HEVC **Main 10**, direct-played, with no ffmpeg running anywhere — the WebView was decoding it and coping badly.

The client probes HEVC with `hvc1.1.6.L93.B0`, which is Main profile at **8 bits**, and the server read the resulting claim as covering Main 10 as well. The worst shape of this bug is that nothing fails: a direct play that *errors* records the claim and stops making it, but this one played, and no code anywhere can tell a smooth picture from a glitching one.

So the client now probes `hvc1.2.4.L120.B0` separately and sends **`hevc10`**, which the server treats as permission for a bit depth rather than a codec — exactly as 10-bit H.264 was already handled one codec along.

Scoped to *claims*: `tv` and `safari` list HEVC natively because they are device classes known to decode Main 10 in hardware, and demanding a claim from them would re-encode HDR for the clients that handle it best.

### Also recorded

**[season-page-plan.md](https://github.com/Conqueror-Mod/LANcast/blob/main/docs/season-page-plan.md)**, which settles two things beyond the layout:

- **Specials sort after every numbered season**, and — the half that matters — **Continue never lands on one**. A special sitting unwatched between two seasons is not what anybody means by "next".
- **Five arrangements** for whether a season stays a screen at all, proposing the Netflix-style selector with the season in the URL, because this project already holds every browse control in the URL so a filtered view is linkable and survives reload.

### Verified

- Go: full suite across 38 packages.
- Client: 237 tests across 36 files; lint clean.


---

## v0.7.0 — 2026-08-19

A minor bump, because browsing and shows both became different things this week.

### Browsing: nine filter categories where there were three

Genre, Decade, **Year**, **Actor**, **Director**, **Collection**, Content rating, **Rating** and **Status** — buttons that open a panel, with chips where the set is small and a type-ahead where it cannot be listed. A library has a dozen genres and thousands of credited people; those are not the same control.

Under the bar, **every applied filter is repeated as a removable pill**, so a grid narrowed three ways no longer looks like an unfiltered one showing suspiciously little.

None of it needed new metadata: `person` and `credit` have carried TMDB cast since M2 and nothing had ever read them for browsing.

### Shows became first-class

**Continue watching · Play from start · Randomize episodes.** A show previously offered nothing at all, because its children are seasons and you had to drill into one to watch anything.

Continue is computed from playback state on every press, with `no-store` at both ends, and its rule is the first unwatched episode **after the furthest one watched** — not the earliest unwatched. The difference between those two is exactly the backtracking that makes other players untrustworthy: skip an episode, and earliest-unwatched sends you back to it for ever.

Play all and Randomize all also reach every library whose contents are a queue. A show library queues *episodes*, not shows.

### ffmpeg installs itself

**Settings → Media tools → Download ffmpeg**, pinned to an exact build and checksummed before anything is unpacked ([ADR 0043](https://github.com/Conqueror-Mod/LANcast/blob/main/docs/adr/0043-media-tools-are-fetched-not-bundled.md)).

Without ffmpeg an install appears to work and then does not play things — nothing is probed, so every file direct-plays by fallback and whatever the browser cannot decode is handed to it anyway. It produced a report of "AC-3 is not supported" for a codec that had shipped four releases earlier, which is what a missing dependency does: it degrades into wrong conclusions about the software rather than into a missing button.

LANcast also now finds ffmpeg **beside its own executable**, so dropping the two files in the LANcast folder works — including under a Windows service, where a per-user install was invisible.

### Fixes that came out of using it

- **A finished item starts again instead of resuming past its own end.** This is why pressing play on episode one played episode *three*: a watched episode keeps a saved position after its final frame, so it ended instantly and the queue walked forward through everything already seen.
- **Pressing Back returns you to where you were.** The restore gave up after about 200ms, against a grid that takes longer than that to exist — so a library scrolled into the Z's came back at the A's.
- **The library count is the library's size**, not the page size leaking into the UI.
- **The subtitle button appears only when there is a track to cycle to**, rather than on every video and doing nothing.
- **The two faults in v0.6.48**: a blank detail page from a hook declared below an early return, and an ffmpeg progress bar frozen at 0% by a poll the browser was allowed to cache.

### Both of those are now guarded rather than remembered

**eslint runs first in CI**, scoped to the react-hooks rules that catch a misplaced hook in a second — TypeScript type-checked the broken code and the whole suite passed. And **a detail page is now rendered in a test** with a cold cache, which is the transition that crashed.

### Verified

- Go: full suite across 38 packages; `GOOS=linux go vet ./...` clean.
- Client: 229 tests across 35 files; lint clean.


---

## v0.6.49 — 2026-08-19

Two faults shipped in v0.6.48. Both fixed the same day. **Install this over v0.6.48.**

### Opening any item blanked the app

A black window with no way back — only restarting recovered it.

The show buttons added in v0.6.48 declared their two `useState` calls beside the handlers that use them, which sits *below* `if (isLoading) return`. The first render registered two fewer hooks than the second, React refused to reconcile, and the whole tree unmounted with no error boundary above it to offer a way out.

It was reported as a TV-show bug and it looked like one: a film opened from the grid is usually already cached, so `isLoading` is false on its very first render and the hook counts never differ, while a show fetches on arrival and gets the loading render. A second household on a cold cache saw it on **every** page — which is what it always was. The fault was latent on every detail screen; only the timing differed.

### The ffmpeg progress bar stayed at 0%

The install itself worked — ffmpeg was downloaded and put in place. Only the display never moved, which is worse than a hang: the sane response to a frozen bar is to cancel something that was succeeding.

The status endpoint sent no cache headers, and every poll is a GET of the same URL with no cache-buster, so the browser reused the first "0 bytes" answer for the whole download. It says `no-store` now, asserted by a test, and so do the continue-watching endpoints — set *before* the lookup rather than after, because the response most likely to be kept is the one that went out without a header.

Three pieces of hardening that were **not** the cause of this report:

- `http.DefaultClient` has no timeout of any kind, so a genuinely stalled download would have waited for ever. There is deliberately no *total* timeout — 160MB over a slow line is legitimately minutes — so what is detected is **silence**: a watchdog reset on every chunk abandons a download that delivers nothing for a minute, and says so in those words.
- Progress is reported before the first byte, then every megabyte rather than every four. First movement now appears within a second or two of a working download, which is what tells a slow one from a stuck one at a glance.
- The install **logs when it starts**, with its size and URL, and how long it took at the end. There was no line until it finished, so a stall was indistinguishable from never having begun.

### Verified

- Go: full suite across 38 packages; `GOOS=linux go vet ./...` clean.
- Client: 219 tests across 33 files.
- New: tests that the polling and continue endpoints forbid caching, including on the not-found path.

### Known gap

There is no eslint in this project, and `react-hooks/rules-of-hooks` catches the blank-screen fault in a second. TypeScript cannot see hook ordering and no test renders a detail page. Closing that is the next thing worth doing.


---

## v0.6.48 — 2026-08-18

Shows can finally be put on, ffmpeg installs itself, and Back takes you back to where you were.

### Shows: Continue watching, and it never goes backwards

A show was the one container offering nothing — its children are seasons, so you had to drill into one to watch anything. It now has three actions, because "put this programme on" is three different questions: **Continue watching**, **Play from start** and **Randomize episodes**.

Continue is written against a specific failure: press continue on a long-running show and land about **three episodes back**, on something already watched — one season or seventeen, it does not matter.

That is a stale *read*. The server knows episode 14 was watched and answers 11 because something between the truth and the button holds an older picture of it. So there is **no cache anywhere on this path**: the answer is computed from playback state at the moment it is asked, the response says `no-store`, and the client fetches on the press rather than through a query cache whose staleness would reintroduce exactly this, intermittently.

**The rule:**

1. An episode **in progress** wins, most recently touched first — that is what you were watching.
2. Otherwise the first unwatched episode **after the furthest one watched**. Deliberately not the earliest unwatched, which is the backtracking bug written as a query: skip episode 5, watch through 13, and earliest-unwatched sends you back to 5 on every press. **Progress only moves forward.**
3. Nothing watched: start at the beginning.
4. Everything watched: it says so, rather than silently replaying the finale.

Progress is per user, so one person finishing a season does not move anybody else's place in it.

### Play all and Randomize all, everywhere they mean something

Music has had them since the mini-player; films and shows never did. A show library queues **episodes**, not shows — a queue of containers is not something a player can advance through. Pictures stay excluded until there is a slideshow to put there; a queue of photographs at a film player's pace is a bug that looks like a feature.

### ffmpeg installs itself

**Settings → Media tools → Download ffmpeg.** This is the fix for an install that appears to work and then does not play things: with no ffmpeg nothing is probed, so every file direct-plays by fallback and whatever the browser cannot decode is handed to it anyway, and Live TV is unavailable entirely.

It produced a report of **"AC-3 is not supported"** — when AC-3 shipped in v0.6.45 and its path in a browser is an audio re-encode that needs ffmpeg. The dependency does not degrade into a missing button; it degrades into wrong conclusions about what the software does.

Pinned to an exact build with a **SHA-256 verified before anything is unpacked**, into the data directory rather than Program Files, with version, size and licence shown before the download starts. Nothing is fetched automatically — a media server that contacts the internet without being asked has broken *no phone-home*.

A partial install reports as **absent** rather than present-and-broken, and the running server picks the tools up **without a restart**.

LANcast also now looks for ffmpeg **beside its own executable** before PATH, so dropping the two files in the LANcast folder works — including under a Windows service, where a per-user install was previously invisible.

### Browse: Back works, and the count means something

**Pressing Back returns you to where you were.** Reported as the paging resetting: click a film in the Z's, press Back, land in the A's. The restore loop gave up after twelve animation frames — about **200ms** — which is plenty for a detail page and nowhere near enough for a grid of 1,198 posters, where the document is one page tall for far longer, `scrollTo` is clamped, and the grid is left at the top. It got worse the larger the library, which is the opposite of what an expiring cache would do.

**The library count is the library's size.** "120 of 1,198" was the honest v0.3.2 label for a grid that genuinely stopped at one page; paging fixed that and the label outlived it. What is still arriving is reported by "Loading more" at the bottom edge, where somebody waiting for it is actually looking.

### Verified

- Go: full suite across 38 packages; `GOOS=linux go vet ./...` clean.
- Client: 219 tests across 33 files.
- New this release: 7 tests on where a show resumes, 10 on what the ffmpeg installer refuses to do, and one that fails if the scroll-restore budget is ever tuned back down.


---

## v0.6.47 — 2026-08-18

Browsing gets **nine filter categories where it had three**, and finally says what it is filtering by.

### Filters worth having

The old bar laid every value out flat — a dozen genres, eleven decades, all as chips. That works until the filters worth adding cannot be listed: a library has thousands of credited people and a century of years.

So the bar is now **categories that open a panel**: chips where the set is small, a **type-ahead where it is not**.

New: **Actor**, **Director**, **Year**, **Collection**, **Rating**, **Format**, and a **Status** group that gives *In progress* and *Unmatched* somewhere to live beside the lone *Unwatched* toggle.

**None of it needed new metadata.** `person` and `credit` have carried TMDB cast since M2 and nothing had ever read them for browsing; resolution is a bucket over the width the probe already stored.

### Every applied filter is visible

Under the bar, each one is repeated as a removable pill. This is the half Plex leaves out — there the active filters live inside the dropdown that set them, so a grid narrowed three ways looks like an unfiltered one showing suspiciously little. The pill row answers "why am I seeing 41 of 1,190" without opening anything.

### Three decisions the tests made, not the code

**Year search matches by prefix, not substring.** `99` as a substring also returns 1994, because the digits are in there — so typing *more* would widen the list, which is not what typing means.

**Actor and Director are separate.** "Who is in this" and "who made this" are different questions, and an any-role filter answers both without saying which was meant. Somebody who does both appears under each, once.

**Unrated is not zero.** A rating floor excludes unrated items rather than sinking them; sweeping them to the bottom would quietly hide the unmatched half of a library behind a control that says nothing about matching.

### Resolution reads width, not height

A 2.39:1 film at 4K is 3840×1608 against a 16:9 one at 3840×2160 — same format, heights 550px apart — so a height rule demotes every scope film a tier. The boundaries sit below the nominal widths because real 1080p is often 1912 after cropping, and a file with **no width has not been probed**: it belongs to no tier rather than to SD.

Any category that cannot change the grid is not drawn, and a rating step above what the library holds is not offered.

### Also

**The CC button stops appearing on files with no subtitles.** It rendered on every video and did nothing at all: `cycleSub` walks `[null, ...available]`, so with nothing available the click landed and the cycle returned to where it started. A control that never responds cannot be told apart from a broken one.

### Verified

- Go: full suite passes across 38 packages.
- Client: 208 tests across 31 files.
- The filter bar's behaviour is tested; its **appearance has not been reviewed in the running app**.


---

## v0.6.46 — 2026-08-18

Two fixes, both in the client — so this one needs the **installer**, not the in-app server update.

### A live channel that stutters once no longer stutters for ever

v0.6.41 gave live channels a head start before playing, measured against a real IPTV source: bytes arrive in bursts separated by silences of up to **5,071 ms**, which is HLS segment pacing relayed verbatim because the server copies video through untouched.

That cushion was correct, and it was built exactly once. The effect cleared its own timer on the first successful `play()`, and the video element had no `waiting` handler at all. So the first drought deep enough to empty the buffer spent it permanently:

- The element ran dry and fired `waiting`, and nothing was listening.
- Chromium resumed on its own at `HAVE_FUTURE_DATA` — the exact "first burst arrived" condition the original measurement rejected as too little.
- Playback runs at the rate the source arrives, so **nothing rebuilt the head start**.

Every later gap then reached the decoder. It reads as judder rather than as buffering, because the spinner was dismissed at startup and never came back — which is why this gets reported as a framerate problem rather than a buffering one.

The hold is now re-armed on `waiting`, and it pauses while refilling: an element left playing eats the buffer as fast as it arrives and simply stalls again. It is re-entrant on purpose, because `waiting` fires repeatedly on a stalling channel and restarting the clock each time would push the 12-second deadline out for ever.

The buffering policy itself is unchanged — the same `shouldStartPlayback` and `bufferedAhead`, so there is one rule rather than two that can drift.

**What you will see:** a channel that recovers cleanly after a stall instead of juddering until you restart it. On a channel whose gaps consistently exceed the 8-second cushion you will now see **Buffering…** reappear. That is the honest failure — the droughts are upstream, and no client can invent bytes the provider has not published.

### Pressing Scan updates the counts it changes

The sidebar could read "TV Shows 15" while the grid beside it read 12, and stay wrong for as long as the app stayed open. Scan also looked dead for up to eight seconds, so it got pressed repeatedly.

One cause behind both: starting a scan touched no cache, and the activity indicator only refreshes counts when it *observes* work go from active to idle. A small library that started and finished between two polls was never seen running, so no edge was ever detected and nothing invalidated the library list.

The mutation now claims the work itself — sound rather than optimistic, because `Scanner.Start` sets the state to running before the 202 is written.

### Verified

- Go: 38 packages pass, `go build ./...` clean.
- Client: 183 tests across 29 files.
- Both fixes have regression tests that fail against the unfixed code.


---

## v0.6.45 — 2026-08-18

### Picking an audio track no longer hangs the player

If you have films or episodes with more than one audio track — a foreign default with an English track alongside it, say — choosing the track you wanted left the player spinning and never started. This fixes it.

### What was happening

It wasn't really about having two audio tracks. That was just what exposed it.

When you pick a track other than the file's default, LANcast has to repackage the file rather than send it as-is, because playing a file directly gives you whatever track the player picks. That repackaging step was failing immediately for **Dolby Digital and Dolby Digital Plus (AC-3 / E-AC-3)** audio, and failing in a way nothing reported: the server had already promised the browser a video, so you got a spinner instead of an error.

Since files with a single audio track are usually played directly, they never hit the failing path — which is why this only showed up once you chose a track.

### What this also fixes

**Any AC-3 file that can't be played directly.** If you had a film or episode that buffered for ever and you never worked out why, this is a good candidate — a Dolby Digital TV series that wouldn't play during testing turned out to be the same bug.

### If you were affected

Nothing to do. Update, and try the file again.

---

This release changes the server only, so updating through the in-app panel delivers all of it.

**Full changelog:** https://github.com/Conqueror-Mod/LANcast/compare/v0.6.44...v0.6.45


---

## v0.6.44 — 2026-08-17

### HDR films no longer look washed out — and no longer lie about what they are

If an HDR film looked flat and grey when it played in your browser, this fixes it.

### What was wrong

LANcast wasn't converting HDR to SDR at all. It reduced the bit depth and left everything else alone, which is why the picture came out desaturated.

The less visible half was worse: the file LANcast sent still *claimed* to be HDR. Some players ignored that and showed a flat picture; others believed it and applied an HDR curve to video that had never been HDR-encoded. The same film looked different on different screens, which made the problem almost impossible to report.

This affected every HDR film, because HDR content uses a video format browsers can't play, so it always gets converted.

### What changed

HDR is now properly converted to SDR — colour restored, bright highlights brought back under control — and the result is correctly labelled as standard-range.

If your server's ffmpeg is missing the filters needed to convert, LANcast still labels the file correctly rather than sending a file that contradicts itself. You'll see a line in the log at startup if that's the case.

### Better handling of awkward filenames

Three fixes, all found by tracking down a single film that wouldn't match:

- **A bracketed note before the year** — `Film (Alternate Cut) (2018).mkv` — lost half its bracket, and the broken text went out as the search. That film matched nothing; now it matches.
- **Edition markers in brackets** are now recognised. `(Director's Cut)` and `(Alternate Cut)` are read as editions rather than part of the title.
- **A film whose title ends with an edition word** is no longer truncated. `The Final Cut (2004)` was being reduced to `The`.

Films like *Uncut Gems* and *DC League of Super-Pets*, whose names contain those same words, are unaffected — that's covered by tests.

### Recorded, not built

Two decisions written up for anyone reading the project's history:

- **[ADR 0041](https://github.com/Conqueror-Mod/LANcast/blob/main/docs/adr/0041-a-misplaced-file-is-corrected-on-disk.md)** — when a file is the wrong kind, the fix belongs on disk. Renaming a file to restore information its name lost, or moving a film into the film library, is the correct remedy rather than a feature.
- **[ADR 0042](https://github.com/Conqueror-Mod/LANcast/blob/main/docs/adr/0042-two-files-one-work.md)** — when two files claim the same film, LANcast will report it rather than quietly merging them. On a real 1,209-film library thirteen such pairs already exist, and they turned out to be five different situations, including one file that claimed to be an alternate cut and was a byte-for-byte copy, and one film sitting under a completely different film's identity. Merging them would have hidden all of it.

---

This release changes the server only, so updating through the in-app panel delivers all of it.

**Full changelog:** https://github.com/Conqueror-Mod/LANcast/compare/v0.6.43...v0.6.44


---

## v0.6.43 — 2026-08-17

### A season is no longer searched for by name

If a season of one of your shows was displaying someone else's poster — and every episode under it inherited that poster — this release fixes it, and cleans up the rows it already wrote.

### What was happening

Seasons were being matched against the metadata provider using their own title. But a season's title is a *position*, not a name, so the query that went out was literally:

```
/search/tv?query=Season+2
```

The provider answers that with real shows whose names happen to contain the phrase. The match scored above the auto-apply threshold, so that show's title, year, overview, poster and fanart were written over the season.

Because the query depended only on the season number, **the same wrong show won for every show in the library**. On the library this was found in, season 2 of nine unrelated series all ended up sharing one drama's artwork.

### What changed

A season is now resolved from the show that owns it, or not at all:

- Seasons are never searched for. The season number is looked up exactly against the parent show's id, so there is no scoring and no threshold to get wrong.
- A season gets its own name, overview and poster instead of inheriting the show's.
- A season under a show that hasn't been matched yet waits for it, rather than being recorded as a failure.
- Re-read filenames no longer touches seasons — a filename has nothing better to offer one than `S02 480p Bluray`.

### Cleanup on upgrade

Your database is migrated automatically on first start. Every season that was matched this way has its wrong identity and artwork removed and is queued to be resolved again properly.

**Seasons you edited yourself are left exactly as you left them.** A cleanup reconciles files; it does not undo your decisions.

Season posters will fill back in as enrichment runs. Seasons of a show that is still unmatched will stay blank until that show is matched — that is the honest state, and matching the show brings its seasons with it.

### Also worth knowing

Season sort order is now by number rather than by name, so Season 10 stops appearing between Season 1 and Season 2.

---

This release changes the server only, so updating through the in-app panel delivers all of it.

**Full changelog:** https://github.com/Conqueror-Mod/LANcast/compare/v0.6.42...v0.6.43


---

## v0.6.42 — 2026-08-17

Two fixes, both found by measuring rather than reasoning, and a design record for
a problem that is not a bug.

### An HLS channel no longer plays at 1.5× speed

A channel on a second playlist played visibly fast, with a *duration* on the
scrubber — `0:16 / 0:28` — where a live stream should have none.

The source is an **HLS master playlist with three ABR variants**. ffmpeg's HLS
demuxer defaults to `live_start_index -3`: three segments back from the live
edge. Those segments **already exist**, so ffmpeg fetches them as fast as the
server will serve them, and everything downstream receives media faster than
real time until the backlog drains.

Reproduced outside the app by running LANcast's own arguments against that
channel for twenty seconds of wall clock:

```
default                29.97s of media produced   → 1.50x real time
live_start_index -1    19.97s of media produced   → 1.00x real time
```

An HLS channel now starts one segment from the edge, so there is no backlog to
race through.

**Conditional, deliberately.** `-live_start_index` belongs to the HLS demuxer;
given a plain transport stream ffmpeg does not ignore it but refuses the input
outright — `Option live_start_index not found`. Applying it everywhere would
turn every tuner and raw stream into a dead channel. A channel that could not be
probed keeps the behaviour it has always had.

### A progress total that grows when the queue does

The activity panel read **"682 of 449"** during a rescan.

The total was measured once, when the run began, and never revised — while the
count of finished items kept climbing. Requeueing mid-run is ordinary rather
than exceptional: a scan adds rows while enrichment is already going, *Refresh
metadata* clears a whole library, and *Re-read filenames* requeues everything it
corrected.

The total is now done-plus-outstanding, and never shrinks — a bar that jumps
backwards reads as a fault in the thing it is measuring. Failed items are not
added on top, because a failure stays queued for retry and is already counted
there.

### Recorded, not built: organising a large channel list

A real server now carries **1,862 channels from one provider** with a second
playlist beside it, merged onto one page, and roughly sixty group chips that
wrap to five rows before a single channel is visible — the filter row is taller
than what it filters.

[ADR 0039](https://github.com/Conqueror-Mod/LANcast/blob/main/docs/adr/0039-organising-a-large-channel-list.md)
sets out four changes in cost order: filtering by playlist, groups that open
rather than filter, per-device hidden and favourite channels, and a guide-first
grid explicitly deferred while no provider in use publishes XMLTV. It also names
and rejects virtualising the tile list — the ask is not to be *shown* 1,862
tiles, not to scroll them faster.

Nothing in this release implements it.


---

## v0.6.41 — 2026-08-17

### Live channels build a head start before playing

Stuttering live playback, chased with measurements rather than theories.

Reading the live response body directly and timing every chunk over twenty
seconds, against a real channel:

```
chunks 376   total 2.68 MB
gap median      0 ms
gap p90         3 ms
gap max     5,071 ms   ← five seconds with no bytes at all
```

Bytes arrive in tight bursts separated by multi-second silences. That is HLS
segment pacing seen from the far end: ffmpeg pulls a segment as fast as the
network allows, then waits for the next one to be published. The server copies
video through unchanged, so it relays that pacing verbatim — **the root cause is
upstream, and the provider decides when a segment exists.**

What *is* ours is when playback starts. `canplay` fires at `HAVE_FUTURE_DATA`,
which on a bursty source means only "the first burst arrived" — so playback began
with under a second in hand and ran dry at the next silence. Stutter, every few
seconds, indefinitely.

Playback now waits for **8 seconds** of buffered media, chosen to cover the
measured drought with margin, with a **12-second deadline** so a channel that
trickles still starts. Starting late with a short buffer is exactly what the old
code did immediately, so the fallback is never worse than the old behaviour.

You will see **Buffering…** for a few seconds where a picture previously appeared
and then juddered.

### Three explanations this replaces

Recorded because two of them looked obviously right, and shipping either would
have been a confident no-op:

- **The muxer's interleave delta.** Muxing a synthetic source with the current
  flags and with `-max_interleave_delta 0` produced **byte-identical output**,
  with a longest single-stream packet run of 2 either way.
- **A WebView2-specific decoding fault.** Chrome stutters too: 25.4 seconds of
  media over 41.3 seconds of wall clock, with zero dropped frames. An earlier
  reading of "Chrome plays this perfectly" was a single lucky sample.
- **Per-frame fragment overhead.** Fragmenting is fine; the stream simply stops
  arriving.

### Limits, stated plainly

This stops the gaps reaching the decoder. It does not make an upstream deliver
evenly, and a channel whose silences exceed eight seconds will still stutter.


---

## v0.6.40 — 2026-08-17

Five fixes, all found by driving the running app and querying the live library
rather than by reading code.

**Two of these need a rescan** to take effect — the artist merging and the
episode-marker fix change how names are read. The artwork fixes work
immediately.

### Blank tiles that should not have been blank

Two bugs, one symptom.

**"Recently Played" shelves had no artwork at all** — ten blank rectangles for
ten films whose posters were on screen a few hundred pixels above, in a shelf
that had attached them. Every other list endpoint attaches artwork; this one was
written without it. The omission is a line that is *not there*, which is why it
survived review.

**A track had no cover though its album did** — 8,443 of them on a real library,
which is every music tile on the home page, in Continue Listening and in search,
rendering blank beside film posters that worked. A row of empty rectangles next
to a row of posters does not read as "music has no covers"; it reads as a broken
page. Only the poster is inherited: a backdrop belongs to a page *about* that
item, and a thumbnail is per-item by definition.

### A miniseries was three films

```
Storm.Of.The.Century.[1999].DVDRip.XviD.EP2-BLiTZKRiEG.avi
```

parsed as a **movie**. The episode pattern matches `EP2` perfectly well — but the
noise stripper cuts everything from the first quality marker onward, and
`DVDRip` took `EP2` with it. A three-part miniseries became three identically
named films in a television library, each searched against film data and each
landing in the review queue with nothing a person could fix. Scene naming
routinely puts the ordinal after the tags.

The marker is now looked for in the raw name when the tidied one yields nothing.
Ordered as a fallback deliberately: every name that resolves today resolves
identically, so a release tag that happens to look like an ordinal cannot change
an answer that already works.

### One band, one artist

A real library held `Blut Engel` beside `Blutengel`, `Box Car Racer` beside
`Boxcar Racer`, `t.A.T.u` beside `t.A.T.u.` — and `alt-J` beside `alt‐J`, which
differ only by U+002D against U+2010 and are **visually identical on screen**.
Each pair was two tiles, each holding some of the records, neither showing the
discography.

Artist and album keys now fold on letters and digits alone, so punctuation,
spacing and hyphen lookalikes stop counting as identity. Deliberately *not* the
sort-title normalizer, which drops leading articles and would key a band called
"The The" as "the".

### The blank gap on the Downloads page

Measured at 52 pixels of nothing between the note and the empty state. Two
causes: the shared empty-state paragraph carries padding meant for a grid where
it stands alone on an otherwise empty screen, and the list container rendered
even with no rows, adding its own padding below that.


---

## v0.6.39 — 2026-08-17

### A dead channel says so, instead of looking like a broken app

v0.6.38's logging found this within a second of shipping:

```
level=WARN msg="ffmpeg reported errors" channel=3419
  stderr="Error opening input: Server returned 404 Not Found …"
```

The channel was simply **gone** — a stale entry in a provider's list. The viewer
saw none of that. The response committed to `200 OK` before any bytes existed,
so by the time ffmpeg failed it was already a successful video stream: an empty
body reached the browser, which reported `DEMUXER_ERROR_COULD_NOT_OPEN`.

A list of **1,862 channels** will contain dead ones. That is ordinary. Every one
of them read as LANcast being broken.

**The header is now written only once the first byte of video exists.** A source
that produces nothing answers `502` with something you can act on:

> the channel's source is gone (HTTP 404) — the provider's list may be out of date

Other cases are named too: 401 and 403 as credentials that may have expired,
connections refused, sources that do not respond in time, hosts that cannot be
reached, and streams this server cannot read. Anything unrecognised falls back
to "could not be opened" rather than inventing a cause — a confidently wrong
reason sends you to fix the wrong thing, and the full text is in the log either
way.

Once one byte has been sent there is a stream, and any later failure ends the
connection rather than changing the status. An interruption of something that
was working is a different event, and no status code can be sent by then anyway.

**The upstream URL never reaches a client.** ffmpeg writes the full URL into its
stderr and provider channel URLs are routinely credentialed — publishing one
hands out the subscription — so only a classification derived from that text is
returned. Two tests guard it: the classifier against real ffmpeg output, and the
whole response body scanned end to end.

---

This does not explain stuttering playback, which is still open. It does remove a
channel from that investigation which was never playing at all.


---

## v0.6.38 — 2026-08-17

### ffmpeg's errors now reach the log

A live channel that failed to play produced exactly one line:

```
level=INFO msg="live transcode started" session=ef4bd889… channel=3419 video=copy audio=encode
```

…and nothing else. The browser's half of the story was
`DEMUXER_ERROR_COULD_NOT_OPEN`, which says only that what arrived could not be
opened.

ffmpeg knew more than that — whether the source refused the connection, sent a
codec the mux rejected, or died three seconds in. Every session already captured
its stderr into a bounded ring buffer, and `Session.Stderr()` already existed to
read it. **Nothing ever called it for a stream**, so the explanation sat in
memory until the process exited.

It is logged now when a session ends, from both paths a session ends by: a
client going away or ffmpeg dying, and a session going idle — which it can do
*because* ffmpeg stopped producing.

Quiet on a healthy stream. ffmpeg runs at `-loglevel error`, and a viewer
closing a tab produces nothing, because being killed is not an error ffmpeg
reports.

### Why this release exists

Live TV is the least-tested surface in this project and the hardest to
reproduce: it depends on a provider, a channel, and a moment. An evening spent
diagnosing a stuttering channel produced three theories and disproved two of
them by measurement — and the one measurement that would have settled any of
them was being captured and thrown away on every session.

Nothing here changes playback. It changes whether the next attempt is evidence
or inference.


---

## v0.6.37 — 2026-08-17

Four filename-parsing defects, all found by running a real TV library through
the parser rather than by reading the code. Three of them mean **files that
needed no renaming** — the layouts were fine and the parser was wrong.

**Needs a rescan of any affected TV library.** This changes how names are read;
rows already in the database keep their current grouping until rescanned.

### "ds9" was being read as season 9

```
star.trek.ds9.e099.apocalypse.rising.mkv
  → series "star trek d", season 9, episode 99
```

The season marker matched the `s9` **inside `ds9`**, took `.e099` as the episode
number, and truncated the series name at the false marker. Seventy-eight
episodes filed under a season that does not exist, under a show whose name is a
fragment.

This is the worst shape a parsing bug takes: no error, a confident and
plausible-looking answer, and it hits **any** show abbreviated to letters ending
in `s` followed by a digit.

### A season marker at the end of a folder is now a season

```
Blue Mountain State/BMS S01/…  → show "BMS S01"
Blue Mountain State/BMS S02/…  → show "Blue Mountain State"
```

A folder counted as a season only when the marker *led* its name. `BMS S01`
therefore became a show of its own — while the same series grouped correctly
from its other seasons, whose filenames happen to carry the show name. One show,
listed twice, under two names.

The library that found this uses **two conventions across its own seasons**:
season 1's filenames carry episode titles and no show name, seasons 2 and 3
carry the show name and no titles. Both now resolve to one series, which is what
the test asserts — checking them separately would pass while the grid still
showed two shows.

### A trailing season marker no longer survives in a series name

```
Spider-Noir.Season.1.S01E01.1080p.AMZN….mkv → series "Spider Noir Season 1"
```

The series is read from the text before the episode marker, and the `Season 1`
in front of it stayed. No provider has a show by that name. The season is
already known from the marker that followed it.

### Double episodes keep their title

`S01E01-E02 - Emissary` was titled **"E02 Emissary"**. The second half of the
range now belongs to neither the episode number nor the title.

---

One near-miss worth recording: recognising trailing markers initially made a
`Show S01` folder sitting **directly under the library root** skip to the root
and return no show directory at all — turning the twenty-shows bug ADR 0037
fixed into *zero* shows. There is nothing above such a folder to name the
series, so it names itself and the marker comes off the name instead. Four
existing scan tests caught it before it shipped.


---

## v0.6.36 — 2026-08-17

Four fixes, three of them found by driving the app against a real library rather
than by reading the source.

### Re-read filenames has a button

v0.6.34 added `/reparse` as an endpoint only — usable by hand, invisible in the
app. **Settings → Libraries → Re-read filenames** now does it, and says what
happened:

- *Every item has already been re-read. Nothing to do.*
- *Re-read 12 uncertain items — all already matched their filenames.*
- *Re-read 160 uncertain items and corrected 98. Those are being matched again
  now.*

Both numbers are reported because they answer different questions. "0 changed"
and "nothing left to examine" mean opposite things, and a success state that
reads identically to a no-op is not feedback.

### A season is no longer offered for review

The review queue listed season rows — "Season 1 · NO MATCH · best 0%" with a Fix
button beside each. A season has no identity of its own: its name is a position
within a show rather than the name of a work, so a provider search for it cannot
succeed, on every season, permanently. `meta.Caps.Supports` routes seasons to the
show providers, which is right for fetching a season whose show is known and
wrong for searching one by name.

Shows are still listed. A show's title is a real title and a wrong match on one
is worth correcting — which is the test for belonging in that queue.

### The metadata progress figure was mostly photos

The activity readout showed the same total whatever was being scanned. The
number was real, not stale: **4,238 of the 5,492 pending items were photos**,
which can never be enriched. `meta.Caps.Supports` answers false for `photo` and
`gallery`, so no provider will ever match one.

The queue already excluded music for exactly this reason — that fix was made
once and the picture kinds were missed. The general test is now written down
where the list lives: not *did we forget a kind*, but *can a provider ever
answer for this kind*.

Note that the enrichment worker is **server-wide**: the figure is the whole
backlog, not the library you pressed Scan on.

### Correcting a match now changes the picture

A show matched to the wrong title, fixed by hand, kept its old thumbnail.

`item_artwork`'s primary key includes the artwork id, so `INSERT OR REPLACE`
only replaces when the image is *the same image*. A corrected match downloads a
different one, so it inserted a second row and left both marked selected for the
same kind. Both readers assign in row order with no `ORDER BY`, so which picture
won was whatever SQLite returned last — and the grid and the detail page could
disagree about the same item.

The kind is now deselected in the same transaction before the new image is
written. Deselected rather than deleted: the bytes are content-addressed and
shared, and the row records what the item used to show.

**Existing duplicates are not rewritten by upgrading.** A row that already has
two selected posters keeps them until something re-stores its artwork —
Settings → Libraries → Refresh metadata on that library will do it, and the
corrected image will stick this time.


---

## v0.6.35 — 2026-08-17

### Re-parsing a library twice is now actually free

v0.6.34 introduced `POST /api/libraries/{id}/reparse` and claimed a second run
would be a no-op. Running it against a real 1,216-film library disproved that
within minutes:

```
first  → examined 160, changed 98
second → examined  99, changed 32   ← not 0
third  → examined  99, changed 32   ← not 0
fourth → examined  99, changed  0   ← only with no enrichment in between
```

**Why.** Enrichment writes the provider's answer back over the guess for any row
that stays uncertain. So *"never re-parsed"* and *"re-parsed a minute ago"* were
indistinguishable by the only test the code had — the stored title disagrees
with the filename in both cases. Every press rewrote the same rows and asked the
provider the same question again.

Nothing was corrupted and each cycle needed an operator press, so it was never a
runaway. But it was a repeating write and a repeating provider call that the
documentation promised would not happen.

A row is re-parsed **once** now. Each row examined is stamped — whether or not
it changed, because a row the parser already agrees with has been re-parsed just
as truly as one that moved — and stamped rows are not offered again.

`?force=true` re-offers them. That exists for the one thing the stamp cannot
see: the filename heuristics themselves improving, so rows parsed under the old
rules get another pass. Exactly the situation that produced the folder-year fix
in v0.6.34, so it will be wanted again.

Schema revision 25 adds a nullable `media_item.reparsed_at`. The upgrade is
automatic.

### Upgrading

Rows re-parsed under v0.6.34 carry no stamp, so the first run after upgrading
examines them once more and then settles to `{"examined": 0, "changed": 0}`.
That is correct rather than a leftover.

The API documentation has been corrected: it no longer claims re-running is free
as an accident of comparison, and it now says why the stamp is load-bearing.


---

## v0.6.34 — 2026-08-17

### A film's year can live on its folder

Found by driving a real 1,216-film library and reading the scores behind the
Review queue rather than the queue itself.

The parser read a movie's year from the **filename only**. In the very common
`Title (Year)/Title.ext` layout the year is stated once — on the folder — and
was discarded:

```
year=0  title="Spiderman"     ← Spiderman (2002)\Spiderman.mp4
year=0  title="The Avengers"  ← The Avengers (2012)\The Avengers.mp4
```

A missing year is not a weak signal, it is a **cap**. An absent year scores half
credit, so the best a film could reach was `0.60 + 0.15 + <0.10` — strictly under
the 0.85 auto-accept threshold, because the popularity term is asymptotic and
never reaches 1. A film in a folder-year library was **arithmetically incapable**
of auto-matching, with a perfect title and the correct year sitting one directory
up. Of 140 movies in review, **111 were this** and nothing else.

It also let a wrong identity be *applied*: `Aliens SE (1986)` matched **"Alien
Sexting" (2020)** at 0.683 — above the review threshold, so merged, giving that
row the wrong title, year and poster. With no year in the query there was nothing
to veto a candidate 34 years off.

Only the immediate parent is read. The filename keeps precedence, the library
root supplies nothing (`Movies (2024)/Dredd.mp4` is not a 2024 film), and a year
*range* names a collection rather than a release, so `Alien(1986-2024)` stays
silent and cannot stamp 1986 onto everything beneath it.

### Re-parsing rows an older parser guessed

The fix above reaches files added *after* it. Every row already in the database
keeps the old guess, and neither a rescan nor a refresh closes that gap — a
rescan reconciles *files* and does not re-litigate identity, and refresh asks the
provider the same question again when the **question** was the broken part.

`POST /api/libraries/{id}/reparse` re-runs the filename heuristics over a
library's uncertain rows and requeues the ones that changed. Enrichment builds
its search from the *stored* title and year, so requeueing alone would have
written a better question and never asked it.

Scope is the safety of it: only `review` and `unmatched` rows are touched, since
a matched row's title came from a provider and outranks any filename; `locked`
and `local` are excluded outright; field locks are honoured **individually**, so
an item whose title you corrected still has its year re-parsed; an empty guess
never clears a populated field; and a row already agreeing with its filename is
neither rewritten nor requeued, so running it twice is free.

On the library this was measured against, 130 of 140 rows recover a year and 114
of those agree with the provider — a review queue of 140 becoming roughly 26,
with the survivors being real problems.

**No client button yet** — this release ships the endpoint.

### A displayed score no longer rounds up past its own threshold

The Review queue rendered confidence with `Math.round`, so **0.848 printed as
"85%"** — the auto-accept threshold itself — on a row badged *Uncertain*. The
badge and the number contradicted each other, and the number is the one that
looks authoritative, so a correct decision read as a bug in the matcher.

Scores floor now. Flooring can only understate, and by less than a point, so it
never claims a confidence the scorer did not have. In the Fix-match panel the
*bar* is still drawn from the exact value; only the label is floored.

---

**Upgrading:** the folder-year fix applies to newly scanned files immediately.
Rows already in your library need the re-parse endpoint above — nothing changes
them automatically, by design.


---

## v0.6.33 — 2026-08-16

**Your music was being labelled as television.** One fix, found by looking at a real profile page.

### A track is not an episode

Pearl Jam's *Black* showed as **S00E33**. Garbage's *#1 Crush* as **S00E14**. Disc zero, track thirty-three.

A music track reuses the same three database columns a television episode uses: the scanner writes the album into `series`, the disc into `season` and the track number into `episode`. So the test "does this have a season and an episode?" is true of **every tagged song in the library**, and three separate places formatted an episode code from exactly that:

- the profile's recently-played rows,
- any poster tile showing a track,
- and the download filename, which would have named a downloaded song `Album - S00E14 - Title`.

The kind of the item is the only thing that separates a song from an episode, and it is now checked in one place rather than by each caller in turn.

Episodes are unaffected: a real episode still reads "Cowboy Bebop · S01E01" wherever it appears outside its own show.

### Also

**v0.6.32 was published with CI's gofmt check failing.** Nothing in that release is wrong because of it — the check is formatting, not behaviour — but the tag went out red, and this release makes `main` green again.

### Upgrading

No rescan, no schema change. The in-app updater replaces **LANcast-Server.exe** only, which carries everything above including the web client.


---

## v0.6.32 — 2026-08-16

### v0.6.32

**Season folders are recognized even when the number leads the show name.**

A layout like `Star Trek Deep Space Nine/Season 1 - Star Trek Deep Space
Nine/...mkv` wasn't read as a season folder at all — only an exact "Season
N" folder name matched. The season folder was mistaken for the show itself,
producing one phantom show per season and leaving the real episodes' series
name garbled, which meant their metadata search had nothing sane to query.
Trailing text after the season number is now recognized, but only behind a
separator, so a show whose name happens to start the way a season marker
does (e.g. "S3rvant") still isn't misread as a season.

**Needs a rescan of any TV library using this layout** to regroup existing
episodes under the corrected show — this only fixes how the folder shape is
identified going forward.


---

## v0.6.31 — 2026-08-16

### v0.6.31

**The home screen catches up to the nav bar's count fix.**

v0.6.30 taught the nav bar to count music and picture libraries by what's in
them — songs and photos — instead of the artists and galleries that group
them. The home screen's masthead never got the same fix: it kept summing the
old tile counts, so right below a nav reading the new totals, the "things to
play" line and each library's count on the home page still showed the old
numbers. Both now use the same rule.


---

## v0.6.30 — 2026-08-16

**The nav counts refresh, and each library is counted in the thing it is of.** Two fixes, both reported from a running v0.6.29.

### The nav went stale after removing titles

Removing three files left the nav reading **1,212** beside a grid that had already moved on to **1,209**.

Removing a title invalidated six cached lists, and the code claimed it refreshed "every list that could be showing the item". It was three short — and the important miss is a subtle one: invalidating `items` **does not match the browse grid**, whose cache key is `items-infinite`. Invalidation matches by key prefix, and `items-infinite` is a different name rather than a child of `items`. The library list, which is the nav count, was missing outright, and so were the genre and decade filters, which could outlive the last title that used them.

All three are refreshed now.

### A library is counted in the thing it is *of*

Music showed **1,171 artists** and Photos **67 galleries**, sitting in the same column as Movies **1,209 films** as though the three were comparable quantities. They are not — that music library holds tens of thousands of songs.

The nav now shows **songs** for a music library and **photographs** for a picture library. Movies and TV Shows keep counting films and shows: a film *is* a tile, and "20 shows" is what somebody means by a TV library, so counting it in episodes would answer a question nobody asked of the nav.

Nothing was redefined to do it. The server reports both numbers — the files, and the tiles the grid shows — so the promise from v0.6.29 that a library's count matches its grid is still intact, and a client can show either. For anyone building against the API, `media_count` is new alongside `item_count`.

### Upgrading

No rescan, no schema change. The in-app updater replaces **LANcast-Server.exe** only, which carries everything above including the web client.


---

## v0.6.29 — 2026-08-16

**Two fixes, both found by running v0.6.28 against a real library rather than by reading the code.** One is the answer to "why does the sidebar say 1,381 when the grid says 1,211", and the other is a bug v0.6.28 introduced.

### The library count now counts entities, not containers

A library's sidebar number and its grid have been answering different questions. On a real server, at the same instant:

| | sidebar | grid | difference |
|---|---|---|---|
| Movies | 1,381 | 1,211 | **170** — exactly its collections |
| Music | 1,177 | 1,171 | **6** — exactly its imported `.m3u` playlists |

The count honoured the top-level rules — no parent, not missing, no one-film collections — but nothing excluded the kinds that *group* items rather than being them. The grid excludes those. So three separate fixes in v0.6.28 each changed what the grid **showed** and none changed what the count **counted**.

A collection and a playlist are containers, not entities: they hold films and tracks you already have. They keep their own pages and their own counts; they simply stop being added to "how many films do I have".

**Movies will read 1,211 and Music 1,171** after this, and the Home masthead total follows. **No rescan needed** — it takes effect on restart.

Files that do not fit a library's type were never counted in the first place. They are not rows at all, which is why the Libraries pane reports them separately: "1 audio file ignored — this library's type is Movies."

### Empty shows left behind by the v0.6.28 regrouping

v0.6.28 made a show's identity its series title, which correctly collapsed a season-per-folder library into one row per series. But the sweep that removes emptied containers ran **once**, and a regrouping needs two passes.

The result on a real library: TV Shows went **up**, 60 rows to 64, with one correct "It's Always Sunny · 8 seasons" sitting beside twenty **empty shells** of the same name — full metadata, poster, cast, and no seasons or episodes underneath.

The reason is worth stating because it explains why it looked fine in testing: when the sweep evaluates, an old show still holds its old *season* rows — and those seasons are the empty ones, having just lost their episodes to the new show. The statement deletes the seasons and, in the same pass, judges the shows as still having children. The shows are childless the instant it commits, and nothing looks again until the next scan.

Pruning now repeats until a pass deletes nothing.

**This one needs a rescan of TV Shows** to clear shells already in your database. If you are on v0.6.28 and cannot update yet, simply rescanning TV a second time clears them too — the second scan is the pass that was missing.

### Upgrading

No schema change. The in-app updater replaces **LANcast-Server.exe** only, which carries everything above including the web client.

If you rescanned for v0.6.28 already, the only rescan this release needs is **TV Shows**. Movies and Music need nothing.


---

## v0.6.28 — 2026-08-16

**Your library counts were wrong, and this is why.** Five fixes, all found by looking at a real 1,381-film library against the same collection in Plex, and all of them about the same thing: LANcast counting or grouping something it should not have.

### Read this first: a rescan is required

**None of these take effect until you rescan.** Movies, TV Shows and Music all need one — the fixes change what a scan *produces*, and nothing repairs itself in place.

Expect the numbers to move a lot, and some of it to look alarming:

- Movies drops by however many extras the Libraries pane now reports.
- TV Shows collapses from one row per season folder to one row per series.
- Music sheds a tile for every imported `.m3u`.
- Collections goes up, not down — a page that could only show 120 now shows all of them.
- **A batch of rows will be marked missing** — extras and old per-folder shows that no longer correspond to anything. That is the scanner marking rather than deleting, which is the rule that protects you when a drive is unmounted. Nothing is destroyed.
- The review queue may grow, because re-parsed titles get rematched.

Take a copy of your data directory before the first scan, as always.

### A show is a series title, not a directory

A series stored as `Show S01`, `Show S02`, … — a season per top-level folder — became **one show per season**. Twenty tiles for one series, each reading "1 season", each separately matched so they even shared a poster.

A folder counted as a season folder only when its *entire* name was a season marker, so `It's Always Sunny in Philadelphia S01` was read as a series in its own right. Identity is now the parsed series name, which is the one thing that survives every layout a series can be stored in ([ADR 0037](docs/adr/0037-show-identity-is-the-series-title.md)).

Where a filename yields no series name the folder still decides — with nothing to group on, folding an episode in with its neighbour would be a guess. That is also why `BMS S01` stays separate from `Blue Mountain State`: nothing in a filename resolves an abbreviation, and the honest fix for that row is a metadata match.

Shows that live in one folder keep that folder as their path, so `tvshow.nfo` writing is unchanged. Artwork, match state and **locked fields survive** the regrouping — rows are repointed, never replaced.

### Extras are not works

Every playable video file in a movie library became a movie: `sample.mkv`, `Trailers/`, `Featurettes/`, `Behind The Scenes/`, `Film-trailer.mkv`. Each got a title, a tile, a trip to the metadata provider, and a line in the count.

Now excluded, by the conventions Plex and Kodi both document ([ADR 0038](docs/adr/0038-extras-are-not-works.md)). **One condition matters more than the rest:** a folder named `Trailers` or `Shorts` sitting *directly* inside a library root is a category you keep on purpose, not a film's extras — an extras folder must have a film folder above it. `Specials` is deliberately not on the list; in a shows library that is season zero.

The scan reports `skipped_extras` and the Libraries pane states it, because that number is the entire explanation for a count that disagrees with another server's.

### Collections: the page only ever showed 120

The collections page never loaded past its first page. It rendered 120 tiles and stopped — on a library with 170 collections, every one after roughly "H" was **unreachable**, with nothing on screen to say so. The count read the number loaded, so the truncation reported itself as a total. The alphabet rail made it worse: it filters over what has loaded, so a letter that never arrived simply looked empty.

And **a collection of one is no longer a collection**. A Hitman Collection containing Hitman, an Aquaman Collection containing Aquaman, a hundred more — each a duplicate tile of one film with a "Play all" button. The rule requiring at least two members existed but applied only to the browse grid, so the one page dedicated to collections was the one that ignored it.

### The music grid shows artists

Tiles named like `00-health-rat_wars-16bit-web-flac-2023` stood among the artists. They were the **`.m3u` files scene releases ship** beside the audio — imported correctly, listed in the wrong place, one tile per release. Playlists have their own page and are now excluded from the grid, in every library kind rather than only movie libraries.

And **one band is one tile**: `9VoltRevolt` and `9voltRevolt` were two artists because the grouping key was the raw tag. Album names too, so `Rat Wars` and `RAT WARS` were two records.

### Titles

- Quotes that wrap a whole title come off. A film called `"Wuthering Heights"` — quotes included — sorted ahead of everything, because a quote character orders before every letter. Only a matching *pair* is removed, so `'71` keeps its apostrophe.
- A trailing edition marker comes off, so `Alien DC` and `Alien Resurrection SE` match the films they are editions of instead of matching nothing. Anchored to the end of the title on purpose: at the front, those same two letters are `DC League of Super-Pets`.
- An episode tile says which episode it is. On Continue Watching, "Stray Dog Strut · 1998" read as an obscure film and is Cowboy Bebop S01E02.

### Also

The browse grid no longer offers **missing** files as tiles. The library count excluded them and the grid did not, so a library with unreachable files listed things that could not play.

Seven characters of mangled punctuation in the v0.6.27 API documentation are repaired — em dashes that a scripted edit had re-encoded into invalid bytes.

### Upgrading

No schema change in this release. The in-app updater replaces **LANcast-Server.exe** only, which carries everything above including the web client; the desktop client window and tray are unchanged.


---

## v0.6.27 — 2026-08-16

**Live TV works end to end — channels, playback in any browser, and a guide.** Plus everything from four feature passes that had not reached a tagged build: watch together, ratings, downloads, profiles, a pop-out player, trending, and an account system with a sharing decision behind it.

### Live TV

**Channel lists.** Add an M3U from an IPTV provider or a tuner on your network in Settings → Live TV. Channels are grouped the way the list groups them, because that is what makes six hundred channels navigable. A channel is deliberately not a library item — it has no duration, no file and no identity a metadata provider could match.

**The provider URL never leaves the server.** Channel lists are routinely credentialed — a token in the path, a password in the query — so clients play through this server by channel id and never see the address.

**Channels play in any browser.** Most IPTV is HLS carrying MPEG-TS, which Chromium decodes neither of, so the earlier relay worked in Safari and nowhere else. `/api/channels/{id}/live` puts a channel through the same ffmpeg pipeline files already use and emits fragmented MP4. Usually a *remux*, not a transcode — nearly every channel is H.264, so ffmpeg rewrites the container and copies the video at a few percent of a core rather than a whole one. Audio is treated the opposite way and that asymmetry is the design: a video encode costs a core per viewer and an audio encode costs a few percent, so audio is copied only when it is *known* to be AAC and re-encoded otherwise. Guessing wrong about audio gives you a working picture with silence, which is the failure that looks like success.

**The EPG.** A source can carry a second URL — an XMLTV guide, plain or gzipped. Every channel tile shows what is on now, how far through it is, and what is after it; the channel you are watching gets a schedule under the player; and search reads programme titles as well as channel names, so "is the football on anywhere" is answerable.

Listings attach to channels by `tvg-id` **and nothing else**. Matching on display name is the obvious fallback and it attaches "BBC One" listings to "BBC One HD" with total confidence — a failure that is invisible from the guide, since every title and time looks plausible and the only way to find it is to watch the channel. A channel whose list carries no `tvg-id` says it has no listings instead. Guides refresh themselves every twelve hours; expired listings are pruned a day after they end.

**Not built:** hardware tuners, and recording.

### Watching with other people

**Watch together**, from the player — the thing you want to share is the thing you are already watching, and everybody joins at the host's current position. The server owns the truth and clients converge on it, rather than each broadcasting a position and letting the last writer win. One host drives; the host leaving ends the room rather than promoting somebody nobody chose. A follower only corrects when it is out of step by more than a tolerance, because stuttering every two seconds to fix a quarter of a second is worse than the drift.

**Rooms live on one server.** Everybody joining needs an account on the machine hosting the room — a separate copy of LANcast cannot join it.

### Your account

**Ratings** — five stars over a ten-point score, with the half as a second click. Private to the account that wrote them; there is no household average, and withdrawing a rating is distinct from scoring something 1.

**Sharing is off by default, and stays off until you turn it on.** An upgraded server shares nothing as a side effect of updating — you cannot un-show a history.

**People** is Find Friends named honestly: the accounts already on this server. It says who has *chosen not to share* rather than showing an empty list, because "has not shared" and "watches nothing" are different statements.

**Profile** — identity, watch history and totals in one place. **User management** — create, rename and remove accounts; renaming keeps the account id, so history, ratings and playlists follow the name.

### Elsewhere

- **Downloads.** A Download button on any item with a file, serving the original untranscoded with a filename built from its metadata rather than its path. The `/downloads` page is a receipt list, not a transfer manager — the browser owns the transfer once it starts, so a progress bar here would be a guess.
- **Pop-out player.** Our own always-on-top window, keeping the scrubber, subtitle picker and queue that browser picture-in-picture takes away. Falls back to video PiP, and where neither exists the button is absent rather than dead.
- **Bigscreen mode** (`Ctrl+Shift+B`) — applied before first paint, so a television never flashes the desk layout.
- **Rebindable keyboard shortcuts.** Only your overrides are stored, so defaults that change later still reach you.
- **Trending, per library.** Counts *accounts* rather than plays, and reports how many contributed so it can decline to call one person's history a trend.
- **An add-ons page** and a **home masthead**.
- **A scan now tells you when a library is the wrong kind.** A shows library scanned as films skips nothing and imports everything, so no skip count could ever see it; a census of what the library actually holds can. Runs only after a *successful* scan, so a drive vanishing mid-scan cannot raise a false alarm about a permanent mistake.
- **Crash reports.** A panic becomes an ordinary 500 and a numbered report in the data directory, readable in Settings.

### Fixed

- **An unknown `/api` path answered 200 with an HTML page.** Unmatched API paths fell through to the single-page-app fallback, so a third-party client asking for a mistyped or newer endpoint got a success and a document — the least debuggable answer there is. A browser never noticed, because a browser never asks for a route that does not exist.
- **AAC inside MPEG-TS carries ADTS framing that MP4 refuses**, so live channels emitted a valid file header, rejected the first audio packet and exited — 16 KB where the fix produces 1.05 MB from the same source, and a browser showing one frame before stopping. The most common live format there is, failing in the way hardest to attribute.
- A **failed channel now says why** instead of showing a black rectangle.

### Upgrading

Schema revisions 23 and 24 are applied automatically on first start. There is no downgrade — restore a backup instead of rolling back.

The in-app updater replaces **LANcast-Server.exe** only. Everything above is in the server, including the web client it embeds, so the updater carries all of it; changes to the desktop client window or tray still need the installer.


---

## v0.6.26 — 2026-08-15

Two fixes to the settings screen, both reported from a running v0.6.25.

### Descriptions no longer get cut off

Every explanatory line under a setting was truncated with an ellipsis, which did not shorten the text so much as delete the half that carried the point:

> Folders cannot overlap — one inside another would be scanned twice, and which l…

It stopped exactly where it started being useful. Those lines wrap now, across every pane — Libraries, Metadata, Playback and the rest were all losing text the same way.

### Adding a library location has a Browse button

Typing an absolute server path from memory was the one field on that screen you could not check as you went: a typo was accepted, stored, and only showed up later as a location that scanned nothing.

Both the "add another folder" field and each existing location's move field now open the same folder browser that adding a library already used. Move opens at the location's current path, since a move is usually to a sibling of it.

### Upgrading

Nothing to do. No database changes in this release.


---

## v0.6.25 — 2026-08-15

A library can live in more than one place, the player's settings became one panel, and a scan that could not see a drive stopped emptying the library.

This release folds in the work recorded as v0.6.23 and v0.6.24, neither of which was ever tagged.

### A library in more than one place

Family films on one drive and the main collection on another were one library by every meaning that mattered, and the schema could only express them as two — which split the A–Z rail, split "play all", made collections spanning both impossible, and doubled every setting.

A library now has **locations**. Add, move and remove them in Settings → Libraries.

- **A location that cannot be read is skipped, not fatal.** An external drive asleep while the internal one is fine is ordinary, so the scan does the rest of the work and says "1 of 2 locations scanned — could not read E:/Family. Items there were left alone, not marked missing."
- **Removing a location removes its items**, and says how many before it asks. That is deliberate: a scan *deducing* a file is gone can be wrong and never deletes, but removing a location is you saying it is no longer part of the library.
- **Locations may not overlap.** One folder inside another would be scanned twice with no good answer about which location a file belongs to.
- **Moving one location carries its contents** — the drive-letter case — while the others stay where they are. Matches, artwork and watch progress travel with the folder.

### Playback settings, in one panel

The player's controls had grown into five separate popovers. They are now one **Playback settings** panel: quality, audio stream, audio device, speed, subtitles, subtitle colour, size, position and offset, and auto play. Subtitles keep their own button, because turning them on mid-scene is something you do while watching rather than a setting you change.

**Quality is new.** A ceiling on resolution and bitrate, for when the link between you and the server is the constraint rather than the file. Original is the default and the right answer on a LAN — a ceiling only ever *narrows*, and a file already under it is left completely alone rather than being re-encoded to hit a number.

**The subtitle offset** shifts cues instantly and in both directions, for a subtitle file that runs early or late.

These preferences are stored **per device**, not per account: how much bandwidth there is, what this machine's speakers are, and how big subtitles need to be at this viewing distance are all facts about the screen you are sitting at.

### Fixed: a scan could empty a library

**Scanning a library whose drive was unmounted marked every item in it missing — and reported the scan as successful.**

The directory walk returns "no error, zero files" for a root that is not there, which is indistinguishable further down from every file having been deleted. Nothing was ever deleted from disk, but the whole library vanished from view, and the periodic scan meant it needed no action from you to happen.

There are now two guards: one before anything is read, and one immediately before the only destructive step, which also covers a drive pulled out *during* a scan.

**If a library has ever gone mysteriously empty after a drive was disconnected, this was why.** Rescanning with the drive present restores it.

### Fixed: release group in the title

A file named `veto-beavis.and.butthead.do.america...mkv` inside a folder ending `-VETO` was titled "veto beavis and butthead do america". The group is a trailing marker on the folder and a leading one on the file, and only the trailing form was being stripped.

It is removed now — but only when the folder confirms it, so `Spider-Man.2002.mkv` does not become "Man".

### Also

- HDR files are now identified as HDR. Nothing changes yet; this is what a proper HDR-to-SDR conversion will need, and until now the server genuinely could not tell an HDR file from a 10-bit SDR one.

### Upgrading

The database migrates automatically. Existing libraries become single-location libraries and behave exactly as before.


---

## v0.6.22 — 2026-08-13

**Search everything, from anywhere.**

Search used to look inside one library at a time, which meant knowing where
something was before you could find it. There is now a **Search** in the top bar
— press **/** from anywhere — that looks through every library at once and
groups what it finds by the library it came from.

**Press ? to see the keyboard shortcuts**, wherever you are. They were only
listed in Settings, which is not where you are when you want them.

**A–Z letters on the Collections and Playlists pages too**, matching the library
grid.

**Two fixes in the player:**

- Double-clicking to leave fullscreen no longer pauses the film.
- Escape leaves fullscreen instead of closing the player and leaving the window
  stuck full-size over your library.


---

## v0.6.21 — 2026-08-13

**LANcast now tells you when the app itself needs restarting.**

Updating from inside LANcast replaces the server. It cannot replace the window
you are looking at — a running program cannot overwrite itself — so when a
release changes the desktop app, the window keeps running the old version until
you close and reopen it. Nothing used to say so. Now it does, once, at the top
of the page.

**Fullscreen finishes its job:**

- **Escape** leaves fullscreen.
- **Double-click** the picture to enter or leave it.
- The fullscreen button shows when it is on.

**A library of TV episodes added as a Movies library now says so.** It used to
import quietly and leave you with a grid of individual films, no series and no
seasons, and no clue why. Settings says how many files look like episodes and
what to do about it. (Adding audio to a Movies library has warned since v0.6.3;
this is the case that looked fine and was not.)

**Collections and Playlists keep their headers on screen** as you scroll, so the
way back and "New playlist" stay where you left them.


---

## v0.6.20 — 2026-08-13

**Fullscreen actually goes fullscreen.**

Pressing fullscreen in the LANcast window used to do nothing visible — the app
believed it was fullscreen while the window stayed exactly where it was. It now
fills the screen it is on, taskbar and all, and puts the window back exactly
where it was when you leave. Drag the player to another monitor first and it
fills that one.

**The letters and filters stay put, and take less room.** The A–Z rail was
sliding underneath the top bar as you scrolled; it now stops below it and stays
there. The whole strip is tighter, so more of the window is your library and
less of it is controls.

**After an update installs, the page refreshes itself.** The server was already
swapping itself and restarting without anything closing — but the page in front
of you kept running the old version until you reloaded it by hand. It does that
for you now, once the update is confirmed.


---

## v0.6.19 — 2026-08-13

**Fullscreen works properly, and the browse controls stay where you can reach
them.**

**Fullscreen kept the picture and lost the buttons.** Pressing fullscreen handed
the whole screen to the video and left every control behind — including the
mouse movement that would have brought them back. Fullscreen now takes the
player itself, controls and all.

**The A–Z letters and the filters stay at the top of the page** as you scroll.
Jumping to S from halfway down a library no longer means scrolling back up
first.

**The "ready to install" bar goes away by itself** once the update is installed.
It used to sit there offering to install something you had already installed
until you dismissed it.


---

## v0.6.18 — 2026-08-13

**Diagnostics, playlist tidying, and an install panel that cannot hang.**

**The update panel gives up gracefully.** If it cannot tell which version came
back within 45 seconds, it now says so and asks you to reload, instead of saying
"Installing…" for ever. The update itself is applied by the server, not by that
page — so a panel that loses track of it is a panel being unhelpful, not an
update that failed.

**Debug logging**, in Settings → Logs. Turn it on and the server starts writing
detail immediately — no restart — and keeps doing it after one. Leave it off
unless you are chasing something; it is verbose.

**Clear cached artwork** and **clear transcode scratch**, also in Settings →
Logs. Both come back on their own: artwork downloads again as it is needed, and
transcode files are rebuilt the moment somebody presses play.

**Reset settings** puts everything back to its defaults — and deliberately keeps
your password, your provider API keys, your certificate paths and where ffmpeg
is. Those are things a reset cannot give back to you.

**Playlists**: rename one from its own page (it could only be renamed from the
Playlists grid before), and a playlist tile now says "12 tracks" instead of
"12 items".


---

## v0.6.17 — 2026-08-13

**The update panel now notices when the update is finished.**

In 0.6.16, installing an update worked — the server restarted and came back on
the new version — and the panel kept saying "Installing…" over the top of it,
for ever. Reloading the page was the only way to find out it had already
succeeded.

It compares versions properly now, and it also accepts the simplest evidence
there is: the server came back as something other than what it was.

Nothing else changed. If you are on 0.6.16, this is the update that proves the
fix — the panel should finish on "Updated to LANcast 0.6.17" by itself.


---

## v0.6.16 — 2026-08-13

**Four things a full library makes you want.**

**Jump to a letter.** Movie, show and music libraries now have an A–Z strip above
the grid. Only the letters you actually have appear, and `#` collects everything
that starts with a number or a symbol. Press the same letter again to go back to
everything.

**Rename or move a library.** Settings → Libraries → Edit. Change what a library
is called, or where it is: if a drive letter changed or you moved a folder, point
LANcast at the new place and everything comes with it — matches, artwork, watch
progress, playlists. Nothing is re-identified and nothing is lost.

A library's *type* still cannot be changed. It decides which scanner runs and how
the library is browsed, so changing it would leave a library describing itself as
something it is not. Add the folder again as the type you meant.

**Collections have their own page.** They used to sit in the movie grid among the
films they group, which made a shelf you curated look like one you had not. There
is now a **Collections** button beside Play all. Searching still finds them,
wherever they are.

**Add-ons is in the sidebar**, at the bottom of your libraries — and it is there
even before you have added any, which is when you are most likely to be looking
for it.


---

## v0.6.15 — 2026-08-13

**The update panel now keeps up with the download.**

If you updated to 0.6.14, you may have seen Settings → Updates stuck on
"Downloading…" while the indicator at the top of the window already said the new
version was ready — and closing the server was the only way to move on.

Nothing was actually stuck. The download runs in the background and the panel
had simply stopped asking about it, so it kept showing you a minute-old answer.

- The panel now follows a download until it is ready to install, and stops
  looking once it is.
- The button shows how far along it is. A 16MB download behind a button that
  says only "Downloading…" looks exactly like one that has died.

**This is also the first update that finishes itself**, if you are running
LANcast outside of the service: press Install and restart, and the server stops,
swaps itself and comes back on its own, ending on "Updated to LANcast 0.6.15".


---

## v0.6.14 — 2026-08-13

**Updating and starting LANcast, without guessing.**

Two things that made you do the application's job for it.

**An update now finishes itself.**

Downloading an update used to leave you with one instruction — close LANcast and
open it again — and no way to tell whether it had worked short of starting the
server and reading the version number. LANcast knows exactly when an update
completes. Now it says so.

- Press **Install and restart**. The server stops, swaps itself, and starts again
  on its own, with the same settings it was running with.
- The panel says which stage it is at, and finishes on **"Updated to LANcast
  0.6.14"** — confirmed by asking the server what it is, not by assuming.
- A downloaded update is now a line at the top of the page. It used to be
  visible only in the activity list, which is a list of things the server is
  busy doing — not a good place to put something waiting on your decision.

**Double-clicking LANcast starts LANcast.**

If you have LANcast installed as a service, double-clicking the server used to
start a *second* copy that could not open the installed library, and failed with
a database error that told you nothing. The way through was an administrator
terminal.

Now a double-click starts the service you already have — with a single Windows
permission prompt if one is needed — waits for it to actually be ready, and
opens LANcast. If you have no service installed, nothing changes: it runs in the
tray as before.

The tray menu gained **Check for updates**, and **Quit** now says what it does:
Stop server and quit.

**Errors that tell you what to do.** A failed start used to show whatever the
database said. Now it names the likely cause — data that belongs to the service
account, or something else already using the port — and still shows the original
error underneath, for when you need it.


---

## v0.6.13 — 2026-08-12

**Settings that decide how your server behaves.**

Until now, Settings held API keys and a handful of toggles. This adds five rules
about what LANcast actually does — the kind of thing you set once and then stop
thinking about.

**Settings → Playback**

- **Counts as watched at** (90%). Stop past this much of a film or episode and
  it is finished. Credits are not the film, and a Continue Watching shelf that
  keeps offering you the last ninety seconds is a shelf nobody ever clears.
- **Weeks to keep in Continue Watching** (16). Anything you have not touched for
  longer drops off. Set it to 0 to keep everything for ever.
- **Items in Continue Watching** (40).

**Settings → Libraries**

- **Allow deleting media files from disk** (on). Turn it off and nothing on this
  server can delete your media — removing a title takes it out of the library
  and leaves every file exactly where it is.
- **Rescan libraries automatically** (off). For a server whose media arrives by
  other means: a downloader, a sync job, another machine writing to the drive.
  Hourly through weekly. A library that is already scanning is skipped, never
  queued behind itself.

The watched rule is applied by the **server**, so every device agrees about what
you have finished — your phone, your browser and your TV cannot disagree any
more. It never un-finishes something a player already reported as watched, and
it leaves anything with no known length alone.


---

## v0.6.12 — 2026-08-12

**Somewhere to keep your playlists.**

You could make a playlist, edit it and play it — and then have real trouble
finding it again. A music library's top level is artists, so nothing listed your
playlists; you got back to one by searching for its name and hoping.

**Music libraries now have a Playlists page**, next to Play all and Shuffle.

- Every playlist in that library, as a tile that says how many tracks are in it.
- **New playlist** — make an empty one and fill it, instead of having to start
  from a song.
- **Rename** and **delete**, on the tile.
- Tiles make their own artwork out of the covers of the first few tracks, since
  a playlist has no cover of its own.

Open one and it is the page you already know: reorder, remove, play.

**Three smaller corrections**, all things you would only ever catch by looking:

- **Fix match** no longer appears on a playlist. There is nothing to identify —
  a playlist is a list you named, and searching for it only finds films with
  the same name.
- An album with one track said **"1 tracks"**. It says "1 track".
- A track deliberately listed twice in a playlist was tagged with its filename,
  as though something were wrong with it. Nothing is wrong with it; the tag is
  gone.


---

## v0.6.11 — 2026-08-12

**Add to playlist, somewhere you can actually press it.**

v0.6.10 added playlist editing and put "Add to playlist" on an item's detail
page. For music that page cannot be reached — a track is a row in a track list,
never a poster tile, and nothing in LANcast navigates to a track's own page. The
button shipped and nobody with a music library could use it.

It now sits where a track actually is:

- **On the track row**, beside the other row controls, on every track list.
- **In the player**, whatever is playing — including a single track put on with
  nothing queued behind it, which is the most ordinary thing to want on a list.
- **In the mini-player**, for audio. "Put this on a list" is a thought you have
  while a song is playing, which is exactly when the track is not on screen
  anywhere else.

Nothing else changed: the playlists themselves, the editing, and the rule that a
rescan cannot undo your edit are all as they shipped in v0.6.10.

If you are on v0.6.10 and wondered where the button was — this is the release
that answers it.


---

## v0.6.10 — 2026-08-12

**Playlists you can change.** v0.6.9 shipped playlists that could be imported from
`.m3u` and played, and not edited — you could look at a list and press play on it,
and that was all. This release adds the rest: make a playlist, add to it, reorder
it, drop an entry, delete the list.

- **Add to playlist** appears on anything that plays on its own — a track, a film,
  an episode. It appends, and it can create the playlist on the way, so the first
  one takes a name and a press rather than a trip to another screen.
- **Reorder and remove** live on the playlist's own rows. Buttons rather than a
  drag handle, because this list is used with a remote as well as a mouse and a
  d-pad cannot drag.
- **Rows are numbered by position**, not by the track numbers they carried on
  their own records — a playlist drawn from six albums was numbering itself
  1, 4, 1, 9, 2. For the same reason it is never split into "Disc 1 / Disc 2"
  headings, which described the records the tracks came from and not the list you
  are looking at.

**A rescan still cannot undo your edit.** Editing a playlist marks its membership
as yours, and the scanner leaves it alone from then on — the `.m3u` on disk seeded
the playlist and is not the playlist. Verified against a real library: a scan
re-imported the untouched playlist beside it, left the edited one exactly as
edited, and did not write a byte to either file on disk.

**A playlist may still hold the same track twice**, and every edit respects it. A
set that opens and closes with the same song reorders and removes the copy you
pressed, not the first one with that name.

**Deleting a playlist deletes the playlist.** Not the tracks in it, and not the
`.m3u` that seeded it — being in a playlist was never where a track lived.

Playlist editing needs an account but not an admin one. Editing a list touches no
file and no library, and the audit log records who did it.

Also in this release: the API documentation is now checked against the router on
every build, in both directions. That document is what third-party clients build
against, and it had drifted three ways at once before anything was watching it.


---

## v0.6.9 — 2026-08-12

Playlists, a settings page you can navigate, and a run of playback fixes — two
of which were quietly costing you most of your music.

### Playlists

**The `.m3u` files already sitting in your music library become playlists on
scan.** They appear alongside your albums, open like anything else, and play.

- **A playlist can hold the same track twice.** A reprise, or a track that opens
  and closes a set, is ordinary — and it now works, in the list and in the play
  queue.
- **Nothing is dropped in silence.** A playlist referencing tracks you do not
  have imports the ones you do and tells you: *"imported 47 of 52; the rest are
  not in this library."* A playlist that resolves to nothing is still created,
  so you can see it exists and find out why it is empty.
- **Editing a playlist protects it.** Once you change one in LANcast, a later
  scan will not undo your edit from the file on disk.
- Playlists written by LANcast's own streaming are recognised and skipped rather
  than imported as if they were music.

### Settings

**Categories down the left instead of one long scroll.** Grouped by *whose*
setting it is — **Server** (libraries, metadata, playback, users, add-ons,
updates, activity, logs) and **This device** (account, this app, keyboard). That
split is the one that matters as soon as two people share a server: one group
changes what everyone gets, the other changes only what you get.

New in there, and reachable for the first time:

- **General** — the server version and the API version. Previously the settings
  page could not tell you which server it was configuring.
- **Provider rate limit** and **update checks** — both have been accepted and
  validated by the server since the beginning, editable only by hand-editing
  `config.json`.

### Playback fixes

- **Shuffle was stranding most of your queue.** Turning it on left the playing
  track wherever the shuffle dropped it and continued from there — so shuffling
  a 1,591-track library could start you at number 691 and end 900 tracks later,
  having never played the first 690. It now starts with what is playing and
  reaches everything.
- **The queue panel showed the wrong order.** With shuffle on it listed the
  unshuffled queue — a list of what is coming next that was wrong about what is
  coming next. It also now opens on the track you are hearing instead of at the
  top of a list a thousand rows long.
- **Going into the mini-player and back emptied the queue.** Every skip, shuffle
  and repeat control disappeared and nothing could advance. This affected albums
  and seasons too, not only playlists.
- **A repeated track no longer sends playback backwards.** The queue tracked
  position by identity, so the second copy of a track resumed from the first and
  everything after it was unreachable.

### Under the hood

Schema revision 17 (`playlist_entry`). A new `playlist_id` parameter on
`/api/items`, documented — it is the one listing that can return the same item
twice, which clients need to know before they key a list on id.

The client has its first test suite, running in CI.

**Upgrading:** the database migrates itself on first start. No configuration
change. The API only gained a parameter, so existing clients are unaffected.


---

## v0.6.8 — 2026-08-11

A testing pass over the player and the music library, and a run of faults that
shared a shape: present, plausible, and doing nothing at all.

### The audio track picker never worked

It had never rendered once in this project's history — nothing in the test
library carried two audio tracks, so it was gated off on every file. Generating
one found three bugs stacked behind it, each hidden by the one before:

- The client never sent `?audio=` when asking how to deliver a file, so the
  server answered about the file's *default* track. It then requested a raw byte
  serve, which cannot select a track at all — the tick moved and the sound did
  not.
- The delivery decision would have direct-played a chosen alternate track even
  when it could not deliver it, silently ignoring the choice.
- **Seeking dropped the chosen track**, asked to transcode without one, and was
  refused with a 409 mid-seek — killing playback with no way back.

Choosing an audio track now works, and seeking with one chosen no longer stops
the film.

### Music

- **Sorting.** A music library's top level is artists, and an artist row has no
  year — so "Year" ordered by a column that is empty for every row and returned
  the same list as "Title". Two of three options did the same thing, which is
  indistinguishable from sorting being broken. Year is no longer offered where
  there is nothing to sort by.
- **Deleting a track** was permitted, endpoint-backed and dialog-served, with no
  path to it — a capability you cannot invoke is one you do not have. There is a
  control on each row now, for admins, with the usual choice between dropping it
  from the library and deleting the file.
- **Identically titled tracks show their filename.** Mis-tagged files are
  ordinary: one here carries another song's title *and* track number, so an album
  shows two rows differing only in length. Two identical rows each with a delete
  button is a coin flip, and the losing side takes the file whose tags are right.
- **Play all** and **Shuffle** for a whole music library, shuffle starting from a
  random track rather than always the same one.
- **Play all** on an artist, queueing every track of every album.

### Player

- **Picture-in-picture shows the real running time.** A transcoded film reported
  a clock counting up from twelve seconds. The true timeline is now published
  through the media session — which also gives Windows' media overlay and the
  keyboard media keys a working display, something they had never had.
- **Only one subtitle track renders at a time.** Switching between two subtitle
  files left both showing, stacking cues up the screen with lines doubled.
- **Deleting a downloaded subtitle takes two presses.** It sat one button away
  from the row you press to play it, and deleting one is not undoable.
- A **stop** button, and a picture-in-picture, speed, audio-track and subtitle
  row that no longer drifts out of alignment on a music track.
- The docked mini-player no longer sits on top of its own controls, and its
  volume slider is reachable and the right size.

### Getting around

- **Back returns you to where you were**, instead of landing at the bottom of the
  page — and it sticks to the top so it is reachable from anywhere on a long one.
- **The navigation rail hides itself** once you have chosen somewhere to go.
- **Settings, your account and Sign out** move to the foot of the rail. The
  activity indicator stays where it was.
- An **artist page** shows a square sleeve rather than a film-shaped crop, and
  says how many albums it holds.

### Under the hood

The client has its first test suite, running in CI. It was written to answer one
question ahead of the picture-in-picture rework: whether the media element
survives being moved into another document. It does not — unless it is the only
child of a container React never varies, which it now is.

**Upgrading:** no database migration, no configuration change, no API change.


---

## v0.6.7 — 2026-08-10

The navigation stands up.

### A vertical rail

Library names used to run across the top of the window, competing with each other and with the account controls for one strip of pixels — so adding a fourth library made the third one shorter, and a long name was cut short by the existence of its neighbours.

They now live in a rail down the left side, one to a line, with their item counts in a column of their own. Nothing is shortened by anything else being there.

**It collapses to icons and opens when you point at it**, the way Plex's does. Collapsed it is a narrow strip of glyphs — a film strip, a screen, a note, a framed photograph — and hovering it or tabbing into it widens it to show the names. It opens *over* the page rather than pushing it sideways, because a page that shifts every time the pointer crosses the left edge is harder to use than a narrow rail.

Everything else stayed where it was: the LANcast button at the top left, and Review, activity, Settings, your name and Sign out along the top right.

Under reduced motion the rail still opens — otherwise the names would be unreachable — it simply appears rather than sliding.

### Upgrading

**If you are on v0.6.6, LANcast can do this one for you**, start to finish: Settings, then Updates, then **Download and install**, and when it says it is ready, **Restart now**. That is the first release where the whole path works from inside the app.

Earlier versions need the installer below. Your database migrates in place on first start.


---

## v0.6.6 — 2026-08-10

Updating from inside LANcast now finishes on its own.

### Restart to finish

If you installed an update in v0.6.5 and nothing seemed to happen, this is why. LANcast downloads and verifies an update, then applies it as the server shuts down — and when LANcast runs as a background service, nothing ever shuts it down. Closing the window does not stop a service. The update sat there, correctly installed and never started, and the only way through was to stop the service by hand, which left LANcast not running at all.

Settings now says **"Downloaded and verified. Restart to finish."** and offers a **Restart now** button that does exactly that: it stops the service, waits for it to genuinely finish stopping, and starts it again on the new version.

If your LANcast is not running as a service — you started it yourself, or you use the tray — it says so instead and asks you to close and reopen it, rather than shutting down something it cannot bring back.

### Upgrading

**One more manual install, and then this stops being a chore.** v0.6.5 can download this release but cannot finish installing it, which is the bug being fixed — so run the installer below this once. From v0.6.6 onward, Download and install followed by Restart now completes the whole update from inside the app.

No other action required. Your database migrates in place on first start.


---

## v0.6.5 — 2026-08-10

LANcast holds photographs now, and this is the first release it can install for you.

### Picture libraries

Point LANcast at a folder of photographs, choose **Pictures** as the type, and it scans them the way it scans films and music — no phone-home, no upload, nothing leaves the machine.

**Folders become galleries.** In a picture library the folder is the only grouping that means anything: the filenames are camera serials and UUIDs, and there is no provider to ask. So LANcast takes the folder at its word rather than guessing, and your titles are left exactly as they are on disk — a name that means nothing is still better than a tidied version of a name that means nothing.

**The library opens on a banner** cycling through your pictures, with the gallery grid beneath it. Open a gallery and the banner shows that gallery instead. Press any photo and it moves into the banner rather than sending you to another page — a photograph has no synopsis or cast to read, so showing it to you is the better answer.

**Expand fills the screen.** Arrow keys move through the gallery, escape leaves, the scroll wheel zooms and dragging pans, and there is a slideshow if you want one. It never starts by itself, and everything that moves stops if your system asks for reduced motion.

**Thumbnails are generated in the background** and reported in the activity panel, so a first scan of a large library finishes quickly and the pictures fill in behind it. Measured on a real 3.3 GB library: the scan took six seconds, the thumbnails two and a half minutes, and the cache came to about a quarter of the library's size.

**HEIC works**, along with jpeg, png, webp, gif, bmp and tiff. A phone backup is mostly HEIC, and a picture library that showed grey placeholders for your actual photographs would be a feature that looks finished and is useless. Anything LANcast's built-in decoders cannot read is handed to ffmpeg, which turned out to matter for more than HEIC — eight ordinary BMPs in the test library are readable by ffmpeg and not by anything else.

**Dates come from the picture, not the file.** Where a photo carries EXIF, LANcast reads when it was taken and sorts by that, falling back to the file's own date when there is none. It also reads which way up the camera was held, so a phone photo is not shown on its side.

**Location data is never read.** LANcast has no use for where a photograph was taken, so it does not load it — not to display, not to store, not to ignore later.

### Fixes

**Rescanning a music library no longer re-reads everything.** Every rescan was re-recording every track and re-queueing the whole library for metadata it already had. Present since music arrived in v0.5.0, found while building something unrelated.

### Upgrading

Your database migrates in place on first start.

**If you are on v0.6.4, LANcast can install this one for you** — Settings, then Updates, then Download and install. This is the first release the in-app updater has ever been able to fetch, so it is also the first real test of it. Earlier versions need the installer below.


---

## v0.6.4 — 2026-08-10

The in-app updater could find a new version but never install one. This release fixes that.

### Updating from inside LANcast now works

If you are on v0.6.2 or v0.6.3 and pressed **Download and install**, it sat on "Downloading…" and nothing happened. Two separate faults, both fixed here.

**The download failed immediately.** LANcast asked GitHub for the release details in the wrong format, and GitHub refused with an error the updater never showed you. Checking for updates worked, finding the new version worked, and only fetching it was impossible — which is why it looked like a stalled download rather than a refusal.

**And it did not tell you it had failed.** The download runs in the background after the button is pressed, and the failure was written only to the server log. The panel had no way to learn the outcome, so it kept saying "Downloading…" indefinitely. A download that died half an hour ago looked exactly like one still running.

Settings now reports a failed download in the same place it reports a failed check, and says which of the two happened — they are different problems. "I could not reach the project" is worth retrying later. "I fetched it and could not install it" is worth reading.

### Upgrading

**This one has to be installed by hand.** A broken updater cannot deliver its own fix, so v0.6.2 and v0.6.3 cannot install v0.6.4 for you — download the installer below. From this release onward, LANcast can update itself.

No other action required. The database migrates in place on first start.


---

## v0.6.3 — 2026-08-10

A home page worth opening, and two libraries that stop failing quietly.

### Home opens on something

LANcast's home page was rows of posters. It now opens on a spotlight: the film or show you are part-way through, full-bleed artwork behind a floating poster, with the synopsis, how far you got, and a Resume button. Nothing in progress, and it shows you the newest thing in your library instead.

The whole page has depth now — artwork sits above the background rather than flat against it, tiles lift as you move through them, and the backdrop drifts behind the shelves as you scroll. Artwork is tinted into LANcast's own deep-space field instead of being pasted on top of it, so a hero looks like part of the app rather than a photograph with an app around it.

**Listening is separate from watching.** A half-played track no longer sits between two films in Continue Watching — music has its own **Continue Listening** row, and newly added music has **New Music**, apart from Recently Added. Album covers are square and film posters are not, so a mixed row never shared a baseline; it read as broken alignment rather than as two kinds of thing. Tiles without artwork are no longer empty rectangles either, which matters most in a music library where many are.

Motion is off if your system asks for reduced motion, and the keyboard focus ring is untouched — it stays exactly as visible as it was.

### A library that ignores your files says so

If you add a music folder as a **Movies** library, LANcast scans every track, imports none, and — until this release — reported "0 items · scanned", which reads as *your folder is empty*. On a real library that was 1,592 tracks discarded in silence.

The scan now says what it ignored and why: **"1,592 audio files ignored — this library's type is Movies."** If the library is also empty it tells you the cure, since a library's type is fixed once chosen: remove it and add it again with the right type.

**The library type no longer defaults.** It used to be pre-set to Movies, sitting beside the name field, which is exactly how a library named "Music" pointed at a music folder became a movie library. You now choose the type deliberately, and Create stays disabled until you do. That choice cannot be changed afterwards — it decides which files are scanned at all and how titles are matched — so it is worth the extra click.

Files that are not media of the other sort are still ignored quietly. Artwork, `.nfo` sidecars and subtitles are ignored by every library, and counting those would bury the number that matters.

### Fixes

**The Start menu's LANcast Server entry pointed at the wrong place.** It carried a data-directory argument that never expanded, so starting the server that way opened a second, empty database beside the install instead of the one your library lives in. It now points at the same machine-wide directory the service uses.

### Known gap

A **show** library created as a **movie** library still scans silently. Both types read the same video files, so nothing is ignored and there is no count to report — but the result is subtly wrong: a miniseries can come out as a film in several parts. If a show looks misfiled, check the library's type first. A signal for this case is on the roadmap.

### Upgrading

No action required. The database migrates in place on first start.

If you are on v0.6.1, this is also the release where **LANcast opens in its own window** rather than handing you to a browser — that shipped in v0.6.2. `-browser` still opts out, and the installer offers both.


---

## v0.6.2 — 2026-08-09

LANcast can now update itself, and it opens in its own window by default. The source is public under MIT.

### LANcast updates itself

LANcast checks for a newer release, tells you in the activity panel and in Settings, and can download and install it for you. The install happens on the way down: the update is staged, and the swap completes as LANcast shuts down, so it takes one restart rather than two — no second installer window, no administrator prompt.

**Every release is now signed**, and that is what makes automatic installation defensible rather than merely convenient. Installing an update means a process running as the system fetching a binary and executing it; without proof of who produced it, that is a hole rather than a feature. Each release carries a signature over its checksum list, verified against a key built into LANcast itself — nothing downloaded alongside the update is trusted to vouch for it.

Three outcomes, kept deliberately distinct:

- **A signed release** can be installed automatically.
- **An unsigned release** — anything cut before this one — is offered for a manual download and never installed automatically. Older releases keep working on honest terms instead of breaking.
- **A signature that is present and wrong** is refused outright, and nothing is downloaded.

The check is on by default and can be switched off in Settings, where a manual check still works — wanting no timer is not the same as wanting no answer. It sends a plain request for the list of releases: no install identifier, no library statistics, no version history. LANcast still needs nothing from the internet to run.

LANcast will not offer to downgrade you, will not offer a pre-release as though it were stable, and a development build never offers anything at all.

### The LANcast window is now the default

Opening LANcast opens LANcast, not your browser. The window was optional in v0.6.0 and has been used since; it wins on three things a browser tab cannot do — LANcast can decide what its own close button means, it can confirm it is talking to your server rather than trusting a certificate warning you have to click through, and it has somewhere to put the tray and startup options.

If you prefer the browser, it is still there: the installer's final page offers **"Open LANcast in my browser instead"**, and `-browser` selects it from a shortcut. A machine without the WebView2 runtime falls back to the browser on its own and says which piece is missing. If you set LANcast to open when Windows starts, it remembers which of the two you chose.

Existing shortcuts keep working, including ones that pass `-window`.

### Fixes

**Upgrading over a running LANcast is more reliable.** LANcast could keep its database file open for a moment after reporting a clean stop, if it was stopped while still starting up. The installer stops a running instance before replacing its files, and a stop is likeliest to race a startup exactly when something is restarting it — which is when a held file makes the replacement fail.

**Editing one field no longer rewrites the rest.** When LANcast writes a metadata file beside your media, it now records that you authored the field you actually changed, rather than claiming the whole file.

### Source and licence

The repository is public under the **MIT licence**, which is also what gives the update check something to look at — the release list of a private repository is invisible even to the person who owns it. Vendored components keep their own notices.

### Upgrading

No action required. The database migrates in place on first start.

Updating **to** this release is still a manual download: automatic installation needs a signed release to install, and this is the first one. From here on, LANcast can offer it to you.


---

## v0.6.1 — 2026-08-09

A day of bug fixes, an audit log, and desktop lifecycle controls. Two of the fixes are for problems you would have hit without ever seeing an error.

### Fixes

**Scans no longer die partway through.** A scan could stop with "database is locked" if metadata enrichment committed at the same moment. The transaction failed instantly rather than waiting, because the SQLite error involved is not the one the timeout covers, and a finishing scan is what starts enrichment in the first place — so it was likeliest exactly when the server was busiest.

**New films and shows get metadata again.** On any server with a music library, anything added afterwards was never enriched. Music has no metadata provider, so those items sat permanently at the head of the queue and blocked everything behind them. Fixed twice over: the worker now looks past work it cannot do, and music no longer enters the queue at all — which also means the "remaining" count finally counts things that can actually happen. On a real library it went from 2,198, a number that never moved, to 7.

**The service stops when you tell it to.** `Stop-Service` did not keep LANcast stopped: Windows judged the stop a hang, logged the service as having terminated unexpectedly, and the recovery policy restarted it five seconds later. Shutdown is now bounded and forced at the end, and the service tells Windows how long its stop will take.

**Closing fully closes.** The client used to report itself closed the moment it asked the server to stop, rather than when the server was gone, and background work could still be holding the database after a shutdown reported success. Both now wait, with a bound.

**A wrong title no longer outlives your database.** LANcast writes metadata sidecars beside your media, and it wrote them for films it had failed to identify — committing a guess to disk under its own name, where it survived the database and was inherited by the next one. It now writes a sidecar only for an identity it actually established. Separately, the check that recognises LANcast's own sidecars is now version-tagged, so a future release that hashes a different set of fields cannot silently reclassify every sidecar you own as hand-edited and start trusting stale contents over live metadata.

### Audit log

Who changed what, and when — libraries added or removed, titles deleted, matches overridden, accounts changed, add-ons trusted. Recorded on the server, where the change is authorised, because an audit trail a client writes is forgeable by the client it is auditing.

Readable from Settings, newest first, filterable by action. Browsing and playback are deliberately not recorded: burying the handful of deliberate acts that matter under a million routine ones is how a log becomes unreadable.

Each entry keeps the name of whoever made the change and a sentence describing it, frozen at the time it happened — so "who deleted this library" still answers after both the library and the account are gone.

### Desktop lifecycle

Two options in the LANcast window, under **This computer**, both off by default.

**Close to tray** keeps LANcast in the notification area when you close the window, with Quit in the tray menu to stop it properly. **Open when Windows starts** launches it when you sign in, and the uninstaller clears that entry.

They appear only in the LANcast window, not in a browser tab, because a tab has no tray to reduce to and no close button LANcast owns.

The same section now says plainly what closing the window will do — stop the server, or leave it running because the service owns it. "Closed" has meant three different things in LANcast and all three were correct; this states which one you are looking at.

### Upgrading

No action required. The database migrates in place on first start.

If you had a film with a wrong title that came back after a rebuild, the sidecar beside it on disk is the cause. Correct or delete that file, then use Fix match — a match you confirm is locked and no longer at risk.


---

## v0.6.0 — 2026-08-08

Music becomes a first-class experience in the client, LANcast gets its own window on Windows, and the server finally shows you what it is doing.

### Music has a client

v0.5.0 shipped music libraries server-side with nothing to play them from. That gap is closed: artist and album tiles that read as music rather than as film posters, an album view with a numbered track list in the order the record actually plays, an audio mode in the player, and a docked mini-player so leaving the player no longer stops the record.

Album artwork comes off the disk — the picture embedded in a track first, then a `cover.jpg` or `folder.jpg` beside it. On a real library that found artwork for 369 of 398 albums in under eleven seconds with no network. A directory's image is refused when that directory also holds audio belonging to something else, which is what stops one letter-bucket `folder.jpg` being worn by five unrelated records.

Albums now know their artist and year, derived from their tracks on every scan, so the album view is no longer a bare title and the year sort has something to sort.

Artists without a picture borrow their most substantial album's cover rather than showing an empty tile. A real artist image supersedes it automatically, with nothing to clean up.

### A LANcast window on Windows

`LANcast-Client.exe -window` opens the UI in a window LANcast owns instead of handing a URL to your browser. It is opt-in this release.

The window pins your server's certificate, read from that server's own `cert.pem` on local disk, and validates every other certificate normally. This matters more than expected: against a LAN-bound server a web view does not show a certificate warning you can click through — it fails the handshake and retries, so the app simply never loads.

The binding is pure Go and builds with `CGO_ENABLED=0`, so this costs nothing in the release matrix. Microsoft's signed WebView2 loader ships beside the executable.

### Playback stops re-encoding what your browser can already play

Clients now tell the server what they can decode, and the server widens the playback profile to match. `?profile=` had existed for a release and no client ever used it, so a browser that decodes HEVC in hardware was still served a full re-encode of every HEVC file — most of the "slow between films" problem. A claim that turns out to be false is dropped, remembered, and retried as a conversion.

### See what the server is doing

A new activity indicator in the navigation shows scans, metadata fetches, file probing, artwork extraction and live transcodes as they happen, in one place, with progress. A scan reports what it has seen rather than a percentage it would have to invent, and a scan that fails stays visible instead of disappearing quietly.

The server log is readable from Settings. It has been written beside the database since v0.4.2 and until now could only be read by finding the data directory in a file manager — which is the wrong thing to ask for precisely when it matters, namely a server running as a service with no console and something wrong.

### Fixes

**Scans no longer abort when background work is running.** A scan could die partway through with "database is locked" if metadata enrichment committed at the wrong moment. The transaction failed instantly rather than waiting, because the SQLite error involved is not the one the timeout covers. Since a finishing scan is what starts enrichment, this was likelier the busier your server was.

**New items are enriched again.** On any server with a music library, films and shows added afterwards never received metadata. Music has no metadata provider, so those items stayed queued forever and blocked everything behind them — the queue was never getting past them. Anything added after your music library was scanned in was silently left without metadata.

**Playback and player fixes.** Audio survives leaving the player screen, the player screen is opaque while it starts and says that it is starting, and clicking one film no longer starts the previous one.

**Album grouping fixes.** A shared folder image is no longer adopted as an album cover, and a letter-bucket directory is no longer mistaken for an album.

**Diagnostics.** When a second server is already running, the message names which one — the service, its process id, and its data directory — read at a privilege an unelevated caller actually has.

### Upgrading

No action required. The database migrates in place on first start, and nothing in this release changes on-disk layout.

The native window is opt-in: add `-window` to the client to try it. Your browser remains the default.


---

## v0.5.0 — 2026-08-03

LANcast learns about music. This release adds a fourth library type and the
whole path behind it — scanning, reading embedded tags, grouping tracks into
artists and albums, and delivering audio to a client without mangling it.

It is a server-side release. The web client has no music player yet; see
**What is not here** below.

### Music libraries

A music library is now its own kind, alongside movies, shows and pictures. Point
one at a folder and LANcast walks it, indexes every track it recognises, and
groups them into artists and albums.

The formats it will index are deliberately wider than the formats it can play
directly: MP3, FLAC, M4A, AAC, Ogg, Opus, WAV, AIFF, WMA and ALAC. A file the
server cannot deliver without converting is still better indexed and reported
than silently skipped — you should be able to see that a track exists even when
playing it costs something.

### Tags are the authority, not the filename

For films, LANcast guesses from the filename because that is usually all there
is. Music is the opposite case: the tags were written by a tagger, and the
filename was written by whoever ripped the disc. So for music the embedded tags
win — ID3v2 on MP3, Vorbis comments on FLAC and Ogg, atoms on MP4.

Getting this right meant reading real files rather than a specification, because
taggers do not agree with each other and the specification does not bind them.
Case varies by container. One real file carried the album artist under two
different spellings at once. Track numbers arrive as fractions, and sometimes
malformed ones with no numerator at all. Dates are sometimes a year and
sometimes a full date. All of those are handled.

An untagged file is not an error. It falls back to the folder and filename, so a
badly-kept corner of a library still lands somewhere sensible instead of
vanishing.

### Audio is no longer re-encoded to play it

This is the fix with the most direct effect on quality.

A music file's container *is* its codec — an `.mp3` reports its container as
`mp3`, a `.flac` as `flac`. LANcast's playback decision had never been told
about those, so every track failed the container check and was repackaged into
MP4. MP4 cannot carry FLAC, which meant the fallback was not a cheap repackage
but a **lossy re-encode of a lossless file** — to deliver a format the browser
already plays natively.

Music formats now direct-play. FLAC reaches the client untouched. Apple clients
are told about ALAC and not told about Ogg, matching what Apple actually ships
decoders for. Where a conversion genuinely is needed — 24-bit WAV in a browser,
WMA anywhere — it still happens, and the reason is still stated rather than left
for you to find in a log.

Two related things that only show up on a real machine were fixed alongside it.
Converting a file with no picture used to fail outright, because the conversion
asked for a video stream that was not there; embedded cover art no longer gets
mistaken for one. And a directly-played FLAC is now served with the right type
rather than whatever the Windows registry happened to claim, which is the
difference between playing and prompting you to download it.

### What is not here

**The web client cannot play music yet.** Everything above is the server: the
scanner, the library, the tags, the grouping, and the playback decision. The
player in the browser is still video-only, and nothing in the interface routes a
track to it.

If you are building against the API, music is usable now — the endpoints and
the playback decision are documented in `docs/api.md`. If you are using the web
interface, music will appear there in 0.6.0.

### Upgrading

Nothing changes about your existing databases, settings or libraries. Movie,
show and picture libraries behave exactly as before.

To index music, add a new library and choose the music type. Existing libraries
are untouched.

One note if you have already been experimenting with music from a development
build: playback decisions made by an older build were made without knowing about
audio containers, and nothing in the normal queue revisits an already-probed
file. Re-probe from settings to pick up the new decisions.


---

## v0.4.3 — 2026-08-03

Two fixes to the parts of LANcast that are supposed to keep it honest: the log it writes when something goes wrong, and the guard that stops two servers running at once. Both were broken in ways that only appear on a real installed machine.

### Two servers could run at once

Launching the app while LANcast was already running as a service started a **second server**, on its own port and, before v0.4.1, its own database. It is the reason behind the "which library am I looking at" confusion, and the reason a launch sometimes appeared to do nothing.

The guard for this already tried to span sessions — a service runs in one, your desktop in another — but the check could not see across the boundary. Windows returns the same "access denied" both when an object exists and you may not open it, and when you simply lack the privilege to create it. Reading that as the second case, LANcast fell back to a check that only covered its own session, and reported the name free.

It now asks the question a different way, one that separates those two cases, and creates the shared name so that other sessions can see it. Verified against a real service: the old code said "nothing is running" from a desktop session while the service was up; the new code correctly refuses.

### The service log was empty

v0.4.2 added a log file so a service that stops can say why. It wrote **nothing** — the file was created and stayed at zero bytes, in the one situation it exists for.

A service has no console, so writes to it fail; the log was paired with the console in a way that gave up on the first failure and so never reached the file. The pairing now writes the file first and cannot be stopped by the other half.

If you installed v0.4.2 for the logging, this is the release that makes it work.

### Upgrading

Nothing changes about your database, settings, or libraries. The cross-session guard applies as soon as you run the new build; the log applies to the service the next time it starts.


---

## v0.4.2 — 2026-08-03

Makes a server running as a Windows service diagnosable. Nothing user-facing changes; no database or API change. Take it if you run LANcast as a service, or skip it if v0.4.1 is behaving.

### The service can say why it stopped

A Windows service has no console and no inherited stderr, so **everything LANcast logged was discarded by the operating system**. When a service exited there was no record anywhere except Windows' own "terminated unexpectedly" — which does not say why, and cannot tell a crash apart from someone killing the process.

The server now writes **`lancastd.log` in its data directory**, beside the database — `%ProgramData%\LANcast\lancastd.log` for the default install. One previous generation is kept as `lancastd.log.1`, capped at 4 MB, so a server running for months cannot fill a disk explaining itself.

Every exit is recorded on the way out, including a refusal to start. That is the line worth having and it was previously the one guaranteed to be lost.

### The service comes back on its own

No recovery actions were configured, so when the process died Windows did nothing and LANcast stayed down until somebody noticed it was missing.

New installs restart three times after an unexpected exit, at five seconds, fifteen, then a minute, and stop after that. A server that genuinely cannot start — a database written by a newer build is the case that happens — fails those three and stays down with a clean record, rather than restarting forever.

This applies at install time, so an existing service keeps its current settings until LANcast is reinstalled.

### Reading the event log

`docs/install.md` gains a section on this. Two Windows event IDs are worth telling apart:

- **7023** "terminated with the following error" — the server reported a failure. It decided to stop, and `lancastd.log` says why.
- **7034** "terminated unexpectedly" — the process disappeared without telling Windows anything, which is what an external kill looks like.

That distinction is the difference between "LANcast is broken" and "something stopped it", and it is not obvious from the wording alone.


---

## v0.4.1 — 2026-08-03

Windows fixes. v0.4.0 installed and then behaved badly on the desktop — command windows flashing, a browser refusing to connect, and the app quietly opening the wrong database. Nothing here changes the database or the API.

**If you are on v0.4.0, take this one.** Every fix below affects a fresh install.

### Command windows no longer flash

Opening the app popped three or four command windows before it showed anything, and scanning a library popped one *per file*.

LANcast's Windows executables have no console of their own, so every `ffmpeg` or `ffprobe` it started got a visible console window from Windows. Hardware-encoder detection alone runs one probe plus a test encode per candidate encoder, at every startup; the probe pass then runs `ffprobe` once per media file. Child processes are now started without a window.

### "This site can't provide a secure connection"

The plaintext listener answered with a **permanent** redirect to HTTPS, and browsers cache those indefinitely. But the scheme is not permanent — a server is on HTTPS only while it is secured *and* reachable beyond this machine, so clearing accounts or binding to loopback puts it back on plain HTTP at the same address. The browser kept speaking TLS to a plaintext listener and showed `ERR_SSL_PROTOCOL_ERROR`.

Now a temporary redirect. **If you already hit this, one visit in a private window clears it**, or clear cached files.

### The app could open a second, empty database

Launching the client when the server was not running started one with no data directory set — so it used the per-user location while a service install uses the machine-wide one. Two databases, one port, and whichever won decided which library you saw. After a failed service start it would silently create an empty database rather than show the real one.

The client now starts the server on the shared database when one exists.

### Separate Start menu entries

**LANcast Client** and **LANcast Server**, instead of one entry named LANcast that was actually the client. The server entry is pinned to the same data directory the service uses, so starting it by hand cannot open a second database. The desktop shortcut is still the client. Upgrading removes the old entry.

### Smaller

- **Add library** puts the cursor in the first field. It previously landed on nothing, leaving keyboard users with no focus at all. The field now reads "Library name".
- **`-version` and subcommands print on Windows.** Release builds have no console, so a server started from a terminal said nothing — including when explaining why it would not start.

### Known gap

A server running as a Windows service still writes its log nowhere, so if it exits unexpectedly there is nothing to read. Running the server from a terminal now works for diagnosing that, but the service should keep its own log. Next.


---

## v0.4.0 — 2026-08-02

Playback decisions were making silent, wrong calls. This release fixes them, adds a way to re-read files an older build inspected, and adds a way back in if you lose your password.

### Upgrading: this migrates your database

The schema moves to revision 12, and **there are no down migrations**. Once a database is opened by this version, older builds refuse to start with "database is schema version 12 but this build supports 11". Upgrade the server and any second install that shares the data directory together.

### Playback: five fixes, one of them silent

Every one of these produced no error message.

**The chosen audio track is now the one the decision is about.** Picking a track with `?audio=` mapped that stream in ffmpeg while the decision was still made about the file's *default* track. On a release with an AC-3 default and a DTS alternate, asking for the alternate produced `-c:a copy` on undecodable audio — the film played with no sound and nothing in the logs.

**Client profiles.** `?profile=browser` (default), `safari`, or `tv`. Previously every client was assumed to be a conservative browser, so HEVC always took a full video re-encode. Measured against a 225-film library: Safari drops full re-encodes from 37 to 11; a TV profile direct-plays 214 of 225. The default is unchanged and still excludes HEVC on purpose — Chrome's support is hardware-conditional and Firefox has none.

**Streams are only copied if MP4 can carry them.** "The client can decode this codec" is not "MP4 can hold it". VP8, Vorbis, FLAC and Opus made ffmpeg refuse to start, which reached the player as a dead video with no reason given.

**10-bit detection reads `pix_fmt`.** It previously matched the substring "10" anywhere in the profile name — both missing real 10-bit files and re-encoding innocent ones.

**A video re-encode no longer re-encodes good audio.** Copying it is free and avoids a second lossy generation.

### Re-read media files

Settings gains **Re-read media files**. A probe is only as good as the build that made it, and nothing revisits an already-probed file — so anything inspected by an older version keeps a decision made without what this one knows.

**Only what's missing** re-reads just the files a current build would learn something from. **Everything** re-reads all of them, behind a confirmation. Around 15 seconds per 225 files on local storage.

Also `POST /api/probe/refresh?scope=incomplete|all`.

### Locked out? `reset-auth`

There is no password reset over the network and there will not be one — a recovery endpoint an unauthenticated caller can reach *is* the authentication bypass. Recovery is local instead:

```
lancastd reset-auth            # reports what it would remove
lancastd reset-auth -yes       # removes every account and session
```

Watch history, libraries and settings are kept, and the replacement admin inherits the old one's resume points. Needs an elevated shell when the data directory belongs to a service account — it says so rather than reporting a raw SQLite error, and it names the other database if you point it at the wrong one.

### Other fixes

- **Launching the app opens it.** The first launch started the server and showed a tray icon and nothing else; you had to run it a second time to get a window.
- **NFO sidecars stopped growing.** Each write baked the previous write's indentation into the file as escaped newlines. One real sidecar went from 10 to 16 in a single rewrite, and it compounded on every enrichment pass. Existing files repair themselves on the next write.
- **Library counts match what you see.** A shows library counted seasons and episodes as items, so three series read "21 items" beside a grid holding three tiles.
- **No certificate warning on a loopback-only server.** A server bound to `127.0.0.1` was treated as network-reachable purely because a password was set: it served HTTPS with a self-signed cert and reported itself as LAN-enabled while listening to nothing but itself.
- **"Restart to reach other devices" is only shown when a restart would do that.** With a loopback address configured, restarting changes nothing.
- **`-version` and subcommands print on Windows.** Release builds have no console attached, so their output went nowhere.

### API

`docs/api.md` is current. New or changed: `?profile=` and `?audio=` on the playback and stream endpoints, `POST /api/probe/refresh`, `restart_required` on `GET /api/auth/status`, `pix_fmt` on stream objects, and `lan_enabled` now meaning the socket actually reaches beyond this machine.


---

## v0.3.2 — 2026-08-02

The first release tested against a real library, and it found real problems. This
is the fix release — **upgrade over 0.3.0 by running the installer; it keeps your
library and settings.**

### Playback: most files should now actually play

**If titles played with no sound, or sound with no picture, or hung — this is
the fix.** LANcast inspects each file with ffprobe to decide whether it can be
played as-is, remuxed, or converted. Running as a Windows service it could not
*find* ffprobe: the service account's PATH does not include a per-user install.
So nothing was inspected, every file was sent to the browser untouched, and
anything the browser could not decode failed with no explanation.

LANcast now finds and remembers where ffmpeg lives, and **says so when it cannot**
— Settings shows a "Media tools: missing" row rather than failing silently.

After upgrading, leave it running for a while: it inspects the library in the
background, and playback improves as it goes.

### The whole library is browsable

The grid loaded a single page and stopped, so a large library appeared to end
partway through the alphabet with no sign there was more. It now **pages as you
scroll**, and the count reads honestly ("120 of 1,226") until everything is
loaded.

### Volume

A **volume slider** beside the mute button, with **↑/↓** as shortcuts. The level
is remembered across titles and sessions.

### Knowing which file a title is

Wrongly matched titles could not be corrected when several rows shared a generic
name — the numbered pieces of an anthology are indistinguishable. The **filename
is now shown** on the detail page and in the **Fix match** dialog, and can be
selected and copied into the search. Only the name is shown; the folder it lives
in stays private.

### Upgrading from an earlier build

The installer now **stops anything still running** before it installs. Previously
an old tray client survived the upgrade, held the single-instance lock so the new
one could not start, and left you on the old build with no sign of it.

**Opening LANcast no longer fails with "the server did not come up".** LANcast
serves HTTPS once an account exists; the launcher checked over http, followed the
redirect into its own self-signed certificate, and concluded the server was down
— while it was running the whole time.

### Installer

The welcome screen now reads correctly — the tagline was clipped — and the panel
carries the LANcast mark.

### Also

- The executables are now named **LANcast-Server** and **LANcast-Client**.
- **Only one server and one client run at a time.** Launching either again opens
  the existing one instead of starting a duplicate.
- Double-clicking the server no longer does nothing: it opens the app.

### Known limits

- **Theme music** is not built yet.
- **macOS** is not built — Windows, Linux amd64 and arm64 are.
- The installer is unsigned, so Windows SmartScreen warns on first run.
- ffmpeg is not bundled; install it separately for conversion.


---
