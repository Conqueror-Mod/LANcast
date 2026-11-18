package transcode

import "lancast/internal/probe"

/*
 * Which H.264 level to promise.
 *
 * A level is a ceiling on frame size, and hardware encoders enforce it rather
 * than rounding up to fit. Every hardware encode used to state `4.1`
 * unconditionally, which is exactly right for 1080p and impossible for anything
 * larger — NVENC answers
 *
 *	[h264_nvenc] InitializeEncoder failed: invalid param (8): Invalid Level.
 *	[vost#0:0/h264_nvenc] Error while opening encoder
 *
 * and produces nothing. From the sofa that is a spinner over a black screen on
 * a file that plays perfectly well in anything else.
 *
 * It went unnoticed because it only bites a *video encode*. A file that
 * direct-plays or is merely remuxed never reaches an encoder, and most large
 * files did one of those — until a codec claim was withdrawn and a 2160-wide
 * episode needed converting for the first time.
 *
 * The trap worth remembering is that the file which found this is **1080 tall**:
 * 2160x1080, a scope master. Every height cap in this system waves it through,
 * and the limit it breaks is about total frame area. Height is not resolution;
 * the same mistake the resolution buckets were fixed for.
 */

// The table and the rule live in internal/probe (H264Level), because the
// playback decision needs them too: a client with a level ceiling must not be
// handed a frame its decoder cannot hold (ADR 0125). These names stay so the
// encoder's call sites read as they did.

func mbFor(width, height int) int { return probe.Macroblocks(width, height) }

// H264Level names the lowest level that can carry this frame at this rate.
func H264Level(width, height int, fps float64) string {
	return probe.H264Level(width, height, fps)
}

/*
 * How often the encoder should place a keyframe.
 *
 * Nothing set this, so every encode used the encoder's own default — around 250
 * frames, which is ten seconds at 24fps. The file path fragments on keyframes
 * (`frag_keyframe`), so the browser gets nothing playable until the *second*
 * keyframe: on a progressive stream a seek restarts ffmpeg, and the viewer
 * waits out a whole GOP before the picture returns.
 *
 * Measured on a real episode, seeking to three different offsets twice each:
 * ~2,344ms to the first bytes with the default, ~1,413ms at two seconds. About
 * 930ms off every scrub.
 *
 * Two seconds rather than shorter. A keyframe is expensive — more of them means
 * more bits for the same quality — and the return diminishes quickly: the rest
 * of the delay is process startup, opening the input and initialising the
 * encoder, none of which a GOP length can help. Two seconds also improves
 * seeking *within* what the browser already holds, where a decoder must find a
 * keyframe before it can show anything.
 *
 * Derived from the frame rate rather than fixed, because "48 frames" is two
 * seconds at 24fps and under one at 60. An unknown rate falls back to 48, which
 * is the common case rather than a guess about an unusual one.
 */
const gopSeconds = 2

func gopFrames(fps float64) int {
	if fps <= 0 {
		return 48
	}
	g := int(fps*gopSeconds + 0.5)
	if g < 12 {
		// Below this a GOP costs more in bits than it returns in latency, and
		// a pathological frame rate should not produce a keyframe every frame.
		return 12
	}
	return g
}
