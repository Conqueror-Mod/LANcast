//go:build windows

package clientwindow

import (
	"errors"
	"fmt"

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

	w.Navigate(o.URL)
	w.Run()
	return nil
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
