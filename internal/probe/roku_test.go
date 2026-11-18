package probe

import (
	"strings"
	"testing"
)

/*
 * The `roku` profile (ADR 0125), one case per rule, asserting the decision and
 * its reason in the manner of decide_test.go.
 *
 * Every number here was measured on a Roku TV in roku-plan.md step 0: what the
 * device said it decodes, and which packaging of HLS it played with sound.
 */

func TestRokuIsANamedProfile(t *testing.T) {
	p := ProfileByName("roku")
	if p.Name != "roku" {
		t.Fatalf("ProfileByName(roku) = %q", p.Name)
	}
	if p.Segments != SegmentsMPEGTS {
		t.Errorf("Segments = %q, want mpegts: a Roku hears no fMP4 audio", p.Segments)
	}
	if p.MaxH264Level != "4.1" {
		t.Errorf("MaxH264Level = %q, want 4.1", p.MaxH264Level)
	}
}

// No other profile changes: only a Roku is sent TS, and only it has a level
// ceiling.
func TestOnlyRokuSendsTSOrCapsTheLevel(t *testing.T) {
	for _, p := range []Profile{BrowserProfile(), SafariProfile(), TVProfile(), NativeProfile()} {
		if p.Segments != "" || p.MaxH264Level != "" {
			t.Errorf("%s: Segments=%q MaxH264Level=%q, want neither", p.Name, p.Segments, p.MaxH264Level)
		}
		d := Decide(result("matroska", video("h264", 1080), audio("dts", 6)), p)
		if d.Segments != "" {
			t.Errorf("%s: decision Segments = %q, want fMP4", p.Name, d.Segments)
		}
	}
}

func TestRokuDirectPlaysAnOrdinaryMP4(t *testing.T) {
	d := Decide(result("mp4", video("h264", 1080), audio("aac", 2)), RokuProfile())
	if d.Method != DirectPlay {
		t.Fatalf("Method = %q (%s), want direct play", d.Method, d.Reason)
	}
	// Direct play has no segments, so it names none.
	if d.Segments != "" {
		t.Errorf("Segments = %q on direct play", d.Segments)
	}
}

// Matroska is out of the floor until a Matroska file has played with sound in
// the app; one played silent over DLNA. A remux, never a re-encode.
func TestRokuRemuxesMatroskaIntoTS(t *testing.T) {
	d := Decide(result("matroska", video("h264", 1080), audio("aac", 2)), RokuProfile())
	if d.Method != Remux {
		t.Fatalf("Method = %q (%s), want remux", d.Method, d.Reason)
	}
	if d.Segments != SegmentsMPEGTS {
		t.Errorf("Segments = %q, want mpegts", d.Segments)
	}
}

func TestRokuRemuxesMatroskaOnceClaimed(t *testing.T) {
	p := WithCapabilities(RokuProfile(), []string{"matroska"})
	d := Decide(result("matroska", video("h264", 1080), audio("aac", 2)), p)
	if d.Method != DirectPlay {
		t.Errorf("Method = %q (%s), want direct play once matroska is claimed", d.Method, d.Reason)
	}
}

// AC3 and EAC3 are in every Roku's floor, so a Dolby track costs nothing.
func TestRokuKeepsDolbyAudio(t *testing.T) {
	for _, codec := range []string{"ac3", "eac3"} {
		d := Decide(result("matroska", video("h264", 1080), audio(codec, 6)), RokuProfile())
		if d.Method != Remux || d.AudioAction != "copy" {
			t.Errorf("%s: %s, audio %s (%s), want a remux copying it", codec, d.Method, d.AudioAction, d.Reason)
		}
	}
}

// No DTS and no TrueHD on the device that was asked, so both re-encode, and
// the video is not dragged into it.
func TestRokuEncodesDTSAndTrueHD(t *testing.T) {
	for _, codec := range []string{"dts", "truehd"} {
		d := Decide(result("matroska", video("h264", 1080), audio(codec, 6)), RokuProfile())
		if d.Method != Transcode || d.AudioAction != "encode" || d.VideoAction != "copy" {
			t.Errorf("%s: %s %s/%s (%s), want audio encoded and video copied",
				codec, d.Method, d.VideoAction, d.AudioAction, d.Reason)
		}
		if !strings.Contains(d.Reason, codec) {
			t.Errorf("%s: reason %q does not name the codec", codec, d.Reason)
		}
	}
}

// FLAC plays on a Roku but cannot ride in a TS segment, so a remux has to
// re-encode it, where the MP4 path for another client would have kept it.
func TestRokuReencodesFLACThatTSCannotCarry(t *testing.T) {
	// With flacmp4 claimed, MP4 could carry it, so TS is the only reason left.
	d := Decide(result("matroska", video("h264", 1080), audio("flac", 2)),
		WithCapabilities(RokuProfile(), []string{"flacmp4"}))
	if d.AudioAction != "encode" {
		t.Fatalf("AudioAction = %q (%s), want encode", d.AudioAction, d.Reason)
	}
	if !strings.Contains(d.Reason, "MPEG-TS") {
		t.Errorf("reason %q does not say TS is why", d.Reason)
	}
	// And the same file for a client that does get fMP4 is not affected.
	p := WithCapabilities(TVProfile(), []string{"flacmp4"})
	if d := Decide(result("matroska", video("h264", 1080), audio("flac", 2)), p); d.AudioAction != "copy" {
		t.Errorf("tv + flacmp4: AudioAction = %q, want copy", d.AudioAction)
	}
}

// HEVC is not in the floor: the cheaper Rokus have no decoder for it. The app
// claims it when the device says yes, and 10-bit needs its own claim, as it
// does for every client that claims rather than lists HEVC.
func TestRokuHEVCNeedsAClaim(t *testing.T) {
	r := result("mp4", video("hevc", 2160), audio("aac", 2))
	if d := Decide(r, RokuProfile()); d.VideoAction != "encode" {
		t.Errorf("unclaimed: VideoAction = %q, want encode", d.VideoAction)
	}
	if d := Decide(r, WithCapabilities(RokuProfile(), []string{"hevc"})); d.Method != DirectPlay {
		t.Errorf("claimed: Method = %q (%s), want direct play", d.Method, d.Reason)
	}

	ten := video("hevc", 2160)
	ten.PixFmt = "yuv420p10le"
	r10 := result("mp4", ten, audio("aac", 2))
	if d := Decide(r10, WithCapabilities(RokuProfile(), []string{"hevc"})); d.VideoAction != "encode" {
		t.Errorf("Main 10 with only hevc: VideoAction = %q, want encode", d.VideoAction)
	}
	if d := Decide(r10, WithCapabilities(RokuProfile(), []string{"hevc", "hevc10"})); d.Method != DirectPlay {
		t.Errorf("Main 10 with hevc10: Method = %q (%s), want direct play", d.Method, d.Reason)
	}
}

// 1080p at 24 fits level 4.1, the case that must stay direct.
func TestRokuLevelLetsOrdinary1080pThrough(t *testing.T) {
	v := video("h264", 1080)
	v.Width, v.FrameRate = 1920, "23.976"
	if d := Decide(result("mp4", v, audio("aac", 2)), RokuProfile()); d.Method != DirectPlay {
		t.Errorf("Method = %q (%s), want direct play", d.Method, d.Reason)
	}
}

// 1080p at 60 needs level 4.2: a frame rate 4.1 does not hold at that size.
// The encode is scaled to the tallest frame 4.1 holds at 60, which is 764
// lines: a little over 720p, and still the same shape.
func TestRokuLevelEncodes1080p60To720p(t *testing.T) {
	v := video("h264", 1080)
	v.Width, v.FrameRate = 1920, "60"
	d := Decide(result("mp4", v, audio("aac", 2)), RokuProfile())
	if d.VideoAction != "encode" {
		t.Fatalf("VideoAction = %q (%s), want encode", d.VideoAction, d.Reason)
	}
	if !strings.Contains(d.Reason, "level 4.2") || !strings.Contains(d.Reason, "4.1") {
		t.Errorf("reason %q does not name the level and the ceiling", d.Reason)
	}
	if d.TargetHeight != 764 {
		t.Errorf("TargetHeight = %d, want 764", d.TargetHeight)
	}
	w := 1920 * d.TargetHeight / 1080
	if !FitsH264Level(w, d.TargetHeight, 60, "4.1") {
		t.Errorf("target %dx%d at 60 still does not fit 4.1", w, d.TargetHeight)
	}
}

// 4K H.264 is level 5.1. Scaled to 1080p, which fits 4.1 at 24.
func TestRokuLevelEncodes4KH264To1080p(t *testing.T) {
	v := video("h264", 2160)
	v.Width, v.FrameRate = 3840, "23.976"
	d := Decide(result("mp4", v, audio("aac", 2)), RokuProfile())
	if d.VideoAction != "encode" {
		t.Fatalf("VideoAction = %q (%s), want encode", d.VideoAction, d.Reason)
	}
	if d.TargetHeight != 1080 {
		t.Errorf("TargetHeight = %d, want 1080", d.TargetHeight)
	}
}

// A scope master is 1080 tall and too wide: the ceiling has to shrink the frame,
// not just hold it to a height (the trap internal/transcode/level.go records).
func TestRokuLevelShrinksAScopeMaster(t *testing.T) {
	v := video("h264", 1080)
	v.Width, v.FrameRate = 2160, "23.976"
	d := Decide(result("mp4", v, audio("aac", 2)), RokuProfile())
	if d.VideoAction != "encode" {
		t.Fatalf("VideoAction = %q (%s), want encode", d.VideoAction, d.Reason)
	}
	if d.TargetHeight == 0 || d.TargetHeight >= 1080 {
		t.Fatalf("TargetHeight = %d, want below 1080", d.TargetHeight)
	}
	if !FitsH264Level(2160*d.TargetHeight/1080, d.TargetHeight, 23.976, "4.1") {
		t.Errorf("target height %d does not fit 4.1", d.TargetHeight)
	}
}

// An HEVC source being encoded for an unclaimed Roku comes out as H.264, so
// the ceiling applies to it too, not only to H.264 sources.
func TestRokuLevelCapsAnEncodeFromHEVC(t *testing.T) {
	v := video("hevc", 2160)
	v.Width, v.FrameRate = 3840, "23.976"
	d := Decide(result("matroska", v, audio("aac", 2)), RokuProfile())
	if d.TargetHeight != 1080 {
		t.Errorf("TargetHeight = %d (%s), want 1080", d.TargetHeight, d.Reason)
	}
}

// A lower ceiling from elsewhere (the server's Quality setting, ?max_height=)
// still wins.
func TestRokuLevelKeepsALowerQualityCeiling(t *testing.T) {
	v := video("h264", 2160)
	v.Width, v.FrameRate = 3840, "23.976"
	p := RokuProfile()
	p.MaxHeight = 720
	if d := Decide(result("mp4", v, audio("aac", 2)), p); d.TargetHeight != 720 {
		t.Errorf("TargetHeight = %d, want 720", d.TargetHeight)
	}
}

// An unmeasured frame is not refused: a re-encode on a number nobody has is a
// guess, and an expensive one.
func TestRokuLevelPassesAnUnknownFrame(t *testing.T) {
	v := Stream{Kind: KindVideo, Codec: "h264", Default: true}
	if d := Decide(result("mp4", v, audio("aac", 2)), RokuProfile()); d.Method != DirectPlay {
		t.Errorf("Method = %q (%s), want direct play", d.Method, d.Reason)
	}
}

// Codecs TS cannot carry are re-encoded even when claimed.
func TestTSCarriage(t *testing.T) {
	for codec, want := range map[string]bool{"h264": true, "hevc": true, "mpeg2video": true, "av1": false, "vp9": false} {
		if got := tsCarriesVideo(&Stream{Codec: codec}); got != want {
			t.Errorf("tsCarriesVideo(%s) = %v, want %v", codec, got, want)
		}
	}
	for codec, want := range map[string]bool{"aac": true, "mp3": true, "ac3": true, "eac3": true, "flac": false, "opus": false, "alac": false} {
		if got := tsCarriesAudio(&Stream{Codec: codec}); got != want {
			t.Errorf("tsCarriesAudio(%s) = %v, want %v", codec, got, want)
		}
	}
}

func TestFitsH264Level(t *testing.T) {
	cases := []struct {
		w, h int
		fps  float64
		max  string
		want bool
	}{
		{1920, 1080, 23.976, "4.1", true},
		{1920, 1080, 60, "4.1", false},
		{1280, 720, 60, "4.1", true},
		{3840, 2160, 24, "4.1", false},
		{3840, 2160, 24, "5.1", true},
		{0, 0, 0, "4.1", true},      // unknown frame
		{3840, 2160, 24, "", true},  // no ceiling
		{3840, 2160, 24, "9", true}, // unknown ceiling is none
	}
	for _, c := range cases {
		if got := FitsH264Level(c.w, c.h, c.fps, c.max); got != c.want {
			t.Errorf("FitsH264Level(%dx%d@%v, %q) = %v, want %v", c.w, c.h, c.fps, c.max, got, c.want)
		}
	}
}
