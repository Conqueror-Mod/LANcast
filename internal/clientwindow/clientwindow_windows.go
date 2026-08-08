//go:build windows

package clientwindow

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"

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
	if err := applyCertPin(o.CertPin); err != nil {
		return err
	}

	w := webview2.NewWithOptions(webview2.WebViewOptions{
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

	// Bound before Navigate: the binding is injected at document creation, and a
	// page that has already started loading would miss it and conclude it is
	// running in a browser.
	for name, fn := range o.Bindings {
		if err := w.Bind(name, fn); err != nil {
			return fmt.Errorf("client window: binding %s: %w", name, err)
		}
	}

	w.Navigate(o.URL)
	w.Run()
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
