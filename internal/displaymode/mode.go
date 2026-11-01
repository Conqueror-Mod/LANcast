// Package displaymode matches a monitor's refresh rate to the film playing on
// it (docs/theater-output-plan.md, step 1).
//
// A 23.976 fps film on a 60 Hz desktop is shown by repeating frames in a
// 3:2 pattern, which reads as judder on every slow pan. Kaleidescape, Kodi and
// every disc player avoid it by switching the display to the film's own rate.
//
// Split the way probe is: which mode to pick, and what to remember so a crash
// can be undone, are pure and tested here with no monitor attached. Asking
// Windows for modes and changing them is a thin layer in switch_windows.go.
package displaymode

import (
	"fmt"
	"math"
)

// Mode is one display mode, as Windows lists it.
type Mode struct {
	Width, Height int
	// Bits is the colour depth Windows reports (normally 32).
	Bits int
	// Hz is Windows' integer frequency. It is not the real rate: Windows
	// lists 23.976 as 23 and 59.94 as 59. Rate turns it into the real one.
	Hz int
}

func (m Mode) String() string { return fmt.Sprintf("%dx%d@%d", m.Width, m.Height, m.Hz) }

/*
 * ntscHz are the integer frequencies Windows uses for the 1000/1001 rates.
 *
 * The convention is the driver's, not a law. It holds across NVIDIA, AMD and
 * Intel, and Windows' own display settings show "23.976" for a 23 Hz mode, but
 * it is still a claim, which is why the native player logs the real rational
 * rate read back from the display configuration after every switch.
 */
var ntscHz = map[int]bool{23: true, 29: true, 47: true, 59: true, 71: true, 95: true, 119: true, 143: true, 239: true}

// Rate is the real refresh rate behind one of Windows' integer frequencies.
func Rate(hz int) float64 {
	if ntscHz[hz] {
		return float64(hz+1) / 1.001
	}
	return float64(hz)
}

// maxMultiple is the highest rate-to-film multiple that counts as a match:
// 23.976 at 119.88 Hz is five. Past six it is a gaming display's rate, not one
// chosen for the film.
const maxMultiple = 6

/*
 * tolerance is how far apart two rates may be and still be "the same".
 *
 * 0.05%, chosen against the one pair it has to tell apart: 24 and 23.976
 * differ by 0.1%, and confusing them is the judder this package exists to
 * remove, a dropped or doubled frame every 42 seconds. What is left inside the
 * tolerance, mpv's display-resample covers.
 */
const tolerance = 0.0005

// multiple is k when rate is k times film within tolerance, else 0.
func multiple(film, rate float64) int {
	k := math.Round(rate / film)
	if k < 1 || k > maxMultiple {
		return 0
	}
	if math.Abs(rate-k*film)/rate > tolerance {
		return 0
	}
	return int(k)
}

// The lowest and highest frame rates trusted as a film's. Outside this, the
// container is not saying anything about frames (a VFR file reporting 1000, a
// music file's cover art reporting 1).
const (
	minFilm = 10
	maxFilm = 121
)

// Decision is what Choose decided, and why, in words for the log.
type Decision struct {
	Switch bool
	To     Mode
	Reason string
}

/*
 * Choose picks the mode to switch to for a film at the given frame rate.
 *
 * The rules, in order:
 *
 *  1. A rate outside what films use is not acted on.
 *  2. A display already at a multiple of the film is left alone: a 25 fps
 *     film on a 50 Hz display, or 29.97 on 59.94, shows every frame for the
 *     same time already, and switching would be a black screen for nothing.
 *  3. Otherwise the lowest multiple wins (23.976 before 47.952 before 119.88),
 *     at the current resolution and depth only. A mode change that also
 *     changed the resolution would rescale the desktop and every window on it.
 *  4. Interlaced modes are never offered (switch_windows.go drops them).
 *
 * No match is not an error. Plenty of monitors offer nothing but 60, and the
 * film plays as it does today.
 */
func Choose(film float64, current Mode, offered []Mode) Decision {
	if film != film || film < minFilm || film > maxFilm {
		return Decision{Reason: fmt.Sprintf("no usable film rate (%.3f)", film)}
	}
	if k := multiple(film, Rate(current.Hz)); k > 0 {
		return Decision{Reason: fmt.Sprintf("display already at %s, %d× the film", hz(current.Hz), k)}
	}
	best, bestK := Mode{}, 0
	for _, m := range offered {
		if m.Width != current.Width || m.Height != current.Height || m.Bits != current.Bits {
			continue
		}
		k := multiple(film, Rate(m.Hz))
		if k == 0 {
			continue
		}
		if bestK == 0 || k < bestK {
			best, bestK = m, k
		}
	}
	if bestK == 0 {
		return Decision{Reason: fmt.Sprintf("no mode at %dx%d is a multiple of %.3f fps", current.Width, current.Height, film)}
	}
	return Decision{Switch: true, To: best, Reason: fmt.Sprintf("%s is %d× the film", hz(best.Hz), bestK)}
}

// hz writes an integer frequency as the rate it stands for: "23.976 Hz".
func hz(h int) string {
	if ntscHz[h] {
		return fmt.Sprintf("%.3f Hz", Rate(h))
	}
	return fmt.Sprintf("%d Hz", h)
}
