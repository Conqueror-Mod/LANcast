//go:build windows

package main

import (
	"log/slog"
	"time"

	"lancast/internal/mpv"
)

// applyTiming hands mpv the display's measured rate, or its defaults for 0
// (mpv.DisplayTiming). A refusal is logged and never stops the film.
func applyTiming(p *mpv.Player, hz float64) {
	if p == nil {
		return
	}
	for _, o := range mpv.DisplayTiming(hz) {
		if err := p.Set(o.Name, o.Value); err != nil {
			slog.Warn("display timing not applied", "option", o.Name, "value", o.Value, "error", err)
		}
	}
}

// timingEvery is how often a playing film's frame timing is logged.
const timingEvery = time.Minute

/*
 * logTiming writes one line a minute while film gen plays: dropped, late and
 * mistimed frames, and the display rate mpv uses beside the one it measures.
 *
 * Built after a report of a periodic hitch on the first evening of theater
 * output, so the next report comes with numbers. Counters are cumulative for
 * the film; a line is written only when one of them moved, or once at the
 * start, so a clean film costs a line or two rather than one a minute.
 */
func (n *nativePlayer) logTiming(p *mpv.Player, gen int) {
	t := time.NewTicker(timingEvery)
	defer t.Stop()
	var last [3]int64
	first := true
	for range t.C {
		n.mu.Lock()
		current := n.gate.gen == gen && n.player == p
		n.mu.Unlock()
		if !current {
			return // another film, or a stop
		}
		// A paused film's counters do not move, so it logs nothing new.
		var now [3]int64
		for i, name := range mpv.TimingCounters {
			now[i], _ = p.Int64(name)
		}
		if !first && now == last {
			continue
		}
		first, last = false, now
		rate := map[string]float64{}
		for _, name := range mpv.TimingRates {
			rate[name], _ = p.Double(name)
		}
		slog.Info("playback timing",
			"dropped", now[0], "delayed", now[1], "mistimed", now[2],
			"display_fps", round3(rate["display-fps"]), "measured_fps", round3(rate["estimated-display-fps"]),
			"vsync_jitter", round3(rate["vsync-jitter"]))
	}
}
