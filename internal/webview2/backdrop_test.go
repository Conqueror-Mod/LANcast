package webview2

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"testing"
)

/*
 * The native backdrop matches the colour the page paints over it.
 *
 * This is a cross-file invariant with no compiler to enforce it: one value is
 * a Go constant, the other is a CSS custom property, and nothing connects them
 * but this test. When they drift, the difference is visible for the moment
 * between the web view's background turning opaque and the page repainting —
 * which happens on every transition into and out of native video, so it is not
 * a rare frame.
 *
 * It drifted once already, in the direction that shows up most: the backdrop
 * was WebView2's default **white**, because "put it back" was taken to mean
 * "put it back to the default". That flashed a white rectangle across a black
 * screen every time a film started, was skipped, or stopped. Reported as a
 * repeating black-and-white pattern, which is exactly what it looks like from
 * the front.
 *
 * Deliberately not in overlay.go's package file: that is `//go:build windows`
 * and CI runs `go test ./...` on Linux, so a guard written there would never
 * run in the place that would catch it.
 */
func TestBackdropMatchesTheDesignToken(t *testing.T) {
	const tokens = "../../web/src/styles/tokens.css"

	css, err := os.ReadFile(tokens)
	if err != nil {
		t.Fatalf("read %s: %v", tokens, err)
	}

	// `--space-void: #05070f;` — the page's own floor colour.
	m := regexp.MustCompile(`--space-void:\s*#([0-9a-fA-F]{6})\s*;`).FindSubmatch(css)
	if m == nil {
		t.Fatalf("no --space-void in %s; if the token was renamed, this guard "+
			"needs to follow it rather than be deleted", tokens)
	}

	want, err := strconv.ParseUint(string(m[1]), 16, 32)
	if err != nil {
		t.Fatalf("parse %q: %v", m[1], err)
	}
	wantR := byte(want >> 16)
	wantG := byte(want >> 8)
	wantB := byte(want)

	if backdropR != wantR || backdropG != wantG || backdropB != wantB {
		t.Errorf(
			"backdrop is #%s but --space-void is #%s.\n\n"+
				"These are the two halves of one colour: the native window paints the "+
				"first and the page paints the second on top of it. A difference is "+
				"visible on every transition into and out of native video.",
			hex(backdropR, backdropG, backdropB), hex(wantR, wantG, wantB))
	}
}

// The specific value that caused the fault, named so nobody restores it by
// reaching for WebView2's default again.
func TestBackdropIsNotWhite(t *testing.T) {
	if backdropR == 0xff && backdropG == 0xff && backdropB == 0xff {
		t.Error("the backdrop is white again; that is WebView2's default and it " +
			"flashes across the window on every native-video transition")
	}
}

func hex(r, g, b byte) string { return fmt.Sprintf("%02x%02x%02x", r, g, b) }
