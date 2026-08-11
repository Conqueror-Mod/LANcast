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
// enough browsers that copying it is a coin toss. Re-encoding costs nothing.
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
