package mpv

import (
	"strconv"
	"strings"
)

/*
 * The audio pass on the desktop's player (docs/audio-pass-plan.md, Phase 1).
 *
 * Two controls, turned into one mpv `af` value. The page never supplies any of
 * the text below: the same libavfilter that provides these filters provides
 * `amovie`, a *source* that opens a file by path, so a command that accepted a
 * graph from the page would be arbitrary file read. The page sends numbers,
 * `command` clamps them, and this function is the only thing that writes a
 * filter graph. Everything in its output is a constant or a formatted integer.
 *
 * Pure, like the rest of this package, so every combination is a table test
 * and the graph can be run through a standalone ffmpeg to measure what it does
 * before it is ever played.
 */

// AudioFX is what the person has asked for.
type AudioFX struct {
	// Night compresses the dynamic range: explosions down, speech up.
	Night bool
	// Dialogue is 0 (off), 1 (low) or 2 (high).
	Dialogue int
}

// DialogueMax is the highest Dialogue level.
const DialogueMax = 2

// With applies one control from the page, by the name the native player's
// command set uses. ok is false for a name that is not an audio control, so the
// caller's closed set stays the one place that decides what a name means.
func (fx AudioFX) With(name string, value float64) (_ AudioFX, ok bool) {
	switch name {
	case "night":
		fx.Night = value != 0
	case "dialogue":
		// Clamped before the conversion, not after: int() of NaN or an
		// infinity is implementation-defined in Go, and the value is the page's.
		switch {
		case value != value || value <= 0:
			fx.Dialogue = 0
		case value >= DialogueMax:
			fx.Dialogue = DialogueMax
		default:
			fx.Dialogue = int(value)
		}
	default:
		return fx, false
	}
	return fx, true
}

// nightGraph narrows the gap between the loud scenes and the quiet ones.
//
// It works on **scenes, not bangs**. The first version (-24 dBFS, 4:1, 10 ms
// attack, 250 ms release) reacted to individual transients and let go between
// them. On ten minutes of a real action film it cut the loudness range from
// 18.4 to only 16.9 LU, while raising the whole film 11 dB. Nobody could hear
// it as anything but a volume change, and the person who listened said so.
// The difference between an explosion and the line after it is seconds long,
// so the release is 2 s. The threshold is low enough (-40 dBFS) that ordinary
// speech is inside the compressor rather than under it.
//
// Measured on the same ten minutes, and on a quiet, dialogue-led film
// (docs/audio-pass-plan.md, "Night mode, retuned"):
//
//	action: LRA 18.4 -> 8.3 LU, true peak -1.8 dBTP
//	quiet:  LRA 21.8 -> 8.5 LU, overall level within 0.6 dB of off
//
// No look-ahead filter: dynaudnorm and loudnorm measured about as well, but
// both buffer seconds of audio, and nothing tells mpv to delay the picture to
// match. A compressor and a 5 ms limiter cost no sync.
//
// The limiter's level=0 is not optional. alimiter's default is to normalise
// its output back up to full scale after limiting, which measured +1.1 dBTP:
// a limiter set to hold a ceiling, ending up louder than 0 dB. 0.7 (-3 dB)
// rather than 0.9 because a sample-peak limiter lets inter-sample peaks
// through, and 0.9 measured +0.4 dBTP on real material.
const nightGraph = "acompressor=threshold=0.01:ratio=8:attack=50:release=2000:makeup=5,alimiter=limit=0.7:level=0"

// surroundGain is what every channel except the centre is multiplied by, per
// Dialogue level. Lowering the rest instead of raising the centre is the whole
// trick: a raised centre clips on the loud scenes that need it most, and a
// lowered surround cannot clip at all. The person turns the volume up by the
// same amount and hears the same balance.
var surroundGain = [DialogueMax + 1]string{"", "0.5", "0.35"} // -6 dB, -9 dB

// stereoEnhance is dialoguenhance's `enhance` strength per level.
var stereoEnhance = [DialogueMax + 1]string{"", "1", "2"}

// centre is FC's index in FFmpeg's native order for every layout with six or
// more channels this player can meet: 5.1, 5.1(side), 6.1 and 7.1.
const centre = 2

// AudioFilter returns the `af` value for fx on a source mpv is decoding with
// the given channel count, or "" for no filtering at all. A count of zero
// means mpv has not reported one yet, and dialogue boost waits for it rather
// than guessing.
func AudioFilter(fx AudioFX, channels int) string {
	var parts []string
	if d := clampInt(fx.Dialogue, 0, DialogueMax); d > 0 {
		switch {
		case channels >= 6:
			parts = append(parts, surroundBoost(channels, surroundGain[d]))
		case channels == 2:
			parts = append(parts, "dialoguenhance=enhance="+stereoEnhance[d])
		}
		// Mono has no dialogue to separate from anything; three to five
		// channels are layouts where index 2 is not reliably the centre.
	}
	// Night mode last: it compresses whatever the boost produced, so the
	// balance the boost set is what gets levelled.
	if fx.Night {
		parts = append(parts, nightGraph)
	}
	if len(parts) == 0 {
		return ""
	}
	return "lavfi=[" + strings.Join(parts, ",") + "]"
}

// surroundBoost lowers every channel but the centre, keeping the layout mpv
// negotiated (`c=same`) rather than declaring a new one, as `pan` would.
func surroundBoost(channels int, gain string) string {
	exprs := make([]string, channels)
	for i := range exprs {
		exprs[i] = "val(" + strconv.Itoa(i) + ")"
		if i != centre {
			exprs[i] += "*" + gain
		}
	}
	return "aeval=exprs=" + strings.Join(exprs, "|") + ":c=same"
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
