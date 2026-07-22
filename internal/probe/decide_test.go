package probe

import "testing"

func result(container string, streams ...Stream) *Result {
	return &Result{Container: container, DurationMS: 1000, Streams: streams}
}

func video(codec string, h int) Stream {
	return Stream{Kind: KindVideo, Codec: codec, Width: h * 16 / 9, Height: h, Default: true}
}

func audio(codec string, ch int) Stream {
	return Stream{Kind: KindAudio, Codec: codec, Channels: ch, Default: true}
}

func TestDirectPlayWhenEverythingMatches(t *testing.T) {
	d := Decide(result("mp4", video("h264", 1080), audio("aac", 2)), BrowserProfile())
	if d.Method != DirectPlay {
		t.Errorf("Method = %q (%s), want direct play", d.Method, d.Reason)
	}
	if d.VideoAction != "copy" || d.AudioAction != "copy" {
		t.Errorf("actions = %s/%s, want copy/copy", d.VideoAction, d.AudioAction)
	}
}

// The expensive part is the pixels. If both codecs are fine and only the
// wrapper is wrong, rewrapping is nearly free and re-encoding would be waste.
func TestRemuxWhenOnlyContainerIsWrong(t *testing.T) {
	d := Decide(result("matroska", video("h264", 1080), audio("aac", 2)), BrowserProfile())
	if d.Method != Remux {
		t.Fatalf("Method = %q (%s), want remux", d.Method, d.Reason)
	}
	if d.VideoAction != "copy" || d.AudioAction != "copy" {
		t.Errorf("remux must copy both streams, got %s/%s", d.VideoAction, d.AudioAction)
	}
	if d.TargetFormat != "mp4" {
		t.Errorf("TargetFormat = %q", d.TargetFormat)
	}
}

// Re-encoding audio alone is a fraction of the cost of video, so a bad audio
// codec must not drag the video into an encode.
func TestBadAudioDoesNotForceVideoEncode(t *testing.T) {
	d := Decide(result("matroska", video("h264", 1080), audio("dts", 6)), BrowserProfile())
	if d.Method != Transcode {
		t.Fatalf("Method = %q, want transcode", d.Method)
	}
	if d.VideoAction != "copy" {
		t.Errorf("VideoAction = %q, want copy — only the audio is incompatible", d.VideoAction)
	}
	if d.AudioAction != "encode" {
		t.Errorf("AudioAction = %q, want encode", d.AudioAction)
	}
}

func TestFullTranscodeWhenVideoIsUnsupported(t *testing.T) {
	d := Decide(result("matroska", video("hevc", 2160), audio("dts", 8)), BrowserProfile())
	if d.Method != Transcode || d.VideoAction != "encode" || d.AudioAction != "encode" {
		t.Errorf("decision = %+v, want a full transcode", d)
	}
	if d.Reason == "" {
		t.Error("no reason given for the most expensive decision available")
	}
}

// The trap: the codec name matches, browsers advertise H.264 support, and
// playback still fails because High 10 is outside every browser's baseline.
func Test10BitH264IsNotDirectPlayed(t *testing.T) {
	s := video("h264", 1080)
	s.Profile = "High 10"

	d := Decide(result("mp4", s, audio("aac", 2)), BrowserProfile())
	if d.Method == DirectPlay {
		t.Fatal("10-bit H.264 was direct-played; it will produce a black rectangle")
	}
	if d.VideoAction != "encode" {
		t.Errorf("VideoAction = %q, want encode", d.VideoAction)
	}
}

func Test8BitH264StillDirectPlays(t *testing.T) {
	s := video("h264", 1080)
	s.Profile = "High"
	if d := Decide(result("mp4", s, audio("aac", 2)), BrowserProfile()); d.Method != DirectPlay {
		t.Errorf("Method = %q (%s), want direct play", d.Method, d.Reason)
	}
}

// Browsers decode multichannel AAC. Transcoding it by default cost 18
// needless re-encodes across a real 225-film library.
func TestMultichannelAACDirectPlays(t *testing.T) {
	d := Decide(result("mp4", video("h264", 1080), audio("aac", 6)), BrowserProfile())
	if d.Method != DirectPlay {
		t.Errorf("Method = %q (%s), want direct play — 5.1 AAC is playable", d.Method, d.Reason)
	}
}

// The channel limit still works when explicitly set, for clients that need a
// downmix rather than lack the codec.
func TestChannelLimitWhenSet(t *testing.T) {
	p := BrowserProfile()
	p.MaxAudioChannels = 2

	d := Decide(result("mp4", video("h264", 1080), audio("aac", 6)), p)
	if d.Method != Transcode || d.AudioAction != "encode" {
		t.Errorf("decision = %+v, want an audio re-encode when a limit is set", d)
	}
	if d.VideoAction != "copy" {
		t.Errorf("VideoAction = %q, want copy — only the audio needs work", d.VideoAction)
	}
}

// AC-3 and friends are genuinely undecodable in browsers, which is the honest
// reason to re-encode audio.
func TestAC3RequiresAudioTranscode(t *testing.T) {
	for _, codec := range []string{"ac3", "eac3", "dts", "truehd"} {
		d := Decide(result("mp4", video("h264", 1080), audio(codec, 6)), BrowserProfile())
		if d.Method != Transcode || d.AudioAction != "encode" {
			t.Errorf("%s: decision = %+v, want an audio transcode", codec, d)
		}
		if d.VideoAction != "copy" {
			t.Errorf("%s: VideoAction = %q, want copy", codec, d.VideoAction)
		}
	}
}

func TestHeightLimit(t *testing.T) {
	p := BrowserProfile()
	p.MaxHeight = 1080

	if d := Decide(result("mp4", video("h264", 2160), audio("aac", 2)), p); d.Method != Transcode {
		t.Errorf("Method = %q, want transcode for 4K against a 1080p limit", d.Method)
	}
	if d := Decide(result("mp4", video("h264", 1080), audio("aac", 2)), p); d.Method != DirectPlay {
		t.Errorf("Method = %q, want direct play at exactly the limit", d.Method)
	}
}

func TestBitrateLimit(t *testing.T) {
	p := BrowserProfile()
	p.MaxVideoBitRate = 8_000_000

	s := video("h264", 1080)
	s.BitRate = 40_000_000
	if d := Decide(result("mp4", s, audio("aac", 2)), p); d.Method != Transcode {
		t.Errorf("Method = %q, want transcode for a remux-grade bitrate", d.Method)
	}
}

// Audio-only content has no video to be incompatible with.
func TestAudioOnlyFile(t *testing.T) {
	d := Decide(result("mp4", audio("aac", 2)), BrowserProfile())
	if d.Method != DirectPlay {
		t.Errorf("Method = %q (%s), want direct play", d.Method, d.Reason)
	}
}

// Before probing exists for a file, direct play is the honest answer — it is
// what LANcast did before, and guessing a transcode burns CPU on a hunch.
func TestUnprobedFileDirectPlays(t *testing.T) {
	d := Decide(nil, BrowserProfile())
	if d.Method != DirectPlay {
		t.Errorf("Method = %q, want direct play for an unprobed file", d.Method)
	}
	if d.Reason == "" {
		t.Error("no reason given")
	}
}

// "Why is my server pinned at 100% CPU" must be answerable.
func TestEveryDecisionExplainsItself(t *testing.T) {
	cases := []*Result{
		result("mp4", video("h264", 1080), audio("aac", 2)),
		result("matroska", video("h264", 1080), audio("aac", 2)),
		result("matroska", video("h264", 1080), audio("dts", 6)),
		result("matroska", video("hevc", 2160), audio("truehd", 8)),
		result("avi", video("mpeg4", 480), audio("mp3", 2)),
	}
	for _, r := range cases {
		if d := Decide(r, BrowserProfile()); d.Reason == "" {
			t.Errorf("no reason for %s/%v", r.Container, d.Method)
		}
	}
}

func TestUnknownCodecsAreNotAssumedPlayable(t *testing.T) {
	d := Decide(result("mp4", video("", 1080), audio("", 2)), BrowserProfile())
	if d.Method == DirectPlay {
		t.Error("empty codec names were treated as supported")
	}
}

func TestProfileMatchingIsCaseInsensitive(t *testing.T) {
	d := Decide(result("MP4", video("H264", 1080), audio("AAC", 2)), BrowserProfile())
	if d.Method != DirectPlay {
		t.Errorf("Method = %q, want case-insensitive matching", d.Method)
	}
}
