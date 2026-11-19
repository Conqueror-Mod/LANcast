package transcode

import (
	"fmt"
	"math"
	"strings"

	"lancast/internal/probe"
)

/*
 * A copied film's playlist, listed whole from the source's keyframes (ADR 0125,
 * amended).
 *
 * # Why
 *
 * A copied video track keeps the source's keyframes, so where ffmpeg will cut
 * each segment was unknown until it had cut it, and the playlist waited for
 * the remux to finish (WaitForEndlist). A Roku abandons a playlist request
 * after about thirty seconds. Measured on the HEVC-in-TS test: the TV asked,
 * gave up at 29.5s, 30.4s and 30.5s and asked again, and one film took four
 * tries; another gave up for good and showed a black screen.
 *
 * A Matroska file's index lists every keyframe (internal/matroska, matched
 * against ffprobe's packet scan on six real films). With them, where ffmpeg
 * cuts is arithmetic, so the playlist goes out at once and each segment is
 * waited for when it is asked for, as an encode's already are.
 *
 * # Where ffmpeg cuts
 *
 * The HLS muxer cuts at the first keyframe at or past each boundary, and the
 * boundaries are cumulative from the first packet: 6s, 12s, 18s, not six
 * seconds after the last cut (see SegmentLength). A keyframe gap that spans
 * two boundaries makes one long segment, and the next keyframe then cuts at
 * once because it is past the next boundary too. KeyframeCuts walks the
 * keyframes with exactly that rule.
 *
 * # Where a resume starts
 *
 * The boundaries count from the first packet of *either* stream, and on a
 * resume the audio is a second input with its own seek (ADR 0072). ffmpeg
 * seeks each input to the latest keyframe at or before the seek, less about
 * 0.13s when the video has B-frames (its "DTS heuristic", 3/23 of a second).
 * So a resume asked for at the keyframe itself landed the audio on the
 * keyframe *before* it, and every boundary moved: measured on two films, the
 * first segment was 4.796s and 5.922s against a predicted 7.966s.
 *
 * CopyStart moves the seek a fifth of a second past the keyframe. Less the
 * heuristic or not, both inputs then land on that keyframe, the first packet
 * of each is there, and the boundaries count from it. Checked against ffmpeg
 * itself on real films (keyframeplaylist_real_test.go).
 */

// copySeekPast is how far past the keyframe a copy's seek is placed: more
// than ffmpeg's 3/23s heuristic, so a seek less it is still on the keyframe.
const copySeekPast = 0.2

/*
 * CopyStart is where a copy asked to start at startAt really starts: the
 * keyframe it will begin on, and the seek that lands both inputs there.
 *
 * ok is false when there is no keyframe to start on, or the next one is too
 * close for the seek to sit between them.
 */
func CopyStart(keyframes []float64, startAt float64) (keyframe, seek float64, ok bool) {
	i := -1
	for j, k := range keyframes {
		if k <= startAt+1e-9 {
			i = j
		} else {
			break
		}
	}
	if i < 0 {
		return 0, 0, false
	}
	if i+1 < len(keyframes) && keyframes[i+1]-keyframes[i] <= copySeekPast+0.05 {
		return 0, 0, false
	}
	return keyframes[i], keyframes[i] + copySeekPast, true
}

/*
 * KeyframeCuts returns where each segment of a copy starting at startAt will
 * begin, on the source's clock: the first is the keyframe the copy starts on.
 *
 * nil when there is no keyframe to start on.
 */
func KeyframeCuts(keyframes []float64, startAt, segment float64) []float64 {
	if len(keyframes) == 0 || segment <= 0 {
		return nil
	}
	first := 0
	if startAt > 0 {
		first = -1
		for i, k := range keyframes {
			if k <= startAt+1e-9 {
				first = i
			} else {
				break
			}
		}
		if first < 0 {
			first = 0
		}
	}
	start := keyframes[first]
	cuts := []float64{start}
	for _, k := range keyframes[first+1:] {
		// The next boundary is the segment count so far times the length.
		if k-start >= segment*float64(len(cuts))-1e-9 {
			cuts = append(cuts, k)
		}
	}
	return cuts
}

/*
 * KeyframePlaylist lists the segments that begin at cuts, the last running to
 * end, marked finished.
 *
 * ok is false when the cuts do not describe media that ends after they begin,
 * or there are more of them than a believable file has: the caller then lists
 * the playlist as it did before.
 */
func KeyframePlaylist(cuts []float64, end float64, prefix, segments string) (string, bool) {
	n := len(cuts)
	if n == 0 || n > probe.MaxDurationSeconds || !(end > cuts[n-1]) || end > probe.MaxDurationSeconds {
		return "", false
	}
	durations := make([]float64, n)
	longest := 0.0
	for i := range cuts {
		next := end
		if i+1 < n {
			next = cuts[i+1]
		}
		d := next - cuts[i]
		if !(d > 0) {
			return "", false
		}
		durations[i] = d
		longest = math.Max(longest, d)
	}

	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:7\n")
	fmt.Fprintf(&b, "#EXT-X-TARGETDURATION:%d\n", int(math.Ceil(longest)))
	b.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	b.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")
	b.WriteString("#EXT-X-INDEPENDENT-SEGMENTS\n")
	if segments != probe.SegmentsMPEGTS {
		fmt.Fprintf(&b, "#EXT-X-MAP:URI=\"%sinit.mp4\"\n", prefix)
	}
	ext := SegmentExt(segments)
	for i, d := range durations {
		fmt.Fprintf(&b, "#EXTINF:%.6f,\n%sseg%05d%s\n", d, prefix, i, ext)
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String(), true
}

// ListedCount is how many segments a playlist names.
func ListedCount(playlist string) int {
	return strings.Count(playlist, "#EXTINF:")
}
