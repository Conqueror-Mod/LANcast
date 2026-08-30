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
const eventName = `Local\LANcast-Client-Show`

/*
 * Auto-reset, so each launch wakes the listener exactly once.
 *
 * CreateEvent takes *manualReset*, and passing 1 there is the opposite of what
 * is wanted: a manual-reset event stays signalled, so the wait returns
 * immediately for ever and the window is foregrounded in a loop — a fix worse
 * than the bug. Written the wrong way round first and caught by the test below,
 * which is why that test asserts nothing arrives unbidden rather than only that
 * signals arrive.
 */
const manualReset = 0

func signal() error {
	name, err := windows.UTF16PtrFromString(eventName)
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

func listen(show func()) (func(), error) {
	name, err := windows.UTF16PtrFromString(eventName)
	if err != nil {
		return func() {}, err
	}
	h, err := windows.CreateEvent(nil, manualReset, 0, name)
	if err != nil {
		return func() {}, fmt.Errorf("raise: %w", err)
	}

	/*
	 * A second handle purely to wake the waiter on stop.
	 *
	 * WaitForSingleObject cannot be cancelled, and closing the handle it is
	 * waiting on is undefined rather than merely rude. Signalling it once and
	 * having the loop notice it is stopping is the ordinary shape.
	 */
	var stopped atomic.Bool
	go func() {
		for {
			ev, err := windows.WaitForSingleObject(h, windows.INFINITE)
			if err != nil || ev != windows.WAIT_OBJECT_0 {
				return
			}
			if stopped.Load() {
				return
			}
			show()
		}
	}()

	return func() {
		stopped.Store(true)
		_ = windows.SetEvent(h)
		_ = windows.CloseHandle(h)
	}, nil
}
