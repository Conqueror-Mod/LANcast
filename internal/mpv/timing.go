package mpv

import "strconv"

/*
 * Telling mpv what the display is really doing (docs/theater-output-plan.md,
 * step 1, found on the first evening with the Roku).
 *
 * mpv learns the display's rate from Windows when its video output starts, and
 * relearns it on WM_DISPLAYCHANGE. That message goes to top-level windows only,
 * and mpv draws into a child of LANcast's window, so after the refresh-rate
 * switch mpv went on believing the TV was at 60 Hz while it ran at 24
 * (mpv.log: "display-fps: 60.000000" on a 24p film shown at 24 Hz). Frames
 * timed against the wrong vsync are the periodic hitch that was reported.
 *
 * So once the display is settled, the native player hands mpv the measured
 * rate (clientwindow.RefreshRate, read back from the display configuration)
 * and asks it to resample: video-sync=display-resample paces frames to the
 * display's vsync and nudges the audio by a fraction of a percent to match,
 * which removes the dropped or doubled frame every few minutes that a film at
 * 23.976 against a display at 23.9761 would otherwise get.
 *
 * Off, or with the rate unknown, mpv goes back to its own defaults: its own
 * idea of the rate, and audio as the clock.
 */

// DisplayTiming is what to set on mpv for a display measured at hz, or the
// defaults when hz is zero (unknown, or matching off).
func DisplayTiming(hz float64) []Option {
	if hz != hz || hz <= 0 {
		return []Option{
			{Name: "display-fps-override", Value: "0"},
			{Name: "video-sync", Value: "audio"},
		}
	}
	return []Option{
		{Name: "display-fps-override", Value: strconv.FormatFloat(hz, 'f', 6, 64)},
		{Name: "video-sync", Value: "display-resample"},
	}
}

/*
 * What the native player logs once a minute while a film plays, so "it
 * hitches" becomes a number. TimingCounters are int64 and cumulative for the
 * film: frames dropped, frames shown late, frames shown at the wrong vsync, in
 * that order. TimingRates are doubles: the rate mpv is working to beside the
 * one it measures, and how unsteady the vsync is.
 */
var (
	TimingCounters = []string{"frame-drop-count", "vo-delayed-frame-count", "mistimed-frame-count"}
	TimingRates    = []string{"display-fps", "estimated-display-fps", "vsync-jitter"}
)
