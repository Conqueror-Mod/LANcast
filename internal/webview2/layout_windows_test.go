//go:build windows

package webview2

import "testing"

/*
 * The layout rules of ADR 0076, as the pure functions overlay.go acts on.
 *
 * The page says "pip" when it knows a game is up, but the game window comes
 * and goes on the client's schedule and the two can cross. Each crossing has
 * a failure that looks like nothing but a bug: a docked film under the page
 * over a game, or the page pulled out from over a running game.
 */
func TestEffectiveLayout(t *testing.T) {
	cases := []struct {
		asked  VideoLayout
		gameOn bool
		want   VideoLayout
	}{
		{VideoMini, false, VideoMini},
		{VideoMini, true, VideoPiP}, // the page has not heard the game started
		{VideoPiP, true, VideoPiP},
		{VideoPiP, false, VideoMini}, // the game ended before the page said so
		{VideoFull, true, VideoFull},
		{VideoFull, false, VideoFull},
		{VideoHidden, true, VideoHidden},
		{VideoHidden, false, VideoHidden},
	}
	for _, c := range cases {
		if got := effectiveLayout(c.asked, c.gameOn); got != c.want {
			t.Errorf("effectiveLayout(%d, game=%v) = %d, want %d", c.asked, c.gameOn, got, c.want)
		}
	}
}

func TestTheOverlayIsUpForAFullFilmOrAnyGame(t *testing.T) {
	cases := []struct {
		layout VideoLayout
		gameOn bool
		want   bool
	}{
		{VideoHidden, false, false},
		{VideoMini, false, false},
		{VideoFull, false, true},
		// A game always has the page over it, whatever the film is doing.
		{VideoHidden, true, true},
		{VideoPiP, true, true},
		{VideoFull, true, true},
	}
	for _, c := range cases {
		if got := overlayWanted(c.layout, c.gameOn); got != c.want {
			t.Errorf("overlayWanted(%d, game=%v) = %v, want %v", c.layout, c.gameOn, got, c.want)
		}
	}
}
