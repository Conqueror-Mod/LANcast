//go:build windows

package main

import (
	"errors"
	"strings"
	"testing"
)

/*
 * What a failed launch says.
 *
 * This is not decoration. The windowless launch has exactly one channel to the
 * user — a message box — and the error that actually reached it was "apply
 * schema: attempt to write a readonly database (8)", which is a sentence about
 * SQLite and contains no action. The person reading it had LANcast installed as
 * a service and needed to be told to start the service; instead they went to a
 * terminal, which is the thing a double-click exists to avoid.
 */
func TestExplainStartupFailureSaysWhatToDo(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want []string
	}{
		{
			name: "the readonly database is really a service-ownership problem",
			err:  errors.New("apply schema: attempt to write a readonly database (8)"),
			want: []string{"service account", "Start the LANcast service", "-data"},
		},
		{
			name: "a port clash names the actual situation",
			err:  errors.New("listen tcp :8080: bind: Only one usage of each socket address is normally permitted"),
			want: []string{"already listening", "same port"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := explainStartupFailure(tc.err)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("message does not mention %q:\n%s", want, got)
				}
			}
			// The original always survives. A friendly message that hides the
			// real error is worse than a raw one for anybody trying to fix it,
			// and this text is what ends up pasted into a bug report.
			if !strings.Contains(got, tc.err.Error()) {
				t.Errorf("message drops the underlying error:\n%s", got)
			}
		})
	}
}

// An error nobody has written copy for is passed through rather than replaced
// with something vague. "Something went wrong" is the failure mode this
// function exists to avoid, not the one it should introduce.
func TestExplainStartupFailurePassesThroughTheUnfamiliar(t *testing.T) {
	err := errors.New("some future failure nobody has met yet")
	if got := explainStartupFailure(err); got != err.Error() {
		t.Errorf("rewrote an unfamiliar error:\ngot  %s\nwant %s", got, err.Error())
	}
}

// A refused UAC prompt is a decision, not a fault, and has to be distinguishable
// from a service that genuinely would not start — they deserve different words.
func TestCancelledElevationIsItsOwnError(t *testing.T) {
	if !errors.Is(errCancelled, errCancelled) {
		t.Fatal("errCancelled is not identifiable")
	}
	if strings.Contains(strings.ToLower(errCancelled.Error()), "fail") {
		t.Errorf("a refused prompt reads as a failure: %q", errCancelled.Error())
	}
}
