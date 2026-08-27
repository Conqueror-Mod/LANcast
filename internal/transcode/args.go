// Package transcode runs ffmpeg to deliver media a client cannot play directly.
//
// Argument construction is separated from process execution so the command
// line — where the subtle mistakes live — is testable without spawning
// anything or having media on disk.
package transcode

import (
	"fmt"
	"strconv"
	"strings"

	"lancast/internal/probe"
)

// Output is the delivery format.
type Output string

const (
	// HLS produces fMP4 segments and a playlist. Seekable, and what a TV
	// client will want.
	HLS Output = "hls"
	// Progressive produces one fragmented MP4 stream on stdout. No playlist,
	// no seeking beyond what has been produced — but it plays in any browser
	// with no client-side library, which HLS does not.
	Progressive Output = "progressive"
)

// SegmentSeconds is the target segment length. Six seconds is the common
// default: short enough that a seek does not wait long, long enough that
// per-segment overhead stays small.
const SegmentSeconds = 6

// Options describe one transcode.
type Options struct {
	Input      string
	Output     Output
	Decision   probe.Decision
	StartAt    float64 // seconds into the file
	OutputDir  string  // HLS only
	AudioIndex int     // absolute stream index; -1 means let ffmpeg choose

	// AudioBitrate for re-encoded audio, in kbit/s.
	AudioBitrate int
	// AudioChannels for re-encoded audio. 2 keeps browsers happy.
	AudioChannels int
	// CRF controls video quality when re-encoding. Lower is better; 23 is the
	// x264 default and a reasonable balance for a home server. Hardware
	// encoders spell this differently but mean the same thing.
	CRF int
	// Preset trades encode speed for file size. Software encoder only.
	Preset string
	// Encoder chooses how video is re-encoded. The zero value is software.
	Encoder Encoder

	/*
	 * Live marks input that never ends and cannot be sought.
	 *
	 * It changes the command line in four ways, each of which is wrong for a
	 * file and necessary for a channel:
	 *
	 *   - reconnect flags, because a live HTTP source drops and a dropped
	 *     source must not end the viewing;
	 *   - a read timeout, so a provider that stops sending without closing the
	 *     socket does not leave ffmpeg blocked for ever;
	 *   - `-fflags +genpts`, because MPEG-TS from a broadcast chain routinely
	 *     arrives with timestamp discontinuities that fMP4 will not accept;
	 *   - no `-ss`, since there is nothing to seek in.
	 *
	 * It is a separate flag rather than an Output value because it is
	 * orthogonal to the delivery format: live content still goes out as
	 * progressive fMP4, and the two decisions do not belong in one field.
	 */
	Live bool

	/*
	 * HLSInput marks a live source that is an HLS playlist, which changes where
	 * ffmpeg starts reading.
	 *
	 * The HLS demuxer defaults to `live_start_index -3`: three segments back
	 * from the live edge. Those segments already exist, so ffmpeg fetches them
	 * as fast as the server will serve them — and everything downstream
	 * receives media faster than real time until the backlog is drained.
	 *
	 * Measured against a real channel, running LANcast's own arguments for
	 * twenty seconds of wall clock:
	 *
	 *	default (-3):            29.97s of media   → 1.50x real time
	 *	live_start_index -1:     19.97s of media   → 1.00x real time
	 *
	 * A separate flag rather than something inferred inside Args, because the
	 * answer comes from the probe and Args is pure. And conditional rather than
	 * unconditional because `-live_start_index` belongs to the HLS demuxer:
	 * given a plain transport stream ffmpeg does not ignore it, it refuses the
	 * input outright with "Option live_start_index not found" — turning a
	 * working channel into a dead one.
	 */
	HLSInput bool

	/*
	 * CanTonemap and CanTagSDR report what this ffmpeg build can do about HDR.
	 * They are properties of the build, not of the job, and are set by the
	 * Manager from a real capability probe the same way Encoder is — so Args
	 * stays pure.
	 *
	 * CanTonemap means `zscale` and `tonemap` are both present. `zscale` needs
	 * libzimg and LANcast runs whatever ffmpeg is on the machine (ADR 0016), so
	 * it is not guaranteed. An unrecognised filter is not degraded output:
	 * ffmpeg exits before the first frame, so a missing filter must cost a flat
	 * picture and never a dead stream.
	 *
	 * CanTagSDR means `setparams` is present, which relabels frame colour
	 * properties without converting anything. It is core libavfilter and needs
	 * no external library, so it is available in builds where `zscale` is not.
	 */
	CanTonemap bool
	CanTagSDR  bool
}

/*
 * tonemapFilters converts HDR to SDR on the CPU (ADR 0033).
 *
 * Read as a pipeline: to linear light, to a float format with the headroom to
 * be scaled in, to BT.709 primaries, tone map, back to a BT.709 transfer with
 * BT.709 matrix and TV range, and finally to the 8-bit 4:2:0 the encoder wants.
 *
 * `hable` is the operator the ADR chose and is not configurable: it preserves
 * midtone contrast, which is where faces are. `desat=0` turns off the filter's
 * default highlight desaturation — the complaint that opened ADR 0033 was a
 * washed-out picture measured at 3.7x saturation loss, and desaturating on
 * purpose on the way out would give some of that back.
 *
 * `npl=100` is the nominal peak luminance of the SDR target, in nits.
 *
 * CPU rather than GPU because there is no `tonemap_cuda` in stock ffmpeg.
 *
 * This used to add "and because nothing here decodes on the GPU anyway", which
 * stopped being true when `-hwaccel auto` was added above. It still costs
 * nothing, but for a different reason and one worth writing down: `-hwaccel` is
 * set without `-hwaccel_output_format`, so ffmpeg brings frames back to system
 * memory after decoding and this remains a plain filter insertion rather than
 * the download ADR 0033 warned it might cost.
 *
 * Measured rather than reasoned, because ADR 0033 exists on account of a
 * shipped colour bug that was hard to reproduce. LANcast's own arguments over a
 * real HDR10 source (Casino Royale, HEVC 10-bit, smpte2084), five seconds, with
 * and without the flag:
 *
 *	                 colour out            SSIM vs the other
 *	software decode  bt709/bt709/bt709     —
 *	-hwaccel auto    bt709/bt709/bt709     0.9957
 *
 * Identical tags and a difference consistent with NVENC's own run-to-run
 * variance. Worth knowing too: hardware decode buys almost nothing on a
 * tonemapped file — 28.1s of CPU against 26.8s — because this filter chain, not
 * the decode, is the expensive half. The gain is on the ordinary HEVC re-encode
 * that does not tone map.
 */
var tonemapFilters = []string{
	"zscale=t=linear:npl=100",
	"format=gbrpf32le",
	"zscale=p=bt709",
	"tonemap=hable:desat=0",
	"zscale=t=bt709:m=bt709:r=tv",
	"format=yuv420p",
}

/*
 * sdrTags state the output's colour, and are the half of ADR 0033 that must
 * never regress with the other.
 *
 * ffmpeg copies the source's colour metadata to the output by default, so
 * without these an 8-bit H.264 file goes out claiming `smpte2084`/`bt2020` —
 * asserting a transfer function its contents do not have. A client that ignores
 * the tags renders it flat; one that honours them applies a PQ curve to values
 * that were never PQ-encoded. That inconsistency is what makes the bug
 * irreproducible across displays.
 */
var sdrTags = []string{
	"-colorspace", "bt709",
	"-color_primaries", "bt709",
	"-color_trc", "bt709",
}

/*
 * sdrRelabel forces the frame colour properties to BT.709 without converting a
 * pixel, and it is required for sdrTags to actually work.
 *
 * Measured, because the flags alone are not enough: x264 writes its VUI from the
 * *frame* properties it is handed, which the decoder set from the source, and
 * `-color_trc`/`-color_primaries` do not override them. Running LANcast's own
 * arguments against a real HDR10 clip:
 *
 *	tonemapped:        bt709     / bt709     / bt709      ← zscale rewrote them
 *	tags only:         bt709     / smpte2084 / bt2020     ← hybrid
 *	untouched (today): bt2020nc  / smpte2084 / bt2020
 *
 * The middle row is the trap. It is not "tagged honestly" — it is a file whose
 * matrix and transfer disagree, which is worse than either consistent state and
 * is precisely the differently-wrong-per-display bug ADR 0033 was written to
 * remove. So on a build that cannot tone map, the labels are only rewritten
 * where they can be rewritten *consistently*; where they cannot, the output is
 * left exactly as it is today rather than made incoherent.
 *
 * Relabelling without converting is still a claim about pixels that were never
 * converted. It is the better claim: every client then renders the picture the
 * same flat way, where the hybrid renders differently on each. A consistently
 * disappointing picture is a quality complaint; an incoherent file is a bug
 * report nobody can reproduce.
 */
const sdrRelabel = "setparams=color_primaries=bt709:color_trc=bt709:colorspace=bt709"

// withDefaults fills in the values most callers do not care about.
func (o Options) withDefaults() Options {
	if o.AudioBitrate == 0 {
		o.AudioBitrate = 192
	}
	if o.AudioChannels == 0 {
		o.AudioChannels = 2
	}
	if o.CRF == 0 {
		o.CRF = 23
	}
	if o.Preset == "" {
		// veryfast, not medium: a home server transcoding on demand needs to
		// stay ahead of playback in real time, and the size difference matters
		// far less than not stuttering.
		o.Preset = "veryfast"
	}
	if o.Encoder.Name == "" {
		o.Encoder = Software
	}
	if !o.Encoder.Hardware && o.Preset != "" {
		o.Encoder.presetValue = o.Preset
	}
	return o
}

/*
 * Fragmented-MP4 muxer flags, and the one that took a bug report to find.
 *
 * `empty_moov` writes the moov atom up front so the stream can start before the
 * file is finished — the whole point of progressive fMP4. But the MP4 muxer
 * cannot describe AC-3 or E-AC-3 until it has parsed a packet, because the
 * `dac3`/`dec3` box is built from the bitstream, so writing the moov first is a
 * contradiction and ffmpeg refuses:
 *
 *	Cannot write moov atom before EAC3 packets parsed.
 *	Could not write header (incorrect codec parameters ?): Invalid argument
 *
 * That is a *dead stream*, not degraded audio. ffmpeg exits before the first
 * byte, and the client — already committed to a 200 with a spinner — waits for
 * media that will never arrive.
 *
 * `delay_moov` holds the moov back until the first packets have been parsed,
 * which satisfies both constraints. Measured against real files, ten seconds
 * copied out of each:
 *
 *	                        empty_moov       + delay_moov
 *	E-AC-3 5.1 (mkv)        946 bytes, dead  8,029,116 bytes
 *	AC-3 (mkv)              946 bytes, dead  3,343,714 bytes
 *	AAC (mp4)               worked           worked, unchanged
 *
 * It is not a dual-audio bug, which is only how it surfaced: picking a
 * non-default track forces a remux where the default track would have
 * direct-played, so the failure appears the moment a track is chosen and hides
 * whenever one is not. Any AC-3 file that cannot direct-play hits it.
 *
 * `probe.mp4CarriesAudio` was right all along that MP4 carries these codecs.
 * The muxer flags were what made the claim false.
 *
 * HLS is unaffected and deliberately not changed: that muxer writes its own
 * init segment after parsing, and copying AC-3 through it already worked.
 */
const (
	// fragFileFlags stream a file that already exists.
	fragFileFlags = "frag_keyframe+empty_moov+delay_moov+default_base_moof"
	/*
	 * fragLiveFlags stream a source that never ends.
	 *
	 * Not `frag_keyframe`: fragmenting on keyframes means the browser waits for
	 * the first one, which on a channel with a long GOP is several seconds of
	 * blank screen that reads as a broken stream. That reasoning is why
	 * `frag_every_frame` was here, and it still holds — the fragment interval
	 * has to be short and it cannot depend on the source's GOP.
	 *
	 * What `frag_every_frame` also did was corrupt the timestamps of every live
	 * channel it produced. Measured against a *fixed* 40s MPEG-TS capture, so
	 * this is the flag rather than the stream: the source's video DTS steps
	 * cleanly by 0.033333, and the fMP4 came out irregular and duplicated —
	 *
	 *	source  1.400000 1.433333 1.466667 1.500000 1.533333 1.566667
	 *	output  0.000000 0.066667 0.100000 0.133333 0.166667 0.166667
	 *
	 * with the same input and only this constant changed:
	 *
	 *	frag_every_frame   2192 warnings, duplicate DTS
	 *	frag_keyframe         0 warnings, clean
	 *	frag_duration 200ms   0 warnings, clean, +0.9% bytes
	 *
	 * (`delay_moov` was tested separately and is not involved.) The warnings
	 * are ffmpeg's "Packet duration: -3000 out of range" on video, "-1024" on
	 * audio, and "pts has no value" — all of them non-monotonic DTS by another
	 * name, and a browser demuxer requires DTS to increase strictly.
	 *
	 * So the interval moves off the frame and onto the clock: liveFragDuration
	 * below fragments on time instead, which keeps the short interval the long
	 * GOP argument demands without deriving it from frames.
	 */
	fragLiveFlags = "empty_moov+delay_moov+default_base_moof"

	/*
	 * liveFragDuration is how often a live fragment is emitted, in microseconds.
	 *
	 * 200ms is short enough that the picture still starts immediately — which
	 * is the whole reason the keyframe default was rejected — and long enough
	 * that fragmentation costs 0.9% of the bytes rather than one box per frame.
	 */
	liveFragDuration = "200000"
)

// Args builds the ffmpeg command line.
func Args(o Options) []string {
	o = o.withDefaults()
	a := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}

	// -ss before -i seeks by keyframe without decoding everything up to the
	// offset. Placing it after -i would decode and discard, which on a
	// two-hour film means minutes of wasted work before the first frame.
	if o.StartAt > 0 && !o.Live {
		a = append(a, "-ss", strconv.FormatFloat(o.StartAt, 'f', 3, 64))
	}

	/*
	 * Hardware *decode*, which is the half that was missing.
	 *
	 * The encoder was already being chosen from a real capability probe, so a
	 * re-encode ran on NVENC and looked accelerated. Decoding was still
	 * software every time, and decoding 1080p HEVC in software is the expensive
	 * half of the job — the GPU sat mostly idle while a CPU core did the work
	 * that made the machine lag.
	 *
	 * Named after the encoder's own driver stack, never `auto`.
	 *
	 * `auto` shipped in v0.8.0 and broke HEVC playback: it chose DXVA2, DXVA2
	 * needs a Direct3D device, and the server runs as a Windows service in
	 * session 0 where there is no desktop and no D3D. ffmpeg did not fall back
	 * the way that comment claimed it would — it exited before the first byte.
	 * See Encoder.decodeAccel, which is where that reasoning now lives and
	 * where the empty case means software.
	 *
	 * Deliberately without `-hwaccel_output_format`. Leaving it unset brings
	 * frames back to system memory after decoding, so the filter chain and the
	 * encoder see ordinary frames and nothing downstream has to change — in
	 * particular the tonemap filter, which has no CUDA equivalent in stock
	 * ffmpeg and must run on the CPU regardless (see the filter chain below).
	 * The copy back costs a little bandwidth and buys the whole decode.
	 *
	 * Only when re-encoding on a hardware encoder. A stream copy decodes
	 * nothing, and pulling hardware init into a job that never touches a pixel
	 * is cost with no work to pay for it.
	 */
	/*
	 * Whether the decoded frames stay on the card.
	 *
	 * They can only stay there if nothing downstream needs to touch a pixel in
	 * system memory, and two things do: the tone map (zscale and tonemap are
	 * CPU filters with no CUDA equivalent in stock ffmpeg, which ADR 0033
	 * already records) and a resolution cap (`scale` likewise; `scale_cuda`
	 * exists but takes different arguments and does not accept the `-2` this
	 * uses to keep widths even). Either of those, and the frames come back.
	 *
	 * Worth the condition, because the download is not free and on a 10-bit
	 * source it is the dominant cost. Measured over sixty seconds of a 1080p
	 * HEVC Main 10 file, encoding on NVENC either way:
	 *
	 *	software decode        7.11 cores, 10.5x realtime
	 *	cuda, frames copied    2.66 cores,  8.2x realtime
	 *	cuda, frames stay      0.67 cores, 18.2x realtime
	 *
	 * The middle row is the one that shipped, and it is *slower in wall time
	 * than decoding in software* — NVDEC hands back p010, every frame is copied
	 * off the card, and a CPU swscale converts it to 8-bit for the encoder. The
	 * copy and the conversion serialise what the card had already finished.
	 */
	accel := o.Encoder.decodeAccel()
	decoding := !o.Decision.AudioOnly && o.Decision.VideoAction != "copy"
	framesOnGPU := decoding && accel == "cuda" &&
		!o.Decision.TonemapHDR && o.Decision.TargetHeight == 0

	if accel != "" && decoding {
		a = append(a, "-hwaccel", accel)
		if framesOnGPU {
			a = append(a, "-hwaccel_output_format", "cuda")
		}
	}

	if o.Live {
		a = append(a, liveInputArgs()...)
		if o.HLSInput {
			// Start one segment from the live edge rather than the default
			// three, so there is no backlog to drain at full speed. See
			// Options.HLSInput for the measurement.
			a = append(a, "-live_start_index", "-1")
		}
	}

	a = append(a, "-i", o.Input)

	// Map explicitly. Without this ffmpeg picks one stream per type by its own
	// rules, which quietly selects the wrong audio track on files with several.
	//
	// A music file has no video stream to map, and `-map 0:v:0` against one is
	// not a warning — ffmpeg exits before producing a byte. The cover art an
	// audio file often carries makes this worse rather than better: it *is* a
	// video stream, so the map would succeed and then hang trying to encode a
	// single still frame for the length of the track.
	if !o.Decision.AudioOnly {
		a = append(a, "-map", "0:v:0")
	}
	if o.AudioIndex >= 0 {
		a = append(a, "-map", fmt.Sprintf("0:%d", o.AudioIndex))
	} else {
		a = append(a, "-map", "0:a:0?")
	}

	// Subtitles are dropped for now. Burning them in forces a video re-encode
	// even when the video is fine, and carrying them in fMP4 needs WebVTT
	// conversion — both belong with the subtitle work, not here.
	a = append(a, "-sn")

	switch {
	case o.Decision.AudioOnly:
		// No video stream was mapped, so there is nothing for -c:v to describe.
		// Naming an encoder here would also pull hardware init into a job that
		// never touches a pixel.
	case o.Decision.VideoAction == "copy":
		a = append(a, "-c:v", "copy")

	default:
		// The encoder supplies its own codec, quality and profile flags:
		// x264 wants -crf, NVENC -cq, QSV -global_quality, AMF -qp_i. Same
		// intent, four spellings, and each hardware encoder needs profile and
		// level stated or it defaults to something browsers refuse.
		a = append(a, o.Encoder.EncoderArgs(o.CRF, framesOnGPU)...)

		/*
		 * The video filter chain: one -vf, built from every filter this job
		 * needs.
		 *
		 * ffmpeg takes the *last* -vf and silently discards the others, so
		 * these must compose into a single flag. Two flags would not stack —
		 * the second would replace the first, and a quality ceiling would stop
		 * being honoured with nothing in the output to say why.
		 *
		 * Scale before tone map: tone mapping is per-pixel work, so doing it
		 * after the downscale is the same conversion on fewer pixels.
		 */
		var filters []string

		/*
		 * The 10-bit to 8-bit conversion, done where the frames already are.
		 *
		 * This is what -pix_fmt yuv420p does when the frames are in system
		 * memory, and it is the reason they no longer have to be. Only reached
		 * when nothing else in this chain needs a CPU filter — see framesOnGPU.
		 */
		if framesOnGPU {
			filters = append(filters, "scale_cuda=format=yuv420p")
		}

		// The quality ceiling, if the decision carries one.
		//
		// -2 rather than -1 on the width: the height is fixed and the width is
		// derived from the aspect ratio, and H.264 needs even dimensions. -1
		// computes the exact width and hands ffmpeg an odd number on plenty of
		// ordinary aspect ratios, where the encoder does not round — it exits.
		// -2 asks for the same computation constrained to a multiple of two.
		if o.Decision.TargetHeight > 0 {
			filters = append(filters,
				fmt.Sprintf("scale=-2:%d", o.Decision.TargetHeight))
		}
		/*
		 * HDR to SDR, in whichever of the three states this build allows.
		 *
		 * Convert if the filters exist. Otherwise relabel, so the output is at
		 * least coherent about what it is. Otherwise leave it alone: the tags
		 * cannot be made consistent without touching the frame properties, and
		 * a half-relabelled file is worse than the one that ships today. See
		 * sdrRelabel for the measurements.
		 */
		colourFixed := false
		switch {
		case !o.Decision.TonemapHDR:
			// Not HDR. Its colour metadata is already right; rewriting it would
			// be inventing a conversion that did not happen.
		case o.CanTonemap:
			filters = append(filters, tonemapFilters...)
			colourFixed = true
		case o.CanTagSDR:
			filters = append(filters, sdrRelabel)
			colourFixed = true
		}

		if len(filters) > 0 {
			a = append(a, "-vf", strings.Join(filters, ","))
		}
		// Asserted separately from the filter chain, because the two halves of
		// ADR 0033 must not be able to regress together.
		if colourFixed {
			a = append(a, sdrTags...)
		}
		// A ceiling, not a target: -maxrate with -bufsize is rate *limiting* on
		// top of the quality-based encode above, so a scene that compresses well
		// still comes in under it rather than being padded up to it. bufsize at
		// twice maxrate is the usual choice — smaller makes the limiter clamp on
		// short bursts and visibly softens hard cuts.
		if o.Decision.TargetVideoBitRate > 0 {
			kbit := o.Decision.TargetVideoBitRate / 1000
			a = append(a,
				"-maxrate", strconv.FormatInt(kbit, 10)+"k",
				"-bufsize", strconv.FormatInt(kbit*2, 10)+"k",
			)
		}

		if o.Output == HLS {
			// Force keyframes on segment boundaries, or segments cannot start
			// with an IDR frame and seeking breaks.
			a = append(a,
				"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", SegmentSeconds))
		}
	}

	if o.Decision.AudioAction == "copy" {
		a = append(a, "-c:a", "copy")
		/*
		 * AAC out of MPEG-TS carries ADTS framing, and MP4 will not take it.
		 *
		 * This is the single most common live case — H.264 with AAC in a
		 * transport stream — and without the filter ffmpeg emits a valid ftyp
		 * box, refuses the first audio packet with "Malformed AAC bitstream",
		 * and exits. The browser shows one frame and stops, which reads as a
		 * broken channel rather than a broken command line.
		 *
		 * Only for live: a file being remuxed came from a container that
		 * already stores AAC the way MP4 wants it, and applying the filter
		 * there would be a conversion with nothing to convert.
		 */
		if o.Live {
			a = append(a, "-bsf:a", "aac_adtstoasc")
		}
	} else {
		a = append(a,
			"-c:a", "aac",
			"-b:a", strconv.Itoa(o.AudioBitrate)+"k",
			"-ac", strconv.Itoa(o.AudioChannels),
		)
	}

	switch o.Output {
	case HLS:
		a = append(a,
			"-f", "hls",
			"-hls_time", strconv.Itoa(SegmentSeconds),
			"-hls_playlist_type", "vod",
			"-hls_segment_type", "fmp4",
			// independent_segments tells the player each segment can be
			// decoded alone, which is what makes seeking work.
			"-hls_flags", "independent_segments",
			"-hls_list_size", "0",
			"-hls_fmp4_init_filename", "init.mp4",
			"-hls_segment_filename", o.OutputDir+"/seg%05d.m4s",
			o.OutputDir+"/index.m3u8",
		)
	default:
		if o.Live {
			/*
			 * A live fMP4 stream, flushed as it is produced.
			 *
			 * `frag_keyframe` alone is not enough here. It fragments on
			 * keyframes, which on a channel with a long GOP can be several
			 * seconds apart — so the browser waits for the first keyframe
			 * before showing anything, and the picture arrives late enough to
			 * look broken. `-frag_duration` fragments on the clock instead,
			 * which starts the picture immediately without letting the source
			 * decide the interval. See fragLiveFlags for why it is not
			 * `frag_every_frame`.
			 *
			 * `-flush_packets 1` matters for the same reason: without it
			 * ffmpeg buffers its output, and a buffered live stream is one
			 * that arrives in bursts behind whatever the buffer holds.
			 */
			a = append(a,
				"-movflags", fragLiveFlags,
				"-frag_duration", liveFragDuration,
				"-flush_packets", "1",
				"-f", "mp4",
				"pipe:1",
			)
			break
		}
		a = append(a,
			"-movflags", fragFileFlags,
			"-f", "mp4",
			"pipe:1",
		)
	}

	return a
}

/*
 * liveInputArgs are the flags that make a never-ending network source
 * survivable.
 *
 * Every one of these is here because of a failure it prevents rather than as
 * defensive decoration:
 *
 *   - **reconnect**: an IPTV source drops. Without these ffmpeg exits on the
 *     first blip and the viewer sees the channel die rather than stutter.
 *     `reconnect_streamed` is the one that matters for live — plain
 *     `reconnect` only covers seekable input.
 *   - **rw_timeout**: a provider that stops sending without closing the socket
 *     leaves ffmpeg blocked indefinitely, holding a process and a connection
 *     for a viewer who has long since given up. In microseconds, which is a
 *     genuine ffmpeg trap: the obvious value of 10 is ten *microseconds*.
 *   - **genpts**: broadcast MPEG-TS arrives with timestamp discontinuities at
 *     every ad break and programme junction, and fMP4 refuses them. Generating
 *     presentation timestamps is what keeps the mux from failing partway
 *     through an evening.
 */
func liveInputArgs() []string {
	return []string{
		"-reconnect", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
		"-rw_timeout", "15000000",
		"-fflags", "+genpts",
	}
}

// NeedsTranscode reports whether a decision requires ffmpeg at all.
func NeedsTranscode(d probe.Decision) bool {
	return d.Method != probe.DirectPlay
}
