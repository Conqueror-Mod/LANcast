package raise

import (
	"testing"
	"time"
)

/*
 * A second launch brings the window forward instead of opening a browser.
 *
 * Reported from a mouse button bound to the client shortcut: the Start menu
 * entry opened the window, the same shortcut fired again opened a browser
 * beside it. The single-instance branch called OpenBrowser under a comment
 * saying it "reopens the UI" — and the browser is a different interface,
 * without the pinned certificate, showing the warning page the window exists to
 * avoid.
 */

func TestSignalReachesAListener(t *testing.T) {
	got := make(chan struct{}, 1)
	stop, err := Listen(func() { got <- struct{}{} })
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer stop()

	if err := Signal(); err != nil {
		t.Fatalf("signal: %v", err)
	}
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("the running client was never told to show itself")
	}
}

/*
 * Auto-reset, which is why each press is one raise.
 *
 * A manual-reset event stays signalled, so the listener would spin and the
 * window would be foregrounded for ever — a fix that is worse than the bug.
 */
func TestEachSignalWakesTheListenerOnce(t *testing.T) {
	got := make(chan struct{}, 8)
	stop, err := Listen(func() { got <- struct{}{} })
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer stop()

	for i := 0; i < 3; i++ {
		if err := Signal(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-got:
		case <-time.After(2 * time.Second):
			t.Fatalf("signal %d was not delivered", i+1)
		}
	}
	// Nothing extra arrived on its own.
	select {
	case <-got:
		t.Error("the listener fired without being signalled")
	case <-time.After(200 * time.Millisecond):
	}
}

/*
 * Signalling with nobody there is what a first launch looks like, and it must
 * not be an error — the caller goes straight on to become that client.
 */
func TestSignallingNobodyIsNotAnError(t *testing.T) {
	if err := Signal(); err != nil {
		t.Errorf("signalling with no client running failed: %v", err)
	}
}

func TestStopEndsTheListener(t *testing.T) {
	fired := make(chan struct{}, 1)
	stop, err := Listen(func() { fired <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	stop()

	// The handle is closed, so a later launch finds nobody — which is the same
	// state as before any client ran, and is what a closed window should be.
	_ = Signal()
	select {
	case <-fired:
		t.Error("a stopped listener still raised the window")
	case <-time.After(300 * time.Millisecond):
	}
}
