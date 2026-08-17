package transcode

import (
	"testing"

	"lancast/internal/probe"
)

/*
 * What a live channel's decision should be, given what the probe found.
 *
 * The rule these encode: **copy video whenever possible, and only copy audio
 * when it is already AAC.** A video encode costs a whole core per viewer; an
 * audio encode costs a few percent. So video gets the benefit of the doubt and
 * audio does not — because the failure from guessing wrong about audio is a
 * channel with a picture and no sound, which looks like it nearly worked.
 */

// Tested against BrowserProfile rather than a zero Profile: an empty profile
// declares support for nothing, so every codec looks incompatible and the test
// would assert the opposite of what a real client asks for.
func result(videoCodec, audioCodec string) *probe.Result {
	r := &probe.Result{}
	if videoCodec != "" {
		r.Streams = append(r.Streams, probe.Stream{Kind: probe.KindVideo, Codec: videoCodec})
	}
	if audioCodec != "" {
		r.Streams = append(r.Streams, probe.Stream{Kind: probe.KindAudio, Codec: audioCodec})
	}
	return r
}

func TestLiveDecisionCopiesH264AndAAC(t *testing.T) {
	d := LiveDecision(result("h264", "aac"), probe.BrowserProfile(), nil)
	if d.VideoAction != "copy" {
		t.Errorf("video = %q, want copy — re-encoding H.264 costs a core for nothing", d.VideoAction)
	}
	if d.AudioAction != "copy" {
		t.Errorf("audio = %q, want copy for AAC", d.AudioAction)
	}
}

/*
 * AC-3 is the case that would otherwise produce silence.
 *
 * No browser decodes it, and the ADTS filter is AAC-specific so copying it
 * fails the mux outright. Either way the viewer gets a channel that does not
 * work; re-encoding costs a few percent and always works.
 */
func TestLiveDecisionReEncodesAudioItCannotCopy(t *testing.T) {
	for _, codec := range []string{"ac3", "eac3", "mp2", "dts"} {
		d := LiveDecision(result("h264", codec), probe.BrowserProfile(), nil)
		if d.AudioAction != "encode" {
			t.Errorf("%s: audio = %q, want encode", codec, d.AudioAction)
		}
		// The picture is still copied: only the audio was the problem.
		if d.VideoAction != "copy" {
			t.Errorf("%s: video = %q, want copy", codec, d.VideoAction)
		}
	}
}

// An unprobed channel copies video and encodes audio, for the same asymmetry:
// the expensive guess is given the benefit of the doubt, the cheap one is not.
func TestLiveDecisionWithoutAProbe(t *testing.T) {
	d := LiveDecision(nil, probe.BrowserProfile(), nil)
	if d.VideoAction != "copy" {
		t.Errorf("video = %q, want copy", d.VideoAction)
	}
	if d.AudioAction != "encode" {
		t.Errorf("audio = %q, want encode when nothing is known about it", d.AudioAction)
	}
	if d.Reason == "" {
		t.Error("a decision with no reason cannot be debugged from a log")
	}
}

// A channel is never "direct play": the whole point of this path is that the
// browser could not take the source as it stands.
func TestLiveDecisionIsNeverDirectPlay(t *testing.T) {
	d := LiveDecision(result("h264", "aac"), probe.BrowserProfile(), nil)
	if d.Method == probe.DirectPlay {
		t.Errorf("method = %q; the container is being rewritten regardless", d.Method)
	}
}

// IsHLS is what decides whether the live-edge option is safe to pass, so its
// nil case matters: an unprobed channel must read as "not HLS" rather than
// panicking or guessing, because the wrong guess refuses the input outright.
func TestIsHLS(t *testing.T) {
	if IsHLS(nil) {
		t.Error("a failed probe read as HLS; an unprobed channel must keep the safe path")
	}
	if !IsHLS(&probe.Result{Container: "hls"}) {
		t.Error("an HLS playlist was not recognised")
	}
	if !IsHLS(&probe.Result{Container: "HLS"}) {
		t.Error("container matching must not be case-sensitive")
	}
	if IsHLS(&probe.Result{Container: "mpegts"}) {
		t.Error("a transport stream read as HLS")
	}
}
