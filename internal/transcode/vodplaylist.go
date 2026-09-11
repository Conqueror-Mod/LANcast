package transcode

import (
	"fmt"
	"math"
	"strings"
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

// SegmentLength estimates how long each segment of an encode will run.
//
// ffmpeg cuts at the first keyframe at or past each SegmentSeconds boundary,
// and the encode places a keyframe every gopFrames frames, so a segment is the
// smallest whole number of GOPs that reaches SegmentSeconds. At 23.976fps that
// is three 48-frame GOPs, 144 frames, 6.006s — which is what ffmpeg wrote for
// all 49 segments of the film this was measured on.
func SegmentLength(fps float64) float64 {
	if fps <= 0 || math.IsNaN(fps) || math.IsInf(fps, 0) {
		return SegmentSeconds
	}
	g := float64(gopFrames(fps))
	gops := math.Ceil(SegmentSeconds*fps/g - 1e-9)
	if gops < 1 {
		gops = 1
	}
	return gops * g / fps
}

// CompletePlaylist lists every segment of a media length, marked finished.
//
// A final sliver shorter than half a segment is folded into the segment before
// it rather than listed on its own: the estimate is not frame-exact, and a
// listed segment ffmpeg never writes would fail at the very end of the film
// where a slightly long last segment costs nothing.
func CompletePlaylist(mediaSeconds, segment float64, prefix string) string {
	if segment <= 0 {
		segment = SegmentSeconds
	}
	n := int(math.Round(mediaSeconds / segment))
	if n < 1 {
		n = 1
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
	fmt.Fprintf(&b, "#EXT-X-MAP:URI=\"%sinit.mp4\"\n", prefix)
	for i := 0; i < n; i++ {
		d := segment
		if i == n-1 {
			d = last
		}
		fmt.Fprintf(&b, "#EXTINF:%.6f,\n%sseg%05d.m4s\n", d, prefix, i)
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
