//go:build windows

package main

import (
	"log/slog"
	"math"
	"time"

	"lancast/internal/clientwindow"
	"lancast/internal/mpv"
)

// applyTiming hands mpv the display's measured rate, or its defaults for 0
// (mpv.DisplayTiming). A refusal is logged and never stops the film.
func applyTiming(p *mpv.Player, hz float64) {
	setAll(p, mpv.DisplayTiming(hz))
}

func setAll(p *mpv.Player, opts []mpv.Option) {
	if p == nil {
		return
	}
	for _, o := range opts {
		if err := p.Set(o.Name, o.Value); err != nil {
			slog.Warn("display timing not applied", "option", o.Name, "value", o.Value, "error", err)
		}
	}
}

// namingTolerance is how near a monitor's rate must be to the measured pace
// for the warning to name it: 3%, wide enough for mpv's estimate, narrow
// enough that a 60 Hz and a 72 Hz monitor are never confused.
const namingTolerance = 0.03

const (
	// timingFirst is when a film's frame timing is first looked at: past the
	// display settle and a few seconds of frames, so mpv has measured the
	// rate its presents run at. A short test still gets a number.
	timingFirst = 15 * time.Second
	// timingEvery is how often after that.
	timingEvery = time.Minute
)

/*
 * logTiming watches film gen's frame timing while it plays.
 *
 * Once, at timingFirst, it decides whether pacing to the display is earned
 * (mpv.PacedByDisplay): with the TV as the clock, display-resample goes on;
 * with another monitor setting the pace, it stays off and the log names that
 * monitor, because the fix is on the desk, not in the player.
 *
 * Then a line whenever the dropped, late or mistimed counters move, and at
 * the first look, so a clean film costs a line or two and a bad one says how
 * bad. Built after a periodic stutter on the first evening of theater output:
 * "measured_fps=71.91" on a 23.976 Hz TV is how the cause was found.
 */
func (n *nativePlayer) logTiming(p *mpv.Player, gen int) {
	wait := timingFirst
	var last [3]int64
	first := true
	for {
		time.Sleep(wait)
		wait = timingEvery
		n.mu.Lock()
		current := n.gate.gen == gen && n.player == p
		hz := n.timedHz
		n.mu.Unlock()
		if !current {
			return // another film, or a stop
		}
		var now [3]int64
		for i, name := range mpv.TimingCounters {
			now[i], _ = p.Int64(name)
		}
		rate := map[string]float64{}
		for _, name := range mpv.TimingRates {
			rate[name], _ = p.Double(name)
		}
		measured := rate["estimated-display-fps"]
		if first && hz > 0 {
			decidePacing(p, hz, measured)
		}
		// A paused film's counters do not move, so it logs nothing new.
		if !first && now == last {
			continue
		}
		first, last = false, now
		slog.Info("playback timing",
			"dropped", now[0], "delayed", now[1], "mistimed", now[2],
			"display_fps", round3(rate["display-fps"]), "measured_fps", round3(measured),
			"vsync_jitter", round3(rate["vsync-jitter"]))
	}
}

// decidePacing ends the resampling trial matchDisplay began: kept when mpv's
// presents really run on the display's clock, taken back (audio as the clock
// again) when they do not, with the monitor whose clock it is named.
func decidePacing(p *mpv.Player, displayHz, measured float64) {
	if mpv.PacedByDisplay(displayHz, measured) {
		slog.Info("frames paced by the display, resampling to it",
			"display_hz", round3(displayHz), "measured_fps", round3(measured))
		return
	}
	applyTiming(p, displayHz)
	// Naming the monitor only needs the nearest rate, not the strict match
	// pacing does: the theater PC measured 71.538 against a monitor at 71.91,
	// 0.5% apart, and the first warning named nobody.
	pacer, nearest := "", namingTolerance
	for _, m := range clientwindow.Monitors() {
		if r, ok := clientwindow.RefreshRate(m.Device); ok && r > 0 && math.Abs(measured-r)/r < nearest {
			nearest = math.Abs(measured-r) / r
			pacer = m.Name
			if pacer == "" {
				pacer = m.Device
			}
		}
	}
	slog.Warn("frames paced by another clock, not resampling; expect some stutter",
		"display_hz", round3(displayHz), "measured_fps", round3(measured), "paced_by", pacer,
		"fix", "make the TV the only display, or give every monitor the same rate")
}
