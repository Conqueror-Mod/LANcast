//go:build windows

package clientwindow

import "testing"

/*
 * The switch reaches the web view.
 *
 * Turning developer tools on has two halves and only one of them was wired.
 * The browser argument opens the pane; the settings applied at creation decide
 * whether devtools exist at all, and they were told `false` unconditionally —
 * so the pane opened onto nothing. Nothing failed, nothing logged, and the
 * blank pane was written down as a WebView2 defect.
 *
 * These assert the half that had no test, which is the half that was wrong.
 */

func TestDevToolsOffLeavesTheWindowAsShipped(t *testing.T) {
	opts := viewOptions(Options{Title: "LANcast"})
	if opts.Debug {
		t.Error("developer tools are enabled without being asked for")
	}
}

func TestDevToolsOnEnablesThemInTheWebView(t *testing.T) {
	opts := viewOptions(Options{Title: "LANcast", DevTools: true})
	if !opts.Debug {
		t.Fatal("the preference does not reach the web view, so the inspector " +
			"is opened by the browser argument and then switched off by the " +
			"settings — which is a blank pane, not a working one")
	}
}

// The rest of the window is not a casualty of the switch: the same title, size
// and data path either way, and the keyboard model still owned by the UI.
func TestDevToolsChangesNothingElse(t *testing.T) {
	o := Options{Title: "LANcast", Width: 1280, Height: 800, DataDir: `C:\data`}
	off, on := viewOptions(o), viewOptions(Options{
		Title: o.Title, Width: o.Width, Height: o.Height,
		DataDir: o.DataDir, DevTools: true,
	})
	off.Debug = true
	// OnClose is installed by the caller rather than here, and a func is not
	// comparable — so the fields are compared rather than the struct.
	if off.WindowOptions != on.WindowOptions || off.DataPath != on.DataPath ||
		off.AutoFocus != on.AutoFocus || off.Debug != on.Debug {
		t.Errorf("developer tools changed more than the inspector:\n off=%+v\n on =%+v", off, on)
	}
	if !on.AutoFocus {
		t.Error("AutoFocus lost; the UI's keyboard model (ADR 0004) depends on it")
	}
}
