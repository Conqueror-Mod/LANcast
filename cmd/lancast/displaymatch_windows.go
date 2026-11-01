//go:build windows

package main

import (
	"log/slog"
	"math"
	"time"

	"lancast/internal/clientwindow"
	"lancast/internal/displaymode"
)

/*
 * Refresh-rate matching for the native player (docs/theater-output-plan.md,
 * step 1).
 *
 * The film's rate is read from mpv once the file is open, the monitor the
 * video window is on is switched to a multiple of it (displaymode.Choose
 * decides), and the film starts once the display has had time to resync.
 * Everything that can go wrong ends the same way: the film starts at the
 * display's own rate, as it always has, and the log says why.
 *
 * Known gap: a window dragged to another monitor mid-film leaves the first
 * monitor switched until the film stops. The next film matches the new
 * monitor and puts the old one back (Switcher.Match).
 */

// maxSettle bounds the page's settle setting. Ten seconds is past any TV
// measured; more would read as a film that will not start.
const maxSettle = 10 * time.Second

/*
 * defaultSettle applies until the page sends its setting (prefs.ts holds the
 * same number). Measured, not guessed: on the first evening with a Roku
 * 55R4AX, 2 s lost about half a second of the opening sound to the black
 * screen and 3 s lost none. The TV needs about 2.5 s to resync at 4K.
 */
const defaultSettle = 3 * time.Second

// restoreGrace is how long a stopped film keeps the display's rate. The next
// film in a queue arrives well inside it, and a film of the same rate then
// needs no switch at all.
const restoreGrace = 5 * time.Second

// display is the switcher, made on first use. n.mu must be held.
func (n *nativePlayer) display() *displaymode.Switcher {
	if n.disp == nil {
		n.disp = displaymode.NewSwitcher(clientDataDir())
	}
	return n.disp
}

// matchDisplay runs off the player's event goroutine: a mode change sends
// WM_DISPLAYCHANGE to every window, and the settle wait is seconds long.
func (n *nativePlayer) matchDisplay(gen int) {
	n.mu.Lock()
	p, wid, disp := n.player, n.wid, n.display()
	settle := defaultSettle
	if n.settleSet {
		settle = n.settle
	}
	n.mu.Unlock()
	if p == nil {
		return
	}
	start := time.Now()
	fps, err := p.Double("container-fps")
	if err != nil || fps <= 0 {
		// Some containers carry no rate; the decoder's estimate, read from the
		// frames themselves, is the fallback.
		fps, _ = p.Double("estimated-vf-fps")
	}
	res, err := disp.Match(wid, fps)
	switch {
	case err != nil:
		slog.Warn("display not matched", "film_fps", round3(fps), "error", err)
	case res.Decision.Switch:
		got, _ := clientwindow.RefreshRate(res.Device)
		slog.Info("display matched", "film_fps", round3(fps), "from", res.From.String(),
			"to", res.Decision.To.String(), "reported_hz", round3(got),
			"monitor", res.Device, "reason", res.Decision.Reason)
		time.Sleep(settle)
	default:
		slog.Info("display not matched", "film_fps", round3(fps), "monitor", res.Device,
			"reason", res.Decision.Reason)
	}
	// Switched or not, mpv is told the rate the screen is really at: it
	// cannot hear the change itself (mpv.DisplayTiming says why).
	if hz, ok := clientwindow.RefreshRate(res.Device); ok && err == nil {
		applyTiming(p, hz)
		slog.Info("display timing given to mpv", "display_hz", round3(hz), "video_sync", "display-resample")
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	play, ok := n.gate.settled(gen)
	if !ok {
		return // a newer film, or a stop, came first
	}
	slog.Info("film start held for the display", "held_ms", time.Since(start).Milliseconds(), "playing", play)
	if play && n.player != nil {
		if err := n.player.Set("pause", "no"); err != nil {
			slog.Warn("starting the film after matching", "error", err)
		}
	}
}

/*
 * scheduleRestore puts the display back after restoreGrace, unless another
 * film opens first. n.mu must be held.
 *
 * The timer lets go of n.mu before it changes the mode. A mode change sends
 * WM_DISPLAYCHANGE to the window, and the window's thread may be sitting in a
 * binding waiting for n.mu: holding it across the change would wait for a
 * thread that is waiting for us.
 */
func (n *nativePlayer) scheduleRestore() {
	if n.disp == nil || !n.disp.Active() {
		return
	}
	n.cancelRestore()
	var t *time.Timer
	t = time.AfterFunc(restoreGrace, func() {
		n.mu.Lock()
		if n.restoreT != t {
			n.mu.Unlock()
			return // a film opened, or matching was turned off, since
		}
		n.restoreT = nil
		disp := n.disp
		n.mu.Unlock()
		restoreNow(disp)
	})
	n.restoreT = t
}

// cancelRestore stops a scheduled restore. n.mu must be held.
func (n *nativePlayer) cancelRestore() {
	if n.restoreT != nil {
		n.restoreT.Stop()
		n.restoreT = nil
	}
}

// restoreDisplay puts a switched display back now. n.mu must be held, and
// only on the window's own thread or after the window is gone (see
// scheduleRestore for why).
func (n *nativePlayer) restoreDisplay() { restoreNow(n.disp) }

func restoreNow(disp *displaymode.Switcher) {
	if disp == nil || !disp.Active() {
		return
	}
	if err := disp.Restore(); err != nil {
		slog.Warn("display not restored", "error", err)
		return
	}
	slog.Info("display restored")
}

// shutdown runs as the app exits: a display switched for a film goes back
// before the process does.
func (n *nativePlayer) shutdown() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.cancelRestore()
	n.restoreDisplay()
}

// recoverDisplay undoes a switch the last run left behind (a crash, or a kill
// from Task Manager mid-film). Called at startup, before the window.
func recoverDisplay() {
	r, ok, err := displaymode.Recover(clientDataDir())
	if !ok {
		return
	}
	if err != nil {
		slog.Warn("display left switched by the last run, not restored", "monitor", r.Device, "error", err)
		return
	}
	slog.Info("display left switched by the last run, restored", "monitor", r.Device,
		"was_hz", r.ToHz, "back_to_hz", r.FromHz)
}

func round3(f float64) float64 { return math.Round(f*1000) / 1000 }
