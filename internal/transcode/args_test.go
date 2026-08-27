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

// audioEncodeDecision is a video file whose audio needs re-encoding — not to be
// confused with audioOnlyDecision, which is content with no picture at all.
func audioEncodeDecision() probe.Decision {
	return probe.Decision{Method: probe.Transcode, VideoAction: "copy", AudioAction: "encode"}
}

func audioOnlyDecision() probe.Decision {
	return probe.Decision{
		Method: probe.Transcode, VideoAction: "copy", AudioAction: "encode",
		AudioOnly: true,
	}
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
func TestAudioEncodeCopiesVideo(t *testing.T) {
	args := Args(Options{Input: "in.mkv", Output: Progressive, Decision: audioEncodeDecision()})
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

// A music file has no video stream. `-map 0:v:0` against one makes ffmpeg exit
// before producing a byte, so the failure is a dead player rather than degraded
// audio.
func TestAudioOnlyMapsNoVideoStream(t *testing.T) {
	args := Args(Options{Input: "track.wma", Output: Progressive, Decision: audioOnlyDecision(), AudioIndex: -1})
	if hasArgPair(args, "-map", "0:v:0") {
		t.Errorf("video stream mapped on audio-only content; ffmpeg would fail outright: %v", args)
	}
	if !hasArgPair(args, "-map", "0:a:0?") {
		t.Errorf("audio stream is not mapped: %v", args)
	}
	if !hasSequence(args, "-c:a", "aac") {
		t.Error("audio is not re-encoded to aac")
	}
}

// -c:v with no mapped video stream is at best noise and at worst hardware
// encoder initialisation for a job that never touches a pixel.
func TestAudioOnlyNamesNoVideoCodec(t *testing.T) {
	args := Args(Options{Input: "track.wma", Output: Progressive, Decision: audioOnlyDecision(), AudioIndex: -1})
	if argIndex(args, "-c:v") >= 0 {
		t.Errorf("-c:v present with no video stream: %v", args)
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

/*
 * Hardware decode, one case per shape — the encoder was accelerated and the
 * decoder was not, which left the expensive half of a 1080p HEVC re-encode on
 * the CPU while the GPU idled.
 */
func TestHardwareEncodeAlsoDecodesInHardware(t *testing.T) {
	args := Args(Options{
		Input: "in.mkv", Output: Progressive,
		Decision: fullDecision(), Encoder: candidates[0],
	})
	/*
	 * cuda, not auto. `auto` picked DXVA2, DXVA2 needs a Direct3D device, and
	 * the server runs as a Windows service in session 0 where there is none —
	 * so v0.8.0 shipped with HEVC playback broken outright. Naming the
	 * encoder's own driver stack is the fix, and asserting the exact value is
	 * what stops `auto` coming back as a tidy-up.
	 */
	if !hasSequence(args, "-hwaccel", "cuda") {
		t.Error("NVENC should decode on CUDA; -hwaccel auto is what broke v0.8.0")
	}
	if argIndex(args, "auto") >= 0 {
		t.Error("-hwaccel auto is back; it cannot create a D3D device as a service")
	}
	// An input option. After -i it applies to the output and does nothing.
	if h, i := argIndex(args, "-hwaccel"), argIndex(args, "-i"); h > i {
		t.Errorf("-hwaccel at %d is after -i at %d; it must be an input option", h, i)
	}
}

// A stream copy decodes nothing, so hardware init is cost with no work to pay
// for it.
func TestNoHardwareDecodeOnAVideoCopy(t *testing.T) {
	args := Args(Options{
		Input: "in.mkv", Output: Progressive,
		Decision: audioEncodeDecision(), Encoder: candidates[0],
	})
	if argIndex(args, "-hwaccel") >= 0 {
		t.Error("-hwaccel on a video copy, which decodes no video at all")
	}
}

// Content with no picture must not pull a GPU into the job.
func TestNoHardwareDecodeOnAudioOnly(t *testing.T) {
	args := Args(Options{
		Input: "in.m4a", Output: Progressive,
		Decision: audioOnlyDecision(), Encoder: candidates[0],
	})
	if argIndex(args, "-hwaccel") >= 0 {
		t.Error("-hwaccel on audio-only content")
	}
}

/*
 * When the decoded frames are allowed to stay on the card.
 *
 * This is the difference between 2.66 cores and 0.67 on a 1080p Main 10 file,
 * and the shipping path was *slower in wall time than decoding in software* —
 * NVDEC hands back p010, every frame is copied off the card, and a CPU swscale
 * converts it to 8-bit because -pix_fmt said so.
 *
 * The conditions are the point. Anything downstream that needs a pixel in
 * system memory pulls them back, and there are two: the tone map (zscale and
 * tonemap are CPU filters, ADR 0033) and a resolution cap (`scale` likewise).
 * Each has a case here so that adding a third CPU filter without thinking about
 * this fails loudly rather than producing a chain ffmpeg refuses.
 */
func TestFramesStayOnTheCardWhenNothingNeedsThemBack(t *testing.T) {
	args := Args(Options{
		Input: "in.mkv", Output: Progressive,
		Decision: fullDecision(), Encoder: candidates[0],
	})
	if !hasSequence(args, "-hwaccel_output_format", "cuda") {
		t.Error("frames are being copied back for nothing")
	}
	if !hasArgPair(args, "-vf", "scale_cuda=format=yuv420p") {
		t.Error("no scale_cuda: something has to do the 10-bit conversion")
	}
	// The whole saving. -pix_fmt is a system-memory format and naming it drags
	// the frames off the card, which is the cost this avoids.
	if argIndex(args, "-pix_fmt") >= 0 {
		t.Error("-pix_fmt with frames on the card undoes the entire change")
	}
}

// Tone mapping needs the frames in system memory: there is no tonemap_cuda in
// stock ffmpeg, which is ADR 0033's constraint and has not changed.
func TestTonemapPullsTheFramesBack(t *testing.T) {
	dec := fullDecision()
	dec.TonemapHDR = true
	args := Args(Options{
		Input: "in.mkv", Output: Progressive, Decision: dec,
		Encoder: candidates[0], CanTonemap: true, CanTagSDR: true,
	})
	if argIndex(args, "-hwaccel_output_format") >= 0 {
		t.Error("frames left on the card with a CPU tone map downstream")
	}
	if !hasSequence(args, "-pix_fmt", "yuv420p") {
		t.Error("frames are in system memory and nothing forces 8-bit")
	}
	if argIndex(args, "scale_cuda=format=yuv420p") >= 0 {
		t.Error("scale_cuda alongside CPU filters")
	}
}

/*
 * A resolution cap does too. `scale_cuda` exists but takes different arguments
 * and will not accept the `-2` this uses to keep widths even, so the cap stays
 * a CPU filter and the frames come back to meet it.
 */
func TestAResolutionCapPullsTheFramesBack(t *testing.T) {
	dec := fullDecision()
	dec.TargetHeight = 720
	args := Args(Options{
		Input: "in.mkv", Output: Progressive, Decision: dec, Encoder: candidates[0],
	})
	if argIndex(args, "-hwaccel_output_format") >= 0 {
		t.Error("frames left on the card with a CPU scale downstream")
	}
	if !hasArgPair(args, "-vf", "scale=-2:720") {
		t.Error("the cap stopped being applied")
	}
	if !hasSequence(args, "-pix_fmt", "yuv420p") {
		t.Error("frames are in system memory and nothing forces 8-bit")
	}
}

/*
 * AMF stays on software decode. Its Windows decode path is D3D-backed, which is
 * exactly what failed in session 0, and there is no AMD machine here to prove
 * otherwise on — guessing is what caused the regression this test guards.
 */
func TestAMFDecodesInSoftwareUntilSomebodyProvesOtherwise(t *testing.T) {
	var amf Encoder
	for _, c := range candidates {
		if c.Name == "h264_amf" {
			amf = c
		}
	}
	args := Args(Options{
		Input: "in.mkv", Output: Progressive,
		Decision: fullDecision(), Encoder: amf,
	})
	if argIndex(args, "-hwaccel") >= 0 {
		t.Error("-hwaccel with AMF, which is unverified in a service context")
	}
}

// The software encoder is chosen when no hardware was found, so asking for
// hardware decode alongside it is asking for a device that is not there.
func TestNoHardwareDecodeWithSoftwareEncoder(t *testing.T) {
	args := Args(Options{
		Input: "in.mkv", Output: Progressive,
		Decision: fullDecision(), Encoder: Software,
	})
	if argIndex(args, "-hwaccel") >= 0 {
		t.Error("-hwaccel alongside libx264")
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
	args := Args(Options{Input: "in.mkv", Output: Progressive, Decision: audioEncodeDecision(), AudioIndex: 3})
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

// ---- quality ceilings -------------------------------------------------------

// -2, not -1, on the derived width. H.264 requires even dimensions and -1
// computes an exact width that lands odd on plenty of ordinary aspect ratios;
// the encoder does not round it, it exits.
func TestScaleFilterKeepsWidthEven(t *testing.T) {
	d := probe.Decision{Method: probe.Transcode, VideoAction: "encode", AudioAction: "copy", TargetHeight: 720}
	args := Args(Options{Input: "in.mkv", Output: Progressive, Decision: d, AudioIndex: -1})
	if got := argValue(args, "-vf"); got != "scale=-2:720" {
		t.Errorf("-vf = %q, want scale=-2:720", got)
	}
}

// The cap has to be rate limiting on top of the quality-based encode, with a
// buffer. -maxrate without -bufsize is ignored by x264.
func TestBitrateCeilingLimitsRate(t *testing.T) {
	d := probe.Decision{Method: probe.Transcode, VideoAction: "encode", AudioAction: "copy",
		TargetVideoBitRate: 4_000_000}
	args := Args(Options{Input: "in.mkv", Output: Progressive, Decision: d, AudioIndex: -1})
	if got := argValue(args, "-maxrate"); got != "4000k" {
		t.Errorf("-maxrate = %q, want 4000k", got)
	}
	if got := argValue(args, "-bufsize"); got != "8000k" {
		t.Errorf("-bufsize = %q, want 8000k", got)
	}
}

// A copy has no filter chain to put a scale into. Emitting one against
// -c:v copy is not a no-op — ffmpeg refuses to start.
func TestNoScaleOnAVideoCopy(t *testing.T) {
	d := probe.Decision{Method: probe.Remux, VideoAction: "copy", AudioAction: "copy", TargetHeight: 720}
	args := Args(Options{Input: "in.mkv", Output: Progressive, Decision: d, AudioIndex: -1})
	if argIndex(args, "-vf") >= 0 {
		t.Errorf("-vf emitted alongside -c:v copy: %v", args)
	}
}

// No ceiling, no flags. An unconstrained encode must look exactly as it did
// before quality selection existed.
func TestNoCeilingEmitsNothing(t *testing.T) {
	d := probe.Decision{Method: probe.Transcode, VideoAction: "encode", AudioAction: "copy"}
	args := Args(Options{Input: "in.mkv", Output: Progressive, Decision: d, AudioIndex: -1})
	if argIndex(args, "-vf") >= 0 || argIndex(args, "-maxrate") >= 0 {
		t.Errorf("unconstrained encode carries ceiling flags: %v", args)
	}
}

/*
 * Live input, where the command line differs from a file in ways that are all
 * invisible until an evening's viewing falls over.
 *
 * Same model as the decision tests: one named case per rule, asserting the
 * flag *and* what it prevents.
 */
func TestLiveInputReconnects(t *testing.T) {
	got := Args(Options{
		Input: "https://provider.example/one.m3u8",
		Live:  true,
		Decision: probe.Decision{
			Method: probe.Remux, VideoAction: "copy", AudioAction: "copy",
		},
	})
	line := strings.Join(got, " ")

	// A dropped source must stutter, not end the channel. reconnect_streamed is
	// the one that covers live input — plain reconnect only covers seekable.
	for _, want := range []string{"-reconnect 1", "-reconnect_streamed 1", "-reconnect_delay_max 5"} {
		if !strings.Contains(line, want) {
			t.Errorf("missing %q in %s", want, line)
		}
	}
}

// A provider that stops sending without closing the socket would otherwise hold
// a process and a connection for a viewer who has gone.
func TestLiveInputTimesOutOnASilentSource(t *testing.T) {
	got := strings.Join(Args(Options{Input: "u", Live: true}), " ")
	// Microseconds. The obvious value of 10 means ten microseconds, which is
	// the trap this assertion exists to pin.
	if !strings.Contains(got, "-rw_timeout 15000000") {
		t.Errorf("no read timeout in %s", got)
	}
}

// Broadcast MPEG-TS carries timestamp discontinuities at every junction, and
// fMP4 refuses them.
func TestLiveGeneratesTimestamps(t *testing.T) {
	got := strings.Join(Args(Options{Input: "u", Live: true}), " ")
	if !strings.Contains(got, "-fflags +genpts") {
		t.Errorf("no genpts in %s", got)
	}
}

/*
 * A live stream fragments on the clock, not on keyframes and not on frames.
 *
 * frag_keyframe waits for the next IDR, and a channel with a long GOP can be
 * several seconds between them — so the picture arrives late enough to look
 * broken. The unbuffered flush is the same argument: a buffered live stream
 * arrives in bursts behind whatever the buffer holds.
 */
func TestLiveFragmentsOnDurationAndFlushes(t *testing.T) {
	got := strings.Join(Args(Options{Input: "u", Live: true}), " ")
	if !strings.Contains(got, "-frag_duration "+liveFragDuration) {
		t.Errorf("live output has no fragment interval: %s", got)
	}
	if !strings.Contains(got, "-flush_packets 1") {
		t.Errorf("live output is buffered: %s", got)
	}
	if strings.Contains(got, "frag_keyframe") {
		t.Errorf("live output kept the file movflags: %s", got)
	}
}

// There is nothing to seek in a live stream, and -ss against one delays the
// start while ffmpeg looks for a position that does not exist.
func TestLiveIgnoresAnOffset(t *testing.T) {
	got := strings.Join(Args(Options{Input: "u", Live: true, StartAt: 120}), " ")
	if strings.Contains(got, "-ss") {
		t.Errorf("live command carried a seek: %s", got)
	}
}

// And a file is untouched by any of it — the flags are wrong for a file, and
// the shared builder is exactly where that could go unnoticed.
func TestAFileGetsNoLiveFlags(t *testing.T) {
	got := strings.Join(Args(Options{Input: "/media/film.mkv", StartAt: 30}), " ")
	for _, unwanted := range []string{"-reconnect", "-rw_timeout", "-frag_duration", "+genpts"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("file command carried %q: %s", unwanted, got)
		}
	}
	if !strings.Contains(got, "-ss 30.000") {
		t.Errorf("file command lost its seek: %s", got)
	}
}

// Remux is the case that matters most for cost: nearly every IPTV channel is
// H.264/AAC in MPEG-TS, which becomes fMP4 with no re-encode at all.
func TestLiveRemuxCopiesBothStreams(t *testing.T) {
	got := strings.Join(Args(Options{
		Input: "u", Live: true,
		Decision: probe.Decision{
			Method: probe.Remux, VideoAction: "copy", AudioAction: "copy",
		},
	}), " ")
	if !strings.Contains(got, "-c:v copy") || !strings.Contains(got, "-c:a copy") {
		t.Errorf("a remuxable channel is being re-encoded: %s", got)
	}
}

/*
 * AAC out of MPEG-TS needs its framing converted, and this is the case that
 * shipped broken.
 *
 * H.264 with AAC in a transport stream is the single most common live format.
 * Without the filter ffmpeg emits a valid ftyp box, refuses the first audio
 * packet with "Malformed AAC bitstream", and exits — so the browser shows one
 * frame and stops, which reads as a broken channel rather than a broken command
 * line. Measured: 16 KB produced without it, 1.05 MB with it, on the same
 * source.
 */
func TestLiveCopyConvertsADTSFraming(t *testing.T) {
	got := strings.Join(Args(Options{
		Input: "u", Live: true,
		Decision: probe.Decision{VideoAction: "copy", AudioAction: "copy"},
	}), " ")
	if !strings.Contains(got, "-bsf:a aac_adtstoasc") {
		t.Errorf("no ADTS conversion on a live audio copy: %s", got)
	}
}

// A file remuxed from a container that already stores AAC the way MP4 wants it
// must not get the filter — there would be nothing to convert.
func TestAFileCopyDoesNotConvertFraming(t *testing.T) {
	got := strings.Join(Args(Options{
		Input:    "/media/film.mkv",
		Decision: probe.Decision{VideoAction: "copy", AudioAction: "copy"},
	}), " ")
	if strings.Contains(got, "aac_adtstoasc") {
		t.Errorf("a file copy carried the live bitstream filter: %s", got)
	}
}

// An encoded audio track needs no filter: it is being produced fresh, in the
// framing the muxer wants.
func TestLiveEncodedAudioNeedsNoFilter(t *testing.T) {
	got := strings.Join(Args(Options{
		Input: "u", Live: true,
		Decision: probe.Decision{VideoAction: "copy", AudioAction: "encode"},
	}), " ")
	if strings.Contains(got, "aac_adtstoasc") {
		t.Errorf("filter applied to an encode: %s", got)
	}
	if !strings.Contains(got, "-c:a aac") {
		t.Errorf("audio is not being encoded to AAC: %s", got)
	}
}

/*
 * An HLS live source starts at the live edge.
 *
 * The HLS demuxer defaults to three segments back, and those segments already
 * exist — so ffmpeg fetches them as fast as the server serves them and
 * everything downstream receives media faster than real time until the backlog
 * drains. Measured on a real channel with LANcast's own arguments, over twenty
 * seconds of wall clock: 29.97s of media produced (1.50x) by default, 19.97s
 * (1.00x) with this flag.
 */
func TestHLSLiveStartsAtTheEdge(t *testing.T) {
	got := Args(Options{
		Input: "https://example.invalid/master.m3u8", Output: Progressive,
		Live: true, HLSInput: true,
		Decision: remuxDecision(),
	})
	if !hasArgPair(got, "-live_start_index", "-1") {
		t.Errorf("no -live_start_index -1 for an HLS live source:\n%v", got)
	}
}

/*
 * And a live source that is not HLS must not get it.
 *
 * Not a tidiness point: `-live_start_index` belongs to the HLS demuxer, and
 * against a plain transport stream ffmpeg does not ignore it — it refuses the
 * input with "Option live_start_index not found" and produces nothing. Applying
 * it unconditionally would turn every non-HLS channel into a dead one.
 */
func TestANonHLSLiveSourceGetsNoHLSOption(t *testing.T) {
	got := Args(Options{
		Input: "http://tuner.invalid:9981/stream/channel/1", Output: Progressive,
		Live: true, HLSInput: false,
		Decision: remuxDecision(),
	})
	for _, a := range got {
		if a == "-live_start_index" {
			t.Errorf("an HLS-only option reached a plain stream:\n%v", got)
		}
	}
}

// A file is not live and gets neither.
func TestAFileGetsNoLiveStartIndex(t *testing.T) {
	got := Args(Options{
		Input: `C:\m\film.mkv`, Output: Progressive, HLSInput: true,
		Decision: remuxDecision(),
	})
	for _, a := range got {
		if a == "-live_start_index" {
			t.Errorf("a file got a live option:\n%v", got)
		}
	}
}

// ---- HDR to SDR (ADR 0033) --------------------------------------------------

func encodeHDR() probe.Decision {
	return probe.Decision{
		Method: probe.Transcode, VideoAction: "encode", AudioAction: "copy",
		TargetFormat: "mp4", TonemapHDR: true,
	}
}

// hdrArgs builds the command line for an HDR source under a given build's
// capabilities.
func hdrArgs(tonemap, tagSDR bool) []string {
	return Args(Options{
		Input: "in.mkv", Output: Progressive, Decision: encodeHDR(), AudioIndex: -1,
		CanTonemap: tonemap, CanTagSDR: tagSDR,
	})
}

func hasColourTags(args []string) bool {
	return hasSequence(args, "-colorspace", "bt709") &&
		hasSequence(args, "-color_primaries", "bt709") &&
		hasSequence(args, "-color_trc", "bt709")
}

func TestTonemapFilterOnHDRSource(t *testing.T) {
	vf := argValue(hdrArgs(true, true), "-vf")
	for _, want := range []string{
		"zscale=t=linear:npl=100", "format=gbrpf32le", "zscale=p=bt709",
		"tonemap=hable:desat=0", "zscale=t=bt709:m=bt709:r=tv",
	} {
		if !strings.Contains(vf, want) {
			t.Errorf("-vf %q missing %q", vf, want)
		}
	}
}

/*
 * The half of ADR 0033 that must not regress with the other half, asserted
 * separately from the filter chain for exactly that reason.
 *
 * ffmpeg copies the source's colour metadata to the output by default, so
 * without these an 8-bit H.264 file goes out claiming smpte2084/bt2020 —
 * asserting a transfer function its contents do not have.
 */
func TestConvertedHDROutputIsTaggedSDR(t *testing.T) {
	if !hasColourTags(hdrArgs(true, true)) {
		t.Error("a tonemapped output is not tagged bt709")
	}
}

// An ffmpeg without zscale must produce a flat picture, never a dead stream: an
// unrecognised filter makes ffmpeg exit before the first frame.
func TestNoTonemapFilterWhenTheBuildCannot(t *testing.T) {
	vf := argValue(hdrArgs(false, true), "-vf")
	if strings.Contains(vf, "tonemap") || strings.Contains(vf, "zscale") {
		t.Errorf("-vf %q uses filters this build does not have", vf)
	}
}

/*
 * Without the conversion, the labels are still made coherent — and the relabel
 * filter is what makes that work.
 *
 * Measured against a real HDR10 clip through LANcast's own arguments: x264 writes
 * its VUI from the frame properties it is handed, so the output flags alone
 * produced bt709 / smpte2084 / bt2020 — a file whose matrix and transfer
 * disagree. setparams forces the frame properties so all three agree.
 */
func TestRelabelWhenTheBuildCannotTonemap(t *testing.T) {
	a := hdrArgs(false, true)
	if vf := argValue(a, "-vf"); !strings.Contains(vf, "setparams=") {
		t.Errorf("-vf %q does not relabel the frame colour properties", vf)
	}
	if !hasColourTags(a) {
		t.Error("relabelled output is not tagged bt709")
	}
}

/*
 * The third state, and the one worth being deliberate about: a build that can
 * neither convert nor relabel leaves the output exactly as it is today.
 *
 * Emitting the tags alone is what produces the incoherent file above, which is
 * worse than the self-consistent HDR tags that ship now. Doing nothing is the
 * least wrong option available, not an oversight.
 */
func TestNoColourTagsWhenNothingCanBeMadeCoherent(t *testing.T) {
	a := hdrArgs(false, false)
	if argIndex(a, "-vf") >= 0 {
		t.Errorf("-vf %q present with no usable colour filter", argValue(a, "-vf"))
	}
	if argIndex(a, "-colorspace") >= 0 || argIndex(a, "-color_trc") >= 0 {
		t.Error("tags emitted without the frame properties to back them; that is the hybrid file")
	}
}

// An SDR source must be left entirely alone — no filter, and no re-tagging of
// colour metadata that was already correct.
func TestSDRSourceIsNotTonemappedOrRetagged(t *testing.T) {
	d := encodeHDR()
	d.TonemapHDR = false
	a := Args(Options{Input: "in.mkv", Output: Progressive, Decision: d,
		AudioIndex: -1, CanTonemap: true, CanTagSDR: true})

	if vf := argValue(a, "-vf"); strings.Contains(vf, "tonemap") || strings.Contains(vf, "setparams") {
		t.Errorf("-vf %q touches an SDR source", vf)
	}
	if argIndex(a, "-colorspace") >= 0 || argIndex(a, "-color_trc") >= 0 {
		t.Error("SDR source had its colour metadata rewritten")
	}
}

/*
 * Scale and tonemap must arrive as one -vf.
 *
 * ffmpeg takes the last -vf and silently discards the others, so two flags would
 * not stack — the tonemap would replace the scale, and a quality ceiling would
 * stop being honoured with nothing in the output to say why. Scale comes first
 * because tone mapping is per-pixel work and doing it after the downscale is the
 * same conversion on fewer pixels.
 */
func TestScaleAndTonemapComposeIntoOneFilterFlag(t *testing.T) {
	d := encodeHDR()
	d.TargetHeight = 720

	a := Args(Options{Input: "in.mkv", Output: Progressive, Decision: d,
		AudioIndex: -1, CanTonemap: true, CanTagSDR: true})

	var vfCount int
	for _, arg := range a {
		if arg == "-vf" {
			vfCount++
		}
	}
	if vfCount != 1 {
		t.Fatalf("%d -vf flags, want exactly 1 — ffmpeg keeps only the last", vfCount)
	}

	vf := argValue(a, "-vf")
	scale := strings.Index(vf, "scale=-2:720")
	tone := strings.Index(vf, "tonemap=")
	switch {
	case scale < 0:
		t.Errorf("-vf %q lost the quality ceiling", vf)
	case tone < 0:
		t.Errorf("-vf %q lost the tonemap", vf)
	case scale > tone:
		t.Errorf("-vf %q tone maps before scaling; that is the same work on more pixels", vf)
	}
}

// A copy has no filter chain and no encoder to tag. The decision never sets
// TonemapHDR on a copy (asserted in probe), and Args must not invent one.
func TestCopiedVideoGetsNoColourArgs(t *testing.T) {
	a := Args(Options{Input: "in.mkv", Output: Progressive, AudioIndex: -1,
		CanTonemap: true, CanTagSDR: true,
		Decision: probe.Decision{
			Method: probe.Remux, VideoAction: "copy", AudioAction: "copy",
			TargetFormat: "mp4",
		}})

	if argIndex(a, "-vf") >= 0 {
		t.Error("a remux carries a filter chain")
	}
	if argIndex(a, "-colorspace") >= 0 {
		t.Error("a remux rewrites colour metadata")
	}
}

// ---- AC-3 in fragmented MP4 --------------------------------------------------

/*
 * The muxer must be able to describe AC-3, or the stream is dead before it
 * starts.
 *
 * `empty_moov` writes the moov atom up front, but the MP4 muxer builds the
 * `dac3`/`dec3` box from a parsed packet, so it cannot describe AC-3 or E-AC-3
 * yet — and ffmpeg refuses outright rather than degrading:
 *
 *	Cannot write moov atom before EAC3 packets parsed.
 *	Could not write header (incorrect codec parameters ?): Invalid argument
 *
 * ffmpeg exits before the first byte while the client sits on a committed 200
 * with a spinner. Measured on real files, ten seconds copied out of each:
 * E-AC-3 946 bytes and dead, 8,029,116 with delay_moov; AC-3 946 bytes and
 * dead, 3,343,714 with it; AAC unchanged either way.
 */
func TestFragmentedOutputCanDescribeAC3(t *testing.T) {
	for _, o := range []Options{
		{Input: "in.mkv", Output: Progressive, AudioIndex: -1},
		{Input: "in.ts", Output: Progressive, AudioIndex: -1, Live: true},
	} {
		flags := argValue(Args(o), "-movflags")
		if !strings.Contains(flags, "delay_moov") {
			t.Errorf("live=%v: -movflags %q cannot carry AC-3", o.Live, flags)
		}
		// delay_moov replaces neither of the flags that make it a *stream*.
		if !strings.Contains(flags, "empty_moov") {
			t.Errorf("live=%v: -movflags %q lost empty_moov", o.Live, flags)
		}
		if !strings.Contains(flags, "default_base_moof") {
			t.Errorf("live=%v: -movflags %q lost default_base_moof", o.Live, flags)
		}
	}
}

// Live still gets a short fragment interval that does not depend on the
// source's GOP: fragmenting on keyframes makes the browser wait for the first
// one, which on a long GOP is seconds of blank screen that reads as a broken
// channel.
func TestLiveFragmentIntervalDoesNotDependOnTheGOP(t *testing.T) {
	live := Args(Options{Input: "in.ts", Output: Progressive, AudioIndex: -1, Live: true})
	if got := argValue(live, "-frag_duration"); got != liveFragDuration {
		t.Errorf("-frag_duration %q, want %q for live", got, liveFragDuration)
	}
	if flags := argValue(live, "-movflags"); strings.Contains(flags, "frag_keyframe") {
		t.Errorf("-movflags %q ties the live interval to the GOP", flags)
	}
	file := argValue(Args(Options{Input: "in.mkv", Output: Progressive, AudioIndex: -1}), "-movflags")
	if !strings.Contains(file, "frag_keyframe") {
		t.Errorf("-movflags %q, want frag_keyframe for a file", file)
	}
}

/*
 * frag_every_frame never comes back, on any path.
 *
 * It was here for a good reason — a short interval the source's GOP cannot
 * lengthen — and it corrupted the timestamps of every live channel it
 * produced. Measured against one fixed MPEG-TS capture, changing only this
 * constant: 2,192 ffmpeg warnings and duplicate DTS with it, zero without.
 * A browser demuxer requires DTS to increase strictly, so the picture froze
 * while ffmpeg stayed healthy and went on producing bytes.
 *
 * The reason it is worth a test rather than a comment is that the argument
 * *for* the flag still reads as correct, so the obvious repair for a
 * late-starting picture is to put it back.
 */
func TestNothingFragmentsOnEveryFrame(t *testing.T) {
	for _, o := range []Options{
		{Input: "in.ts", Output: Progressive, AudioIndex: -1, Live: true},
		{Input: "in.mkv", Output: Progressive, AudioIndex: -1},
		{Input: "in.ts", Output: HLS, OutputDir: "d", AudioIndex: -1, Live: true},
	} {
		if got := strings.Join(Args(o), " "); strings.Contains(got, "frag_every_frame") {
			t.Errorf("live=%v output=%s: %s", o.Live, o.Output, got)
		}
	}
}

// HLS writes its own init segment after parsing, so it never had this problem
// and must not grow MP4 muxer flags it does not use.
func TestHLSCarriesNoMovflags(t *testing.T) {
	a := Args(Options{Input: "in.mkv", Output: HLS, OutputDir: "/tmp/x", AudioIndex: -1})
	if argIndex(a, "-movflags") >= 0 {
		t.Errorf("HLS output carries -movflags %q", argValue(a, "-movflags"))
	}
}

/*
 * The playlist type is a claim about the source, and getting it wrong is not
 * cosmetic.
 *
 * A live channel emitted with `-hls_playlist_type vod` produces no playlist at
 * all while it runs: measured against a real channel, nine good segments were
 * written over 60s and index.m3u8 never appeared, so nothing could play it.
 * These two tests are what stop that returning.
 */
func TestLiveHLSIsAnEventPlaylist(t *testing.T) {
	args := Args(Options{
		Input: "http://tuner.invalid/ch.m3u8", Output: HLS, OutputDir: "/tmp/x",
		Live: true, Decision: remuxDecision(), AudioIndex: -1,
	})
	if got := argValue(args, "-hls_playlist_type"); got != "event" {
		t.Fatalf("live HLS playlist type = %q, want event", got)
	}
}

func TestFileHLSStaysVOD(t *testing.T) {
	args := Args(Options{
		Input: "in.mkv", Output: HLS, OutputDir: "/tmp/x",
		Decision: remuxDecision(),
	})
	if got := argValue(args, "-hls_playlist_type"); got != "vod" {
		t.Fatalf("file HLS playlist type = %q, want vod", got)
	}
}
