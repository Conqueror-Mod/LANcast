package probe

/*
 * H.264 levels, shared by the encoder (which promises one) and the decision
 * (which refuses to direct-play above a client's ceiling). See
 * internal/transcode/level.go for how the encoder found it needed this.
 */

// Macroblocks returns the macroblock count of a frame, which is what a level bounds.
// Macroblocks are 16x16 and partial ones count, so each dimension rounds up.
func Macroblocks(width, height int) int {
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
	mb := Macroblocks(width, height)
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

// levelRank is a level's place in the table, or -1 for a name it does not hold.
func levelRank(name string) int {
	for i, l := range levels {
		if l.name == name {
			return i
		}
	}
	return -1
}

/*
 * FitsH264Level reports whether a frame at this rate is within a level.
 *
 * Worked out from the frame rather than read from the stream's own level tag.
 * The tag is not stored, so reading it would mean a schema change and a
 * re-probe of every library before the rule did anything, and the frame is
 * what a decoder actually runs out of room for. An unknown frame fits, the same
 * way H264Level answers 4.1 for one: refusing a file on a number nobody
 * measured is a guess, and a full re-encode is an expensive guess.
 *
 * An unknown ceiling (an empty or unrecognised name) is no ceiling.
 */
func FitsH264Level(width, height int, fps float64, max string) bool {
	limit := levelRank(max)
	if limit < 0 {
		return true
	}
	return levelRank(H264Level(width, height, fps)) <= limit
}

/*
 * HeightForH264Level is the tallest frame, keeping the shape, that fits a level
 * at this rate. Zero when the frame already fits, or nothing is known.
 *
 * Needed because a ceiling that only refused direct play would still let the
 * encode it forced come out too large. A 4K source encoded for a 4.1 client
 * has to be scaled to fit, and so does 1080p at 60 frames, which 4.1 holds only
 * up to about 720p. Width is what breaks a scope master, so the frame is
 * shrunk as a whole and not just held to a height.
 */
func HeightForH264Level(width, height int, fps float64, max string) int {
	if width <= 0 || height <= 0 || FitsH264Level(width, height, fps, max) {
		return 0
	}
	for h := height - height%2; h >= 2; h -= 2 {
		w := width * h / height
		if FitsH264Level(w, h, fps, max) {
			return h
		}
	}
	return 0
}
