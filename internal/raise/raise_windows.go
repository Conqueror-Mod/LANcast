package raise

import (
	"fmt"
	"sync/atomic"

	"golang.org/x/sys/windows"
)

/*
 * Session-local, not Global.
 *
 * A window can only be brought forward within its own session, so a name that
 * crossed sessions would let one desktop's launch signal another's client and
 * do nothing visible — a signal delivered somewhere nobody is looking. Local
 * also needs no privilege, where Global does; the singleton mutex has to reach
 * across sessions to find a service and pays for that, and this does not.
 */
/*
 * The two things the server's tray says to the client.
 *
 * A variable rather than a constant so a test can take a name of its own. These
 * are auto-reset events and exactly one waiter wakes per signal, so a test
 * sharing the real names loses every signal to a client that happens to be
 * running — which is not a flake but a correct feature behaving correctly
 * against a test that was not isolated.
 */
var eventPrefix = `Local\LANcast-Client`

func showEventName() string { return eventPrefix + "-Show" }
func quitEventName() string { return eventPrefix + "-Quit" }

/*
 * Auto-reset, so each launch wakes the listener exactly once.
 *
 * CreateEvent takes *manualReset*, and passing 1 is the opposite of what is
 * wanted: a manual-reset event stays signalled, so the wait returns immediately
 * for ever and the window is foregrounded in a loop — a fix worse than the bug.
 * Written the wrong way round first and caught by a test asserting that nothing
 * arrives unbidden.
 */
const manualReset = 0

func signalShow() error { return signal(showEventName()) }
func signalQuit() error { return signal(quitEventName()) }

func signal(event string) error {
	name, err := windows.UTF16PtrFromString(event)
	if err != nil {
		return err
	}
	h, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
	if err != nil {
		// Nobody is listening. Not an error worth surfacing: it is what a
		// first launch looks like, and the caller goes on to be that client.
		return nil
	}
	defer windows.CloseHandle(h)
	if err := windows.SetEvent(h); err != nil {
		return fmt.Errorf("raise: %w", err)
	}
	return nil
}

func listen(show, quit func()) (func(), error) {
	showH, err := openOwn(showEventName())
	if err != nil {
		return func() {}, err
	}
	quitH, err := openOwn(quitEventName())
	if err != nil {
		windows.CloseHandle(showH)
		return func() {}, err
	}

	/*
	 * One waiter per event rather than WaitForMultipleObjects.
	 *
	 * Two goroutines blocked on a handle each is the same cost and reads as
	 * what it is; a multiple-wait would have to decode which index fired and
	 * re-arm, which is where this kind of code goes wrong.
	 */
	var stopped atomic.Bool
	watch := func(h windows.Handle, fn func()) {
		for {
			ev, err := windows.WaitForSingleObject(h, windows.INFINITE)
			if err != nil || ev != windows.WAIT_OBJECT_0 {
				return
			}
			if stopped.Load() {
				return
			}
			fn()
		}
	}
	go watch(showH, show)
	go watch(quitH, quit)

	return func() {
		stopped.Store(true)
		// Wake both so neither goroutine outlives the client holding a handle.
		_ = windows.SetEvent(showH)
		_ = windows.SetEvent(quitH)
		_ = windows.CloseHandle(showH)
		_ = windows.CloseHandle(quitH)
	}, nil
}

func openOwn(event string) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(event)
	if err != nil {
		return 0, err
	}
	h, err := windows.CreateEvent(nil, manualReset, 0, name)
	/*
	 * ERROR_ALREADY_EXISTS comes back *with a valid handle*.
	 *
	 * It means the event was opened rather than created, which is an ordinary
	 * state: a client already running holds these names. Treating it as a
	 * failure — which the first version of this did — made Listen refuse
	 * whenever anything else had the name, so the second client to start
	 * silently could not be raised or quit.
	 *
	 * Caught by a test that happened to run on a machine with LANcast open.
	 * That is luck rather than rigour, and is why the tests below now assert
	 * against a listener rather than against an empty desktop.
	 */
	if err != nil && err != windows.ERROR_ALREADY_EXISTS {
		return 0, fmt.Errorf("raise: %w", err)
	}
	if h == 0 {
		return 0, fmt.Errorf("raise: no handle for %s", event)
	}
	return h, nil
}

/*
 * The tray's presence, published the same way the two verbs are addressed.
 *
 * A named event held open for the life of the tray. Nothing is ever signalled
 * on it — it exists to be *openable*, which is the whole question — and it goes
 * away with the process that holds it, including one that crashes, because
 * Windows closes handles on exit. A file or a registry key would have to be
 * cleaned up by the thing least able to do it.
 */
func trayEventName() string { return eventPrefix + "-Tray" }

func trayPresent() bool {
	name, err := windows.UTF16PtrFromString(trayEventName())
	if err != nil {
		return false
	}
	h, err := windows.OpenEvent(windows.SYNCHRONIZE, false, name)
	if err != nil {
		return false
	}
	windows.CloseHandle(h)
	return true
}

func holdTray() (func(), error) {
	name, err := windows.UTF16PtrFromString(trayEventName())
	if err != nil {
		return func() {}, err
	}
	h, err := windows.CreateEvent(nil, manualReset, 0, name)
	if err != nil {
		// ERROR_ALREADY_EXISTS means another tray holds it, and the handle is
		// still valid — the same reasoning listen() uses. Two trays claiming
		// presence is not a problem worth failing over; the answer to "can
		// anything restore the window" is yes either way.
		if h == 0 {
			return func() {}, fmt.Errorf("raise: %w", err)
		}
	}
	return func() { windows.CloseHandle(h) }, nil
}
