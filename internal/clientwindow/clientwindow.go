// Package clientwindow opens the LANcast UI in a window this program owns,
// rather than handing a URL to whichever browser the machine happens to have
// ([ADR 0023](../../docs/adr/0023-native-desktop-client.md) stage 1).
//
// The contract is one function and it is deliberately small: show the UI at a
// URL, block until the user closes it. Everything else the desktop needs —
// finding or starting the server, holding the single-instance name, the tray —
// already exists in cmd/lancast and is untouched by this.
//
// Per-platform, the same shape as tray_windows.go / tray_other.go: Windows has
// an implementation, everything else reports that it has none so the caller can
// fall back to the browser.
package clientwindow

// Options is what the window needs to know. Kept as a struct because a window
// grows options (size, position, zoom) and a widening function signature is a
// worse way to absorb that.
type Options struct {
	// URL is the address to show. The caller resolves scheme and host; this
	// package does no discovery of its own.
	URL string
	// Title is the window title.
	Title string
	// Width and Height are the initial size in logical pixels. Zero means the
	// platform default.
	Width, Height int
	// DataDir is where the web view keeps its own profile — cookies, local
	// storage, cache.
	//
	// It matters that this is explicit. The session cookie lives here, so a
	// default that lands somewhere per-run would sign the user out on every
	// launch, and one shared with another application would be a surprise in
	// both directions.
	DataDir string
}

// Open shows the window and blocks until it is closed.
//
// Returns ErrUnsupported where there is no implementation, which the caller is
// expected to treat as "use the browser" rather than as a failure.
func Open(o Options) error { return open(o) }

// Check reports whether a window can be opened here, and if not, why — the
// reason is the whole point of it existing separately from Open.
//
// The two ways this fails need different sentences. A missing
// WebView2Loader.dll means the *install* is incomplete, which is LANcast's
// fault and fixable by reinstalling. A missing WebView2 *runtime* means the
// machine lacks a Microsoft component, which is not LANcast's fault and is
// fixed somewhere else entirely. Telling someone to install a runtime they
// already have, because a file next to the executable went missing, sends them
// to the wrong place — and a caller that only had a bool could not do better.
//
// nil means a window can be opened. Any error is a reason to use the browser,
// not a reason to fail.
func Check() error { return check() }

// Available is Check as a boolean, for callers that only branch on it.
func Available() bool { return Check() == nil }
