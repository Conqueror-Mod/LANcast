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
	if o.StartAt > 0 {
		a = append(a, "-ss", strconv.FormatFloat(o.StartAt, 'f', 3, 64))
	}

	a = append(a, "-i", o.Input)

	// Map explicitly. Without this ffmpeg picks one stream per type by its own
	// rules, which quietly selects the wrong audio track on files with several.
	a = append(a, "-map", "0:v:0")
	if o.AudioIndex >= 0 {
		a = append(a, "-map", fmt.Sprintf("0:%d", o.AudioIndex))
	} else {
		a = append(a, "-map", "0:a:0?")
	}

	// Subtitles are dropped for now. Burning them in forces a video re-encode
	// even when the video is fine, and carrying them in fMP4 needs WebVTT
	// conversion — both belong with the subtitle work, not here.
	a = append(a, "-sn")

	if o.Decision.VideoAction == "copy" {
		a = append(a, "-c:v", "copy")
	} else {
		// The encoder supplies its own codec, quality and profile flags:
		// x264 wants -crf, NVENC -cq, QSV -global_quality, AMF -qp_i. Same
		// intent, four spellings, and each hardware encoder needs profile and
		// level stated or it defaults to something browsers refuse.
		a = append(a, o.Encoder.EncoderArgs(o.CRF)...)

		if o.Output == HLS {
			// Force keyframes on segment boundaries, or segments cannot start
			// with an IDR frame and seeking breaks.
			a = append(a,
				"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", SegmentSeconds))
		}
	}

	if o.Decision.AudioAction == "copy" {
		a = append(a, "-c:a", "copy")
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
		a = append(a,
			"-movflags", "frag_keyframe+empty_moov+default_base_moof",
			"-f", "mp4",
			"pipe:1",
		)
	}

	return a
}

// NeedsTranscode reports whether a decision requires ffmpeg at all.
func NeedsTranscode(d probe.Decision) bool {
	return d.Method != probe.DirectPlay
}
