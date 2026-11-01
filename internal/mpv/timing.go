package mpv

import (
	"math"
	"strconv"
)

/*
 * Telling mpv what the display is really doing (docs/theater-output-plan.md,
 * step 1, found on the first evening with the Roku).
 *
 * Two faults, both found by measuring.
 *
 * 1. mpv learns the display's rate from Windows when its video output starts,
 *    and relearns it on WM_DISPLAYCHANGE. That message goes to top-level
 *    windows only, and mpv draws into a child of LANcast's window, so after
 *    the refresh-rate switch mpv went on believing the TV was at 60 Hz while
 *    it ran at 24 (mpv.log: "display-fps: 60.000000" on a 24p film at 24 Hz).
 *    So it is told: DisplayTiming hands it the rate read back from the
 *    display configuration.
 *
 * 2. Where mpv's frames are paced is not up to mpv. The page's controls sit
 *    over the video, so Windows composes the two before the screen sees them,
 *    and on a desktop of monitors at different rates it composes on one
 *    clock. On the theater PC that was the 71.91 Hz desk monitor, not the
 *    TV at 23.976: "playback timing … display_fps=23.976 measured_fps=71.91
 *    delayed=200 mistimed=209" in one minute, a stutter that came and went as
 *    the two clocks drifted past each other. With the TV as the only display
 *    the same film played smoothly.
 *
 *    video-sync=display-resample paces frames to the display's vsync, and is
 *    the smoothest there is when that vsync is the TV's. When it is another
 *    monitor's, resampling to it makes the stutter worse (it did, the first
 *    time). So resampling is a trial, not an assumption. mpv only measures
 *    the rate its presents really run at while it resamples (with audio as
 *    the clock, estimated-display-fps reads 0), so the player resamples for
 *    the first seconds of a film, measures, and keeps it only if
 *    PacedByDisplay agrees.
 */

// DisplayTiming is what to set on mpv for a display measured at hz, or mpv's
// own defaults when hz is zero (unknown, or matching off). Audio stays the
// clock either way; Resample is what changes that, once it is earned.
func DisplayTiming(hz float64) []Option {
	if hz != hz || hz <= 0 {
		return []Option{
			{Name: "display-fps-override", Value: "0"},
			{Name: "video-sync", Value: "audio"},
		}
	}
	return []Option{
		{Name: "display-fps-override", Value: strconv.FormatFloat(hz, 'f', 6, 64)},
		{Name: "video-sync", Value: "audio"},
	}
}

// Resample paces frames to the display's vsync. Only for a display that
// PacedByDisplay says is really the one setting the pace.
var Resample = []Option{{Name: "video-sync", Value: "display-resample"}}

// pacingTolerance is how close the measured present rate must be to the
// display's for the display to count as the clock: 0.5%, far wider than any
// display's own wander and far narrower than the 3× of a 72 Hz desk monitor
// against a 24 Hz TV.
const pacingTolerance = 0.005

/*
 * PacedByDisplay reports whether mpv's presents run on the display's own
 * clock: measured (mpv's estimated-display-fps) within pacingTolerance of the
 * display's rate. Unknown (zero) is no, because resampling to a clock nobody
 * has measured is the mistake this guards against.
 */
func PacedByDisplay(displayHz, measured float64) bool {
	if displayHz <= 0 || measured <= 0 || displayHz != displayHz || measured != measured {
		return false
	}
	return math.Abs(measured-displayHz)/displayHz <= pacingTolerance
}

/*
 * What the native player logs while a film plays, so "it hitches" becomes a
 * number. TimingCounters are int64 and cumulative for the film: frames
 * dropped, frames shown late, frames shown at the wrong vsync, in that order.
 * TimingRates are doubles: the rate mpv is working to beside the one it
 * measures, and how unsteady the vsync is.
 */
var (
	TimingCounters = []string{"frame-drop-count", "vo-delayed-frame-count", "mistimed-frame-count"}
	TimingRates    = []string{"display-fps", "estimated-display-fps", "vsync-jitter"}
)
