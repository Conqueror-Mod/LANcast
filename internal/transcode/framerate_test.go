package transcode

import (
	"testing"

	"lancast/internal/probe"
	"lancast/internal/store"
)

/*
 * The frame rate has to survive the whole road from the database to the
 * command line, not just the last step of it (R09-3).
 *
 * The level tests hand SourceFrameRate in directly, so they passed while every
 * real decision carried a rate of zero: ResultWithStreams rebuilt the video
 * stream from the track list, which has no rate, and ignored the item's
 * column. A 1080p60 HEVC film was then promised level 4.1, which NVENC refuses
 * ("InitializeEncoder failed: invalid param (8): Invalid Level") before
 * encoding a frame. This test starts where the API starts.
 */
func TestStoredFrameRateReachesTheEncoder(t *testing.T) {
	cases := []struct {
		name      string
		w, h      int
		fps       float64
		wantLevel string
		wantGOP   string
	}{
		{"1080p59.94", 1920, 1080, 60000.0 / 1001, "4.2", "120"},
		{"1080p50", 1920, 1080, 50, "4.2", "100"},
		{"2160p60", 3840, 2160, 60, "5.2", "120"},
		// The ordinary case keeps the level it always had.
		{"1080p23.976", 1920, 1080, 24000.0 / 1001, "4.1", "48"},
		// A Matroska time base probed as a rate. Believed, it set a keyframe
		// every 2,000 frames and level 6.1; it is treated as unknown instead,
		// which is what every decision did before R09-3.
		{"1080p probed at 1000fps", 1920, 1080, 1000, "4.1", "48"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			probed := int64(1)
			dur := int64(7_200_000)
			container, vcodec, acodec := "mkv", "hevc", "aac"
			w, h, ch, fps := c.w, c.h, 2, c.fps
			it := &store.Item{
				ProbedAt: &probed, DurationMS: &dur, Container: &container,
				VideoCodec: &vcodec, Width: &w, Height: &h, FrameRate: &fps,
				AudioCodec: &acodec, AudioChannels: &ch,
			}
			// The track list as the store returns it: no rate on any stream.
			streams := []store.MediaStream{
				{Index: 0, Kind: "video", Codec: "hevc", Width: w, Height: h, Default: true},
				{Index: 1, Kind: "audio", Codec: "aac", Channels: 2, Default: true},
			}

			d := probe.Decide(probe.ResultWithStreams(it, streams), probe.BrowserProfile())
			if d.VideoAction != "encode" {
				t.Fatalf("decision = %+v, want a video encode for HEVC in a browser", d)
			}
			a := Args(Options{
				Input: "in.mkv", Output: HLS, AudioIndex: -1,
				Encoder:  Encoder{Name: "h264_nvenc", qualityFlag: "-cq"},
				Decision: d,
			})
			if got := argValue(a, "-level"); got != c.wantLevel {
				t.Errorf("-level = %q, want %s (SourceFrameRate %v)", got, c.wantLevel, d.SourceFrameRate)
			}
			if got := argValue(a, "-g"); got != c.wantGOP {
				t.Errorf("-g = %q, want %s", got, c.wantGOP)
			}
		})
	}
}
