//go:build windows

package loader

import (
	"errors"
	"strings"
	"testing"
)

// The message an operator reads when the shipped DLL is missing.
//
// It has to name the file and say where it belongs, because the two failures
// here are easy to confuse and lead somewhere different: a missing
// WebView2Loader.dll is an incomplete LANcast install, while a missing WebView2
// *runtime* is a Microsoft component fetched from elsewhere. Sending someone to
// install a runtime they already have is the wrong instruction, and this
// message is what prevents it.
func TestLoaderMissingSaysWhatIsMissingAndWhere(t *testing.T) {
	err := &ErrLoaderMissing{Err: errors.New("The specified module could not be found.")}
	msg := err.Error()

	for _, want := range []string{"WebView2Loader.dll", "beside", "LANcast"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not mention %q", msg, want)
		}
	}
	// The underlying cause survives, so a log carries the real Windows error
	// rather than only our sentence about it.
	if !strings.Contains(msg, "could not be found") {
		t.Errorf("message %q drops the underlying error", msg)
	}
}

// Unwrap works, so callers can errors.Is/As past our wrapper to the cause.
func TestLoaderMissingUnwraps(t *testing.T) {
	cause := errors.New("boom")
	err := &ErrLoaderMissing{Err: cause}
	if !errors.Is(err, cause) {
		t.Error("errors.Is could not reach the wrapped cause")
	}
}
