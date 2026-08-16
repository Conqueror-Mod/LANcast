// Package transcode runs ffmpeg to deliver media a client cannot play directly.
//
// Argument construction is separated from process execution so the command
// line — where the subtle mistakes live — is testable without spawning
// anything or having media on disk.
package transcode

import (
	"fmt"
	"strconv"

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
}

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

	if o.Live {
		a = append(a, liveInputArgs()...)
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
		a = append(a, o.Encoder.EncoderArgs(o.CRF)...)

		// The quality ceiling, if the decision carries one.
		//
		// -2 rather than -1 on the width: the height is fixed and the width is
		// derived from the aspect ratio, and H.264 needs even dimensions. -1
		// computes the exact width and hands ffmpeg an odd number on plenty of
		// ordinary aspect ratios, where the encoder does not round — it exits.
		// -2 asks for the same computation constrained to a multiple of two.
		if o.Decision.TargetHeight > 0 {
			a = append(a, "-vf",
				fmt.Sprintf("scale=-2:%d", o.Decision.TargetHeight))
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
			 * look broken. `frag_every_frame` costs a little overhead and
			 * starts the picture immediately.
			 *
			 * `-flush_packets 1` matters for the same reason: without it
			 * ffmpeg buffers its output, and a buffered live stream is one
			 * that arrives in bursts behind whatever the buffer holds.
			 */
			a = append(a,
				"-movflags", "frag_every_frame+empty_moov+default_base_moof",
				"-flush_packets", "1",
				"-f", "mp4",
				"pipe:1",
			)
			break
		}
		a = append(a,
			"-movflags", "frag_keyframe+empty_moov+default_base_moof",
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
