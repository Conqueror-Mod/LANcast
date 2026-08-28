package transcode

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

// mbFor returns the macroblock count of a frame, which is what a level bounds.
// Macroblocks are 16x16 and partial ones count, so each dimension rounds up.
func mbFor(width, height int) int {
	if width <= 0 || height <= 0 {
		return 0
	}
	return ((width + 15) / 16) * ((height + 15) / 16)
}

/*
 * levels, smallest first, from Table A-1 of the H.264 specification.
 *
 * Only the two limits that a media server can actually breach are modelled:
 * MaxFS, the frame in macroblocks, and MaxMBPS, macroblocks per second, which
 * is what separates 4K30 from 4K60. Bitrate ceilings are deliberately not
 * modelled — they bound the *output*, which is chosen here rather than given,
 * and a level picked to accommodate a bitrate we also control would be
 * circular.
 */
var levels = []struct {
	name    string
	maxFS   int
	maxMBPS int
}{
	{"4.1", 8192, 245760},
	{"4.2", 8704, 522240},
	{"5.0", 22080, 589824},
	{"5.1", 36864, 983040},
	{"5.2", 36864, 2073600},
	{"6.0", 139264, 4177920},
	{"6.1", 139264, 8355840},
	{"6.2", 139264, 16711680},
}

/*
 * H264Level names the lowest level that can carry this frame at this rate.
 *
 * It starts at 4.1 rather than at the bottom of the table on purpose: 4.1 is
 * what every encode here promised before, it is the level browsers and devices
 * are most reliably happy with, and nothing is gained by telling a decoder that
 * a 480p stream is level 3.0. This only ever moves *up*, and only when the
 * frame genuinely does not fit.
 *
 * fps may be zero when the source did not report a frame rate. The frame size
 * still decides, and the rate is simply not consulted — better a level that is
 * right about the thing we know than one guessed from a number we do not have.
 *
 * An unknown frame size answers 4.1, which is the behaviour this replaces: with
 * nothing to go on, the previous default is the conservative answer rather than
 * an invented one.
 */
func H264Level(width, height int, fps float64) string {
	mb := mbFor(width, height)
	if mb == 0 {
		return "4.1"
	}
	rate := 0
	if fps > 0 {
		rate = int(float64(mb) * fps)
	}

	for _, l := range levels {
		if mb <= l.maxFS && (rate == 0 || rate <= l.maxMBPS) {
			return l.name
		}
	}
	// Beyond the table is beyond H.264 as specified. Naming the highest level
	// is a better failure than naming one that is certainly too small: the
	// encoder may still refuse, and it will say so, which is the outcome this
	// whole file exists to stop being silent.
	return levels[len(levels)-1].name
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
