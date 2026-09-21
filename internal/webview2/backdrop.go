package webview2

/*
 * The colour the web view paints behind the page when it is not the
 * transparent overlay.
 *
 * Here rather than in overlay.go, which is Windows-only, so the invariant
 * below can be tested on the Linux runner that actually runs `go test`.
 *
 * It must match `--space-void` in web/src/styles/tokens.css, because that is
 * what the page paints over it. When they disagree, the difference is visible
 * for the moment between the background turning opaque and the page painting:
 * leaving the overlay reparents the Chromium control, and the backdrop is all
 * there is to see until it repaints. It was white for a while, which produced
 * a white rectangle flashing across a black screen every time a film started,
 * was skipped, or stopped.
 */
const (
	backdropR = 0x05
	backdropG = 0x07
	backdropB = 0x0f
)
