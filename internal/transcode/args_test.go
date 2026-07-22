package transcode

import (
	"strings"
	"testing"

	"lancast/internal/probe"
)

// argIndex returns the position of flag, or -1.
func argIndex(args []string, flag string) int {
	for i, a := range args {
		if a == flag {
			return i
		}
	}
	return -1
}

// argValue returns the value following flag.
func argValue(args []string, flag string) string {
	i := argIndex(args, flag)
	if i < 0 || i+1 >= len(args) {
		return ""
	}
	return args[i+1]
}

func hasSequence(args []string, a, b string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == a && args[i+1] == b {
			return true
		}
	}
	return false
}

// hasArgPair looks for a value after any occurrence of flag, since a flag like
// -map legitimately appears more than once.
func hasArgPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func remuxDecision() probe.Decision {
	return probe.Decision{Method: probe.Remux, VideoAction: "copy", AudioAction: "copy"}
}

func audioOnlyDecision() probe.Decision {
	return probe.Decision{Method: probe.Transcode, VideoAction: "copy", AudioAction: "encode"}
}

func fullDecision() probe.Decision {
	return probe.Decision{Method: probe.Transcode, VideoAction: "encode", AudioAction: "encode"}
}

// Seeking before -i seeks by keyframe without decoding up to the offset.
// After -i it would decode and discard — minutes of waste on a long film.
func TestSeekIsBeforeInput(t *testing.T) {
	args := Args(Options{Input: "in.mkv", Output: Progressive, Decision: remuxDecision(), StartAt: 600})
	ss := argIndex(args, "-ss")
	in := argIndex(args, "-i")
	if ss < 0 {
		t.Fatal("no -ss for a seeked transcode")
	}
	if ss > in {
		t.Errorf("-ss at %d is after -i at %d; it must precede input", ss, in)
	}
}

func TestNoSeekWhenStartIsZero(t *testing.T) {
	args := Args(Options{Input: "in.mkv", Output: Progressive, Decision: remuxDecision()})
	if argIndex(args, "-ss") >= 0 {
		t.Error("-ss present with no start offset")
	}
}

// The audio-only case is the one that matters for a real library: video is
// copied, only audio re-encoded.
func TestAudioOnlyCopiesVideo(t *testing.T) {
	args := Args(Options{Input: "in.mkv", Output: Progressive, Decision: audioOnlyDecision()})
	if !hasSequence(args, "-c:v", "copy") {
		t.Error("video is not copied in an audio-only transcode")
	}
	if !hasSequence(args, "-c:a", "aac") {
		t.Error("audio is not re-encoded to aac")
	}
	if argIndex(args, "libx264") >= 0 {
		t.Error("libx264 present when the video should be copied — that is a needless full encode")
	}
}

func TestRemuxCopiesBoth(t *testing.T) {
	args := Args(Options{Input: "in.mkv", Output: Progressive, Decision: remuxDecision()})
	if !hasSequence(args, "-c:v", "copy") || !hasSequence(args, "-c:a", "copy") {
		t.Errorf("remux must copy both streams: %v", args)
	}
}

func TestFullTranscodeEncodesBoth(t *testing.T) {
	args := Args(Options{Input: "in.mkv", Output: Progressive, Decision: fullDecision()})
	if !hasSequence(args, "-c:v", "libx264") {
		t.Error("video is not encoded with libx264")
	}
	if !hasSequence(args, "-c:a", "aac") {
		t.Error("audio is not encoded with aac")
	}
	// yuv420p, or the encode produces profiles browsers refuse — the same trap
	// the decision engine catches on input.
	if !hasSequence(args, "-pix_fmt", "yuv420p") {
		t.Error("no yuv420p; a 10-bit source would re-encode to an unplayable profile")
	}
}

// Explicit stream mapping. ffmpeg's default picks one stream per type by its
// own rules, which selects the wrong audio on files with several tracks.
func TestExplicitStreamMapping(t *testing.T) {
	// -1 is what the API passes when the client names no specific track.
	args := Args(Options{Input: "in.mkv", Output: Progressive, Decision: fullDecision(), AudioIndex: -1})
	if !hasArgPair(args, "-map", "0:v:0") {
		t.Error("video stream is not mapped explicitly")
	}
	if !hasArgPair(args, "-map", "0:a:0?") {
		t.Error("default audio mapping is missing")
	}
}

func TestSpecificAudioTrackMapped(t *testing.T) {
	args := Args(Options{Input: "in.mkv", Output: Progressive, Decision: audioOnlyDecision(), AudioIndex: 3})
	if !hasSequence(args, "-map", "0:3") {
		t.Errorf("requested audio track 3 not mapped: %v", args)
	}
}

func TestSubtitlesDropped(t *testing.T) {
	args := Args(Options{Input: "in.mkv", Output: Progressive, Decision: fullDecision()})
	if argIndex(args, "-sn") < 0 {
		t.Error("-sn missing; subtitles would force a video re-encode via burn-in")
	}
}

func TestProgressiveOutputsFragmentedMP4ToPipe(t *testing.T) {
	args := Args(Options{Input: "in.mkv", Output: Progressive, Decision: remuxDecision()})
	if args[len(args)-1] != "pipe:1" {
		t.Errorf("progressive output does not end at pipe:1: %v", args[len(args)-3:])
	}
	movflags := argValue(args, "-movflags")
	for _, need := range []string{"frag_keyframe", "empty_moov"} {
		if !strings.Contains(movflags, need) {
			t.Errorf("movflags %q missing %q — the stream would not be seekable-as-produced", movflags, need)
		}
	}
}

func TestHLSOutput(t *testing.T) {
	args := Args(Options{Input: "in.mkv", Output: HLS, Decision: fullDecision(), OutputDir: "/tmp/x"})
	if !hasSequence(args, "-f", "hls") {
		t.Error("not an HLS output")
	}
	if !hasSequence(args, "-hls_segment_type", "fmp4") {
		t.Error("segments are not fMP4")
	}
	if argValue(args, "-hls_segment_filename") != "/tmp/x/seg%05d.m4s" {
		t.Errorf("segment path = %q", argValue(args, "-hls_segment_filename"))
	}
	if args[len(args)-1] != "/tmp/x/index.m3u8" {
		t.Errorf("playlist path = %q", args[len(args)-1])
	}
}

// HLS video re-encode must force keyframes on segment boundaries, or a segment
// cannot begin with an IDR frame and seeking breaks.
func TestHLSForcesKeyframesWhenEncoding(t *testing.T) {
	args := Args(Options{Input: "in.mkv", Output: HLS, Decision: fullDecision(), OutputDir: "/tmp/x"})
	if argIndex(args, "-force_key_frames") < 0 {
		t.Error("no forced keyframes; HLS segments would not be independently seekable")
	}
}

func TestNeedsTranscode(t *testing.T) {
	if NeedsTranscode(probe.Decision{Method: probe.DirectPlay}) {
		t.Error("direct play should not need transcoding")
	}
	if !NeedsTranscode(probe.Decision{Method: probe.Remux}) {
		t.Error("remux needs ffmpeg")
	}
	if !NeedsTranscode(probe.Decision{Method: probe.Transcode}) {
		t.Error("transcode needs ffmpeg")
	}
}

func TestDefaultsApplied(t *testing.T) {
	args := Args(Options{Input: "in.mkv", Output: Progressive, Decision: fullDecision()})
	if argValue(args, "-crf") != "23" {
		t.Errorf("CRF = %q, want the default 23", argValue(args, "-crf"))
	}
	if argValue(args, "-preset") != "veryfast" {
		t.Errorf("preset = %q, want veryfast for real-time transcoding", argValue(args, "-preset"))
	}
	if argValue(args, "-ac") != "2" {
		t.Errorf("channels = %q, want a stereo downmix", argValue(args, "-ac"))
	}
}
