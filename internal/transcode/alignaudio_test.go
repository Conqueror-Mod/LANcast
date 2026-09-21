package transcode

import (
	"strings"
	"testing"

	"lancast/internal/probe"
)

/*
 * A copied resume starts both streams at the same point (ADR 0072).
 *
 * A copied video begins at a keyframe, because nothing is decoded and there is
 * nothing else to begin at; the re-encoded audio begins exactly where it was
 * asked. The picture then runs behind the sound by the distance between them,
 * measured at 0.33s on one resume of Jay and Silent Bob Reboot and 3.1s on
 * another of the same film.
 *
 * The seek cannot close it — ffmpeg will not take a keyframe until the target
 * is ~200ms past it, so asking for the keyframe drops to the previous one and
 * the gap becomes five seconds. So the audio gets its own input, seeked to the
 * keyframe. These assert the shape of that command, which is the part a test
 * can hold: whether it aligns was settled by measurement, in the ADR.
 */

func copyVideoEncodeAudio() probe.Decision {
	return probe.Decision{
		Method: probe.Transcode, VideoAction: "copy", AudioAction: "encode",
		TargetFormat: "mp4",
	}
}

// args returns the built command for one set of options.
func argsFor(o Options) []string {
	return Args(o)
}

func countArg(a []string, want string) int {
	n := 0
	for _, s := range a {
		if s == want {
			n++
		}
	}
	return n
}

// indexAfter returns the value following the nth occurrence of flag.
func valuesAfter(a []string, flag string) []string {
	var out []string
	for i, s := range a {
		if s == flag && i+1 < len(a) {
			out = append(out, a[i+1])
		}
	}
	return out
}

func TestAlignedResumeOpensTheFileTwice(t *testing.T) {
	a := argsFor(Options{
		Input: "film.mkv", Decision: copyVideoEncodeAudio(),
		StartAt: 400, AudioStartAt: 399.649, AudioIndex: -1,
	})

	if got := countArg(a, "-i"); got != 2 {
		t.Fatalf("%d inputs, want 2:\n%s", got, strings.Join(a, " "))
	}
	seeks := valuesAfter(a, "-ss")
	if len(seeks) != 2 || seeks[0] != "400.000" || seeks[1] != "399.649" {
		t.Errorf("seeks = %v, want the resume then the keyframe:\n%s",
			seeks, strings.Join(a, " "))
	}
}

// The audio must come from the second input, or the alignment does nothing
// while paying for a second demuxer.
func TestAlignedResumeTakesAudioFromTheSecondInput(t *testing.T) {
	a := argsFor(Options{
		Input: "film.mkv", Decision: copyVideoEncodeAudio(),
		StartAt: 400, AudioStartAt: 399.649, AudioIndex: -1,
	})

	maps := valuesAfter(a, "-map")
	var video, audio string
	for _, m := range maps {
		if strings.Contains(m, ":v:") {
			video = m
		}
		if strings.Contains(m, ":a:") {
			audio = m
		}
	}
	if video != "0:v:0" {
		t.Errorf("video mapped from %q, want the first input", video)
	}
	if audio != "1:a:0?" {
		t.Errorf("audio mapped from %q, want the second input", audio)
	}
}

// A chosen audio track is an index into the same file, so it carries across to
// the second input unchanged.
func TestAChosenAudioTrackComesFromTheSecondInputToo(t *testing.T) {
	a := argsFor(Options{
		Input: "film.mkv", Decision: copyVideoEncodeAudio(),
		StartAt: 400, AudioStartAt: 399.649, AudioIndex: 3,
	})

	maps := valuesAfter(a, "-map")
	found := false
	for _, m := range maps {
		if m == "1:3" {
			found = true
		}
		if m == "0:3" {
			t.Error("the chosen track was taken from the first input, which is " +
				"seeked to the resume rather than the keyframe")
		}
	}
	if !found {
		t.Errorf("no map of the chosen track from the second input: %v", maps)
	}
}

/*
 * Everything below is a case with nothing to align, and each one must produce
 * exactly the command it did before. The cost of being wrong here is a second
 * demuxer on every playback in the library.
 */

func TestAReEncodedVideoIsNotAligned(t *testing.T) {
	a := argsFor(Options{
		Input: "film.mkv",
		Decision: probe.Decision{
			Method: probe.Transcode, VideoAction: "encode", AudioAction: "encode",
			TargetFormat: "mp4",
		},
		StartAt: 400, AudioStartAt: 399.649, AudioIndex: -1,
	})

	if got := countArg(a, "-i"); got != 1 {
		t.Errorf("%d inputs, want 1: a decoded video starts where it is asked", got)
	}
}

func TestAResumeAtZeroIsNotAligned(t *testing.T) {
	a := argsFor(Options{
		Input: "film.mkv", Decision: copyVideoEncodeAudio(),
		StartAt: 0, AudioStartAt: 0, AudioIndex: -1,
	})

	if got := countArg(a, "-i"); got != 1 {
		t.Errorf("%d inputs, want 1: there is nothing to be out of step with", got)
	}
}

func TestNoKeyframeMeansNoSecondInput(t *testing.T) {
	a := argsFor(Options{
		Input: "film.mkv", Decision: copyVideoEncodeAudio(),
		StartAt: 400, AudioStartAt: 0, AudioIndex: -1,
	})

	if got := countArg(a, "-i"); got != 1 {
		t.Errorf("%d inputs, want 1: an alignment that cannot be improved is "+
			"not a reason to change the command", got)
	}
}

/*
 * A keyframe after the resume would seek the audio *forward* of the picture,
 * which is the same fault pointed the other way and larger — the next keyframe
 * on the measured file was 4.9 seconds on.
 */
func TestAKeyframeAfterTheResumeIsRefused(t *testing.T) {
	a := argsFor(Options{
		Input: "film.mkv", Decision: copyVideoEncodeAudio(),
		StartAt: 400, AudioStartAt: 404.571, AudioIndex: -1,
	})

	if got := countArg(a, "-i"); got != 1 {
		t.Errorf("%d inputs, want 1: the audio would start after the picture", got)
	}
}

func TestLiveIsNotAligned(t *testing.T) {
	a := argsFor(Options{
		Input: "http://tuner/stream", Decision: copyVideoEncodeAudio(),
		Live: true, StartAt: 400, AudioStartAt: 399.649, AudioIndex: -1,
	})

	if got := countArg(a, "-i"); got != 1 {
		t.Errorf("%d inputs, want 1: live has no resume", got)
	}
}
