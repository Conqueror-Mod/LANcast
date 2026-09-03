//go:build windows

package clientwindow

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"

	"lancast/internal/webview2"
	"lancast/internal/webview2/loader"
)

// open hosts the UI in a WebView2 window.
//
// The web view runs on the thread that creates it and Run() pumps that thread's
// message loop, so this blocks until the window closes — which is the contract
// Open documents and the reason the caller can treat it like "show the app".
func open(o Options) error {
	if o.URL == "" {
		return errors.New("client window: no URL to open")
	}

	// Ask before building. Creating the view when the runtime is absent returns
	// a nil interface, and the caller would be left with a window that never
	// appeared and no reason why — the exact silent failure v0.4.x kept
	// producing.
	if err := check(); err != nil {
		return err
	}

	// Applied before the environment is created — the web view reads this on
	// creation and ignores later changes.
	if err := applyBrowserArgs(o.CertPin, o.DevTools); err != nil {
		return err
	}

	/*
	 * The window, and the close handler, declared before either is built.
	 *
	 * The handler has to reach the window to read its position, and the window
	 * needs the handler at construction — so the closure captures the variable
	 * and reads it when it runs, by which time it is set.
	 *
	 * Order is the whole point here. This wrapper was originally installed
	 * *after* NewWithOptions, which takes the callback by value: the webview
	 * kept the original, the wrapper was never called, and the window position
	 * was silently never saved. It shipped that way, because the rules it
	 * guards are pure and well tested while the wiring that reaches them was
	 * not tested at all.
	 */
	var w webview2.WebView
	var placed placement

	userClose := o.OnClose
	onClose := closeHandler(
		func() {
			if w != nil {
				placed.capture(uintptr(w.Window()))
			}
		},
		userClose,
	)

	w = webview2.NewWithOptions(webview2.WebViewOptions{
		OnClose: onClose,
		WindowOptions: webview2.WindowOptions{
			Title:  o.Title,
			Width:  uint(o.Width),
			Height: uint(o.Height),
			Center: true,
		},
		DataPath: o.DataDir,
		// The UI owns its own keyboard model (ADR 0004), and a browser's
		// accelerators would fight it.
		AutoFocus: true,
	})
	if w == nil {
		return errors.New("client window: the web view could not be created")
	}
	defer w.Destroy()

	// Fullscreen is the host's job, not the page's.
	//
	// WebView2 tells its host that a page wants fullscreen and the host is what
	// has to act on it — resize, drop the frame, cover the taskbar. Nothing
	// listened, so requestFullscreen() left the page believing it was
	// fullscreen inside a window that had not changed size, which is
	// indistinguishable from a button that does not work. The page calls this
	// instead when it is running in the LANcast window.
	var fs fullscreener
	if o.Bindings == nil {
		o.Bindings = map[string]any{}
	}
	o.Bindings["lancastToggleFullscreen"] = func() bool {
		return fs.Toggle(uintptr(w.Window()))
	}

	// Bound before Navigate: the binding is injected at document creation, and a
	// page that has already started loading would miss it and conclude it is
	// running in a browser.
	for name, fn := range o.Bindings {
		if err := w.Bind(name, fn); err != nil {
			return fmt.Errorf("client window: binding %s: %w", name, err)
		}
	}

	if o.OnReady != nil {
		o.OnReady(&controller{w: w, placed: &placed})
	}

	/*
	 * Put the window back where it was, before it is shown.
	 *
	 * Center: true above is what happens when there is nothing remembered, and
	 * it stays the fallback rather than being replaced: Resolve refuses a
	 * placement it cannot honour, and a centred window is always on a screen
	 * somebody is looking at, which no remembered position can promise.
	 */
	if o.Placement.Valid() {
		applyPlacement(uintptr(w.Window()), o.Placement)
	}

	w.Navigate(o.URL)
	w.Run()

	/*
	 * Deliver the position captured on the way out, not one read here.
	 *
	 * Run() returns when the message loop ends, by which time the window is
	 * being torn down and its handle answers for nothing — reading the
	 * rectangle at this point gets a destroyed window, and a plausible
	 * rectangle from a destroyed window is worse than none, because it would
	 * be saved.
	 *
	 * So the position is taken while the window is demonstrably alive: in the
	 * close handler, and in the tray's Close. Both are the moment a person
	 * decided to stop, which is also the position worth remembering.
	 */
	if o.OnPlacement != nil {
		if p, ok := placed.get(); ok {
			o.OnPlacement(p)
		}
	}
	return nil
}

// browserArgsEnv is the documented way to pass Chromium switches to a WebView2
// environment. The alternative is implementing ICoreWebView2EnvironmentOptions
// as a COM object in Go to set AdditionalBrowserArguments — the same setting,
// reached through a vtable we would have to write and maintain. The variable is
// read at environment creation, and programmatic options would take precedence
// if we ever set any.
const browserArgsEnv = "WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS"

// applyCertPin narrows certificate trust to one public key.
//
// --ignore-certificate-errors-spki-list is Chromium's own pinning switch: it
// exempts the listed SubjectPublicKeyInfo hashes from certificate errors and
// leaves everything else validated. It is emphatically not
// --ignore-certificate-errors, which would accept any certificate from anyone
// and hand the LAN exactly the attack TLS exists to prevent.
//
// The pin is rejected rather than passed through if it does not look like a
// base64 SHA-256: the switch takes a comma-separated list, so a value with a
// comma or a space in it could append switches of its own. The value comes off
// local disk rather than a network, which makes that unlikely — and validating
// it is two lines, which makes not validating it indefensible.
func applyCertPin(pin string) error {
	if pin == "" {
		return nil
	}
	if !validPin(pin) {
		return fmt.Errorf("client window: refusing a malformed certificate pin %q", pin)
	}
	if existing := os.Getenv(browserArgsEnv); existing != "" {
		// Someone is already steering the browser. Adding to it silently would
		// be a surprise in both directions.
		return fmt.Errorf("client window: %s is already set (%q); not adding a certificate pin",
			browserArgsEnv, existing)
	}
	return os.Setenv(browserArgsEnv, "--ignore-certificate-errors-spki-list="+pin)
}

// validPin accepts exactly a base64-encoded SHA-256: 43 base64 characters and
// one '=' of padding. Anything else cannot be a key hash, and the only reason a
// value would arrive in another shape is that something went wrong upstream.
func validPin(pin string) bool {
	raw, err := base64.StdEncoding.DecodeString(pin)
	return err == nil && len(raw) == sha256.Size
}

// check asks the two questions that have to hold before a window can exist, and
// keeps their answers apart.
//
// GetInstalledVersion returns an ErrLoaderMissing when WebView2Loader.dll
// cannot be resolved — LANcast's own shipped file, so an incomplete install —
// and returns "" with a nil error when the DLL loaded fine but no runtime is
// installed, which is Microsoft's component and a different fix. Collapsing the
// two would send someone to install a runtime they already have.
func check() error {
	v, err := loader.GetInstalledVersion()
	if err != nil {
		return fmt.Errorf("client window: %w", err)
	}
	if v == "" {
		return errors.New("client window: the Microsoft Edge WebView2 runtime is not installed on this machine")
	}
	return nil
}

// controller drives an open window from another thread.
//
// Every call goes through Dispatch, which posts onto the window's own message
// loop. The tray runs on a different OS thread out of necessity — two message
// queues, one per thread — and calling ShowWindow across that boundary is the
// kind of thing that works until it deadlocks.
//
// The two procs are declared here rather than added to internal/webview2/w32,
// which is vendored: a local need does not belong in a copy of someone else's
// package when four lines here will do.
var (
	user32                  = windows.NewLazySystemDLL("user32")
	procShowWindow          = user32.NewProc("ShowWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
)

const (
	swHide    = 0
	swShow    = 5
	swRestore = 9
)

type controller struct {
	w webview2.WebView
	// placed is the same recorder open() reads after the loop ends, so a quit
	// from the tray remembers where the window was rather than losing it.
	placed *placement
}

func (c *controller) Show() {
	c.w.Dispatch(func() {
		hwnd := uintptr(c.w.Window())
		// Restore before showing: a window hidden while minimised comes back
		// minimised, which reads as the tray having done nothing.
		_, _, _ = procShowWindow.Call(hwnd, swRestore)
		_, _, _ = procShowWindow.Call(hwnd, swShow)
		_, _, _ = procSetForegroundWindow.Call(hwnd)
	})
}

func (c *controller) Hide() {
	c.w.Dispatch(func() {
		_, _, _ = procShowWindow.Call(uintptr(c.w.Window()), swHide)
	})
}

// Close ends the message loop, which returns from Open and lets the caller run
// its normal shutdown — including stopping a server it started.
//
// Dispatched, despite Terminate being documented as safe from a background
// thread. It is not, on this backend: Terminate calls PostQuitMessage, and
// PostQuitMessage posts WM_QUIT to the *calling* thread's queue. Called from the
// tray's thread it quits the tray and leaves the window open — which is exactly
// what happened the first time, a tray icon that vanished while the app stayed
// on screen. Going through Dispatch puts it on the window's own thread, where
// the quit belongs.
func (c *controller) Close() {
	c.w.Dispatch(func() {
		// On the window's thread and before Terminate, which is the last
		// moment the handle means anything.
		if c.placed != nil {
			c.placed.capture(uintptr(c.w.Window()))
		}
		c.w.Terminate()
	})
}

/*
 * applyBrowserArgs composes every Chromium switch this window needs, once.
 *
 * There is one environment variable and it is read once at environment
 * creation, so anything wanting a switch has to come through here — a second
 * caller doing its own Setenv would silently replace the certificate pin, which
 * is the switch that makes the app work at all beyond loopback.
 *
 * applyCertPin keeps its own guard rather than being folded in, because a
 * malformed pin must fail loudly whether or not anything else wanted a switch,
 * and because its tests are about that rule specifically.
 */
func applyBrowserArgs(pin string, devTools bool) error {
	if !devTools {
		return applyCertPin(pin)
	}
	/*
	 * Opens the inspector with the window rather than merely permitting it.
	 *
	 * WebView2 allows devtools already; what this window does not give anyone
	 * is a way to reach them — the host owns the keyboard model (ADR 0004) so
	 * F12 does not arrive, and there is no context menu to right-click. Opening
	 * it up front is the honest version of a switch labelled "developer tools".
	 */
	const devToolsArg = "--auto-open-devtools-for-tabs"
	if pin == "" {
		if existing := os.Getenv(browserArgsEnv); existing != "" {
			return fmt.Errorf("client window: %s is already set (%q); not adding developer tools",
				browserArgsEnv, existing)
		}
		return os.Setenv(browserArgsEnv, devToolsArg)
	}
	if err := applyCertPin(pin); err != nil {
		return err
	}
	// The pin has been validated and set by the line above, so appending here
	// is appending to a value this function just wrote rather than to somebody
	// else's.
	return os.Setenv(browserArgsEnv, os.Getenv(browserArgsEnv)+" "+devToolsArg)
}
