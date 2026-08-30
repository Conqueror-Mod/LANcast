package raise

import (
	"fmt"
	"os"
	"testing"
	"time"
)

/*
 * Names of its own, per test.
 *
 * These are auto-reset events: exactly one waiter wakes per signal. Two things
 * follow, and both were learned the hard way.
 *
 * A test sharing the shipped names loses every signal to a LANcast that happens
 * to be running on the machine — the feature working correctly against a test
 * that was not isolated.
 *
 * And a name shared *between tests* is no better: a listener that has been
 * stopped may still have a goroutine in its final wait, which is a waiter on
 * that name and will take the next signal. That passed alone and failed in the
 * full run, which is the shape of a flake nobody enjoys.
 */
func isolate(t *testing.T) {
	t.Helper()
	prev := eventPrefix
	eventPrefix = fmt.Sprintf(`Local\LANcast-Test-%d-%s`, os.Getpid(), t.Name())
	t.Cleanup(func() { eventPrefix = prev })
}

func TestSignalReachesAListener(t *testing.T) {
	isolate(t)
	got := make(chan struct{}, 1)
	stop, err := Listen(func() { got <- struct{}{} }, func() {})
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
	isolate(t)
	got := make(chan struct{}, 8)
	stop, err := Listen(func() { got <- struct{}{} }, func() {})
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
	isolate(t)
	if err := Signal(); err != nil {
		t.Errorf("signalling with no client running failed: %v", err)
	}
}

func TestStopEndsTheListener(t *testing.T) {
	isolate(t)
	fired := make(chan struct{}, 1)
	stop, err := Listen(func() { fired <- struct{}{} }, func() {})
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

/*
 * Quit is a separate verb, and must not be confusable with show.
 *
 * They are the two things the server's tray says to the client, and the client
 * acts on them very differently — one raises a window, the other ends the
 * program. An event name shared or crossed between them would make "Open the
 * LANcast app" close it.
 */
func TestQuitAndShowDoNotCrossWires(t *testing.T) {
	isolate(t)
	shown := make(chan struct{}, 4)
	quit := make(chan struct{}, 4)
	stop, err := Listen(func() { shown <- struct{}{} }, func() { quit <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	if err := Quit(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-quit:
	case <-shown:
		t.Fatal("Quit raised the window instead of closing it")
	case <-time.After(2 * time.Second):
		t.Fatal("Quit was never delivered")
	}

	if err := Signal(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-shown:
	case <-quit:
		t.Fatal("Signal closed the app instead of raising it")
	case <-time.After(2 * time.Second):
		t.Fatal("Signal was never delivered")
	}
}

// Quitting with no app running is what pressing the item on an empty desktop
// looks like, and is not a failure worth reporting.
func TestQuittingNobodyIsNotAnError(t *testing.T) {
	isolate(t)
	if err := Quit(); err != nil {
		t.Errorf("quitting with no app running failed: %v", err)
	}
}

/*
 * Whether anything can bring a hidden window back.
 *
 * Close-to-tray hides rather than closes, which is only a feature while an icon
 * exists to restore from. The client gave up its own icon deliberately — two
 * LANcast icons in the notification area was the complaint — and the server's
 * tray took over. But nothing *starts* that tray: a machine that has only
 * booted runs the service without one, and the X then hid a window nothing
 * could restore. Reported as the process staying open with no way to reopen it.
 *
 * So the client asks this before hiding, and the answer has to be false when
 * there is genuinely no tray — the safe direction, because it makes the X mean
 * close.
 */
func TestNoTrayMeansNothingCanRestoreTheWindow(t *testing.T) {
	isolate(t)
	if TrayPresent() {
		t.Error("claimed a tray with none running, so the window would hide " +
			"behind an icon that does not exist")
	}
}

func TestAHeldTrayIsVisibleToTheClient(t *testing.T) {
	isolate(t)
	release, err := HoldTray()
	if err != nil {
		t.Fatalf("HoldTray: %v", err)
	}
	if !TrayPresent() {
		t.Error("a running tray was not seen, so the X would close the app " +
			"even though an icon was there to restore it")
	}
	release()
}

/*
 * And presence ends with the tray.
 *
 * The reason this is a handle rather than a file or a registry key: it goes
 * away with the process that holds it, including one that dies badly, so a
 * crashed tray cannot leave the client hiding into nothing for ever.
 */
func TestPresenceEndsWhenTheTrayDoes(t *testing.T) {
	isolate(t)
	release, err := HoldTray()
	if err != nil {
		t.Fatalf("HoldTray: %v", err)
	}
	release()
	if TrayPresent() {
		t.Error("a released tray still reported itself present")
	}
}
