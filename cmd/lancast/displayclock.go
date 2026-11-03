package main

import (
	"log/slog"
	"math"
	"runtime"
	"strconv"
	"sync"
	"time"

	"lancast/internal/clientwindow"
)

/*
 * The screen's own clock, for the visualizers (ADR 0113).
 *
 * The page draws on requestAnimationFrame, and Chromium ticks that at one
 * rate for the whole desktop. On a TV at 30 Hz beside a 72 Hz monitor, the
 * page drew 72 frames a second, the TV took one every 33 ms, and each picture
 * stood for 2 or 3 of them in turn: motion stepping 28 ms, then 42 ms, for
 * ever. PresentMon showed 58% of the frames thrown away and the page on time.
 *
 * So while a visualizer asks, a thread here waits on the vertical blank of
 * the monitor the window is on and tells the page each time. The page draws
 * one frame per blank and moves by exactly one refresh. It asks only when its
 * own clock disagrees with this monitor's (lancastDisplayRate), so a window
 * on the main monitor never sees a tick.
 *
 * Two functions:
 *   - lancastDisplayRate() -> {hz, device} for the window's monitor now;
 *   - lancastDisplayTicks(on) -> running: start or stop the ticks.
 * Each tick is window.__lancastDisplayTick(n, hz): n counts blanks since the
 * ticks began, so a tick the page never saw (a busy window thread) shows as a
 * jump in n and the page moves by two refreshes rather than one. hz 0 means
 * the ticks have stopped on their own and the page goes back to its clock.
 */

// displayRecheck is how often the ticking thread looks for the window having
// moved to another monitor.
const displayRecheck = time.Second

// displayWaitFailures in a row and the ticks stop; the page falls back.
const displayWaitFailures = 5

// blankWaiter is one monitor's vertical blank (internal/vsync on Windows).
type blankWaiter interface {
	Wait() error
	Close()
}

type displayClock struct {
	win     func() clientwindow.Controller
	monitor func(hwnd uintptr) (string, error)
	rate    func(device string) (float64, bool)
	open    func(device string) (blankWaiter, error)
	now     func() time.Time

	mu   sync.Mutex
	stop chan struct{} // non-nil while ticking
	done chan struct{} // closed when that thread has returned
}

// where is the window's monitor and its rate, or "" when it cannot be found.
func (c *displayClock) where() (string, float64) {
	w := c.win()
	if w == nil {
		return "", 0
	}
	hwnd := w.Window()
	if hwnd == 0 {
		return "", 0
	}
	dev, err := c.monitor(hwnd)
	if err != nil || dev == "" {
		return "", 0
	}
	hz, ok := c.rate(dev)
	if !ok {
		return dev, 0
	}
	return dev, hz
}

func (c *displayClock) bindings() map[string]any {
	return map[string]any{
		"lancastDisplayRate": func() map[string]any {
			dev, hz := c.where()
			if dev == "" || hz <= 0 {
				return map[string]any{"error": "monitor not known"}
			}
			return map[string]any{"hz": hz3(hz), "device": dev}
		},
		"lancastDisplayTicks": func(on bool) bool {
			if on {
				return c.start()
			}
			c.halt()
			return false
		},
	}
}

func (c *displayClock) start() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stop != nil {
		return true
	}
	c.stop = make(chan struct{})
	c.done = make(chan struct{})
	go c.run(c.stop, c.done)
	return true
}

// halt stops the ticks and returns once the thread has, so a start straight
// after cannot leave two threads ticking.
func (c *displayClock) halt() {
	c.mu.Lock()
	stop, done := c.stop, c.done
	c.stop, c.done = nil, nil
	c.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-done // at most one blank away
}

func (c *displayClock) push(n int, hz float64) {
	w := c.win()
	if w == nil {
		return
	}
	// Two numbers and nothing else: the script is built from no string the
	// page or a monitor supplied.
	w.Eval("window.__lancastDisplayTick&&window.__lancastDisplayTick(" +
		strconv.Itoa(n) + "," + strconv.FormatFloat(hz3(hz), 'f', -1, 64) + ")")
}

func (c *displayClock) run(stop <-chan struct{}, done chan<- struct{}) {
	// The wait blocks a thread for a whole refresh; keep it off the
	// scheduler's shared ones.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(done)

	var (
		w        blankWaiter
		dev      string
		hz       float64
		checked  time.Time
		n, fails int
	)
	defer func() {
		if w != nil {
			w.Close()
		}
	}()
	quit := func(why string, args ...any) {
		slog.Info("display ticks stopped: "+why, append([]any{"device", dev}, args...)...)
		c.push(n, 0)
	}
	for {
		select {
		case <-stop:
			return
		default:
		}
		if w == nil || c.now().Sub(checked) >= displayRecheck {
			checked = c.now()
			d, r := c.where()
			if d == "" || r <= 0 {
				quit("the window's monitor is not known")
				return
			}
			if d != dev || w == nil {
				if w != nil {
					w.Close()
					w = nil
				}
				nw, err := c.open(d)
				if err != nil {
					dev = d
					quit("cannot wait for the monitor's refresh", "error", err)
					return
				}
				if dev != d {
					slog.Info("display ticks following the window's monitor", "device", d, "hz", hz3(r))
				}
				w, dev = nw, d
			}
			hz = r
		}
		if err := w.Wait(); err != nil {
			fails++
			if fails >= displayWaitFailures {
				quit("the refresh wait kept failing", "error", err)
				return
			}
			w.Close()
			w = nil
			time.Sleep(50 * time.Millisecond)
			continue
		}
		fails = 0
		n++
		c.push(n, hz)
	}
}

// hz3 is a rate to three places: 29.97, not 29.970029970029973.
func hz3(f float64) float64 { return math.Round(f*1000) / 1000 }
