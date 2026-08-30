// Package raise lets a second launch bring the running client's window
// forward instead of opening a second, different user interface.
//
// # Why this exists
//
// Launching the client while one is already running acquired nothing from the
// singleton mutex and then **opened a browser**, under a comment saying it
// "reopens the UI". It does not: the browser is a different interface, with no
// pinned certificate and a warning page the window exists to avoid (ADR 0023).
// Pressing a shortcut a second time and getting a browser is the wrong answer
// twice over — it is not what was asked for, and it is the fallback rather than
// the front door.
//
// Reported from a mouse button bound to the client shortcut: the Start menu
// entry opened the window, the same shortcut fired again opened a browser
// beside it.
//
// # Why a named event rather than finding the window
//
// The obvious alternative is to look for the existing window and foreground it.
// The webview registers its class as `"webview"`, which is not LANcast's — any
// Go program using the same library answers to it — so a search would be
// matching on a title, in a list of windows this process does not own.
//
// A named event is exact: the running client creates it, a second launch opens
// it by name and sets it, and nothing else can be mistaken for either.
package raise

// Signal asks a running client to show its window. It is a no-op where the
// mechanism does not exist, which reads as "there was nobody to tell".
func Signal() error { return signalShow() }

/*
 * Quit asks a running client to close.
 *
 * A second verb rather than a second package, because it is the same
 * conversation: the client is a window somebody may not be able to see, and
 * these are the two things to say to it.
 *
 * It exists because the client's own tray icon was the *only* way to quit it
 * once close-to-tray was on — the window's X hides rather than closes — so
 * removing that icon to leave a single LANcast presence in the notification
 * area would have left the app unquittable. The server's tray controller says
 * this instead.
 */
func Quit() error { return signalQuit() }

// Listen calls show whenever another launch asks for the window, and quit when
// something asks the client to close, until the returned stop is called.
//
// Errors are returned rather than logged: a client that cannot listen still
// works perfectly as a window, and the caller is the half that knows whether
// that is worth saying out loud.
func Listen(show, quit func()) (stop func(), err error) { return listen(show, quit) }

/*
 * TrayPresent reports whether something is there to bring the window back.
 *
 * Close-to-tray hides the window rather than closing it, which is only a
 * feature while an icon exists to restore it from. The client stopped having
 * one of its own deliberately — two LANcast icons in the notification area was
 * reported as exactly that — and the server's tray took over saying Open and
 * Quit to it.
 *
 * But nothing *starts* the server's tray. The service runs without one, so a
 * machine that has only ever booted has no icon anywhere: the X then hid a
 * window nothing could restore, and the app became a process with no way in and
 * no way out. Reported as the process staying open with no way to reopen it.
 *
 * So the hiding is now conditional on this. A tray that is present keeps the
 * behaviour it was built for; a tray that is absent means the X closes, which
 * is what a window with no icon behind it has to do.
 *
 * False where the mechanism does not exist, which is the safe direction: it
 * makes closing mean closing.
 */
func TrayPresent() bool { return trayPresent() }

/*
 * HoldTray marks a tray as running, until the returned release is called.
 *
 * Held by the tray rather than published by the client, because the question is
 * about the tray: the client is asking "if I hide, can anything bring me back",
 * and only the thing that would bring it back can answer honestly.
 */
func HoldTray() (release func(), err error) { return holdTray() }
