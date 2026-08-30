package transcode

import (
	"testing"

	"lancast/internal/probe"
)

/*
 * Audio that survives a seek.
 *
 * `The Wedding Singer (1998).avi` — MPEG-4 ASP video, MP3 audio — resumed at
 * 4343 seconds with this package's own command line, video re-encoded and
 * audio copied:
 *
 *	video  start_time =  0.000000
 *	audio  start_time = -1.997143
 *
 * Two seconds of sound ahead of the first frame, reported as *"around a 1.8s
 * delay between audio and video, with video being behind"*. The same command
 * against a well-formed MP4 gave 0.000 and 0.000, so it is the container.
 *
 * `-avoid_negative_ts make_zero` was tried first and only moved the gap —
 * video 1.997, audio 0.000 — because AVI has no per-frame timestamps and the
 * extra audio is real data rather than a bad timestamp. Re-encoding re-times
 * the track from the seek point: measured 0.000 and 0.000.
 *
 * One named case per condition, asserting the decision, in the manner of
 * decide_test.go.
 */

func audioCodec(a []string) string {
	for i, v := range a {
		if v == "-c:a" && i+1 < len(a) {
			return a[i+1]
		}
	}
	return ""
}

func seekOpts(container string, startAt float64) Options {
	return Options{
		Input: "in." + container, Output: Progressive, AudioIndex: -1,
		StartAt:       startAt,
		AudioBitrate:  192,
		AudioChannels: 2,
		Decision: probe.Decision{
			Method: probe.Transcode, VideoAction: "encode", AudioAction: "copy",
			TargetFormat: "mp4", SourceContainer: container,
			SourceWidth: 688, SourceHeight: 384, SourceFrameRate: 23.976,
		},
	}
}

// The reported fault. Seeking into an AVI must not copy the audio.
func TestResumingAnAVIReEncodesItsAudio(t *testing.T) {
	if got := audioCodec(Args(seekOpts("avi", 4343))); got != "aac" {
		t.Errorf("audio codec on a sought AVI = %q, want aac — a copy lands ~2s out", got)
	}
}

/*
 * From the beginning the copy is kept, and this is the half that stops the fix
 * costing something for nothing.
 *
 * There is nothing to be out of step with at offset zero: the measured failure
 * is the gap between where a seek puts the picture and where it puts the sound.
 * A copy is free and lossless, and spending an encode on every AVI would be
 * paying for a problem that is not there.
 */
func TestPlayingAnAVIFromTheStartStillCopies(t *testing.T) {
	if got := audioCodec(Args(seekOpts("avi", 0))); got != "copy" {
		t.Errorf("audio codec on an unsought AVI = %q, want copy", got)
	}
}

// A container that seeks properly keeps its copy, sought or not. Measured:
// the same command against a well-formed MP4 produced 0.000 and 0.000.
func TestSeekingAContainerThatCanBeSoughtStillCopies(t *testing.T) {
	for _, c := range []string{"mp4", "matroska", "mov"} {
		if got := audioCodec(Args(seekOpts(c, 1800))); got != "copy" {
			t.Errorf("audio codec on a sought %s = %q, want copy", c, got)
		}
	}
}

/*
 * A decision that already re-encodes the audio is untouched.
 *
 * Nothing here should be able to turn an encode back into a copy — this rule
 * only ever removes a copy, which is the safe direction for a rule driven by a
 * list of containers somebody has to maintain.
 */
func TestAnAudioEncodeIsNotDisturbed(t *testing.T) {
	o := seekOpts("mp4", 1800)
	o.Decision.AudioAction = "encode"
	if got := audioCodec(Args(o)); got != "aac" {
		t.Errorf("audio codec = %q, want aac", got)
	}
}

/*
 * Live never takes this path.
 *
 * A channel has no `-ss` at all — there is nothing to seek in — so applying a
 * seek repair to it would re-encode audio on every channel for a seek that
 * never happens. `StartAt` can still be non-zero on a live decision, which is
 * why this is asserted rather than assumed from the flag.
 */
func TestALiveSourceKeepsItsCopy(t *testing.T) {
	o := seekOpts("avi", 4343)
	o.Live = true
	if got := audioCodec(Args(o)); got != "copy" {
		t.Errorf("audio codec on a live source = %q, want copy", got)
	}
}

// And the vocabulary itself, so a container is added on evidence rather than
// by editing a switch and hoping.
func TestOnlyWhatWasMeasuredIsListed(t *testing.T) {
	if !probe.SeekLosesAudioSync("avi") {
		t.Error("avi is the container this was measured on and must be listed")
	}
	if !probe.SeekLosesAudioSync("AVI") {
		t.Error("the check must not depend on how the container is spelt")
	}
	for _, c := range []string{"mp4", "matroska", "mov", "mpegts", "webm", ""} {
		if probe.SeekLosesAudioSync(c) {
			t.Errorf("%q was listed without having been measured — every entry "+
				"costs an audio encode on every seek", c)
		}
	}
}
