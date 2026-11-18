package transcode

import (
	"fmt"
	"math"
	"strings"

	"lancast/internal/probe"
)

/*
 * A converted film's playlist, written by the server rather than by ffmpeg.
 *
 * # Why the server writes it
 *
 * ffmpeg writes an `EVENT` playlist that grows as segments are produced, and
 * WebView2 — the engine the desktop client ships — cannot play one. Reproduced
 * outside the app with the server's exact command (cmd/wv2harness,
 * cmd/segserve): the element fetches the playlist and `seg00000`, **reloads the
 * playlist 88ms later**, finds it has not grown, and fails with code 4
 * `DEMUXER_ERROR_COULD_NOT_PARSE`. It treats a growing playlist as live. That
 * was the fallback seen on 21 of 21 file playbacks in the log, and on every one
 * the bytes themselves were fine.
 *
 * The same engine plays a finished playlist — over HTTP, and over HTTPS with the
 * pinned certificate. So the server lists the whole film up front and marks it
 * finished, and segments not yet produced are waited for when they are asked
 * for. Measured on a 4K HDR film started 22 minutes in: played from the first
 * segment, one playlist fetch, segments requested one at a time about six
 * seconds ahead of the picture, as the encoder produced them.
 *
 * # Why the durations may be estimates
 *
 * The listed lengths only have to be close. With every segment listed at 5.5s
 * against a real 6.006s, the same engine played exactly as well: it places a
 * fragment by its own timestamps, not by the playlist's arithmetic. And the
 * client never seeks inside one of these playlists — a seek while converting
 * re-requests with a new `t=`, which is a new session and a new playlist.
 *
 * # Why only when video is encoded
 *
 * A segment can only start on a keyframe. When video is encoded the server
 * decides where those are (a fixed GOP and forced keyframes), so it knows
 * roughly where every cut will fall before the encode starts. A copied video
 * track brings the source's own keyframes, at intervals nothing here has read,
 * so a copy keeps ffmpeg's growing playlist.
 */

/*
 * SegmentLength estimates how long each segment of an encode will run.
 *
 * ffmpeg's segmenter cuts at the first keyframe at or past each boundary, and
 * the boundaries are **cumulative** — 6s, 12s, 18s from the start — not six
 * seconds after the last cut. The encode places a keyframe every gopFrames
 * frames, so the cuts fall on whole GOPs, and over a film they average exactly
 * SegmentSeconds. The length of almost every segment is therefore the whole
 * number of GOPs *nearest* SegmentSeconds:
 *
 *   - 23.976fps: 48-frame GOPs of 2.002s, three of them, 6.006s — what ffmpeg
 *     wrote for all 49 segments of the film this was first measured on;
 *   - 66.67fps (a VFR file probed at 29933/449): 133-frame GOPs of 1.995s.
 *     Measured with NVENC, one 7.98s segment and then fifteen of 5.985s.
 *
 * This used to take the *smallest* whole number of GOPs reaching
 * SegmentSeconds, which is only the first segment. Below six seconds by a
 * hair, three GOPs became four: Spider-Man was listed at 7.98s a segment
 * against a real 5.985s, so its playlist named about three quarters of the
 * segments ffmpeg wrote and the film ended early. It went unseen while every
 * decision carried a frame rate of zero (fixed by R09-3), which fell back to
 * SegmentSeconds below.
 *
 * The listed length only has to be close — a player places a fragment by its
 * own timestamps — but the *count* it implies has to be right.
 */
func SegmentLength(fps float64) float64 {
	if fps <= 0 || math.IsNaN(fps) || math.IsInf(fps, 0) {
		return SegmentSeconds
	}
	g := float64(gopFrames(fps))
	gops := math.Round(SegmentSeconds * fps / g)
	if gops < 1 {
		gops = 1
	}
	return gops * g / fps
}

// SegmentExt is the file extension of a segment in this format (see
// probe.Decision.Segments): ".ts" for MPEG-TS, ".m4s" for fragmented MP4.
func SegmentExt(segments string) string {
	if segments == probe.SegmentsMPEGTS {
		return ".ts"
	}
	return ".m4s"
}

// CompletePlaylist lists every segment of a media length, marked finished.
//
// A final sliver shorter than half a segment is folded into the segment before
// it rather than listed on its own: the estimate is not frame-exact, and a
// listed segment ffmpeg never writes would fail at the very end of the film
// where a slightly long last segment costs nothing.
//
// The length is capped at probe.MaxDurationSeconds (R09-8): the playlist is
// built in memory on every request, one entry per segment, so a length the
// file merely claimed must not decide how much memory that is. The parser
// already refuses such a length; this holds whatever the caller passes.
func CompletePlaylist(mediaSeconds, segment float64, prefix, segments string) string {
	if segment <= 0 {
		segment = SegmentSeconds
	}
	if !(mediaSeconds <= probe.MaxDurationSeconds) {
		mediaSeconds = probe.MaxDurationSeconds // NaN and Inf too
	}
	n := int(math.Round(mediaSeconds / segment))
	if n < 1 {
		n = 1
	}
	// And the count itself, whatever the segment: never more entries than
	// one-second segments over the longest believable file.
	if n > probe.MaxDurationSeconds {
		n = probe.MaxDurationSeconds
		mediaSeconds = float64(n) * segment
	}

	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:7\n")
	last := mediaSeconds - float64(n-1)*segment
	target := math.Max(segment, last)
	fmt.Fprintf(&b, "#EXT-X-TARGETDURATION:%d\n", int(math.Ceil(target)))
	b.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	b.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")
	b.WriteString("#EXT-X-INDEPENDENT-SEGMENTS\n")
	// MPEG-TS has no init segment: every segment carries its own tables.
	if segments != probe.SegmentsMPEGTS {
		fmt.Fprintf(&b, "#EXT-X-MAP:URI=\"%sinit.mp4\"\n", prefix)
	}
	ext := SegmentExt(segments)
	for i := 0; i < n; i++ {
		d := segment
		if i == n-1 {
			d = last
		}
		fmt.Fprintf(&b, "#EXTINF:%.6f,\n%sseg%05d%s\n", d, prefix, i, ext)
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

/*
 * completeFor decides whether a session's playlist is listed whole, and if so
 * how much media and how long each segment.
 *
 * Only a file whose video is encoded: the encode places the keyframes, so the
 * cuts are known in advance. A copied video track keeps the source's keyframes
 * and ffmpeg's growing playlist. A channel never ends, and audio alone has no
 * GOP to reason from. An unknown length cannot be listed.
 */
func completeFor(o Options) (mediaSeconds, segment float64, ok bool) {
	d := o.Decision
	if o.Live || d.AudioOnly || d.VideoAction != "encode" || o.Duration <= 0 {
		return 0, 0, false
	}
	media := o.Duration - o.StartAt
	if media <= 0 {
		return 0, 0, false
	}
	return media, SegmentLength(d.SourceFrameRate), true
}

/*
 * Listed reports whether ffmpeg's own playlist names a segment.
 *
 * It is how a segment is known to be finished. ffmpeg writes segments in place,
 * so a file that exists — even one with bytes in it — may still be mid-write,
 * and a player reading from a complete playlist asks for segments *ahead* of the
 * encode. Checking for a non-empty file served a 0-byte init segment in exactly
 * that experiment, and the element failed with APPEND_FAILED. ffmpeg adds a
 * segment to its playlist only once the segment is closed.
 */
func Listed(playlist, name string) bool {
	for _, line := range strings.Split(playlist, "\n") {
		if strings.TrimSpace(line) == name {
			return true
		}
	}
	return false
}
