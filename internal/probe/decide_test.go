package probe

import (
	"strings"
	"testing"
)

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

// --- audio track selection ------------------------------------------------
//
// The decision and ffmpeg's -map must be about the same stream. Deciding
// against the default AAC track and then mapping the DTS track produces
// `-c:a copy` on undecodable audio: the player runs, and the film is silent.

func track(index int, codec string, ch int, isDefault bool) Stream {
	return Stream{Index: index, Kind: KindAudio, Codec: codec, Channels: ch, Default: isDefault}
}

func TestChosenAudioTrackDrivesTheDecision(t *testing.T) {
	r := result("matroska",
		Stream{Index: 0, Kind: KindVideo, Codec: "h264", Height: 1080, Default: true},
		track(1, "aac", 2, true),
		track(2, "dts", 6, false),
	)

	if d := DecideTrack(r, BrowserProfile(), 2); d.AudioAction != "encode" {
		t.Errorf("AudioAction = %q for the DTS track, want encode (%s)", d.AudioAction, d.Reason)
	}
	if d := DecideTrack(r, BrowserProfile(), 1); d.AudioAction != "copy" {
		t.Errorf("AudioAction = %q for the AAC track, want copy (%s)", d.AudioAction, d.Reason)
	}
}

// A non-default AAC track must not inherit the default track's transcode.
//
// This asserted direct play until the picker was first exercised against a real
// dual-audio file. It never encoded, which was the point of the test and is
// still true — but direct play cannot *select* a track, so the guarantee it
// claimed was being provided by the browser skipping a TrueHD track it could
// not decode, rather than by us. Where both tracks are playable that accident
// does not happen and the choice is silently ignored.
//
// It rewraps now: copy/copy, no encode, and the chosen track actually mapped.
func TestChoosingACompatibleTrackAvoidsAnEncode(t *testing.T) {
	r := result("mp4",
		Stream{Index: 0, Kind: KindVideo, Codec: "h264", Height: 1080, Default: true},
		track(1, "truehd", 8, true),
		track(2, "aac", 6, false),
	)

	d := DecideTrack(r, BrowserProfile(), 2)
	if d.Method != Remux {
		t.Errorf("Method = %q (%s), want remux — the chosen track has to be mapped", d.Method, d.Reason)
	}
	if d.AudioAction != "copy" {
		t.Errorf("AudioAction = %q, want copy — the chosen track is AAC and needs nothing", d.AudioAction)
	}
	if d.VideoAction != "copy" {
		t.Errorf("VideoAction = %q, want copy", d.VideoAction)
	}
}

func TestDecideUsesTheDefaultTrack(t *testing.T) {
	r := result("mp4",
		Stream{Index: 0, Kind: KindVideo, Codec: "h264", Height: 1080, Default: true},
		track(1, "dts", 6, true),
		track(2, "aac", 2, false),
	)
	if d := Decide(r, BrowserProfile()); d.AudioAction != "encode" {
		t.Errorf("AudioAction = %q, want encode — the default track is DTS", d.AudioAction)
	}
}

func TestAudioByIndexIgnoresNonAudioStreams(t *testing.T) {
	r := result("mp4",
		Stream{Index: 0, Kind: KindVideo, Codec: "h264", Height: 1080},
		track(1, "aac", 2, true),
	)
	if s := r.AudioByIndex(0); s != nil {
		t.Errorf("AudioByIndex(0) returned the video stream: %+v", s)
	}
	if s := r.AudioByIndex(9); s != nil {
		t.Errorf("AudioByIndex(9) = %+v, want nil for a track that does not exist", s)
	}
	if s := r.AudioByIndex(1); s == nil || s.Codec != "aac" {
		t.Errorf("AudioByIndex(1) = %+v, want the AAC track", s)
	}
}

// --- container muxability -------------------------------------------------
//
// "The client can decode this codec" is not "MP4 can carry it". Copying a
// stream into a container that cannot hold it does not degrade playback —
// ffmpeg refuses to start and the player dies with no reason given.

func TestVorbisIsNotCopiedIntoMP4(t *testing.T) {
	// Vorbis is in the browser profile and genuinely plays — in WebM.
	d := Decide(result("matroska", video("h264", 1080), audio("vorbis", 2)), BrowserProfile())
	if d.Method == Remux || d.AudioAction != "encode" {
		t.Errorf("decision = %+v, want an audio encode — Vorbis cannot be muxed into MP4", d)
	}
	if d.VideoAction != "copy" {
		t.Errorf("VideoAction = %q, want copy — only the audio needs work", d.VideoAction)
	}
}

func TestVP8IsNotCopiedIntoMP4(t *testing.T) {
	d := Decide(result("matroska", video("vp8", 720), audio("aac", 2)), BrowserProfile())
	if d.Method == Remux || d.VideoAction != "encode" {
		t.Errorf("decision = %+v, want a video encode — VP8 cannot be muxed into MP4", d)
	}
}

// The subtle one: FLAC in fragmented MP4 is legal by spec and unplayable in
// enough browsers that copying it blind is a coin toss. The floor therefore
// still re-encodes it — a client that can answer for FLAC-in-MP4 says so and
// gets it copied instead (TestFLACSurvivesARewrapWhenTheClientCan).
func TestFLACIsNotCopiedIntoMP4(t *testing.T) {
	d := Decide(result("matroska", video("h264", 1080), audio("flac", 2)), BrowserProfile())
	if d.AudioAction != "encode" {
		t.Errorf("AudioAction = %q (%s), want encode", d.AudioAction, d.Reason)
	}
	if d.VideoAction != "copy" {
		t.Errorf("VideoAction = %q, want copy", d.VideoAction)
	}
}

// A WebM the browser can already play must still direct-play: the mux rules
// apply to the rewrap target, not to a file nobody is rewrapping.
func TestWebMWithVorbisStillDirectPlays(t *testing.T) {
	d := Decide(result("webm", video("vp8", 720), audio("vorbis", 2)), BrowserProfile())
	if d.Method != DirectPlay {
		t.Errorf("Method = %q (%s), want direct play", d.Method, d.Reason)
	}
}

func TestMuxBlockedDecisionsStillExplainThemselves(t *testing.T) {
	cases := []*Result{
		result("matroska", video("h264", 1080), audio("vorbis", 2)),
		result("matroska", video("vp8", 720), audio("aac", 2)),
		result("matroska", video("h264", 1080), audio("flac", 2)),
	}
	for _, r := range cases {
		if d := Decide(r, BrowserProfile()); d.Reason == "" {
			t.Errorf("no reason given for %s", r.Container)
		}
	}
}

// --- bit depth ------------------------------------------------------------

// pix_fmt is the reliable signal; a probe that reports it must be believed
// even when the profile name says nothing.
func TestTenBitDetectedFromPixFmt(t *testing.T) {
	s := video("h264", 1080)
	s.PixFmt = "yuv420p10le"

	if d := Decide(result("mp4", s, audio("aac", 2)), BrowserProfile()); d.VideoAction != "encode" {
		t.Errorf("VideoAction = %q, want encode for 10-bit H.264 (%s)", d.VideoAction, d.Reason)
	}
}

// The old substring test matched any profile containing "10". A false
// positive here is a needless full re-encode on every file that has one.
func TestProfileNamesContainingTenAreNotAllTenBit(t *testing.T) {
	s := video("h264", 1080)
	s.Profile = "High"
	s.PixFmt = "yuv420p"

	if d := Decide(result("mp4", s, audio("aac", 2)), BrowserProfile()); d.Method != DirectPlay {
		t.Errorf("Method = %q (%s), want direct play for 8-bit High", d.Method, d.Reason)
	}
}

// 10-bit is an H.264 problem specifically. HEVC, VP9 and AV1 carry it fine,
// and applying the rule to them would re-encode most modern 4K files.
func TestTenBitAV1IsNotPenalised(t *testing.T) {
	s := video("av1", 2160)
	s.PixFmt = "yuv420p10le"

	if d := Decide(result("mp4", s, audio("aac", 2)), BrowserProfile()); d.Method != DirectPlay {
		t.Errorf("Method = %q (%s), want direct play — 10-bit AV1 is fine", d.Method, d.Reason)
	}
}

// A video encode is no reason to re-encode good audio alongside it. Copying
// costs nothing and avoids a second lossy generation.
func TestCompatibleAudioIsCopiedThroughAVideoEncode(t *testing.T) {
	d := Decide(result("matroska", video("hevc", 2160), audio("aac", 6)), BrowserProfile())
	if d.VideoAction != "encode" {
		t.Fatalf("VideoAction = %q, want encode", d.VideoAction)
	}
	if d.AudioAction != "copy" {
		t.Errorf("AudioAction = %q, want copy — the AAC track is fine", d.AudioAction)
	}
}

// --- named client profiles ------------------------------------------------

func TestProfileByNameFallsBackToBrowser(t *testing.T) {
	for _, name := range []string{"", "nonsense", "  "} {
		if got := ProfileByName(name).Name; got != "browser" {
			t.Errorf("ProfileByName(%q) = %q, want the conservative default", name, got)
		}
	}
	if got := ProfileByName("SAFARI").Name; got != "safari" {
		t.Errorf("ProfileByName is case-sensitive: got %q", got)
	}
}

// The gap that costs the most: HEVC against the default profile is a full
// video re-encode, and Safari decodes it in hardware.
func TestHEVCDirectPlaysOnSafariAndTranscodesOnTheDefault(t *testing.T) {
	r := result("mp4", video("hevc", 2160), audio("aac", 6))

	if d := Decide(r, SafariProfile()); d.Method != DirectPlay {
		t.Errorf("safari: Method = %q (%s), want direct play", d.Method, d.Reason)
	}
	if d := Decide(r, BrowserProfile()); d.VideoAction != "encode" {
		t.Errorf("browser: VideoAction = %q, want encode — HEVC is not universally decodable", d.VideoAction)
	}
}

// AC-3 in MP4 plays on Safari, and is muxable, so a rewrap must not become an
// audio encode.
func TestAC3RemuxesRatherThanEncodesOnSafari(t *testing.T) {
	d := Decide(result("matroska", video("h264", 1080), audio("ac3", 6)), SafariProfile())
	if d.Method != Remux {
		t.Fatalf("Method = %q (%s), want remux", d.Method, d.Reason)
	}
	if d.AudioAction != "copy" {
		t.Errorf("AudioAction = %q, want copy — AC-3 plays on Safari and muxes into MP4", d.AudioAction)
	}
}

// A TV client with a hardware decoder is the case where transcoding is pure
// waste, which is the entire reason probing exists.
func TestTVProfileDirectPlaysTheHardCases(t *testing.T) {
	cases := []*Result{
		result("matroska", video("hevc", 2160), audio("truehd", 8)),
		result("matroska", video("h264", 1080), audio("dts", 6)),
		result("mp4", video("av1", 2160), audio("eac3", 6)),
	}
	for _, r := range cases {
		if d := Decide(r, TVProfile()); d.Method != DirectPlay {
			t.Errorf("%s: Method = %q (%s), want direct play", r.Container, d.Method, d.Reason)
		}
	}
}

// --- audio containers -----------------------------------------------------
//
// A music library is mostly files whose container *is* the codec. Before these
// were listed, every one of them failed the container check and was rewrapped
// into MP4 — which for FLAC means a lossless file re-encoded to AAC to deliver
// a format the browser plays natively.

func TestMusicContainersDirectPlayInABrowser(t *testing.T) {
	cases := []*Result{
		result("mp3", audio("mp3", 2)),
		result("flac", audio("flac", 2)),
		result("ogg", audio("vorbis", 2)),
		result("ogg", audio("opus", 2)),
		result("wav", audio("pcm_s16le", 2)),
		result("mov", audio("aac", 2)), // .m4a
	}
	for _, r := range cases {
		d := Decide(r, BrowserProfile())
		if d.Method != DirectPlay {
			t.Errorf("%s/%s: Method = %q (%s), want direct play",
				r.Container, r.Audio().Codec, d.Method, d.Reason)
		}
	}
}

// The one that costs the most to get wrong: a rewrap to MP4 cannot carry FLAC,
// so the fallback is not a cheap repackage but a lossy re-encode of a lossless
// file.
func TestFLACIsNotReEncodedForABrowser(t *testing.T) {
	d := Decide(result("flac", audio("flac", 2)), BrowserProfile())
	if d.AudioAction != "copy" {
		t.Errorf("AudioAction = %q (%s), want copy — a lossless file must not be re-encoded",
			d.AudioAction, d.Reason)
	}
}

// Apple ships no Ogg decoder and its own lossless codec instead. Both halves of
// that have to be true or the profile is claiming support that produces silence.
func TestSafariTakesALACAndConvertsOgg(t *testing.T) {
	if d := Decide(result("mov", audio("alac", 2)), SafariProfile()); d.Method != DirectPlay {
		t.Errorf("alac: Method = %q (%s), want direct play", d.Method, d.Reason)
	}
	d := Decide(result("ogg", audio("vorbis", 2)), SafariProfile())
	if d.AudioAction != "encode" {
		t.Errorf("ogg: AudioAction = %q (%s), want encode — Safari has no Ogg decoder",
			d.AudioAction, d.Reason)
	}
}

// ALAC in a browser is the inverse: the container is fine and the codec is not,
// so the audio is re-encoded and nothing pretends otherwise.
func TestALACIsConvertedForABrowser(t *testing.T) {
	d := Decide(result("mov", audio("alac", 2)), BrowserProfile())
	if d.Method != Transcode || d.AudioAction != "encode" {
		t.Errorf("Method = %q, AudioAction = %q (%s), want a transcode that encodes audio",
			d.Method, d.AudioAction, d.Reason)
	}
	if !d.AudioOnly {
		t.Error("AudioOnly is false on a file with no video stream")
	}
}

// AudioOnly is what keeps `-map 0:v:0` off the ffmpeg command line, so it has
// to be set on the direct-play path too — a client may ask about a decision it
// then never acts on.
func TestAudioOnlyIsSetForMusicAndNotForFilm(t *testing.T) {
	if d := Decide(result("flac", audio("flac", 2)), BrowserProfile()); !d.AudioOnly {
		t.Error("AudioOnly is false for a music file")
	}
	if d := Decide(result("mp4", video("h264", 1080), audio("aac", 2)), BrowserProfile()); d.AudioOnly {
		t.Error("AudioOnly is true for a file with a video stream")
	}
}

// Cover art is stored as a video stream. If it were mistaken for a picture the
// decision would map it, and ffmpeg would sit encoding one still frame for the
// length of the track.
func TestCoverArtDoesNotMakeATrackLookLikeVideo(t *testing.T) {
	r := result("flac", Stream{Kind: KindVideo, Codec: "mjpeg", Width: 600, Height: 600},
		audio("flac", 2))
	d := Decide(r, BrowserProfile())
	if !d.AudioOnly {
		t.Error("AudioOnly is false on a tagged track carrying embedded cover art")
	}
	if d.Method != DirectPlay {
		t.Errorf("Method = %q (%s), want direct play", d.Method, d.Reason)
	}
}

// 24-bit WAV is the deliberate gap: browser support is not universal, and a
// needless encode there is cheap where a wrong claim is silence.
func TestHighBitDepthWAVIsConvertedForABrowserAndNotForATV(t *testing.T) {
	r := result("wav", audio("pcm_s24le", 2))
	if d := Decide(r, BrowserProfile()); d.AudioAction != "encode" {
		t.Errorf("browser: AudioAction = %q (%s), want encode", d.AudioAction, d.Reason)
	}
	if d := Decide(r, TVProfile()); d.Method != DirectPlay {
		t.Errorf("tv: Method = %q (%s), want direct play", d.Method, d.Reason)
	}
}

// ---- choosing an audio track --------------------------------------------
//
// Direct play serves the file's bytes and nothing else, so it cannot deliver a
// track other than the one the file leads with: the browser opens the whole
// file and picks by its own rules. Deciding "direct play" for an explicitly
// chosen alternate track therefore produces a picker that appears to work and
// changes nothing — which is worse than one that fails, because nobody
// investigates it.

func audioTrack(index int, codec string, ch int, def bool) Stream {
	return Stream{Kind: KindAudio, Index: index, Codec: codec, Channels: ch, Default: def}
}

// The case the dual-audio test file exercises: the second track is a codec the
// browser cannot decode, so it has to be encoded whatever else happens.
func TestChoosingAnUndecodableAudioTrackTranscodes(t *testing.T) {
	r := result("mp4",
		Stream{Kind: KindVideo, Index: 0, Codec: "h264", Width: 1920, Height: 1080, Default: true},
		audioTrack(1, "aac", 2, true),
		audioTrack(2, "ac3", 2, false),
	)
	d := DecideTrack(r, BrowserProfile(), 2)
	if d.Method != Transcode {
		t.Fatalf("Method = %q (%s), want transcode", d.Method, d.Reason)
	}
	if d.AudioAction != "encode" {
		t.Errorf("AudioAction = %q, want encode — ac3 cannot be decoded", d.AudioAction)
	}
	if d.VideoAction != "copy" {
		t.Errorf("VideoAction = %q, want copy — only the audio is the problem", d.VideoAction)
	}
}

// The subtle one, and the reason this rule exists at all. Everything is
// playable and the container is fine, so every other signal says direct play —
// but direct play cannot select the second track, so it would be ignored in
// silence.
func TestChoosingAPlayableAlternateTrackStillRemuxes(t *testing.T) {
	r := result("mp4",
		Stream{Kind: KindVideo, Index: 0, Codec: "h264", Width: 1920, Height: 1080, Default: true},
		audioTrack(1, "aac", 2, true),
		audioTrack(2, "aac", 6, false),
	)
	d := DecideTrack(r, BrowserProfile(), 2)
	if d.Method != Remux {
		t.Fatalf("Method = %q (%s), want remux — direct play cannot select a track", d.Method, d.Reason)
	}
	if d.VideoAction != "copy" || d.AudioAction != "copy" {
		t.Errorf("actions = %s/%s, want copy/copy — nothing needs re-encoding", d.VideoAction, d.AudioAction)
	}
	// The reason is shown to the user. Blaming the container here would be a
	// lie: the container is supported and is not why this is being rewrapped.
	if d.Reason != "a different audio track was chosen, which direct play cannot select" {
		t.Errorf("Reason = %q, want the alternate-track reason", d.Reason)
	}
}

// Asking for the track the file already leads with is not a change, and must
// not cost a rewrap.
func TestChoosingTheDefaultTrackStillDirectPlays(t *testing.T) {
	r := result("mp4",
		Stream{Kind: KindVideo, Index: 0, Codec: "h264", Width: 1920, Height: 1080, Default: true},
		audioTrack(1, "aac", 2, true),
		audioTrack(2, "ac3", 2, false),
	)
	d := DecideTrack(r, BrowserProfile(), 1)
	if d.Method != DirectPlay {
		t.Fatalf("Method = %q (%s), want direct play", d.Method, d.Reason)
	}
}

// No explicit choice is the ordinary case and must be untouched by any of this.
func TestNoAudioChoiceIsUnaffected(t *testing.T) {
	r := result("mp4",
		Stream{Kind: KindVideo, Index: 0, Codec: "h264", Width: 1920, Height: 1080, Default: true},
		audioTrack(1, "aac", 2, true),
		audioTrack(2, "ac3", 2, false),
	)
	d := DecideTrack(r, BrowserProfile(), -1)
	if d.Method != DirectPlay {
		t.Fatalf("Method = %q (%s), want direct play", d.Method, d.Reason)
	}
}

// ---- quality ceilings -------------------------------------------------------

// A ceiling below the source is what forces the encode, and the target has to
// travel with the decision: ffmpeg is built from the decision, not the profile,
// so a cap the decision does not carry is a cap that never reaches a pixel.
func TestHeightCeilingForcesEncodeAndSetsTarget(t *testing.T) {
	p := BrowserProfile()
	p.MaxHeight = 720
	d := Decide(result("mp4", video("h264", 1080), audio("aac", 2)), p)
	if d.Method != Transcode || d.VideoAction != "encode" {
		t.Fatalf("decision = %+v, want a video encode", d)
	}
	if d.TargetHeight != 720 {
		t.Errorf("TargetHeight = %d, want 720", d.TargetHeight)
	}
}

// A limit is not a request. Scaling a 480p file up to the 720p ceiling pays for
// an encode that adds no detail and costs more bandwidth than the source did.
func TestCeilingAboveSourceIsNotAnUpscale(t *testing.T) {
	p := BrowserProfile()
	p.MaxHeight = 720
	d := Decide(result("mp4", video("h264", 480), audio("aac", 2)), p)
	if d.Method != DirectPlay {
		t.Fatalf("Method = %q (%s), want direct play — the file is already under the ceiling", d.Method, d.Reason)
	}
	if d.TargetHeight != 0 {
		t.Errorf("TargetHeight = %d, want 0", d.TargetHeight)
	}
}

func TestBitrateCeilingSetsTarget(t *testing.T) {
	p := BrowserProfile()
	p.MaxVideoBitRate = 4_000_000
	v := video("h264", 1080)
	v.BitRate = 12_000_000
	d := Decide(result("mp4", v, audio("aac", 2)), p)
	if d.Method != Transcode || d.VideoAction != "encode" {
		t.Fatalf("decision = %+v, want a video encode", d)
	}
	if d.TargetVideoBitRate != 4_000_000 {
		t.Errorf("TargetVideoBitRate = %d, want 4000000", d.TargetVideoBitRate)
	}
}

// A copy cannot honour a ceiling. Reporting one on a remux would have the
// client believe a cap applied to bytes nothing re-encoded.
func TestNoTargetOnACopy(t *testing.T) {
	p := BrowserProfile()
	p.MaxHeight = 720
	p.MaxVideoBitRate = 4_000_000
	// 720p h264 in matroska: the container forces a remux, nothing else.
	d := Decide(result("matroska", video("h264", 720), audio("aac", 2)), p)
	if d.Method != Remux {
		t.Fatalf("Method = %q (%s), want remux", d.Method, d.Reason)
	}
	if d.TargetHeight != 0 || d.TargetVideoBitRate != 0 {
		t.Errorf("remux carries a target: height %d, bitrate %d", d.TargetHeight, d.TargetVideoBitRate)
	}
}

// ---- HDR detection (ADR 0033) -----------------------------------------------

func TestIsHDRDetectsPQAndHLG(t *testing.T) {
	for _, tr := range []string{"smpte2084", "arib-std-b67", "SMPTE2084", " smpte2084 "} {
		if !IsHDR(&Stream{Kind: KindVideo, ColorTransfer: tr}) {
			t.Errorf("ColorTransfer %q not detected as HDR", tr)
		}
	}
}

// The trap the rule exists to avoid. 10-bit is correlated with HDR and does not
// mean it — yuv420p10le is what HDR10 reports and what 10-bit SDR reports, and
// tone mapping an SDR file damages it just as surely as not tone mapping an HDR
// one does.
func TestTenBitSDRIsNotHDR(t *testing.T) {
	s := &Stream{Kind: KindVideo, Codec: "hevc", PixFmt: "yuv420p10le", ColorTransfer: "bt709"}
	if IsHDR(s) {
		t.Error("10-bit SDR reported as HDR — bit depth is not the signal")
	}
}

// BT.2020 primaries turn up on SDR content. The transfer curve decides.
func TestWideGamutSDRIsNotHDR(t *testing.T) {
	s := &Stream{Kind: KindVideo, ColorPrimaries: "bt2020", ColorSpace: "bt2020nc", ColorTransfer: "bt709"}
	if IsHDR(s) {
		t.Error("wide-gamut SDR reported as HDR")
	}
}

// Every row predates the probe change until re-probed. Guessing HDR from a
// blank field would tone map a whole library on no evidence.
func TestUnprobedColourIsNotHDR(t *testing.T) {
	if IsHDR(&Stream{Kind: KindVideo, Codec: "hevc", PixFmt: "yuv420p10le"}) {
		t.Error("a stream with no recorded transfer reported as HDR")
	}
	if IsHDR(nil) {
		t.Error("nil reported as HDR")
	}
}

// ---- HDR on the decision (ADR 0033) -----------------------------------------

// hdrVideo is an HDR10 stream: HEVC Main10, PQ transfer, BT.2020 primaries.
// Exactly what an HDR file in a real library reports.
func hdrVideo(h int) Stream {
	s := video("hevc", h)
	s.PixFmt = "yuv420p10le"
	s.ColorTransfer = "smpte2084"
	s.ColorPrimaries = "bt2020"
	s.ColorSpace = "bt2020nc"
	return s
}

// The common path, not an edge case: HDR content is HEVC Main10, the browser
// profile excludes HEVC, so every HDR file re-encodes for a browser. There is no
// configuration in which a browser gets an HDR file and this flag is not needed.
func TestHDRSourceEncodingCarriesTheTonemapFlag(t *testing.T) {
	d := Decide(result("matroska", hdrVideo(2160), audio("eac3", 6)), BrowserProfile())
	if d.VideoAction != "encode" {
		t.Fatalf("VideoAction = %q (%s), want encode", d.VideoAction, d.Reason)
	}
	if !d.TonemapHDR {
		t.Error("TonemapHDR = false on an HDR source being re-encoded")
	}
}

// A copy delivers the source's own video bytes, which are HDR and are correctly
// described as HDR. Flagging one would have the command line tag a passthrough
// stream bt709 — producing the misdescribed file the flag exists to prevent.
func TestCopiedHDRIsNotTonemapped(t *testing.T) {
	// A profile that can take HEVC in mp4, so the video is copyable and only
	// the container forces work.
	p := BrowserProfile()
	p.VideoCodecs = append(p.VideoCodecs, "hevc")
	p.AudioCodecs = append(p.AudioCodecs, "eac3")

	d := Decide(result("matroska", hdrVideo(2160), audio("eac3", 6)), p)
	if d.VideoAction != "copy" {
		t.Fatalf("VideoAction = %q (%s), want copy", d.VideoAction, d.Reason)
	}
	if d.TonemapHDR {
		t.Error("TonemapHDR = true on a video copy; nothing re-encodes those bytes")
	}
}

// The trap, at the decision level rather than in IsHDR. A 10-bit SDR file
// re-encoding must not be tone mapped: the conversion would damage it exactly as
// surely as skipping it damages an HDR file.
func TestTenBitSDREncodingIsNotTonemapped(t *testing.T) {
	v := video("hevc", 1080)
	v.PixFmt = "yuv420p10le"
	v.ColorTransfer = "bt709"

	d := Decide(result("matroska", v, audio("eac3", 6)), BrowserProfile())
	if d.VideoAction != "encode" {
		t.Fatalf("VideoAction = %q (%s), want encode", d.VideoAction, d.Reason)
	}
	if d.TonemapHDR {
		t.Error("TonemapHDR = true on 10-bit SDR")
	}
}

// Every row predates the probe change that records colour until it is
// re-probed. Guessing HDR from a blank field would tone map a whole library on
// no evidence.
func TestUnprobedColourEncodingIsNotTonemapped(t *testing.T) {
	d := Decide(result("matroska", video("hevc", 1080), audio("eac3", 6)), BrowserProfile())
	if d.VideoAction != "encode" {
		t.Fatalf("VideoAction = %q (%s), want encode", d.VideoAction, d.Reason)
	}
	if d.TonemapHDR {
		t.Error("TonemapHDR = true with no recorded transfer function")
	}
}

// tenBitVideo is Main 10 without the HDR signalling — which is what the film
// that produced this bug actually is. 10-bit is correlated with HDR and does not
// mean it, and the rule here is about the bit depth alone.
func tenBitVideo(codec string, h int) Stream {
	s := video(codec, h)
	s.PixFmt = "yuv420p10le"
	return s
}

/*
 * 10-bit HEVC, and the claim that does not cover it.
 *
 * Found on a real film: Main 10 in MP4 direct-played with perfect audio and a
 * picture that stuttered. The client had claimed "hevc" from an 8-bit
 * `hvc1.1.6` probe and the server read it as covering Main 10 as well.
 *
 * The worst shape this bug comes in is that nothing fails. A direct play that
 * *errors* records the claim and stops making it; this one played, badly, and
 * no code anywhere can tell a smooth picture from a glitching one.
 */
func TestClaimedHEVCDoesNotCoverTenBit(t *testing.T) {
	p := WithCapabilities(BrowserProfile(), []string{"hevc"})

	// 8-bit HEVC is what the claim was actually about, and still direct-plays.
	if d := Decide(result("mp4", video("hevc", 1080), audio("aac", 2)), p); d.VideoAction != "copy" {
		t.Errorf("8-bit HEVC = %q (%s), want copy", d.VideoAction, d.Reason)
	}

	// Main 10 does not.
	d := Decide(result("mp4", tenBitVideo("hevc", 1080), audio("aac", 2)), p)
	if d.VideoAction != "encode" {
		t.Fatalf("10-bit HEVC = %q (%s), want encode", d.VideoAction, d.Reason)
	}
	if !strings.Contains(d.Reason, "10-bit HEVC") {
		t.Errorf("reason = %q, want it to name the bit depth", d.Reason)
	}
}

// A client that answered for Main 10 specifically gets it direct-played, which
// is the whole point of asking the engine rather than guessing.
func TestClaimedTenBitHEVCIsDirectPlayed(t *testing.T) {
	p := WithCapabilities(BrowserProfile(), []string{"hevc", "hevc10"})

	d := Decide(result("mp4", tenBitVideo("hevc", 1080), audio("aac", 2)), p)
	if d.VideoAction != "copy" {
		t.Errorf("VideoAction = %q (%s), want copy", d.VideoAction, d.Reason)
	}
}

/*
 * The distrust belongs to the guess, not to the profile.
 *
 * `tv` and `safari` list HEVC natively because they are device classes known to
 * decode Main 10 in hardware. Demanding a claim from them would re-encode HDR
 * for exactly the clients that handle it best.
 */
func TestNativeHEVCProfilesStillTakeTenBit(t *testing.T) {
	for _, p := range []Profile{TVProfile(), SafariProfile()} {
		d := Decide(result("mp4", tenBitVideo("hevc", 2160), audio("aac", 6)), p)
		if d.VideoAction != "copy" {
			t.Errorf("%s: 10-bit HEVC = %q (%s), want copy", p.Name, d.VideoAction, d.Reason)
		}
	}
}

/*
 * Lossless audio survives a container rewrite — when the client can take it.
 *
 * A codec MP4 cannot carry has to be re-encoded even when the client decodes it
 * perfectly well, because the only thing wrong with the file is the box it
 * arrived in. For FLAC and ALAC that meant lossless became AAC to fix a
 * container: the one conversion in this system that cannot be undone.
 *
 * Gated on a claim rather than widened into the floor, because "can decode
 * FLAC" and "can decode FLAC inside MP4" are different questions and only the
 * first is true of every browser.
 */
func TestFLACSurvivesARewrapWhenTheClientCan(t *testing.T) {
	p := WithCapabilities(BrowserProfile(), []string{"flacmp4"})
	d := Decide(result("matroska", video("h264", 1080), audio("flac", 2)), p)
	if d.Method != Remux {
		t.Fatalf("Method = %q (%s), want remux", d.Method, d.Reason)
	}
	if d.AudioAction != "copy" {
		t.Errorf("AudioAction = %q (%s), want copy — re-encoding lossless to change a container destroys it",
			d.AudioAction, d.Reason)
	}
}

func TestOpusSurvivesARewrapWhenTheClientCan(t *testing.T) {
	p := WithCapabilities(BrowserProfile(), []string{"opusmp4"})
	d := Decide(result("matroska", video("h264", 1080), audio("opus", 2)), p)
	if d.Method != Remux {
		t.Fatalf("Method = %q (%s), want remux", d.Method, d.Reason)
	}
	if d.AudioAction != "copy" {
		t.Errorf("AudioAction = %q (%s), want copy", d.AudioAction, d.Reason)
	}
}

/*
 * One claim does not licence the other.
 *
 * They are separate engine answers — a browser may carry one and not the other
 * — and reading either as covering both is the mistake hevc10 was created to
 * undo, where a claim measured for 8-bit was taken as permission for Main 10.
 */
func TestTheTwoLosslessClaimsAreIndependent(t *testing.T) {
	p := WithCapabilities(BrowserProfile(), []string{"flacmp4"})
	d := Decide(result("matroska", video("h264", 1080), audio("opus", 2)), p)
	if d.AudioAction != "encode" {
		t.Errorf("AudioAction = %q (%s), want encode — only FLAC was claimed",
			d.AudioAction, d.Reason)
	}
}

/*
 * ALAC turns on the client, not the container.
 *
 * MP4 is ALAC's native home so carriage was never the question; whether
 * anything can decode it is. The browser floor cannot, so it is still
 * re-encoded there — and a device that says it can gets the lossless kept,
 * with no claim needed.
 */
func TestALACIsCopiedOnlyForAClientThatCanPlayIt(t *testing.T) {
	browser := Decide(result("matroska", video("h264", 1080), audio("alac", 2)), BrowserProfile())
	if browser.AudioAction != "encode" {
		t.Errorf("browser AudioAction = %q (%s), want encode — no browser decodes ALAC",
			browser.AudioAction, browser.Reason)
	}
	tv := Decide(result("matroska", video("h264", 1080), audio("alac", 2)), TVProfile())
	if tv.AudioAction != "copy" {
		t.Errorf("tv AudioAction = %q (%s), want copy", tv.AudioAction, tv.Reason)
	}
}

/*
 * Vorbis is left out on purpose.
 *
 * It has no settled mapping into MP4, and every Vorbis file measured was in Ogg
 * — which the browser takes as it stands, so carrying it would be a risk run
 * for no file that exists. Asserted rather than merely omitted so that adding
 * it later is a decision somebody makes rather than a line somebody tidies.
 */
func TestVorbisIsNotCarriedIntoMP4(t *testing.T) {
	p := WithCapabilities(BrowserProfile(), []string{"flacmp4", "opusmp4"})
	d := Decide(result("matroska", video("h264", 1080), audio("vorbis", 2)), p)
	if d.AudioAction != "encode" {
		t.Errorf("AudioAction = %q (%s), want encode", d.AudioAction, d.Reason)
	}
}

/*
 * The bare containers still direct-play, which is the case this change must not
 * disturb: 131 FLAC files and 11 Ogg files in the measured library are already
 * served untouched, and the point of the carriage work is the rewrap path.
 */
func TestABareLosslessFileIsStillLeftAlone(t *testing.T) {
	d := Decide(result("flac", audio("flac", 2)), BrowserProfile())
	if d.Method != DirectPlay {
		t.Errorf("Method = %q (%s), want direct play", d.Method, d.Reason)
	}
}

/*
 * 10-bit H.264, once the client says it can.
 *
 * High 10 is most of an anime library and was a full video re-encode every
 * time — on files the engine can play. Chromium answers `probably` for
 * `avc1.6e0033`, so the question was worth asking rather than assuming.
 */
func TestTenBitH264DirectPlaysWhenClaimed(t *testing.T) {
	s := video("h264", 1080)
	s.Profile = "High 10"

	p := WithCapabilities(BrowserProfile(), []string{"high10"})
	d := Decide(result("mp4", s, audio("aac", 2)), p)
	if d.Method != DirectPlay {
		t.Errorf("Method = %q (%s), want direct play", d.Method, d.Reason)
	}
}

// The pix_fmt route has to honour the claim too, or the same file decides
// differently depending on which signal the prober happened to report.
func TestTenBitH264FromPixFmtAlsoHonoursTheClaim(t *testing.T) {
	s := video("h264", 1080)
	s.PixFmt = "yuv420p10le"

	p := WithCapabilities(BrowserProfile(), []string{"high10"})
	if d := Decide(result("mp4", s, audio("aac", 2)), p); d.Method != DirectPlay {
		t.Errorf("Method = %q (%s), want direct play", d.Method, d.Reason)
	}
}

/*
 * A native listing is not a claim, and this is where High 10 differs from
 * Main 10 on purpose.
 *
 * `hevc10` trusts a profile that lists HEVC natively, because `tv` and `safari`
 * are device classes known to decode Main 10 in hardware. H.264 is listed
 * natively by *every* profile here, so the same rule would hand High 10 to a
 * set-top box on the strength of it decoding 8-bit H.264 — and High 10 is
 * missing from most fixed-function decoders that manage High profile fine.
 */
func TestTenBitH264IsNotGrantedByANativeH264Listing(t *testing.T) {
	s := video("h264", 1080)
	s.Profile = "High 10"

	for _, p := range []Profile{TVProfile(), SafariProfile()} {
		d := Decide(result("mp4", s, audio("aac", 2)), p)
		if d.VideoAction != "encode" {
			t.Errorf("%s: VideoAction = %q (%s), want encode — no claim was made",
				p.Name, d.VideoAction, d.Reason)
		}
	}
}

// The two bit-depth claims are separate questions about separate codecs, and
// neither licences the other.
func TestHigh10AndHEVC10DoNotLicenceEachOther(t *testing.T) {
	h264 := video("h264", 1080)
	h264.Profile = "High 10"
	hevc := video("hevc", 2160)
	hevc.PixFmt = "yuv420p10le"

	onlyHEVC := WithCapabilities(BrowserProfile(), []string{"hevc", "hevc10"})
	if d := Decide(result("mp4", h264, audio("aac", 2)), onlyHEVC); d.VideoAction != "encode" {
		t.Errorf("VideoAction = %q (%s), want encode — hevc10 says nothing about H.264",
			d.VideoAction, d.Reason)
	}

	onlyHigh10 := WithCapabilities(BrowserProfile(), []string{"hevc", "high10"})
	if d := Decide(result("mp4", hevc, audio("aac", 2)), onlyHigh10); d.VideoAction != "encode" {
		t.Errorf("VideoAction = %q (%s), want encode — high10 says nothing about HEVC",
			d.VideoAction, d.Reason)
	}
}

// 8-bit H.264 was never in question and must not change.
func TestHigh10ClaimDoesNotDisturbOrdinaryH264(t *testing.T) {
	p := WithCapabilities(BrowserProfile(), []string{"high10"})
	d := Decide(result("mp4", video("h264", 1080), audio("aac", 2)), p)
	if d.Method != DirectPlay {
		t.Errorf("Method = %q (%s), want direct play", d.Method, d.Reason)
	}
}

/*
 * The 10-bit fallback has to know HEVC's profile names, not only H.264's.
 *
 * The guard above `isTenBit` exists *for* 10-bit HEVC, and the fallback it
 * leans on when pix_fmt is missing listed `high 10` and friends — all H.264 —
 * and none of HEVC's. So on a row with no pix_fmt, a Main 10 file read as
 * 8-bit and direct-played into exactly the judder the guard was written to
 * prevent.
 *
 * It hid because pix_fmt is populated on anything a current build probed.
 * pix_fmt arrived in schema revision 12; a row probed before it has an empty
 * one until something re-probes, and those rows are the ones that fall here.
 */
func TestTenBitHEVCIsCaughtByProfileWhenPixFmtIsMissing(t *testing.T) {
	s := video("hevc", 1080)
	s.Profile = "Main 10"
	s.PixFmt = "" // a row probed before pix_fmt was recorded

	p := WithCapabilities(BrowserProfile(), []string{"hevc"}) // 8-bit claim only
	if d := Decide(result("mp4", s, audio("aac", 2)), p); d.Method == DirectPlay {
		t.Errorf("Method = %q (%s), want a re-encode — Main 10 with an 8-bit claim",
			d.Method, d.Reason)
	}
}

// The claim still decides. A client that answered for Main 10 keeps its direct
// play, whether the depth was learned from pix_fmt or from the profile name.
func TestTenBitHEVCFromProfileStillHonoursTheClaim(t *testing.T) {
	s := video("hevc", 1080)
	s.Profile = "Main 10"
	s.PixFmt = ""

	p := WithCapabilities(BrowserProfile(), []string{"hevc", "hevc10"})
	if d := Decide(result("mp4", s, audio("aac", 2)), p); d.Method != DirectPlay {
		t.Errorf("Method = %q (%s), want direct play — the client claimed hevc10",
			d.Method, d.Reason)
	}
}

// 8-bit HEVC must not be swept up. "Main" and "Main Still Picture" are 8-bit,
// and re-encoding them would undo the direct play most HEVC files get.
func TestEightBitHEVCProfilesAreNotTreatedAsTenBit(t *testing.T) {
	for _, profile := range []string{"Main", "Main Still Picture"} {
		s := video("hevc", 1080)
		s.Profile = profile
		s.PixFmt = ""

		p := WithCapabilities(BrowserProfile(), []string{"hevc"})
		if d := Decide(result("mp4", s, audio("aac", 2)), p); d.Method != DirectPlay {
			t.Errorf("profile %q: Method = %q (%s), want direct play for 8-bit HEVC",
				profile, d.Method, d.Reason)
		}
	}
}

// The deeper HEVC profiles carry more than ten bits and belong on the same side.
func TestHEVCProfilesDeeperThanTenBitAreCaught(t *testing.T) {
	for _, profile := range []string{"Main 12", "Main 4:2:2 10", "Main 4:4:4 12"} {
		s := video("hevc", 1080)
		s.Profile = profile
		s.PixFmt = ""

		p := WithCapabilities(BrowserProfile(), []string{"hevc"})
		if d := Decide(result("mp4", s, audio("aac", 2)), p); d.Method == DirectPlay {
			t.Errorf("profile %q direct-played with an 8-bit claim (%s)", profile, d.Reason)
		}
	}
}
