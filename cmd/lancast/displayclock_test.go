package main

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"lancast/internal/clientwindow"
)

// clockWindow is the window as the display clock sees it: a handle and Eval.
type clockWindow struct {
	clientwindow.Controller
	mu    sync.Mutex
	evals []string
}

func (w *clockWindow) Window() uintptr { return 42 }
func (w *clockWindow) Eval(js string) {
	w.mu.Lock()
	w.evals = append(w.evals, js)
	w.mu.Unlock()
}
func (w *clockWindow) got() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.evals...)
}

// fakeBlanks is a monitor whose blanks come every millisecond, or fail.
type fakeBlanks struct {
	fail   bool
	closed *int
}

func (f *fakeBlanks) Wait() error {
	time.Sleep(time.Millisecond)
	if f.fail {
		return errors.New("no blank")
	}
	return nil
}
func (f *fakeBlanks) Close() { *f.closed++ }

type clockRig struct {
	c      *displayClock
	w      *clockWindow
	mu     sync.Mutex
	device string
	opened []string
	closed int
	fail   bool
}

func newClockRig() *clockRig {
	r := &clockRig{w: &clockWindow{}, device: `\\.\DISPLAY3`}
	r.c = &displayClock{
		win: func() clientwindow.Controller { return r.w },
		monitor: func(uintptr) (string, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			return r.device, nil
		},
		rate: func(d string) (float64, bool) {
			if d == `\\.\DISPLAY3` {
				return 30000.0 / 1001, true
			}
			return 72, true
		},
		open: func(d string) (blankWaiter, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.opened = append(r.opened, d)
			return &fakeBlanks{fail: r.fail, closed: &r.closed}, nil
		},
		now: time.Now,
	}
	return r
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// The rate is the monitor's, to three places, with its device name.
func TestDisplayRateIsTheWindowsMonitor(t *testing.T) {
	r := newClockRig()
	got := r.c.bindings()["lancastDisplayRate"].(func() map[string]any)()
	if got["hz"] != 29.97 || got["device"] != `\\.\DISPLAY3` {
		t.Errorf("rate = %v", got)
	}
	r.c.win = func() clientwindow.Controller { return nil }
	if got := r.c.bindings()["lancastDisplayRate"].(func() map[string]any)(); got["error"] == nil {
		t.Errorf("no window should be an error, got %v", got)
	}
}

// Started, it ticks with a rising count and the rate; stopped, it stops, and
// a second start does not make a second thread.
func TestDisplayTicksStartAndStop(t *testing.T) {
	r := newClockRig()
	ticks := r.c.bindings()["lancastDisplayTicks"].(func(bool) bool)
	if !ticks(true) || !ticks(true) {
		t.Fatal("start reported not running")
	}
	waitFor(t, "ticks", func() bool { return len(r.w.got()) >= 5 })
	ticks(false)
	n := len(r.w.got())
	time.Sleep(10 * time.Millisecond)
	if len(r.w.got()) != n {
		t.Error("ticks continued after stop")
	}
	evals := r.w.got()
	if evals[0] != "window.__lancastDisplayTick&&window.__lancastDisplayTick(1,29.97)" ||
		evals[1] != "window.__lancastDisplayTick&&window.__lancastDisplayTick(2,29.97)" {
		t.Errorf("first ticks = %q", evals[:2])
	}
	if len(r.opened) != 1 {
		t.Errorf("opened %d waiters for one start; two threads were ticking", len(r.opened))
	}
	if r.closed != 1 {
		t.Errorf("waiter closed %d times, want 1", r.closed)
	}
}

// A window moved to another monitor is followed, with that monitor's rate.
func TestDisplayTicksFollowTheWindow(t *testing.T) {
	r := newClockRig()
	clock := time.Now()
	var cmu sync.Mutex
	r.c.now = func() time.Time { cmu.Lock(); defer cmu.Unlock(); return clock }
	ticks := r.c.bindings()["lancastDisplayTicks"].(func(bool) bool)
	ticks(true)
	defer ticks(false)
	waitFor(t, "ticks", func() bool { return len(r.w.got()) >= 2 })
	r.mu.Lock()
	r.device = `\\.\DISPLAY1`
	r.mu.Unlock()
	cmu.Lock()
	clock = clock.Add(displayRecheck)
	cmu.Unlock()
	waitFor(t, "a tick at 72 Hz", func() bool {
		e := r.w.got()
		return strings.HasSuffix(e[len(e)-1], ",72)")
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.opened) != 2 || r.opened[1] != `\\.\DISPLAY1` {
		t.Errorf("opened %v", r.opened)
	}
}

// A refresh wait that keeps failing stops the ticks and says so to the page
// with hz 0, so it goes back to its own clock rather than freezing.
func TestDisplayTicksGiveUpAndTellThePage(t *testing.T) {
	r := newClockRig()
	r.fail = true
	ticks := r.c.bindings()["lancastDisplayTicks"].(func(bool) bool)
	ticks(true)
	defer ticks(false)
	waitFor(t, "the give-up tick", func() bool { return len(r.w.got()) == 1 })
	if got := r.w.got()[0]; got != "window.__lancastDisplayTick&&window.__lancastDisplayTick(0,0)" {
		t.Errorf("give-up = %q", got)
	}
}
