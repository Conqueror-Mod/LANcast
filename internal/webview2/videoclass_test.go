//go:build windows

package webview2

import "testing"

/*
 * The picture window gets a class of its own, with a black background.
 *
 * Without it nothing erases that window and it shows whatever the compositor
 * left there until mpv presents a frame — reported twice, first as a
 * black-and-white pattern on every start and stop, then, once the page
 * backdrop was fixed, as a plain white rectangle over the whole picture area.
 *
 * What can go wrong here is silent. A malformed WNDCLASSEX, a name clash or a
 * brush that could not be obtained all make RegisterClassEx return zero, and
 * the code then falls back to the shared class — which is exactly the bug,
 * restored, with nothing said. So this asserts the registration happened.
 *
 * Windows-only, and CI runs `go test ./...` on Linux, so this guard runs when
 * somebody builds on the platform it is about. That is a real limit and still
 * better than asserting nothing.
 */
func TestTheVideoWindowHasItsOwnClass(t *testing.T) {
	if videoClass() == nil {
		t.Error("the video window class did not register, so the picture " +
			"window falls back to the shared class and is painted by nothing")
	}
}

// Registration happens once and the same name comes back, since the window is
// created every time video starts.
func TestTheVideoClassIsRegisteredOnce(t *testing.T) {
	first := videoClass()
	second := videoClass()
	if first != second {
		t.Error("videoClass returned a different name on the second call; " +
			"registering twice would fail and lose the brush")
	}
}
