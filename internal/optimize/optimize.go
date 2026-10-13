/*
 * Package optimize holds the rules of optimized copies (ADR 0087) that are
 * worth stating as functions: what a copy is, when playback should prefer it,
 * how big it will be, and where it may live.
 *
 * Pure, like probe's decision: the worker that runs ffmpeg is in the API
 * package beside the transcode manager, and everything it decides with is
 * here, tested without a file or a process.
 */
package optimize

import (
	"fmt"
	"path/filepath"
	"strings"

	"lancast/internal/probe"
)

// RateKbps is the preset's video bit rate: "about 8 Mbit/s" (Chris's
// decision), as a cap.
const RateKbps = 8000

// audioKbps is what the pipeline gives stereo AAC, for the size estimate.
const audioKbps = 192

/*
 * Profile is a universal device: H.264 and AAC in MP4, at most 1080p and
 * 8 Mbit/s, stereo. The copy is exactly the decision the existing rules make
 * for it — no second set of conversion rules exists to disagree with the
 * first. Almost every browser, phone and TV plays the result directly.
 */
var Profile = probe.Profile{
	Name: "optimized",
	// "mov" because that is what the parser calls an MP4: it keeps the first
	// of ffprobe's "mov,mp4,m4a,..." (probe.primaryFormat). With "mp4" alone
	// a finished copy was judged to need remuxing, for ever.
	Containers:       []string{"mp4", "mov"},
	VideoCodecs:      []string{"h264"},
	AudioCodecs:      []string{"aac"},
	MaxHeight:        1080,
	MaxVideoBitRate:  RateKbps * 1000,
	MaxAudioChannels: 2,
}

// FileName is the copy's name in the optimized folder.
func FileName(itemID int64) string { return fmt.Sprintf("%d.mp4", itemID) }

// EstimateBytes is how big a copy of this length will be at the preset's
// rate: what the limit is checked against before a conversion starts. An
// unknown length estimates nothing, and the copy is measured when it is done.
func EstimateBytes(durationMS int64) int64 {
	if durationMS <= 0 {
		return 0
	}
	return durationMS * (RateKbps + audioKbps) / 8
}

/*
 * Prefer decides whether a device should play the copy instead of the
 * original (ADR 0087 §6).
 *
 * Only when it helps: the device's own decision for the original is to
 * convert, and the copy is something the same rules say it can play
 * directly. A device that plays the original directly keeps the original —
 * it is the better file — and a copy the device would also have to convert
 * is no improvement.
 *
 * A particular audio track asked for keeps the original too: the copy holds
 * one audio track, and swapping in a copy would quietly change what was
 * chosen. The caller passes audioIndex as the request carried it.
 */
func Prefer(original probe.Decision, copy *probe.Result, client probe.Profile, audioIndex int) (probe.Decision, bool) {
	if copy == nil || audioIndex >= 0 || original.Method == probe.DirectPlay {
		return original, false
	}
	d := probe.Decide(copy, client)
	if d.Method != probe.DirectPlay {
		return original, false
	}
	return d, true
}

/*
 * Inside reports whether dir is a library location or inside one.
 *
 * The optimized folder must not be: a scan walks every location, and a copy
 * found there would be taken for a new film (ADR 0087 §4). Compared as
 * cleaned absolute paths, case-insensitively on Windows' terms, so a folder
 * typed with different capitals or a trailing slash is still caught.
 */
func Inside(dir string, roots []string) bool {
	d, err := filepath.Abs(dir)
	if err != nil {
		return true // a folder that cannot be resolved is not trusted
	}
	d = strings.ToLower(filepath.Clean(d))
	for _, root := range roots {
		r, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		r = strings.ToLower(filepath.Clean(r))
		if d == r || strings.HasPrefix(d, r+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
