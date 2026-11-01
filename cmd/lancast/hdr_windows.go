//go:build windows

package main

import (
	"log/slog"

	"lancast/internal/clientwindow"
	"lancast/internal/displaymode"
	"lancast/internal/mpv"
)

/*
 * What happened to this film's picture (docs/theater-output-plan.md, step 2).
 *
 * One log line per film, once frames are moving: the film's colour space,
 * what mpv sent the display, and what Windows says about the screen. "My TV
 * isn't showing HDR" then has its answer in the log, and the answer names
 * which of the three switches is off (mpv.VideoReport.Verdict).
 */
func (n *nativePlayer) reportVideo(p *mpv.Player, wid uintptr, hint bool) {
	if p == nil {
		return
	}
	read := func(name string) string {
		s, _ := p.String(name)
		return s
	}
	r := mpv.VideoReport{
		FilmGamma: read("video-params/gamma"), FilmPrimaries: read("video-params/primaries"),
		TargetGamma: read("video-target-params/gamma"), TargetPrimaries: read("video-target-params/primaries"),
	}
	if r.FilmGamma == "" {
		return // music, or a file with no picture: nothing to say
	}
	device, _ := displaymode.MonitorOf(wid)
	supported, enabled, ok := clientwindow.HDRState(device)
	slog.Info("video output",
		"film", r.FilmGamma+"/"+r.FilmPrimaries,
		"sent", r.TargetGamma+"/"+r.TargetPrimaries,
		"hint", hint, "display_hdr", enabled, "display_can_hdr", supported, "display_known", ok,
		"monitor", device, "verdict", r.Verdict(hint, enabled))
}
