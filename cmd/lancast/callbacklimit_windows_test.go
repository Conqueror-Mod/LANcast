//go:build windows

package main

import "testing"

// topLevelWindows runs on a loop while a game's window is watched for, and it
// made a callback per call, as clientwindow.Monitors did when it killed the
// window mid-film. Go has about 2,000 and frees none, so 2,500 calls crash the
// test binary unless the callback is made once.
func TestTopLevelWindowsCanBeCalledPastTheCallbackLimit(t *testing.T) {
	for i := 0; i < 2500; i++ {
		if len(topLevelWindows()) == 0 {
			t.Fatalf("call %d found no windows at all", i)
		}
	}
}
