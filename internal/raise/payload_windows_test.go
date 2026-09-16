//go:build windows

package raise

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

/*
 * Carrying a destination as well as a knock.
 *
 * A named event is a doorbell and says nothing but "somebody is here". That was
 * enough while the only message was "show yourself"; it stopped being enough
 * when the server's tray gained items naming a *place* — and because it could
 * not say the second half, the tray opened a browser instead, beside a window
 * that was already on screen.
 */

func waitFor(t *testing.T, got <-chan string) string {
	t.Helper()
	select {
	case v := <-got:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("no signal arrived")
		return ""
	}
}

func TestShowCarriesTheDestination(t *testing.T) {
	isolate(t)
	got := make(chan string, 1)
	stop, err := Listen(func(pane string) { got <- pane }, func() {})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer stop()

	delivered, err := Show("libraries")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !delivered {
		t.Fatal("Show reported nobody listening while a listener was running")
	}
	if pane := waitFor(t, got); pane != "libraries" {
		t.Errorf("pane = %q, want libraries", pane)
	}
}

/*
 * The bool is the point of the whole change.
 *
 * Without it the tray could only guess whether a window existed, and a guess is
 * either a browser nobody wanted or a menu item that silently does nothing. It
 * has to be false when there is no client, or the fallback never runs.
 */
func TestShowReportsWhenNobodyIsListening(t *testing.T) {
	isolate(t)
	delivered, err := Show("libraries")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if delivered {
		t.Error("Show claimed delivery with no listener; the tray would never fall back")
	}
}

/*
 * A destination is consumed by the signal it arrived with.
 *
 * Left behind, it would be read again by the next plain raise — a second launch
 * from the Start menu, which names no destination — and the window would jump
 * to whatever the tray last asked for, minutes later, for no visible reason.
 */
func TestADestinationIsNotReadTwice(t *testing.T) {
	isolate(t)
	got := make(chan string, 2)
	stop, err := Listen(func(pane string) { got <- pane }, func() {})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer stop()

	if _, err := Show("updates"); err != nil {
		t.Fatal(err)
	}
	if pane := waitFor(t, got); pane != "updates" {
		t.Fatalf("first pane = %q, want updates", pane)
	}

	// A plain raise, naming nowhere.
	if err := Signal(); err != nil {
		t.Fatal(err)
	}
	if pane := waitFor(t, got); pane != "" {
		t.Errorf("second raise carried %q; a stale destination was read again", pane)
	}
}

// An empty destination is a plain raise, which is what a second launch of the
// client means and what this did before it could carry anything.
func TestShowWithNoDestination(t *testing.T) {
	isolate(t)
	got := make(chan string, 1)
	stop, err := Listen(func(pane string) { got <- pane }, func() {})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer stop()

	if _, err := Show(""); err != nil {
		t.Fatal(err)
	}
	if pane := waitFor(t, got); pane != "" {
		t.Errorf("pane = %q, want empty", pane)
	}
}

/*
 * Too long to fit is refused rather than truncated.
 *
 * Half a destination is worse than none: it would navigate somewhere nobody
 * asked for, where none at all just shows the window.
 */
func TestAnOversizedDestinationIsDropped(t *testing.T) {
	isolate(t)
	got := make(chan string, 1)
	stop, err := Listen(func(pane string) { got <- pane }, func() {})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer stop()

	delivered, err := Show(strings.Repeat("x", payloadBytes))
	if err != nil {
		t.Fatal(err)
	}
	// The raise still happens — the window coming forward is the useful half.
	if !delivered {
		t.Error("an oversized destination stopped the raise")
	}
	if pane := waitFor(t, got); pane != "" {
		t.Errorf("pane = %q, want empty — it should be dropped, not truncated", pane)
	}
}

/*
 * A listener that did not create the section still reads the destination.
 *
 * Deterministic on purpose, and the reason this file has two tests about it.
 * One listener means there is no question who wakes, so the assertion is about
 * the section and nothing else: the pane is written into a mapping this
 * listener did not create, and it must still come back.
 *
 * This is the regression. createPayload treated the name already existing as a
 * failure, so every listener but the first mapped nothing and read "" — and
 * because a second client is an ordinary state, two clients running meant the
 * tray could raise a window but never say where to.
 */
func TestAListenerReadsASectionItDidNotCreate(t *testing.T) {
	isolate(t)

	// Somebody else owns the section before this listener starts, which is what
	// a client meeting an already-running client finds.
	ownerH, ownerAddr, err := createPayload()
	if err != nil {
		t.Fatalf("owning section: %v", err)
	}
	t.Cleanup(func() {
		windows.UnmapViewOfFile(ownerAddr)
		windows.CloseHandle(ownerH)
	})

	got := make(chan string, 1)
	stop, err := Listen(func(pane string) { got <- pane }, func() {})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer stop()

	if _, err := Show("libraries"); err != nil {
		t.Fatal(err)
	}
	if pane := waitFor(t, got); pane != "libraries" {
		t.Errorf("pane = %q, want libraries — the destination was written to a section this listener never mapped", pane)
	}
}

/*
 * The same rule through the front door, with two real listeners.
 *
 * Two Listen calls share a prefix, so the second meets a section that already
 * exists. The event is auto-reset, so exactly one of them wakes and which one
 * is the operating system's choice — so *both* report here and the assertion is
 * about what arrived rather than who brought it. Asserting on only one of the
 * two is what made this flaky: it passed while the section's owner happened to
 * win the wake-up, which is what an idle machine does, and failed under the
 * load of a full test run when the other one did.
 */
func TestListeningSurvivesASectionItDidNotCreate(t *testing.T) {
	isolate(t)
	got := make(chan string, 2)
	first, err := Listen(func(pane string) { got <- pane }, func() {})
	if err != nil {
		t.Fatalf("first listen: %v", err)
	}
	defer first()

	second, err := Listen(func(pane string) { got <- pane }, func() {})
	if err != nil {
		t.Fatalf("second listen: %v", err)
	}
	defer second()

	if _, err := Show("libraries"); err != nil {
		t.Fatal(err)
	}
	// Whichever woke, the destination must have survived the trip.
	if pane := waitFor(t, got); pane != "libraries" {
		t.Errorf("pane = %q, want libraries", pane)
	}
	// And only one of them woke, which is what auto-reset means.
	select {
	case extra := <-got:
		t.Errorf("both listeners woke on one signal (second got %q)", extra)
	case <-time.After(250 * time.Millisecond):
	}
}
