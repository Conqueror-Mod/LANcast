package mpv

import "math"

/*
 * Volume is the value to give mpv's volume property for a slider at v (0..1).
 *
 * mpv's volume is cubic: the gain it applies is (volume/100)³. Measured on the
 * shipped libmpv, rendering a film to a file: volume 25 was 36.1 dB below 100
 * and volume 20 was 41.9 dB below, which is 0.25³ and 0.2³ to the decimal.
 * Handed the slider straight through as a percentage, a quarter of the way up
 * put a film's dialogue near -66 dB, which is silence, where the same slider on
 * music (played by the media element, whose volume is plain amplitude) is
 * 12 dB down.
 *
 * So the slider is taken as the amplitude wanted, and mpv is given its cube
 * root: the gain mpv then applies is v itself, and one slider means the same
 * loudness whichever engine is playing.
 */
func Volume(v float64) float64 {
	if v != v || v <= 0 {
		return 0
	}
	if v >= 1 {
		return 100
	}
	return 100 * math.Cbrt(v)
}
