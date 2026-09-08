package raise

import (
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

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
 * A shared section beside the show event, carrying where to go.
 *
 * A named event is a doorbell: it says somebody is at the door and nothing
 * else. That was enough while the only thing to say was "show yourself", and it
 * stopped being enough when the server's tray gained menu items naming a
 * *destination* -- "Update libraries..." is not "show the window", it is "show
 * the window at the library settings", and a doorbell cannot say the second
 * half. Without it the tray opened a browser instead, which is the interface
 * the window exists to avoid (ADR 0023).
 *
 * Shared memory rather than a file: same family as the event, same session
 * scope, same exactness of naming, and nothing lands on disk. A file would also
 * outlive the process that wrote it, which is precisely wrong for a message
 * whose entire meaning is "right now".
 *
 * The client owns it. It creates the section while it listens, so the section
 * existing *is* the statement that somebody is home -- and a signaller that
 * cannot open it has learned something true rather than hit an error.
 */
func payloadName() string { return eventPrefix + "-Payload" }

/*
 * OpenFileMapping, declared here because x/sys/windows does not wrap it.
 *
 * It has CreateFileMapping and not the open, and the difference matters: create
 * would *make* a section when none existed, so a tray signalling a client that
 * is not running would quietly create the thing whose existence is supposed to
 * mean somebody is home. Four lines to open properly beats a wrapper that says
 * the wrong thing — the same trade clientwindow makes for ShowWindow.
 */
var (
	kernel32             = windows.NewLazySystemDLL("kernel32")
	procOpenFileMappingW = kernel32.NewProc("OpenFileMappingW")
)

func openFileMapping(access uint32, name *uint16) (windows.Handle, error) {
	h, _, err := procOpenFileMappingW.Call(uintptr(access), 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return 0, err
	}
	return windows.Handle(h), nil
}

/*
 * 512 bytes, which is a pane name with three orders of magnitude to spare.
 *
 * Fixed, because a shared section has its size chosen when it is created and
 * the reader must agree; growable shared memory is a protocol, and this is one
 * short string. Anything longer is refused at the writer rather than truncated
 * -- half a destination is worse than none, because it would navigate somewhere
 * nobody asked for.
 */
const payloadBytes = 512

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

func signalQuit() error { _, err := signal(quitEventName()); return err }

/*
 * signalShow writes the destination, then rings the bell.
 *
 * That order is the whole correctness argument. The listener reads the section
 * when it wakes, so a payload written afterwards would be read by the *next*
 * signal or not at all -- the window would open on the previous menu item's
 * pane, which is the kind of wrongness that reads as a random glitch.
 *
 * A destination that cannot be delivered does not stop the raise. An older
 * client, listening on the event but with no section, is exactly that case: it
 * comes forward showing whatever it was showing, which is worse than the
 * destination and much better than a browser.
 */
func signalShow(pane string) (bool, error) {
	if pane != "" {
		writePayload(pane)
	}
	return signal(showEventName())
}

// signal reports whether anything was listening, which is what lets the caller
// fall back rather than assume it was heard.
func signal(event string) (bool, error) {
	name, err := windows.UTF16PtrFromString(event)
	if err != nil {
		return false, err
	}
	h, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
	if err != nil {
		// Nobody is listening. Not an error worth surfacing: it is what a
		// first launch looks like, and the caller goes on to be that client.
		return false, nil
	}
	defer windows.CloseHandle(h)
	if err := windows.SetEvent(h); err != nil {
		return false, fmt.Errorf("raise: %w", err)
	}
	return true, nil
}

/*
 * writePayload puts the destination where the listener will look.
 *
 * Best-effort by construction and silent on failure: every way this fails means
 * the same thing to the caller -- the window is raised without a destination --
 * and none of them is worth stopping the raise over.
 *
 * The layout is a two-byte little-endian length followed by UTF-8. A fixed
 * section with no length would leave the reader guessing where the string ends,
 * and a trailing NUL could not tell an empty payload from a stale one.
 */
func writePayload(pane string) {
	b := []byte(pane)
	if len(b) > payloadBytes-2 {
		return
	}
	name, err := windows.UTF16PtrFromString(payloadName())
	if err != nil {
		return
	}
	h, err := openFileMapping(windows.FILE_MAP_WRITE, name)
	if err != nil {
		// No section: an older client, or none at all. The bell still rings.
		return
	}
	defer windows.CloseHandle(h)
	addr, err := windows.MapViewOfFile(h, windows.FILE_MAP_WRITE, 0, 0, payloadBytes)
	if err != nil {
		return
	}
	defer windows.UnmapViewOfFile(addr)

	view := mappedBytes(addr)
	binary.LittleEndian.PutUint16(view[:2], uint16(len(b)))
	copy(view[2:], b)
}

/*
 * mappedBytes views a mapped section as a slice.
 *
 * The one place a uintptr becomes a pointer, so the reasoning is written once.
 * `go vet` flags this conversion because a uintptr that names Go memory can go
 * stale the moment the collector moves it — which is the right warning for Go
 * memory and does not apply here. This address comes from MapViewOfFile: it is
 * a mapping the operating system owns and pins until UnmapViewOfFile, and the
 * collector neither knows about it nor can move it.
 *
 * Both callers hold their mapping for the whole life of the slice, which is the
 * other half of what makes it safe.
 */
func mappedBytes(addr uintptr) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(addr)), payloadBytes)
}

/*
 * readPayload takes the destination and clears it.
 *
 * Clearing is the part worth stating. A payload left behind would be read again
 * by the next plain show -- a second launch from the Start menu, which names no
 * destination -- and the window would jump to whatever the tray last asked for.
 * Read-and-consume makes the section mean "the destination for the signal I
 * just received" rather than "the last destination anybody mentioned".
 */
func readPayload(addr uintptr) string {
	if addr == 0 {
		return ""
	}
	view := mappedBytes(addr)
	n := int(binary.LittleEndian.Uint16(view[:2]))
	if n <= 0 || n > payloadBytes-2 {
		return ""
	}
	pane := string(view[2 : 2+n])
	binary.LittleEndian.PutUint16(view[:2], 0)
	return pane
}

/*
 * createPayload makes the section the client holds while it listens.
 *
 * Backed by the page file rather than a file on disk -- an invalid file handle
 * is how Windows spells "memory only". It lives exactly as long as the handle,
 * so a client that exits takes its section with it and a tray signalling
 * afterwards learns there is nobody home.
 */
func createPayload() (windows.Handle, uintptr, error) {
	name, err := windows.UTF16PtrFromString(payloadName())
	if err != nil {
		return 0, 0, err
	}
	h, err := windows.CreateFileMapping(windows.InvalidHandle, nil,
		windows.PAGE_READWRITE, 0, payloadBytes, name)
	if err != nil {
		return 0, 0, err
	}
	addr, err := windows.MapViewOfFile(h, windows.FILE_MAP_WRITE, 0, 0, payloadBytes)
	if err != nil {
		windows.CloseHandle(h)
		return 0, 0, err
	}
	return h, addr, nil
}

func listen(show func(string), quit func()) (func(), error) {
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
	 * A section that cannot be created is not fatal.
	 *
	 * Listening still works and every raise still raises; what is lost is the
	 * destination. Refusing to listen at all because the optional half failed
	 * would trade a working window for a missing convenience.
	 */
	payloadH, payloadAddr, _ := createPayload()

	/*
	 * One waiter per event rather than WaitForMultipleObjects.
	 *
	 * Two goroutines blocked on a handle each is the same cost and reads as
	 * what it is; a multiple-wait would have to decode which index fired and
	 * re-arm, which is where this kind of code goes wrong.
	 */
	var stopped atomic.Bool
	var waiting sync.WaitGroup
	waiting.Add(2)
	watch := func(h windows.Handle, fn func()) {
		defer waiting.Done()
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
	// The destination is read here, on waking, rather than inside the callback:
	// it belongs to this signal, and reading it anywhere else would let it be
	// read twice or not at all.
	go watch(showH, func() { show(readPayload(payloadAddr)) })
	go watch(quitH, quit)

	var once sync.Once
	return func() {
		once.Do(func() {
			stopped.Store(true)
			// Wake both so neither goroutine outlives the client.
			_ = windows.SetEvent(showH)
			_ = windows.SetEvent(quitH)
			/*
			 * Wait for them to leave before closing the handles.
			 *
			 * Closing a handle another thread is blocked on is undefined, and
			 * the way it went wrong here is worth writing down: the wait does
			 * not necessarily fail. Windows reuses handle *values*, so a
			 * goroutine still parked on a closed one can end up waiting on
			 * whatever was opened next — and these are auto-reset events, where
			 * exactly one waiter wakes per signal. The stale goroutine takes
			 * the wake-up, sees its own `stopped` and returns, and the signal is
			 * gone: the new listener waits for something already consumed.
			 *
			 * It showed up as a test failing two or three runs in ten, always
			 * on the *first* signal after another listener had been stopped, and
			 * never when run alone. The same shape in the field is a tray's
			 * Open doing nothing, once, for no reason anybody could reproduce.
			 *
			 * Bounded rather than infinite: a goroutine that cannot be woken
			 * must not hang the caller shutting down. Leaking a handle is the
			 * lesser fault, and it goes away with the process.
			 */
			done := make(chan struct{})
			go func() { waiting.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
			}
			_ = windows.CloseHandle(showH)
			_ = windows.CloseHandle(quitH)
			if payloadAddr != 0 {
				_ = windows.UnmapViewOfFile(payloadAddr)
			}
			if payloadH != 0 {
				_ = windows.CloseHandle(payloadH)
			}
		})
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
