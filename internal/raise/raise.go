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
func Signal() error { return signal() }

// Listen calls show whenever another launch asks for the window, until the
// returned stop is called.
//
// Errors are returned rather than logged: a client that cannot listen still
// works perfectly as a window, and the caller is the half that knows whether
// that is worth saying out loud.
func Listen(show func()) (stop func(), err error) { return listen(show) }
